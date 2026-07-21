package auth0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	auth0MaxPages = 10000
	auth0PerPage  = 100
)

// auth0APIError carries the HTTP status so callers distinguish an absent scope/feature
// (403/404 → best-effort skip) from a fatal auth failure (401) or a transient error (429/5xx →
// Warn). Status is 0 for pre-response (transport) errors.
type auth0APIError struct {
	Status int
	msg    string
}

func (e *auth0APIError) Error() string { return e.msg }

// auth0AccessToken is the short-lived Management API Bearer minted by the client-credentials
// exchange in connect(). auth0BearerToken prefers a static AUTH0_API_TOKEN (the exchange
// bypass) when set.
var auth0AccessToken string

func auth0BearerToken() string {
	if t := strings.TrimSpace(os.Getenv("AUTH0_API_TOKEN")); t != "" {
		return t
	}
	return auth0AccessToken
}

// auth0Base constructs the tenant base URL from AUTH0_DOMAIN (host-from-config, like Okta),
// stripping any scheme/slashes a user pastes in. Empty if unset. https is forced — the Bearer
// / client_secret are tenant-master secrets.
func auth0Base() string {
	d := cleanHostPart(os.Getenv("AUTH0_DOMAIN"))
	if !validDomain(d) {
		return "" // malformed host → no URL is formed (preflight surfaces the error)
	}
	return "https://" + d
}

func cleanHostPart(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.Trim(s, "/")
}

// validDomain restricts AUTH0_DOMAIN to a bare hostname shape (alphanumerics, '.', '-'). This
// rejects an '@' (userinfo splice — e.g. legit.auth0.com@evil.com would send the token to
// evil.com on the FIRST request, before the redirect-refuser can engage), a path, a port, or a
// query — the whole secret-safety posture rests on the token reaching only the configured host.
func validDomain(d string) bool {
	if d == "" {
		return false
	}
	for _, r := range d {
		if r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// auth0HTTPClient refuses redirects so neither the client_secret nor the access_token can be
// replayed to another host on a 3xx.
var auth0HTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (auth secrets must not leave the configured host)", req.URL.Host)
	},
}

// auth0Exchange performs the OAuth2 client-credentials grant against /oauth/token and returns
// the access_token. The POST is itself UNAUTHENTICATED; the client_secret rides ONLY in the
// JSON body, never in the URL, an error, or a log (the error carries only the server's own
// error text, not the request body). A package var so tests can fake it.
var auth0Exchange = func(ctx context.Context) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     os.Getenv("AUTH0_CLIENT_ID"),
		"client_secret": os.Getenv("AUTH0_CLIENT_SECRET"),
		"audience":      auth0Base() + "/api/v2/",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth0Base()+"/oauth/token", bytes.NewReader(payload))
	if err != nil {
		return "", &auth0APIError{msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := auth0HTTPClient.Do(req)
	if err != nil {
		return "", &auth0APIError{msg: fmt.Sprintf("auth0 /oauth/token: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", &auth0APIError{Status: resp.StatusCode, msg: fmt.Sprintf("auth0 token exchange failed: HTTP %d: %s", resp.StatusCode, auth0ErrMsg(body))}
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", &auth0APIError{Status: resp.StatusCode, msg: "auth0 token exchange returned no access_token"}
	}
	return tok.AccessToken, nil
}

// auth0Do performs a Management API GET against base+path and returns the raw body + status. A
// package var so tests can fake it. The access_token rides ONLY on the Authorization header,
// never in the URL, errors, or logs.
var auth0Do = func(ctx context.Context, method, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, auth0Base()+path, nil)
	if err != nil {
		return nil, 0, &auth0APIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+auth0BearerToken())
	req.Header.Set("Accept", "application/json")

	resp, err := auth0HTTPClient.Do(req)
	if err != nil {
		return nil, 0, &auth0APIError{msg: fmt.Sprintf("auth0 %s: %v", redactURL(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &auth0APIError{Status: resp.StatusCode, msg: fmt.Sprintf("auth0 %s: read body: %v", redactURL(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &auth0APIError{Status: resp.StatusCode, msg: fmt.Sprintf("auth0 %s: HTTP %d: %s", redactURL(path), resp.StatusCode, auth0ErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func auth0ErrMsg(body []byte) string {
	var e struct {
		Message          string `json:"message"`
		ErrorDescription string `json:"error_description"`
		Error            string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil {
		switch {
		case e.Message != "":
			return e.Message
		case e.ErrorDescription != "":
			return e.ErrorDescription
		case e.Error != "":
			return e.Error
		}
	}
	return "request failed"
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

// auth0List paginates a keyed-envelope endpoint ({"<key>":[...],"total","start","length"}) with
// 0-based ?page=&per_page=&include_totals=true, stopping when start+length >= total (or a short/
// empty page). The nesting key is a parameter.
func auth0List[T any](ctx context.Context, path, key string) ([]T, error) {
	var all []T
	for page := 0; ; page++ {
		if page >= auth0MaxPages {
			return nil, &auth0APIError{msg: fmt.Sprintf("auth0 %s: pagination exceeded %d pages", redactURL(path), auth0MaxPages)}
		}
		url := fmt.Sprintf("%s%spage=%d&per_page=%d&include_totals=true", path, sep(path), page, auth0PerPage)
		body, _, err := auth0Do(ctx, http.MethodGet, url)
		if err != nil {
			// Return the pages already accumulated: Auth0 caps page-based paging at ~1000
			// records with a 400, and the caller's range loop still adopts `all` before the
			// error is classified — better than dropping the whole large list.
			return all, err
		}
		items, total, start, length, err := decodeKeyed[T](path, key, body)
		if err != nil {
			return all, err
		}
		all = append(all, items...)
		if length == 0 || start+length >= total {
			return all, nil
		}
	}
}

func decodeKeyed[T any](path, key string, body []byte) (items []T, total, start, length int, err error) {
	if len(body) == 0 {
		return nil, 0, 0, 0, nil
	}
	var m map[string]json.RawMessage
	if uerr := json.Unmarshal(body, &m); uerr != nil {
		return nil, 0, 0, 0, &auth0APIError{msg: fmt.Sprintf("auth0 %s: decode: %v", redactURL(path), uerr)}
	}
	total = intField(m["total"])
	start = intField(m["start"])
	length = intField(m["length"])
	if raw, ok := m[key]; ok && len(raw) > 0 {
		if uerr := json.Unmarshal(raw, &items); uerr != nil {
			return nil, 0, 0, 0, &auth0APIError{msg: fmt.Sprintf("auth0 %s: decode %q: %v", redactURL(path), key, uerr)}
		}
	}
	// Envelopes occasionally omit length; fall back to the page item count so the loop still
	// terminates against total.
	if length == 0 {
		length = len(items)
	}
	return items, total, start, length, nil
}

func intField(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	_ = json.Unmarshal(raw, &n)
	return n
}

// auth0GetArray fetches a bare-array endpoint (log streams) into []T.
func auth0GetArray[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := auth0Do(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	var items []T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, &auth0APIError{msg: fmt.Sprintf("auth0 %s: decode: %v", redactURL(path), err)}
		}
	}
	return items, nil
}

// auth0GetObject fetches a singleton object into T (e.g. GET /api/v2/tenants/settings).
func auth0GetObject[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := auth0Do(ctx, http.MethodGet, path)
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &auth0APIError{msg: fmt.Sprintf("auth0 %s: decode: %v", redactURL(path), err)}
		}
	}
	return out, nil
}
