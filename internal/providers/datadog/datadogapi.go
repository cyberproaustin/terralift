package datadog

import (
	"bytes"
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
	datadogDefaultBase = "https://api.datadoghq.com"
	datadogMaxPages    = 10000
	datadogPageSize    = 1000
)

// datadogAPIError carries the HTTP status so callers distinguish an absent feature/
// permission (403/404 → best-effort skip) from a transient/real failure (401/429/5xx →
// surface loudly). Status is 0 for pre-response (transport) errors.
type datadogAPIError struct {
	Status int
	msg    string
}

func (e *datadogAPIError) Error() string { return e.msg }

// datadogBase resolves the site-specific API base URL. Datadog is multi-site (US1/US3/
// US5/EU1/AP1/US1-FED); read DD_HOST (fallback DATADOG_HOST) as the full base URL,
// defaulting to US1. A bare host (e.g. "datadoghq.eu") is promoted to https://.
func datadogBase() string {
	h := os.Getenv("DD_HOST")
	if h == "" {
		h = os.Getenv("DATADOG_HOST")
	}
	if h == "" {
		return datadogDefaultBase
	}
	h = strings.TrimRight(h, "/")
	// DD_HOST always designates a Datadog site, all of which are HTTPS. Promote a bare
	// host and upgrade an explicit http:// so the two secret keys are never sent in
	// plaintext (they ride on request headers).
	switch {
	case strings.HasPrefix(h, "https://"):
	case strings.HasPrefix(h, "http://"):
		h = "https://" + strings.TrimPrefix(h, "http://")
	default:
		h = "https://" + h
	}
	return h
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func datadogAPIKey() string { return firstEnv("DD_API_KEY", "DATADOG_API_KEY") }
func datadogAppKey() string { return firstEnv("DD_APP_KEY", "DATADOG_APP_KEY") }

// datadogHTTPClient refuses to follow redirects. The two auth keys ride on CUSTOM headers
// (DD-API-KEY / DD-APPLICATION-KEY); Go's redirect handling only strips Authorization/
// Cookie on a cross-host 3xx, NOT custom headers, so an auto-followed redirect from the
// configured host to another host would re-send both org-scoped secrets in cleartext to
// that host. These list endpoints answer 200 directly, so a redirect is treated as a hard
// error (the request to the redirect target is never sent — CheckRedirect fires first).
var datadogHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (auth headers must not leave the configured host)", req.URL.Host)
	},
}

// datadogDo performs a request against a FULL url and returns the raw body + status. A
// package var so tests can fake it. Datadog needs TWO auth headers on every request:
// DD-API-KEY and DD-APPLICATION-KEY (note the header name is DD-APPLICATION-KEY while the
// env var is DD_APP_KEY). Both keys are only ever on their headers, never in errors/logs.
// URLs are always built from datadogBase()+path here (we never follow a server-supplied
// next-link), and redirects are refused (datadogHTTPClient), so the keys are never sent to
// an unexpected host.
var datadogDo = func(ctx context.Context, method, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, 0, &datadogAPIError{msg: err.Error()}
	}
	req.Header.Set("DD-API-KEY", datadogAPIKey())
	req.Header.Set("DD-APPLICATION-KEY", datadogAppKey())
	req.Header.Set("Accept", "application/json")
	resp, err := datadogHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &datadogAPIError{msg: fmt.Sprintf("datadog %s: %v", redactURL(url), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &datadogAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("datadog %s: read body: %v", redactURL(url), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &datadogAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("datadog %s: HTTP %d: %s", redactURL(url), resp.StatusCode, datadogErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

// redactURL strips any query string from a URL before it appears in an error/log. Datadog
// keys ride on headers (never the query), so this is belt-and-suspenders against a future
// endpoint that takes a token in the query.
func redactURL(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i]
	}
	return url
}

func datadogErrMsg(body []byte) string {
	var e struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 {
		return strings.Join(e.Errors, "; ")
	}
	return "request failed"
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

// --- v1 helpers ------------------------------------------------------------

// datadogGetArray fetches a v1 bare-array endpoint (no pagination) and unmarshals into
// []T. Used for logs pipelines and legacy downtime.
func datadogGetArray[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := datadogDo(ctx, http.MethodGet, datadogBase()+path)
	if err != nil {
		return nil, err
	}
	var items []T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: decode: %v", path, err)}
		}
	}
	return items, nil
}

// datadogListArrayPaged paginates a v1 bare-array endpoint with 0-based ?page=&page_size=,
// stopping on a short/empty page. Used for /api/v1/monitor.
func datadogListArrayPaged[T any](ctx context.Context, path string) ([]T, error) {
	var all []T
	for page := 0; ; page++ {
		if page >= datadogMaxPages {
			return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: pagination exceeded %d pages", path, datadogMaxPages)}
		}
		url := fmt.Sprintf("%s%s%spage=%d&page_size=%d", datadogBase(), path, sep(path), page, datadogPageSize)
		body, _, err := datadogDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var items []T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &items); err != nil {
				return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: decode page %d: %v", path, page, err)}
			}
		}
		all = append(all, items...)
		if len(items) < datadogPageSize {
			return all, nil
		}
	}
}

