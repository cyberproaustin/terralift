package gitlab

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
	glMaxPages = 10000
	glPerPage  = 100
)

// gitlabAPIError carries the HTTP status so callers distinguish an absent/forbidden resource
// (403/404 → best-effort skip) from a fatal auth failure (401) or a transient error (429/5xx →
// Warn). Status is 0 for pre-response (transport) errors.
type gitlabAPIError struct {
	Status int
	msg    string
}

func (e *gitlabAPIError) Error() string { return e.msg }

// glBase resolves the API base URL from GITLAB_BASE_URL (default https://gitlab.com/api/v4). Unlike
// Vault's /v1/, GitLab's base ALREADY carries the /api/v4 suffix — so a value that already contains
// /api/v<n> is used verbatim, and a bare host (self-managed, e.g. https://gitlab.example.com) has
// /api/v4 appended once. https is forced for a bare host; an '@'/userinfo splice is rejected; empty
// on a malformed value.
func glBase() string {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("GITLAB_BASE_URL")), "/")
	if raw == "" {
		return "https://gitlab.com/api/v4"
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	path := strings.TrimRight(u.Path, "/")
	if !strings.Contains(path, "/api/v") {
		path += "/api/v4"
	}
	return u.Scheme + "://" + u.Host + path
}

// glHost returns the instance host, derived from the ALREADY-VALIDATED base (so a rejected
// userinfo-splice URL yields "" here too, not the attacker host).
func glHost() string {
	u, err := neturl.Parse(glBase())
	if err != nil {
		return ""
	}
	return u.Host
}

// glHTTPClient refuses redirects so the PRIVATE-TOKEN header can never be replayed to another host
// on a 3xx (Go does not strip a custom header on a cross-host redirect).
var glHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the PRIVATE-TOKEN header must not leave the configured host)", req.URL.Host)
	},
}

// glDo performs a GET against base+path and returns the raw body, the X-Next-Page header (empty on
// the last page), and any error. A package var so tests can fake it. Auth via the PRIVATE-TOKEN
// header (a Personal/Project/Group access token); the token rides ONLY on that header, never in the
// URL, errors, or logs. Config enumeration is entirely GET, so there is no request body.
var glDo = func(ctx context.Context, method, path string) ([]byte, string, error) {
	base := glBase()
	if base == "" {
		return nil, "", &gitlabAPIError{msg: "GITLAB_BASE_URL is malformed (must be an http/https URL)"}
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return nil, "", &gitlabAPIError{msg: err.Error()}
	}
	req.Header.Set("PRIVATE-TOKEN", os.Getenv("GITLAB_TOKEN"))
	req.Header.Set("Accept", "application/json")

	resp, err := glHTTPClient.Do(req)
	if err != nil {
		return nil, "", &gitlabAPIError{msg: fmt.Sprintf("gitlab %s: %v", redactURL(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", &gitlabAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("gitlab %s: read body: %v", redactURL(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, "", &gitlabAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("gitlab %s: HTTP %d: %s", redactURL(path), resp.StatusCode, glErrMsg(body))}
	}
	return body, resp.Header.Get("X-Next-Page"), nil
}

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

// glErrMsg reads GitLab's error envelope. `message` may be a string, an object, or an array
// (validation errors); `error` is the OAuth-style string form. Never echoes the request.
func glErrMsg(body []byte) string {
	var e struct {
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil {
		if len(e.Message) > 0 {
			var s string
			if json.Unmarshal(e.Message, &s) == nil && s != "" {
				return s
			}
			return string(e.Message) // object/array — return its JSON text
		}
		if e.Error != "" {
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

// glList paginates a GitLab collection with ?page=&per_page=100, following the X-Next-Page response
// header until it is empty. per_page maxes at 100. Keyset pagination (for >10k collections) is not
// needed at Phase A.
func glList[T any](ctx context.Context, path string) ([]T, error) {
	var all []T
	page := "1"
	for i := 0; ; i++ {
		if i >= glMaxPages {
			return all, &gitlabAPIError{msg: fmt.Sprintf("gitlab %s: pagination exceeded %d pages", redactURL(path), glMaxPages)}
		}
		url := fmt.Sprintf("%s%sper_page=%d&page=%s", path, sep(path), glPerPage, page)
		body, next, err := glDo(ctx, http.MethodGet, url)
		if err != nil {
			return all, err
		}
		var items []T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &items); err != nil {
				return all, &gitlabAPIError{msg: fmt.Sprintf("gitlab %s: decode: %v", redactURL(path), err)}
			}
		}
		all = append(all, items...)
		if next == "" || next == page {
			return all, nil
		}
		page = next
	}
}

// glGet performs a single (non-paged) GET and decodes the JSON object into v.
func glGet(ctx context.Context, path string, v any) error {
	body, _, err := glDo(ctx, http.MethodGet, path)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return &gitlabAPIError{msg: fmt.Sprintf("gitlab %s: decode: %v", redactURL(path), err)}
	}
	return nil
}
