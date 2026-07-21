package launchdarkly

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
	ldDefaultHost = "app.launchdarkly.com"
	ldAPIVersion  = "20220603" // LD-API-Version pin (VERIFY current recommended at Phase B)
	ldMaxPages    = 100000
	ldPageSize    = 50
	ldMaxRetries  = 4
)

// launchdarklyAPIError carries the HTTP status so callers distinguish an absent role/feature/
// plan (403/404 → best-effort skip) from a fatal auth failure (401) or a transient error
// (429/5xx → Warn). Status is 0 for pre-response (transport) errors.
type launchdarklyAPIError struct {
	Status int
	msg    string
}

func (e *launchdarklyAPIError) Error() string { return e.msg }

// ldBaseHost resolves the API host from LAUNCHDARKLY_API_HOST (default app.launchdarkly.com; a
// federal/custom instance overrides it), validated to a bare-hostname shape so the token can't
// be sent to a foreign host. Empty (→ no URL formed) if malformed.
func ldBaseHost() string {
	h := cleanHostPart(os.Getenv("LAUNCHDARKLY_API_HOST"))
	if h == "" {
		h = ldDefaultHost
	}
	if !validHost(h) {
		return ""
	}
	return h
}

func ldBase() string {
	h := ldBaseHost()
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

// validHost restricts the host to a bare hostname shape (alphanumerics, '.', '-'); rejects an
// '@' (userinfo splice), a path, port, or query — the token-safety posture rests on the token
// reaching only the configured host.
func validHost(h string) bool {
	if h == "" {
		return false
	}
	for _, r := range h {
		if r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// isLaunchDarklyURL validates a server-supplied _links.next URL (after relative resolution)
// before the token is re-sent: require https and the exact configured base host. The next href
// is not an HTTP redirect (CheckRedirect never fires, Go does not strip the header), so a
// next-link at another host would leak the account token.
func isLaunchDarklyURL(raw string) bool {
	u, err := neturl.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Host == ldBaseHost()
}

// resolveNext turns a _links.next.href (a base-relative PATH or a full URL) into an absolute
// URL and reports whether it is safe to follow (same-host https).
func resolveNext(href string) (string, bool) {
	if href == "" {
		return "", false
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href, isLaunchDarklyURL(href)
	}
	if !strings.HasPrefix(href, "/") {
		href = "/" + href
	}
	full := ldBase() + href
	return full, isLaunchDarklyURL(full)
}

// ldHTTPClient refuses redirects so the token can never be replayed to another host on a 3xx.
var ldHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the Authorization header must not leave the configured host)", req.URL.Host)
	},
}

// ldDo performs a request against a FULL url and returns the raw body + status, with bounded
// Retry-After backoff on 429 (a bulk flag/segment enumeration WILL hit the per-token rate
// limit; retrying the same page keeps the pages ldList already accumulated rather than
// silently truncating the largest plane). A package var so tests can fake it (the fake
// bypasses the retry loop). LaunchDarkly's auth is a RAW token on the Authorization header —
// NO scheme prefix (not Bearer/Token token=/GenieKey/SSWS): the token IS the whole header
// value. It rides ONLY on that header, never in the URL, errors, or logs.
var ldDo = func(ctx context.Context, method, url string) ([]byte, int, error) {
	for attempt := 0; ; attempt++ {
		body, status, retryAfter, err := ldDoOnce(ctx, method, url)
		if status == 429 && attempt < ldMaxRetries {
			if berr := ldBackoff(ctx, attempt, retryAfter); berr != nil {
				return body, status, err
			}
			continue
		}
		return body, status, err
	}
}

