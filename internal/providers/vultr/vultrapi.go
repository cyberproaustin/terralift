package vultr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
)

const (
	vultrBaseURL  = "https://api.vultr.com/v2"
	vultrMaxPages = 10000
	vultrPageSize = 500
)

// vultrAPIError carries the HTTP status so callers distinguish an absent feature/
// permission (403/404 → best-effort skip) from a transient/real failure (401/429/5xx
// → surface loudly). Status is 0 for pre-response (transport) errors.
type vultrAPIError struct {
	Status int
	msg    string
}

func (e *vultrAPIError) Error() string { return e.msg }

// vultrDo performs a request and returns the raw body + status. A package var so tests
// can fake it. The key is only ever on the Authorization header — never in errors/logs.
var vultrDo = func(ctx context.Context, method, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, vultrBaseURL+path, nil)
	if err != nil {
		return nil, 0, &vultrAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("VULTR_API_KEY"))
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, &vultrAPIError{msg: fmt.Sprintf("vultr %s: %v", path, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &vultrAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("vultr %s: read body: %v", path, err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &vultrAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("vultr %s: HTTP %d: %s", path, resp.StatusCode, vultrErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func vultrErrMsg(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return "request failed"
}

// vultrList flattens a cursor-paginated collection nested under a per-endpoint key
// (Vultr wraps each list under its own name, e.g. "instances", plus a meta.links.next
// cursor). The URL is built from the cursor param, so there is no body-supplied URL to
// validate.
func vultrList[T any](ctx context.Context, path, key string) ([]T, error) {
	var all []T
	cursor := ""
	for i := 0; ; i++ {
		if i >= vultrMaxPages {
			return nil, &vultrAPIError{msg: fmt.Sprintf("vultr %s: pagination exceeded %d pages", path, vultrMaxPages)}
		}
		url := fmt.Sprintf("%s%sper_page=%d", path, sep(path), vultrPageSize)
		if cursor != "" {
			url += "&cursor=" + neturl.QueryEscape(cursor)
		}
		body, _, err := vultrDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, &vultrAPIError{msg: fmt.Sprintf("vultr %s: decode: %v", path, err)}
		}
		if raw, ok := m[key]; ok && len(raw) > 0 {
			var items []T
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, &vultrAPIError{msg: fmt.Sprintf("vultr %s: decode %q: %v", path, key, err)}
			}
			all = append(all, items...)
		}
		next := nextCursor(m["meta"])
		if next == "" {
			return all, nil
		}
		cursor = next
	}
}

// vultrGetOne fetches a singleton whose object is nested under a key (e.g. "account").
func vultrGetOne[T any](ctx context.Context, path, key string) (T, error) {
	var out T
	body, _, err := vultrDo(ctx, http.MethodGet, path)
	if err != nil {
		return out, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return out, &vultrAPIError{msg: fmt.Sprintf("vultr %s: decode: %v", path, err)}
	}
	if raw, ok := m[key]; ok && len(raw) > 0 {
		return out, json.Unmarshal(raw, &out)
	}
	return out, nil
}

// nextCursor extracts meta.links.next (an opaque cursor string; "" on the last page).
func nextCursor(rawMeta json.RawMessage) string {
	if len(rawMeta) == 0 {
		return ""
	}
	var meta struct {
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	}
	_ = json.Unmarshal(rawMeta, &meta)
	return meta.Links.Next
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}
