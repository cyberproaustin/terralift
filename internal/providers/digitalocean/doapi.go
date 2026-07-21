package digitalocean

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
	doBaseURL  = "https://api.digitalocean.com/v2"
	doMaxPages = 10000
)

// doAPIError carries the HTTP status so callers distinguish an absent feature/
// permission (403/404 → best-effort skip) from a transient/real failure (401/429/5xx
// → surface loudly). Status is 0 for pre-response (transport) errors.
type doAPIError struct {
	Status int
	msg    string
}

func (e *doAPIError) Error() string { return e.msg }

// doDo performs a request against a FULL url (the base for the first page, then the
// links.pages.next url for subsequent pages) and returns the raw body + status. A
// package var so tests can fake it. The token is only ever on the Authorization
// header — never in errors or logs.
var doDo = func(ctx context.Context, method, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, 0, &doAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DIGITALOCEAN_TOKEN"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, &doAPIError{msg: fmt.Sprintf("digitalocean %s: %v", url, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &doAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("digitalocean %s: read body: %v", url, err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &doAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("digitalocean %s: HTTP %d: %s", url, resp.StatusCode, doErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

// doErrMsg pulls the error message out of a DigitalOcean error body ({"id","message"}).
func doErrMsg(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return "request failed"
}

// doList paginates a list endpoint, flattening the array nested under `key` (DO
// wraps each endpoint's array under its own name, e.g. "droplets"). It follows
// links.pages.next until absent.
func doList[T any](ctx context.Context, path, key string) ([]T, error) {
	url := doBaseURL + path + perPageSuffix(path)
	var all []T
	for i := 0; url != ""; i++ {
		if i >= doMaxPages {
			return nil, &doAPIError{msg: fmt.Sprintf("digitalocean %s: pagination exceeded %d pages", path, doMaxPages)}
		}
		body, _, err := doDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, &doAPIError{msg: fmt.Sprintf("digitalocean %s: decode: %v", path, err)}
		}
		if raw, ok := m[key]; ok && len(raw) > 0 {
			var items []T
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, &doAPIError{msg: fmt.Sprintf("digitalocean %s: decode %q: %v", path, key, err)}
			}
			all = append(all, items...)
		}
		// The next-page URL comes from the response body and is followed WITH the
		// Authorization token. This is not an HTTP redirect, so Go's cross-host header
		// stripping does not apply — validate the host before sending the token.
		next := nextPage(m["links"])
		if next != "" && !isDigitalOceanURL(next) {
			return nil, &doAPIError{msg: fmt.Sprintf("digitalocean %s: refusing to follow next-page url to unexpected host: %s", path, next)}
		}
		url = next
	}
	return all, nil
}

// isDigitalOceanURL reports whether raw is an https URL on the DigitalOcean API host.
func isDigitalOceanURL(raw string) bool {
	u, err := neturl.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "api.digitalocean.com"
}

// doGetOne fetches a singleton endpoint, unmarshalling the object under `key`.
func doGetOne[T any](ctx context.Context, path, key string) (T, error) {
	var out T
	body, _, err := doDo(ctx, http.MethodGet, doBaseURL+path)
	if err != nil {
		return out, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return out, &doAPIError{msg: fmt.Sprintf("digitalocean %s: decode: %v", path, err)}
	}
	if raw, ok := m[key]; ok && len(raw) > 0 {
		return out, json.Unmarshal(raw, &out)
	}
	return out, nil
}

// nextPage extracts links.pages.next (the full next-page url; empty on the last page).
func nextPage(rawLinks json.RawMessage) string {
	if len(rawLinks) == 0 {
		return ""
	}
	var l struct {
		Pages struct {
			Next string `json:"next"`
		} `json:"pages"`
	}
	_ = json.Unmarshal(rawLinks, &l)
	return l.Pages.Next
}

func perPageSuffix(path string) string {
	if strings.Contains(path, "?") {
		return "&per_page=200"
	}
	return "?per_page=200"
}
