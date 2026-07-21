package opsgenie

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
)

const (
	ogBaseUS   = "https://api.opsgenie.com"
	ogBaseEU   = "https://api.eu.opsgenie.com"
	ogMaxPages = 10000
	ogPageSize = 100
)

// opsgenieAPIError carries the HTTP status so callers distinguish an absent feature (404 →
// best-effort skip) from a fatal auth failure (401/403) or a transient error (429/5xx →
// Warn). Status is 0 for pre-response (transport) errors.
type opsgenieAPIError struct {
	Status int
	msg    string
}

func (e *opsgenieAPIError) Error() string { return e.msg }

// ogBase resolves the region base URL. The TF provider's api_url is a bare host
// ("api.opsgenie.com" | "api.eu.opsgenie.com"); OPSGENIE_API_URL accepts a bare host or a full
// URL, defaulting to US. https is forced — the GenieKey is a bearer-equivalent secret.
func ogBase() string {
	v := strings.TrimSpace(os.Getenv("OPSGENIE_API_URL"))
	if v == "" {
		return ogBaseUS
	}
	return forceHTTPS(strings.TrimRight(v, "/"))
}

func forceHTTPS(v string) string {
	switch {
	case strings.HasPrefix(v, "https://"):
		return v
	case strings.HasPrefix(v, "http://"):
		return "https://" + strings.TrimPrefix(v, "http://")
	default:
		return "https://" + v
	}
}

func ogBaseHost() string {
	u, err := neturl.Parse(ogBase())
	if err != nil {
		return ""
	}
	return u.Host
}

// isOpsgenieURL validates a SERVER-SUPPLIED paging.next URL before the GenieKey is re-sent to
// it. paging.next is not an HTTP redirect (CheckRedirect never fires, Go does not strip the
// header), so a next-link pointing at another host would leak the account key. Require https
// and the exact configured base host.
func isOpsgenieURL(raw string) bool {
	u, err := neturl.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Host == ogBaseHost()
}

// ogAuthHeader builds the distinctive `Authorization: GenieKey <api-key>` value — NOT
// `Bearer`, NOT a custom header. Getting the literal `GenieKey ` prefix wrong is a silent 401.
func ogAuthHeader() string {
	return "GenieKey " + os.Getenv("OPSGENIE_API_KEY")
}

// ogHTTPClient refuses redirects so the GenieKey can never be replayed to another host on a 3xx.
var ogHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the Authorization header must not leave the configured host)", req.URL.Host)
	},
}

// ogDo performs a request against a FULL url (paging.next is a full URL) and returns the raw
// body + status. A package var so tests can fake it. The GenieKey rides ONLY on the
// Authorization header, never in the URL, errors, or logs.
var ogDo = func(ctx context.Context, method, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, 0, &opsgenieAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", ogAuthHeader())
	req.Header.Set("Content-Type", "application/json")

	resp, err := ogHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &opsgenieAPIError{msg: fmt.Sprintf("opsgenie %s: %v", redactURL(url), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &opsgenieAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("opsgenie %s: read body: %v", redactURL(url), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &opsgenieAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("opsgenie %s: HTTP %d: %s", redactURL(url), resp.StatusCode, ogErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

// redactURL strips the query string from a URL before it appears in an error/log.
func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func ogErrMsg(body []byte) string {
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

// ogList fetches a data/paging.next endpoint and follows paging.next (host-validated) until
// absent. path is a base-relative path; the first page is fetched with ?limit=.
func ogList[T any](ctx context.Context, path string) ([]T, error) {
	first := ogBase() + path + sep(path) + "limit=" + itoa(ogPageSize)
	var all []T
	url := first
	for i := 0; url != ""; i++ {
		if i >= ogMaxPages {
			return nil, &opsgenieAPIError{msg: fmt.Sprintf("opsgenie %s: pagination exceeded %d pages", redactURL(first), ogMaxPages)}
		}
		body, _, err := ogDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		items, next, err := decodeData[T](first, body)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next != "" && !isOpsgenieURL(next) {
			return nil, &opsgenieAPIError{msg: "opsgenie: refusing to follow next-page url to unexpected host: " + redactURL(next)}
		}
		url = next
	}
	return all, nil
}

func decodeData[T any](srcURL string, body []byte) ([]T, string, error) {
	if len(body) == 0 {
		return nil, "", nil
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", &opsgenieAPIError{msg: fmt.Sprintf("opsgenie %s: decode: %v", redactURL(srcURL), err)}
	}
	var items []T
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &items); err != nil {
			return nil, "", &opsgenieAPIError{msg: fmt.Sprintf("opsgenie %s: decode data: %v", redactURL(srcURL), err)}
		}
	}
	return items, env.Paging.Next, nil
}

// ogGetData fetches a singleton {"data":{...}} object into T (e.g. GET /v2/account).
func ogGetData[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := ogDo(ctx, http.MethodGet, ogBase()+path)
	if err != nil {
		return out, err
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &env); err != nil {
			return out, &opsgenieAPIError{msg: fmt.Sprintf("opsgenie %s: decode: %v", redactURL(path), err)}
		}
		if len(env.Data) > 0 {
			if err := json.Unmarshal(env.Data, &out); err != nil {
				return out, &opsgenieAPIError{msg: fmt.Sprintf("opsgenie %s: decode data: %v", redactURL(path), err)}
			}
		}
	}
	return out, nil
}

// ogListHeartbeats handles the ODD nested envelope: GET /v2/heartbeats returns
// {"data":{"heartbeats":[…]}} (the array is under data.heartbeats, not data).
func ogListHeartbeats(ctx context.Context) ([]ogHeartbeat, error) {
	body, _, err := ogDo(ctx, http.MethodGet, ogBase()+"/v2/heartbeats")
	if err != nil {
		return nil, err
	}
	var env struct {
		Data struct {
			Heartbeats []ogHeartbeat `json:"heartbeats"`
		} `json:"data"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, &opsgenieAPIError{msg: "opsgenie /v2/heartbeats: decode: " + err.Error()}
		}
	}
	return env.Data.Heartbeats, nil
}
