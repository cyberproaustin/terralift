package okta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	oktaMaxPages   = 10000
	oktaPageSize   = 200
	oktaMaxRetries = 4
)

// oktaAPIError carries the HTTP status so callers distinguish an absent role/feature (403/404
// → best-effort skip) from a fatal auth failure (401) or a transient error (429/5xx → Warn).
// Status is 0 for pre-response (transport) errors.
type oktaAPIError struct {
	Status int
	msg    string
}

func (e *oktaAPIError) Error() string { return e.msg }

// oktaBaseHost constructs the org host from OKTA_ORG_NAME + OKTA_BASE_URL (the Grafana-style
// host-from-config pattern), stripping any scheme/slashes a user pastes into either. Empty if
// either is unset.
func oktaBaseHost() string {
	org := cleanHostPart(os.Getenv("OKTA_ORG_NAME"))
	base := cleanHostPart(os.Getenv("OKTA_BASE_URL"))
	if org == "" || base == "" {
		return ""
	}
	return org + "." + base
}

func oktaBase() string {
	h := oktaBaseHost()
	if h == "" {
		return ""
	}
	return "https://" + h
}

func cleanHostPart(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.Trim(s, "/")
}

// isOktaURL validates a SERVER-SUPPLIED Link rel="next" URL before the SSWS token is re-sent
// to it. The Link header is not an HTTP redirect (CheckRedirect never fires, Go does not strip
// the header), so a next-link pointing at another host would leak the org token. Require https
// and the exact constructed org host.
func isOktaURL(raw string) bool {
	u, err := neturl.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Host == oktaBaseHost()
}

// oktaAuthHeader builds the distinctive `Authorization: SSWS <api-token>` value — NOT
// `Bearer`, NOT a custom header. Getting the literal `SSWS ` prefix wrong is a silent 401.
func oktaAuthHeader() string {
	return "SSWS " + os.Getenv("OKTA_API_TOKEN")
}

// oktaHTTPClient refuses redirects so the token can never be replayed to another host on a 3xx.
var oktaHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the Authorization header must not leave the configured host)", req.URL.Host)
	},
}

// oktaDo performs a request against a FULL url and returns the raw body, status, and the
// parsed Link rel="next" URL (Okta paginates via the Link header, not the body), with bounded
// backoff on Okta's aggressive 429 rate limiting (honouring Retry-After). A package var so
// tests can fake it (the fake bypasses the retry loop, which is fine — the loop is not the
// thing under test). Retrying the SAME page on a transient 429 keeps the pages already
// accumulated by oktaList instead of dropping the whole list. The token rides ONLY on the
// Authorization header, never in the URL, errors, or logs.
var oktaDo = func(ctx context.Context, method, url string) ([]byte, int, string, error) {
	for attempt := 0; ; attempt++ {
		body, status, next, retryAfter, err := oktaDoOnce(ctx, method, url)
		if status == 429 && attempt < oktaMaxRetries {
			if berr := oktaBackoff(ctx, attempt, retryAfter); berr != nil {
				return body, status, next, err
			}
			continue
		}
		return body, status, next, err
	}
}

// oktaDoOnce is a single request. It additionally surfaces the Retry-After delay on a 429 so
// the retry wrapper can honour it.
func oktaDoOnce(ctx context.Context, method, url string) (body []byte, status int, next string, retryAfter time.Duration, err error) {
	req, rerr := http.NewRequestWithContext(ctx, method, url, nil)
	if rerr != nil {
		return nil, 0, "", 0, &oktaAPIError{msg: rerr.Error()}
	}
	req.Header.Set("Authorization", oktaAuthHeader())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, derr := oktaHTTPClient.Do(req)
	if derr != nil {
		return nil, 0, "", 0, &oktaAPIError{msg: fmt.Sprintf("okta %s: %v", redactURL(url), derr)}
	}
	defer resp.Body.Close()
	b, berr := io.ReadAll(resp.Body)
	if berr != nil {
		return nil, resp.StatusCode, "", 0, &oktaAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("okta %s: read body: %v", redactURL(url), berr)}
	}
	if resp.StatusCode == 429 {
		retryAfter = parseRetryAfter(resp.Header)
	}
	if resp.StatusCode >= 400 {
		return b, resp.StatusCode, "", retryAfter, &oktaAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("okta %s: HTTP %d: %s", redactURL(url), resp.StatusCode, oktaErrMsg(b))}
	}
	return b, resp.StatusCode, parseNextLink(resp.Header.Values("Link")), 0, nil
}

// parseRetryAfter reads the Retry-After header (delta-seconds) from a 429. Okta also sends
// X-Rate-Limit-Reset (epoch seconds), but Retry-After is present on 429 and needs no clock.
func parseRetryAfter(h http.Header) time.Duration {
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

// oktaBackoff sleeps the Retry-After delay (or a capped exponential fallback), aborting early
// if the context is cancelled.
func oktaBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	d := retryAfter
	if d <= 0 {
		d = time.Duration(500*(1<<attempt)) * time.Millisecond
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// parseNextLink extracts the rel="next" URL from one or more RFC 5988 Link headers. A response
// commonly carries several (self/next/prev); only rel="next" is followed. The scan is
// BRACKET-AWARE: it walks <uri> tokens and inspects the params up to the next '<', so a comma
// inside a cursor URL cannot truncate the parse (a naive split-on-comma would).
func parseNextLink(links []string) string {
	for _, h := range links {
		for i := 0; i < len(h); {
			lt := strings.IndexByte(h[i:], '<')
			if lt < 0 {
				break
			}
			lt += i
			gt := strings.IndexByte(h[lt:], '>')
			if gt < 0 {
				break
			}
			gt += lt
			uri := h[lt+1 : gt]
			params := h[gt+1:]
			if nxt := strings.IndexByte(params, '<'); nxt >= 0 {
				params = params[:nxt]
			}
			if strings.Contains(params, `rel="next"`) {
				return uri
			}
			i = gt + 1
		}
	}
	return ""
}

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func oktaErrMsg(body []byte) string {
	var e struct {
		ErrorSummary string `json:"errorSummary"`
	}
	if json.Unmarshal(body, &e) == nil && e.ErrorSummary != "" {
		return e.ErrorSummary
	}
	return "request failed"
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

// oktaList fetches a bare-array endpoint and follows the Link rel="next" URL (host-validated)
// until absent. path is base-relative; the first page requests ?limit=200.
func oktaList[T any](ctx context.Context, path string) ([]T, error) {
	first := oktaBase() + path + sep(path) + "limit=" + itoa(oktaPageSize)
	var all []T
	url := first
	for i := 0; url != ""; i++ {
		if i >= oktaMaxPages {
			return nil, &oktaAPIError{msg: fmt.Sprintf("okta %s: pagination exceeded %d pages", redactURL(first), oktaMaxPages)}
		}
		body, _, next, err := oktaDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var items []T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &items); err != nil {
				return nil, &oktaAPIError{msg: fmt.Sprintf("okta %s: decode: %v", redactURL(first), err)}
			}
		}
		all = append(all, items...)
		if next != "" && !isOktaURL(next) {
			return nil, &oktaAPIError{msg: "okta: refusing to follow next-page url to unexpected host: " + redactURL(next)}
		}
		url = next
	}
	return all, nil
}

// oktaGetObject fetches a bare-object singleton into T (e.g. GET /api/v1/org).
func oktaGetObject[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, _, err := oktaDo(ctx, http.MethodGet, oktaBase()+path)
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &oktaAPIError{msg: fmt.Sprintf("okta %s: decode: %v", redactURL(path), err)}
		}
	}
	return out, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
