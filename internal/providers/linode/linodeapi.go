package linode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	linodeBaseURL  = "https://api.linode.com/v4"
	linodeMaxPages = 10000
	linodePageSize = 500
)

// linodeAPIError carries the HTTP status so callers distinguish an absent feature/
// permission (403/404 → best-effort skip) from a transient/real failure (401/429/5xx
// → surface loudly). Status is 0 for pre-response (transport) errors.
type linodeAPIError struct {
	Status int
	msg    string
}

func (e *linodeAPIError) Error() string { return e.msg }

// linodeDo performs a request and returns the raw body + status. An optional JSON
// X-Filter header narrows a collection server-side (used to avoid paginating the whole
// public image/stackscript catalog). A package var so tests can fake it. The token is
// only ever on the Authorization header — never in errors or logs.
var linodeDo = func(ctx context.Context, method, path, xFilter string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, linodeBaseURL+path, nil)
	if err != nil {
		return nil, 0, &linodeAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("LINODE_TOKEN"))
	req.Header.Set("Accept", "application/json")
	if xFilter != "" {
		req.Header.Set("X-Filter", xFilter)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, &linodeAPIError{msg: fmt.Sprintf("linode %s: %v", path, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &linodeAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("linode %s: read body: %v", path, err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &linodeAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("linode %s: HTTP %d: %s", path, resp.StatusCode, linodeErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

// linodeErrMsg pulls the first reason out of a Linode error body ({"errors":[{"reason"}]}).
func linodeErrMsg(body []byte) string {
	var e struct {
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 && e.Errors[0].Reason != "" {
		return e.Errors[0].Reason
	}
	return "request failed"
}

// linodeList paginates a collection (every Linode list wraps its array under a fixed
// `data` key with numeric page/pages). The page URL is built from a counter, so there
// is no body-supplied next-URL to validate.
func linodeList[T any](ctx context.Context, path, xFilter string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		if page > linodeMaxPages {
			return nil, &linodeAPIError{msg: fmt.Sprintf("linode %s: pagination exceeded %d pages", path, linodeMaxPages)}
		}
		url := fmt.Sprintf("%s%spage=%d&page_size=%d", path, sep(path), page, linodePageSize)
		body, _, err := linodeDo(ctx, http.MethodGet, url, xFilter)
		if err != nil {
			return nil, err
		}
		var env struct {
			Data  json.RawMessage `json:"data"`
			Pages int             `json:"pages"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, &linodeAPIError{msg: fmt.Sprintf("linode %s: decode: %v", path, err)}
		}
		if len(env.Data) > 0 {
			var items []T
			if err := json.Unmarshal(env.Data, &items); err != nil {
				return nil, &linodeAPIError{msg: fmt.Sprintf("linode %s: decode data: %v", path, err)}
			}
			all = append(all, items...)
		}
		if page >= env.Pages { // 0 pages (empty) or last page reached
			return all, nil
		}
	}
}

// linodeGetOne fetches a singleton (returned as a bare object, no `data` wrapper).
func linodeGetOne[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := linodeDo(ctx, http.MethodGet, path, "")
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &linodeAPIError{msg: fmt.Sprintf("linode %s: decode: %v", path, err)}
		}
	}
	return out, nil
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}
