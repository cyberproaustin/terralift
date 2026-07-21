package mackerel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// mackerelAPIError carries the HTTP status so callers distinguish an absent feature/permission
// (403/404 → best-effort skip) from a fatal auth failure (401) or a transient error (429/5xx →
// Warn). Status is 0 for pre-response (transport) errors.
type mackerelAPIError struct {
	Status int
	msg    string
}

func (e *mackerelAPIError) Error() string { return e.msg }

// mkKey resolves the API key from MACKEREL_APIKEY (the provider/SDK primary) or the
// MACKEREL_API_KEY alias. It rides ONLY on the X-Api-Key header — never a URL, error, log, body,
// config, or state.
func mkKey() string {
	if v := strings.TrimSpace(os.Getenv("MACKEREL_APIKEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("MACKEREL_API_KEY"))
}

// mkBase resolves the API base URL. MACKEREL_API_BASE (or the MACKEREL_APIURL alias) overrides —
// e.g. the enterprise KCPS endpoint https://kcps-mackerel.io — otherwise the default SaaS host.
// https is forced (the X-Api-Key is a secret).
func mkBase() string {
	for _, env := range []string{"MACKEREL_API_BASE", "MACKEREL_APIURL"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return forceHTTPS(strings.TrimRight(v, "/"))
		}
	}
	return "https://api.mackerelio.com"
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

// mkHTTPClient refuses redirects so the X-Api-Key header can never be replayed to another host on
// a 3xx (Go does not strip a custom header on a cross-host redirect).
var mkHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the X-Api-Key header must not leave the configured host)", req.URL.Host)
	},
}

// mkDo performs a GET against base+path and returns the raw body + status. A package var so tests
// can fake it. Auth via the X-Api-Key header; the key rides ONLY on that header, never in the URL,
// errors, or logs. Mackerel's config plane is entirely GET, so there is no request body.
var mkDo = func(ctx context.Context, method, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, mkBase()+path, nil)
	if err != nil {
		return nil, 0, &mackerelAPIError{msg: err.Error()}
	}
	req.Header.Set("X-Api-Key", mkKey())
	req.Header.Set("Accept", "application/json")

	resp, err := mkHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &mackerelAPIError{msg: fmt.Sprintf("mackerel %s: %v", redactURL(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &mackerelAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("mackerel %s: read body: %v", redactURL(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &mackerelAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("mackerel %s: HTTP %d: %s", redactURL(path), resp.StatusCode, mkErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

// mkErrMsg reads Mackerel's error envelope, which is usually {"error":{"message":"..."}} but is
// sometimes the older {"error":"..."} string form — try both, never echo the request.
func mkErrMsg(body []byte) string {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(e.Error, &obj) == nil && obj.Message != "" {
			return obj.Message
		}
		var s string
		if json.Unmarshal(e.Error, &s) == nil && s != "" {
			return s
		}
	}
	return "request failed"
}

// mkList GETs a Mackerel list endpoint and decodes its named-array envelope. Every config-core
// endpoint wraps its array in an object keyed by `key` (e.g. {"services":[...]},
// {"aws_integrations":[...]}, {"notificationGroups":[...]}) — see decodeEnvelope for the tolerant
// fallback. Mackerel's config plane is unpaginated (only the deferred hosts/alerts planes page).
func mkList[T any](ctx context.Context, path, key string) ([]T, error) {
	body, _, err := mkDo(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	return decodeEnvelope[T](path, key, body)
}

// decodeEnvelope extracts the array from a {<key>:[...]} envelope, falling back to a bare array
// only when the response is genuinely NOT wrapped (e.g. a top-level [...]). The envelope key varies
// per endpoint (services/monitors/channels/dashboards/downtimes are lower-snake of the path, but
// notificationGroups + alertGroupSettings are camelCase and aws_integrations is snake — VERIFY per
// endpoint). A WRONG key against a wrapped object does NOT silently degrade to empty: the bare-array
// fallback then fails to unmarshal the object into a slice and returns a decode error, so a
// mis-keyed endpoint surfaces as a Warn rather than a missing resource type.
func decodeEnvelope[T any](path, key string, body []byte) ([]T, error) {
	if len(body) == 0 {
		return nil, nil
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(body, &env) == nil {
		if raw, ok := env[key]; ok && len(raw) > 0 {
			var items []T
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, &mackerelAPIError{msg: fmt.Sprintf("mackerel %s: decode %q: %v", redactURL(path), key, err)}
			}
			return items, nil
		}
	}
	var items []T
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, &mackerelAPIError{msg: fmt.Sprintf("mackerel %s: decode: %v", redactURL(path), err)}
	}
	return items, nil
}

// mkObj is a flexible list element covering every Mackerel config resource's id + display name.
// Mackerel ids are opaque STRINGS (e.g. "2qtozU21abc"), so no numeric-id juggling is needed.
// Services and roles have NO id — their identity is `name` (roles are additionally service-scoped;
// the composite is built in enumerate). Dashboards label by `title`. Secret fields (channel url,
// aws_integration secret_key, monitor external headers) are deliberately NOT decoded.
type mkObj struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

// label is the human name for the resource (Name, else the dashboard Title, else the opaque id).
func (o mkObj) label() string {
	for _, v := range []string{o.Name, o.Title, o.ID} {
		if v != "" {
			return v
		}
	}
	return ""
}
