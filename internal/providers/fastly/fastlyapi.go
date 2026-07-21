package fastly

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
	fastlyBaseURL  = "https://api.fastly.com"
	fastlyMaxPages = 10000
	fastlyPerPage  = 100
)

// fastlyAPIError carries the HTTP status so callers distinguish an absent feature/
// permission (403/404 → best-effort skip) from a transient/real failure (401/429/5xx
// → surface loudly). Status is 0 for pre-response (transport) errors.
type fastlyAPIError struct {
	Status int
	msg    string
}

func (e *fastlyAPIError) Error() string { return e.msg }

// fastlyDo performs a request against a FULL url and returns the raw body + status. A
// package var so tests can fake it. Fastly authenticates with a custom `Fastly-Key`
// header (NOT Authorization: Bearer); the token is only ever on that header, never in
// errors or logs.
var fastlyDo = func(ctx context.Context, method, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, 0, &fastlyAPIError{msg: err.Error()}
	}
	req.Header.Set("Fastly-Key", os.Getenv("FASTLY_API_KEY"))
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: %v", url, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &fastlyAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("fastly %s: read body: %v", url, err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &fastlyAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("fastly %s: HTTP %d: %s", url, resp.StatusCode, fastlyErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func fastlyErrMsg(body []byte) string {
	var e struct {
		Detail string `json:"detail"`
		Msg    string `json:"msg"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Detail != "" {
			return e.Detail
		}
		if e.Msg != "" {
			return e.Msg
		}
	}
	return "request failed"
}

// fastlyGet fetches a single bare-array endpoint (no pagination) and unmarshals into []T.
func fastlyGet[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := fastlyDo(ctx, http.MethodGet, fastlyBaseURL+path)
	if err != nil {
		return nil, err
	}
	var items []T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: decode: %v", path, err)}
		}
	}
	return items, nil
}

// fastlyGetOne fetches a bare-object singleton and unmarshals into T.
func fastlyGetOne[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := fastlyDo(ctx, http.MethodGet, fastlyBaseURL+path)
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: decode: %v", path, err)}
		}
	}
	return out, nil
}

// fastlyListPaged paginates a bare-array core endpoint (?page=&per_page=), stopping on
// a short/empty page. Used for /service.
func fastlyListPaged[T any](ctx context.Context, path string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		if page > fastlyMaxPages {
			return nil, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: pagination exceeded %d pages", path, fastlyMaxPages)}
		}
		url := fmt.Sprintf("%s%s%spage=%d&per_page=%d", fastlyBaseURL, path, sep(path), page, fastlyPerPage)
		body, _, err := fastlyDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var items []T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &items); err != nil {
				return nil, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: decode page %d: %v", path, page, err)}
			}
		}
		all = append(all, items...)
		if len(items) < fastlyPerPage {
			return all, nil
		}
	}
}

// fastlyListJSONAPI paginates a JSON:API endpoint ({data:[{id,...}], links.next}),
// following links.next until absent. The next url is host-validated before the
// Fastly-Key is sent (it is not an HTTP redirect, so Go does not strip the header).
func fastlyListJSONAPI[T any](ctx context.Context, path string) ([]T, error) {
	url := fmt.Sprintf("%s%s%spage[size]=%d", fastlyBaseURL, path, sep(path), fastlyPerPage)
	var all []T
	for i := 0; url != ""; i++ {
		if i >= fastlyMaxPages {
			return nil, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: pagination exceeded %d pages", path, fastlyMaxPages)}
		}
		body, _, err := fastlyDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var env struct {
			Data  json.RawMessage `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: decode: %v", path, err)}
		}
		if len(env.Data) > 0 {
			var items []T
			if err := json.Unmarshal(env.Data, &items); err != nil {
				return nil, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: decode data: %v", path, err)}
			}
			all = append(all, items...)
		}
		if env.Links.Next != "" && !isFastlyURL(env.Links.Next) {
			return nil, &fastlyAPIError{msg: fmt.Sprintf("fastly %s: refusing to follow next-page url to unexpected host: %s", path, env.Links.Next)}
		}
		url = env.Links.Next
	}
	return all, nil
}

func isFastlyURL(raw string) bool {
	u, err := neturl.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "api.fastly.com"
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}
