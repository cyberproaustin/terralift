package logzio

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
	logzioMaxPages = 10000
	logzioPageSize = 100
)

// logzioAPIError carries the HTTP status so callers distinguish an absent feature/plan
// (403/404 → best-effort skip) from a fatal auth failure (401) or a transient error (429/5xx →
// Warn). Status is 0 for pre-response (transport) errors.
type logzioAPIError struct {
	Status int
	msg    string
}

func (e *logzioAPIError) Error() string { return e.msg }

// lzRegionBases maps a Logz.io region code to its API base URL. The general rule is
// https://api-<region>.logz.io, except us → https://api.logz.io (no region infix).
var lzRegionBases = map[string]string{
	"us": "https://api.logz.io",
	"eu": "https://api-eu.logz.io",
	"au": "https://api-au.logz.io",
	"ca": "https://api-ca.logz.io",
	"uk": "https://api-uk.logz.io",
	"wa": "https://api-wa.logz.io",
}

// lzBase resolves the region base URL. LOGZIO_BASE_URL (a full URL) wins; otherwise
// LOGZIO_REGION (default us) selects the base. https is forced — the X-API-TOKEN is a secret.
func lzBase() string {
	if v := strings.TrimSpace(os.Getenv("LOGZIO_BASE_URL")); v != "" {
		return forceHTTPS(strings.TrimRight(v, "/"))
	}
	region := strings.ToLower(strings.TrimSpace(os.Getenv("LOGZIO_REGION")))
	// Guard the charset before interpolating region into the host: a value carrying '/', '@', or
	// '.' would otherwise shift the host/path the X-API-TOKEN is sent to. Unknown-but-clean codes
	// still flow through the general api-<region>.logz.io rule; anything else falls back to us.
	if region == "" || !validRegion(region) {
		region = "us"
	}
	if base, ok := lzRegionBases[region]; ok {
		return base
	}
	return "https://api-" + region + ".logz.io"
}

// validRegion accepts only a DNS-label-safe region code (lower-case letters, digits, hyphen).
func validRegion(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
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

// lzHTTPClient refuses redirects so the X-API-TOKEN can never be replayed to another host on a
// 3xx (Go does not strip a custom header on a cross-host redirect).
var lzHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the X-API-TOKEN header must not leave the configured host)", req.URL.Host)
	},
}