// datadogGetKeyed fetches a v1 keyed-object endpoint ({<key>:[...]}, DigitalOcean-style)
// and unmarshals the named array into []T. No pagination (dashboards, dashboard lists,
// synthetics tests, logs indexes all return the full set in one call). NB: v1 SLO uses
// key "data" with FLAT objects here (not JSON:API) — see datadogListKeyedOffset.
func datadogGetKeyed[T any](ctx context.Context, path, key string) ([]T, error) {
	body, _, err := datadogDo(ctx, http.MethodGet, datadogBase()+path)
	if err != nil {
		return nil, err
	}
	return decodeKeyed[T](path, key, body)
}

// datadogListKeyedOffset paginates a v1 keyed-object endpoint with ?limit=&offset=,
// stopping on a short page. Used for /api/v1/slo (key "data", flat objects).
func datadogListKeyedOffset[T any](ctx context.Context, path, key string) ([]T, error) {
	var all []T
	for offset := 0; ; offset += datadogPageSize {
		if offset/datadogPageSize >= datadogMaxPages {
			return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: pagination exceeded %d pages", path, datadogMaxPages)}
		}
		url := fmt.Sprintf("%s%s%slimit=%d&offset=%d", datadogBase(), path, sep(path), datadogPageSize, offset)
		body, _, err := datadogDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		items, err := decodeKeyed[T](path, key, body)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < datadogPageSize {
			return all, nil
		}
	}
}

func decodeKeyed[T any](path, key string, body []byte) ([]T, error) {
	if len(body) == 0 {
		return nil, nil
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: decode: %v", path, err)}
	}
	raw, ok := env[key]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var items []T
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: decode %q: %v", path, key, err)}
	}
	return items, nil
}

// --- JSON:API helpers ------------------------------------------------------

// ddID is a JSON:API id that tolerates both a JSON string (the v2 convention: roles,
// users, security rules, downtimes) AND a bare JSON number (v1-shaped notebooks return
// "id": 123). A plain `string` field would fail the whole decode on a numeric id.
type ddID string

func (d *ddID) UnmarshalJSON(b []byte) error {
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
		*d = ddID(s)
		return nil
	}
	*d = ddID(b) // bare number → use its literal text
	return nil
}

// ddJSONAPIItem is a JSON:API resource object: id + type at the top level, everything
// else under attributes. The id is read from the top level (NOT from attributes), the
// same rule the Fastly provider used for its JSON:API plane. raw retains the whole item so
// attr/attrBool can fall back to top-level fields for endpoints that return FLAT objects
// under `data` rather than a real attributes wrapper (e.g. /api/v2/security_monitoring/
// rules, whose id/name/isDefault sit at the item root).
type ddJSONAPIItem struct {
	ID         ddID            `json:"id"`
	Type       string          `json:"type"`
	Attributes json.RawMessage `json:"attributes"`
	raw        json.RawMessage
}

func (it *ddJSONAPIItem) UnmarshalJSON(b []byte) error {
	type alias ddJSONAPIItem
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*it = ddJSONAPIItem(a)
	it.raw = append(json.RawMessage(nil), b...)
	return nil
}

func (it ddJSONAPIItem) id() string { return string(it.ID) }

// attr reads a string field, preferring the attributes sub-object (proper JSON:API) and
// falling back to the item root (flat-object endpoints). "" if absent or non-string.
func (it ddJSONAPIItem) attr(key string) string {
	if s, ok := fieldString(it.Attributes, key); ok {
		return s
	}
	if s, ok := fieldString(it.raw, key); ok {
		return s
	}
	return ""
}

