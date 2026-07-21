package grafana

import (
	"context"
	"encoding/base64"
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
	grafanaMaxPages = 10000
	grafanaPerPage  = 1000
	grafanaDashPage = 5000
)

// grafanaAPIError carries the HTTP status so callers distinguish an absent feature/
// permission (403/404 → best-effort skip) from a fatal auth failure (401) or a transient
// error (429/5xx → Warn). Status is 0 for pre-response (transport) errors.
type grafanaAPIError struct {
	Status int
	msg    string
}

func (e *grafanaAPIError) Error() string { return e.msg }

// grafanaBase returns the trimmed GRAFANA_URL. Unlike every prior provider the host is
// USER-SUPPLIED (self-hosted or Grafana Cloud), so there is no default — validateGrafanaURL
// enforces correctness in preflight. A query/fragment is stripped defensively here too so it
// can never ride into a request URL.
func grafanaBase() string {
	raw := strings.TrimSpace(os.Getenv("GRAFANA_URL"))
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimRight(raw, "/")
}

// validateGrafanaURL enforces that GRAFANA_URL is a well-formed http/https base with a host
// and no query/fragment. A sub-path (root_url) is allowed. There is no fallback host.
func validateGrafanaURL() error {
	raw := strings.TrimSpace(os.Getenv("GRAFANA_URL"))
	if raw == "" {
		return &grafanaAPIError{msg: "GRAFANA_URL is not set"}
	}
	u, err := neturl.Parse(raw)
	if err != nil {
		return &grafanaAPIError{msg: "GRAFANA_URL is not a valid URL: " + err.Error()}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &grafanaAPIError{msg: "GRAFANA_URL must be http or https"}
	}
	if u.Host == "" {
		return &grafanaAPIError{msg: "GRAFANA_URL has no host"}
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return &grafanaAPIError{msg: "GRAFANA_URL must not contain a query string or fragment"}
	}
	return nil
}

// grafanaAuthHeader builds the Authorization header value from GRAFANA_AUTH. Detection
// mirrors gapi: a colon means Basic user:pass (Grafana tokens never contain a colon —
// glsa_/base64url/JWT), otherwise a Bearer token. The credential rides ONLY on this header.
func grafanaAuthHeader() (string, bool) {
	// TrimSpace so a trailing newline (e.g. GRAFANA_AUTH sourced from a secret file) does not
	// produce an invalid header value, matching how GRAFANA_URL/GRAFANA_ORG_ID are read.
	auth := strings.TrimSpace(os.Getenv("GRAFANA_AUTH"))
	if auth == "" {
		return "", false
	}
	if strings.Contains(auth, ":") {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth)), true
	}
	return "Bearer " + auth, true
}

// grafanaHTTPClient refuses redirects. A self-hosted instance behind an auth proxy can 302
// to an SSO/login host; Go does not strip the X-Grafana-Org-Id header (nor Authorization on
// a same-host scheme/port change), and following a 3xx would decode a login page as JSON. The
// list endpoints answer 200 directly, so a redirect is a hard, clearly-surfaced error.
var grafanaHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (auth headers must not leave the configured instance)", req.URL.Host)
	},
}

// grafanaDo performs a request against base+path and returns the raw body + status. A
// package var so tests can fake it. Auth via the Authorization header (Bearer or Basic) and
// the optional X-Grafana-Org-Id header; the token/password is only ever on the header, never
// in the URL, errors, or logs.
var grafanaDo = func(ctx context.Context, method, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, grafanaBase()+path, nil)
	if err != nil {
		return nil, 0, &grafanaAPIError{msg: err.Error()}
	}
	if h, ok := grafanaAuthHeader(); ok {
		req.Header.Set("Authorization", h)
	}
	if org := strings.TrimSpace(os.Getenv("GRAFANA_ORG_ID")); org != "" {
		req.Header.Set("X-Grafana-Org-Id", org)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := grafanaHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: %v", redactPath(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &grafanaAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("grafana %s: read body: %v", redactPath(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &grafanaAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("grafana %s: HTTP %d: %s", redactPath(path), resp.StatusCode, grafanaErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

// redactPath strips any query string from a path before it appears in an error/log (auth
// never rides the query, but a query can carry filter values not worth logging).
func redactPath(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

func grafanaErrMsg(body []byte) string {
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

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// --- response-family helpers -----------------------------------------------

// grafanaGetArray fetches a bare-array endpoint (no pagination) into []T.
func grafanaGetArray[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := grafanaDo(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	var items []T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: decode: %v", redactPath(path), err)}
		}
	}
	return items, nil
}

// grafanaGetOne fetches a bare-object singleton into T.
func grafanaGetOne[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := grafanaDo(ctx, http.MethodGet, path)
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: decode: %v", redactPath(path), err)}
		}
	}
	return out, nil
}

// grafanaListArrayPaged paginates a bare-array endpoint with 1-based ?limit=&page=, stopping
// on a short/empty page. Used for /api/folders and /api/search.
func grafanaListArrayPaged[T any](ctx context.Context, path string, perPage int) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		if page > grafanaMaxPages {
			return nil, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: pagination exceeded %d pages", redactPath(path), grafanaMaxPages)}
		}
		url := fmt.Sprintf("%s%slimit=%d&page=%d", path, sep(path), perPage, page)
		body, _, err := grafanaDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var items []T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &items); err != nil {
				return nil, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: decode page %d: %v", redactPath(path), page, err)}
			}
		}
		all = append(all, items...)
		if len(items) < perPage {
			return all, nil
		}
	}
}

// grafanaListKeyedPaged paginates a keyed-object endpoint ({<key>:[...],"totalCount":n}) with
// 1-based ?<perpageParam>=&page=, stopping on a short page or once totalCount is reached.
// Used for /api/teams/search and /api/serviceaccounts/search.
func grafanaListKeyedPaged[T any](ctx context.Context, path, key, perpageParam string, perPage int) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		if page > grafanaMaxPages {
			return nil, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: pagination exceeded %d pages", redactPath(path), grafanaMaxPages)}
		}
		url := fmt.Sprintf("%s%s%s=%d&page=%d", path, sep(path), perpageParam, perPage, page)
		body, _, err := grafanaDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		items, total, err := decodeKeyedPage[T](path, key, body)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < perPage || (total > 0 && len(all) >= total) {
			return all, nil
		}
	}
}

func decodeKeyedPage[T any](path, key string, body []byte) ([]T, int, error) {
	if len(body) == 0 {
		return nil, 0, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, 0, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: decode: %v", redactPath(path), err)}
	}
	total := 0
	if raw, ok := m["totalCount"]; ok {
		_ = json.Unmarshal(raw, &total)
	}
	var items []T
	if raw, ok := m[key]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, 0, &grafanaAPIError{msg: fmt.Sprintf("grafana %s: decode %q: %v", redactPath(path), key, err)}
		}
	}
	return items, total, nil
}
