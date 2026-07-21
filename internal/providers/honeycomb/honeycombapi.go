package honeycomb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Honeycomb's v1 config plane is bare JSON arrays with NO pagination — each list returns the
// full set in one call — so this client is just a bare-array helper + a singleton helper
// (the Fastly fastlyGet/fastlyGetOne surface, minus the pagers). US default; EU is
// https://api.eu1.honeycomb.io, selected by HONEYCOMB_API_ENDPOINT.
const (
	honeycombBaseUS = "https://api.honeycomb.io"
	honeycombBaseEU = "https://api.eu1.honeycomb.io"
)

// honeycombAPIError carries the HTTP status so callers distinguish an absent scope/feature
// (403/404 → best-effort skip) from a fatal auth failure (401) or transient error (429/5xx →
// Warn). Status is 0 for pre-response (transport) errors.
type honeycombAPIError struct {
	Status int
	msg    string
}

func (e *honeycombAPIError) Error() string { return e.msg }

// honeycombBase resolves the API base URL from env (US default). The user points at EU by
// setting HONEYCOMB_API_ENDPOINT=https://api.eu1.honeycomb.io; HONEYCOMB_API_HOST and the
// Terraformer-era HONEYCOMB_API_URL are accepted as fallbacks.
func honeycombBase() string {
	for _, k := range []string{"HONEYCOMB_API_ENDPOINT", "HONEYCOMB_API_HOST", "HONEYCOMB_API_URL"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			continue
		}
		v = strings.TrimRight(v, "/")
		// Force https so the X-Honeycomb-Team key is never sent in plaintext (it rides on a
		// header); promote a bare host and upgrade an explicit http:// (mirrors datadogBase).
		switch {
		case strings.HasPrefix(v, "https://"):
		case strings.HasPrefix(v, "http://"):
			v = "https://" + strings.TrimPrefix(v, "http://")
		default:
			v = "https://" + v
		}
		return v
	}
	return honeycombBaseUS
}

// honeycombHTTPClient refuses redirects so the X-Honeycomb-Team key can never be replayed to
// another host on a 3xx (defence in depth — the v1 plane answers 200 directly).
var honeycombHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the X-Honeycomb-Team header must not leave the configured host)", req.URL.Host)
	},
}

// honeycombDo performs a request against base+path and returns the raw body + status. A
// package var so tests can fake it. Auth via the X-Honeycomb-Team header (the divergence from
// Fastly-Key/DD-API-KEY); the key rides ONLY on that header, never in the URL, errors, or logs.
var honeycombDo = func(ctx context.Context, method, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, honeycombBase()+path, nil)
	if err != nil {
		return nil, 0, &honeycombAPIError{msg: err.Error()}
	}
	req.Header.Set("X-Honeycomb-Team", os.Getenv("HONEYCOMB_API_KEY"))
	req.Header.Set("Accept", "application/json")

	resp, err := honeycombHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &honeycombAPIError{msg: fmt.Sprintf("honeycomb %s: %v", redactPath(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &honeycombAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("honeycomb %s: read body: %v", redactPath(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &honeycombAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("honeycomb %s: HTTP %d: %s", redactPath(path), resp.StatusCode, honeycombErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func redactPath(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

func honeycombErrMsg(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return "request failed"
}

// honeycombGet fetches a bare-array endpoint (no pagination) into []T.
func honeycombGet[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := honeycombDo(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	var items []T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, &honeycombAPIError{msg: fmt.Sprintf("honeycomb %s: decode: %v", redactPath(path), err)}
		}
	}
	return items, nil
}

// honeycombGetOne fetches a bare-object singleton into T.
func honeycombGetOne[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := honeycombDo(ctx, http.MethodGet, path)
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &honeycombAPIError{msg: fmt.Sprintf("honeycomb %s: decode: %v", redactPath(path), err)}
		}
	}
	return out, nil
}
