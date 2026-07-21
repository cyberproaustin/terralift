package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	pdBaseUS   = "https://api.pagerduty.com"
	pdBaseEU   = "https://api.eu.pagerduty.com"
	pdMaxPages = 10000
	pdPageSize = 100
)

// pagerdutyAPIError carries the HTTP status so callers distinguish an absent scope/feature
// (403/404 → best-effort skip) from a fatal auth failure (401) or transient error (429/5xx →
// Warn). Status is 0 for pre-response (transport) errors.
type pagerdutyAPIError struct {
	Status int
	msg    string
}

func (e *pagerdutyAPIError) Error() string { return e.msg }

// pdBase resolves the service-region base URL. An account provisioned in the EU cannot be
// reached on the US host; PAGERDUTY_API_URL (full URL) wins, else PAGERDUTY_SERVICE_REGION=eu
// selects the EU host, else US. https is forced — the token is a bearer-equivalent secret.
func pdBase() string {
	if v := strings.TrimSpace(os.Getenv("PAGERDUTY_API_URL")); v != "" {
		return forceHTTPS(strings.TrimRight(v, "/"))
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("PAGERDUTY_SERVICE_REGION")), "eu") {
		return pdBaseEU
	}
	return pdBaseUS
}

// pdAuthHeader builds the distinctive `Authorization: Token token=<token>` value — NOT
// `Bearer <token>`, NOT a custom header. Getting the literal `Token token=` prefix wrong is a
// silent 401.
func pdAuthHeader() string {
	return "Token token=" + os.Getenv("PAGERDUTY_TOKEN")
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

// pdHTTPClient refuses redirects so the token can never be replayed to another host on a 3xx.
var pdHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the Authorization header must not leave the configured host)", req.URL.Host)
	},
}

// pdDo performs a request against base+path and returns the raw body + status. A package var
// so tests can fake it. PagerDuty's auth is the distinctive `Authorization: Token token=<t>`
// header (NOT Bearer) plus the vnd.pagerduty+json;version=2 Accept header. `from` sets the
// `From: <user-email>` header required by a few endpoints (response_plays) — pass "" to omit.
// The token rides ONLY on the Authorization header, never in the URL, errors, or logs.
var pdDo = func(ctx context.Context, method, path, from string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, pdBase()+path, nil)
	if err != nil {
		return nil, 0, &pagerdutyAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", pdAuthHeader())
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
	req.Header.Set("Content-Type", "application/json")
	if from != "" {
		req.Header.Set("From", from)
	}

	resp, err := pdHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &pagerdutyAPIError{msg: fmt.Sprintf("pagerduty %s: %v", redactPath(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &pagerdutyAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("pagerduty %s: read body: %v", redactPath(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &pagerdutyAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("pagerduty %s: HTTP %d: %s", redactPath(path), resp.StatusCode, pdErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func redactPath(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

func pdErrMsg(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
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

// pdListPaged paginates the classic keyed offset/limit/`more` envelope
// ({"<key>":[...],"limit":n,"offset":n,"more":bool}). It loops ?limit=&offset=, incrementing
// offset by the page size WHILE `more == true` (`more` is authoritative — `total` is not used).
// The envelope key is passed as a parameter. `from` threads the From header for endpoints that
// require it.
func pdListPaged[T any](ctx context.Context, path, key, from string) ([]T, error) {
	var all []T
	for offset := 0; ; offset += pdPageSize {
		if offset/pdPageSize >= pdMaxPages {
			return nil, &pagerdutyAPIError{msg: fmt.Sprintf("pagerduty %s: pagination exceeded %d pages", redactPath(path), pdMaxPages)}
		}
		url := fmt.Sprintf("%s%slimit=%d&offset=%d", path, sep(path), pdPageSize, offset)
		body, _, err := pdDo(ctx, http.MethodGet, url, from)
		if err != nil {
			return nil, err
		}
		items, more, err := decodePaged[T](path, key, body)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if !more {
			return all, nil
		}
	}
}

func decodePaged[T any](path, key string, body []byte) ([]T, bool, error) {
	if len(body) == 0 {
		return nil, false, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, false, &pagerdutyAPIError{msg: fmt.Sprintf("pagerduty %s: decode: %v", redactPath(path), err)}
	}
	var more bool
	if raw, ok := m["more"]; ok {
		_ = json.Unmarshal(raw, &more)
	}
	var items []T
	if raw, ok := m[key]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false, &pagerdutyAPIError{msg: fmt.Sprintf("pagerduty %s: decode %q: %v", redactPath(path), key, err)}
		}
	}
	return items, more, nil
}