// ldDoOnce is a single request; it additionally surfaces the Retry-After delay on a 429.
func ldDoOnce(ctx context.Context, method, url string) (body []byte, status int, retryAfter time.Duration, err error) {
	req, rerr := http.NewRequestWithContext(ctx, method, url, nil)
	if rerr != nil {
		return nil, 0, 0, &launchdarklyAPIError{msg: rerr.Error()}
	}
	req.Header.Set("Authorization", os.Getenv("LAUNCHDARKLY_ACCESS_TOKEN"))
	req.Header.Set("LD-API-Version", ldAPIVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, derr := ldHTTPClient.Do(req)
	if derr != nil {
		return nil, 0, 0, &launchdarklyAPIError{msg: fmt.Sprintf("launchdarkly %s: %v", redactURL(url), derr)}
	}
	defer resp.Body.Close()
	b, berr := io.ReadAll(resp.Body)
	if berr != nil {
		return nil, resp.StatusCode, 0, &launchdarklyAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("launchdarkly %s: read body: %v", redactURL(url), berr)}
	}
	if resp.StatusCode == 429 {
		retryAfter = parseRetryAfter(resp.Header)
	}
	if resp.StatusCode >= 400 {
		return b, resp.StatusCode, retryAfter, &launchdarklyAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("launchdarkly %s: HTTP %d: %s", redactURL(url), resp.StatusCode, ldErrMsg(b))}
	}
	return b, resp.StatusCode, 0, nil
}

// parseRetryAfter reads the Retry-After header (delta-seconds) from a 429. LaunchDarkly also
// sends X-Ratelimit-Reset (epoch ms), but Retry-After is simpler and clock-free.
func parseRetryAfter(h http.Header) time.Duration {
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

// ldBackoff sleeps the Retry-After delay (or a capped exponential fallback), aborting early if
// the context is cancelled.
func ldBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	d := retryAfter
	if d <= 0 {
		d = time.Duration(200*(1<<attempt)) * time.Millisecond
	}
	if d > 30*time.Second {
		d = 30 * time.Second
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

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func ldErrMsg(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return "request failed"
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

func itoa(n int) string { return strconv.Itoa(n) }

// ldList fetches an {items,_links} endpoint and follows _links.next.href (resolved +
// host-validated) until absent. path is base-relative; the first page requests ?limit=.
func ldList[T any](ctx context.Context, path string) ([]T, error) {
	first := ldBase() + path + sep(path) + "limit=" + itoa(ldPageSize)
	var all []T
	url := first
	for i := 0; url != ""; i++ {
		if i >= ldMaxPages {
			return all, &launchdarklyAPIError{msg: fmt.Sprintf("launchdarkly %s: pagination exceeded %d pages", redactURL(first), ldMaxPages)}
		}
		body, _, err := ldDo(ctx, http.MethodGet, url)
		if err != nil {
			return all, err // keep pages already fetched (large flag lists brush the rate limit)
		}
		items, next, err := decodeItems[T](first, body)
		if err != nil {
			return all, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		resolved, ok := resolveNext(next)
		if !ok {
			return all, &launchdarklyAPIError{msg: "launchdarkly: refusing to follow next-page url to unexpected host: " + redactURL(next)}
		}
		url = resolved
	}
	return all, nil
}

func decodeItems[T any](srcURL string, body []byte) ([]T, string, error) {
	if len(body) == 0 {
		return nil, "", nil
	}
	var env struct {
		Items json.RawMessage `json:"items"`
		Links struct {
			Next struct {
				Href string `json:"href"`
			} `json:"next"`
		} `json:"_links"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", &launchdarklyAPIError{msg: fmt.Sprintf("launchdarkly %s: decode: %v", redactURL(srcURL), err)}
	}
	var items []T
	if len(env.Items) > 0 {
		if err := json.Unmarshal(env.Items, &items); err != nil {
			return nil, "", &launchdarklyAPIError{msg: fmt.Sprintf("launchdarkly %s: decode items: %v", redactURL(srcURL), err)}
		}
	}
	return items, env.Links.Next.Href, nil
}

// ldGetObject fetches a bare-object singleton (e.g. GET /api/v2/members/me) into T.
func ldGetObject[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := ldDo(ctx, http.MethodGet, ldBase()+path)
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &launchdarklyAPIError{msg: fmt.Sprintf("launchdarkly %s: decode: %v", redactURL(path), err)}
		}
	}
	return out, nil
}