// lzDo performs a request against base+path (body is nil for GET, a JSON body for POST-search)
// and returns the raw body + status. A package var so tests can fake it. Auth via the
// X-API-TOKEN header; the token rides ONLY on that header, never in the URL, errors, or logs.
var lzDo = func(ctx context.Context, method, path string, reqBody []byte) ([]byte, int, error) {
	var rdr io.Reader
	if reqBody != nil {
		rdr = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, lzBase()+path, rdr)
	if err != nil {
		return nil, 0, &logzioAPIError{msg: err.Error()}
	}
	req.Header.Set("X-API-TOKEN", os.Getenv("LOGZIO_API_TOKEN"))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := lzHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &logzioAPIError{msg: fmt.Sprintf("logzio %s: %v", redactURL(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &logzioAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("logzio %s: read body: %v", redactURL(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &logzioAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("logzio %s: HTTP %d: %s", redactURL(path), resp.StatusCode, lzErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func lzErrMsg(body []byte) string {
	var e struct {
		Message   string `json:"message"`
		ErrorCode string `json:"errorCode"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Message != "" {
			return e.Message
		}
		if e.ErrorCode != "" {
			return e.ErrorCode
		}
	}
	return "request failed"
}

// lzGet fetches a GET list endpoint into []T. The response SHAPE varies per endpoint, so the
// decode is tolerant (see decodeList): a bare array, an object wrapping a single named array, or a
// single resource object (e.g. archive/settings) all yield the right slice.
func lzGet[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := lzDo(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return decodeList[T](path, body)
}

// decodeList tolerates the three GET-list shapes Logz.io returns across endpoints (spec VERIFY):
//   - a bare JSON array [ ... ]                         → decode directly;
//   - an object wrapping exactly one named array, e.g.
//     {"endpoints":[ ... ]} (optionally beside scalar
//     metadata such as {"results":[...],"total":N})     → unwrap that array;
//   - a single resource object { ... } (e.g. a settings
//     singleton with no array-valued field)             → wrap as a one-element slice.
//
// The "exactly one array-valued key" rule keeps a settings object with several scalar fields from
// being misread as a wrapper. Ambiguous multi-array objects degrade to the single-object path.
// Phase-B live validation pins the real per-endpoint shape.
func decodeList[T any](path string, body []byte) ([]T, error) {
	b := bytes.TrimSpace(body)
	if len(b) == 0 {
		return nil, nil
	}
	switch b[0] {
	case '[':
		return unmarshalList[T](path, b)
	case '{':
		var env map[string]json.RawMessage
		if json.Unmarshal(b, &env) == nil {
			var arrays []json.RawMessage
			for _, raw := range env {
				if r := bytes.TrimSpace(raw); len(r) > 0 && r[0] == '[' {
					arrays = append(arrays, r)
				}
			}
			if len(arrays) == 1 {
				return unmarshalList[T](path, arrays[0])
			}
		}
		// No single wrapped array — treat the object as one resource.
		var one T
		if err := json.Unmarshal(b, &one); err != nil {
			return nil, &logzioAPIError{msg: fmt.Sprintf("logzio %s: decode object: %v", redactURL(path), err)}
		}
		return []T{one}, nil
	default:
		// e.g. a literal null → empty list, no error.
		return unmarshalList[T](path, b)
	}
}

func unmarshalList[T any](path string, raw []byte) ([]T, error) {
	var items []T
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, &logzioAPIError{msg: fmt.Sprintf("logzio %s: decode: %v", redactURL(path), err)}
	}
	return items, nil
}

// lzSearch paginates a POST …/search|/retrieve endpoint that takes a pagination body and
// returns a paged envelope keyed by `key` (e.g. "results"); falls back to a bare array.
func lzSearch[T any](ctx context.Context, path, key string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		if page > logzioMaxPages {
			return all, &logzioAPIError{msg: fmt.Sprintf("logzio %s: pagination exceeded %d pages", redactURL(path), logzioMaxPages)}
		}
		reqBody := []byte(fmt.Sprintf(`{"pagination":{"pageNumber":%d,"pageSize":%d}}`, page, logzioPageSize))
		body, _, err := lzDo(ctx, http.MethodPost, path, reqBody)
		if err != nil {
			return all, err
		}
		items, err := decodeSearch[T](path, key, body)
		if err != nil {
			return all, err
		}
		all = append(all, items...)
		if len(items) < logzioPageSize {
			return all, nil
		}
	}
}

func decodeSearch[T any](path, key string, body []byte) ([]T, error) {
	if len(body) == 0 {
		return nil, nil
	}
	// Preferred: a {<key>:[...]} envelope. Fall back to a bare array.
	var env map[string]json.RawMessage
	if json.Unmarshal(body, &env) == nil {
		if raw, ok := env[key]; ok && len(raw) > 0 {
			var items []T
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, &logzioAPIError{msg: fmt.Sprintf("logzio %s: decode %q: %v", redactURL(path), key, err)}
			}
			return items, nil
		}
	}
	var items []T
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, &logzioAPIError{msg: fmt.Sprintf("logzio %s: decode: %v", redactURL(path), err)}
	}
	return items, nil
}

// lzID is a Logz.io id that tolerates both a JSON number (the common case — stringify) and a
// JSON string (e.g. a drop_filter hash). Never a plain-string field, which would fail the
// whole decode on a numeric id.
type lzID string

func (d *lzID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*d = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*d = lzID(s)
		return nil
	}
	*d = lzID(b) // bare number → its literal text
	return nil
}

// lzObj is a flexible list element covering every Logz.io config resource's id + display name
// (the exact field names vary per endpoint — id/alertId/accountId, name/title/accountName/
// username). Secret fields are deliberately NOT decoded.
type lzObj struct {
	ID          lzID   `json:"id"`
	AlertID     lzID   `json:"alertId"`
	AccountID   lzID   `json:"accountId"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	AccountName string `json:"accountName"`
	Username    string `json:"username"`
}

func (o lzObj) id() string {
	for _, v := range []lzID{o.ID, o.AlertID, o.AccountID} {
		if v != "" {
			return string(v)
		}
	}
	return ""
}

func (o lzObj) label() string {
	for _, v := range []string{o.Title, o.Name, o.AccountName, o.Username} {
		if v != "" {
			return v
		}
	}
	return o.id()
}