// attrBool reads a bool field with the same attributes-then-root precedence.
func (it ddJSONAPIItem) attrBool(key string) bool {
	if b, ok := fieldBool(it.Attributes, key); ok {
		return b
	}
	if b, ok := fieldBool(it.raw, key); ok {
		return b
	}
	return false
}

func fieldString(raw json.RawMessage, key string) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return "", false
	}
	s, ok := m[key].(string)
	return s, ok
}

func fieldBool(raw json.RawMessage, key string) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return false, false
	}
	b, ok := m[key].(bool)
	return b, ok
}

type ddJSONAPIEnvelope struct {
	Data []ddJSONAPIItem `json:"data"`
	Meta struct {
		Page struct {
			TotalCount         int64 `json:"total_count"`
			TotalFilteredCount int64 `json:"total_filtered_count"`
		} `json:"page"`
	} `json:"meta"`
}

// datadogListJSONAPI paginates a v2 JSON:API endpoint with ?page[number]=&page[size]=,
// stopping on a short page or once meta.page.total_count is reached. Used for roles,
// users, security rules, and logs metrics (the last is unpaged — one short page ends it).
func datadogListJSONAPI(ctx context.Context, path string) ([]ddJSONAPIItem, error) {
	var all []ddJSONAPIItem
	for page := 0; ; page++ {
		if page >= datadogMaxPages {
			return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: pagination exceeded %d pages", path, datadogMaxPages)}
		}
		url := fmt.Sprintf("%s%s%spage[number]=%d&page[size]=%d", datadogBase(), path, sep(path), page, datadogPageSize)
		env, err := datadogFetchJSONAPI(ctx, path, url)
		if err != nil {
			return nil, err
		}
		all = append(all, env.Data...)
		if len(env.Data) < datadogPageSize {
			return all, nil
		}
		if env.Meta.Page.TotalCount > 0 && int64(len(all)) >= env.Meta.Page.TotalCount {
			return all, nil
		}
	}
}

// datadogListJSONAPIOffset paginates a v2 JSON:API endpoint with the offset-style
// ?page[offset]=&page[limit]= (a per-resource quirk — v2 downtimes, NOT page[number]),
// stopping on a short page.
func datadogListJSONAPIOffset(ctx context.Context, path string) ([]ddJSONAPIItem, error) {
	var all []ddJSONAPIItem
	for offset := 0; ; offset += datadogPageSize {
		if offset/datadogPageSize >= datadogMaxPages {
			return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: pagination exceeded %d pages", path, datadogMaxPages)}
		}
		url := fmt.Sprintf("%s%s%spage[offset]=%d&page[limit]=%d", datadogBase(), path, sep(path), offset, datadogPageSize)
		env, err := datadogFetchJSONAPI(ctx, path, url)
		if err != nil {
			return nil, err
		}
		all = append(all, env.Data...)
		if len(env.Data) < datadogPageSize {
			return all, nil
		}
	}
}

// datadogListJSONAPIStartCount paginates a v1 JSON:API-shaped endpoint with ?start=&count=
// (notebooks), bounded by meta.page.total_filtered_count and a short page.
func datadogListJSONAPIStartCount(ctx context.Context, path string) ([]ddJSONAPIItem, error) {
	var all []ddJSONAPIItem
	for start := 0; ; start += datadogPageSize {
		if start/datadogPageSize >= datadogMaxPages {
			return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: pagination exceeded %d pages", path, datadogMaxPages)}
		}
		url := fmt.Sprintf("%s%s%sstart=%d&count=%d", datadogBase(), path, sep(path), start, datadogPageSize)
		env, err := datadogFetchJSONAPI(ctx, path, url)
		if err != nil {
			return nil, err
		}
		all = append(all, env.Data...)
		if len(env.Data) < datadogPageSize {
			return all, nil
		}
		if env.Meta.Page.TotalFilteredCount > 0 && int64(len(all)) >= env.Meta.Page.TotalFilteredCount {
			return all, nil
		}
	}
}

func datadogFetchJSONAPI(ctx context.Context, path, url string) (*ddJSONAPIEnvelope, error) {
	body, _, err := datadogDo(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	var env ddJSONAPIEnvelope
	if len(body) > 0 {
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, &datadogAPIError{msg: fmt.Sprintf("datadog %s: decode: %v", path, err)}
		}
	}
	return &env, nil
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
