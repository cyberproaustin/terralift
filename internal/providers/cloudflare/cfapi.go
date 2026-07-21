package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const cfBaseURL = "https://api.cloudflare.com/client/v4"

// cfEnvelope is the wrapper Cloudflare returns for every endpoint.
type cfEnvelope struct {
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		PerPage    int `json:"per_page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// cfAPIError carries the HTTP status so callers can distinguish an absent
// feature/permission (403/404 → best-effort skip) from a transient/real failure
// (429/5xx/network → surface loudly). Status is 0 for pre-response (transport) errors.
type cfAPIError struct {
	Status int
	msg    string
}

func (e *cfAPIError) Error() string { return e.msg }

// cfDo performs a Cloudflare API request (path is joined onto the v4 base URL) and
// returns the decoded envelope. It is a package var (not a plain func) so tests can
// substitute a fake API; do not call it concurrently with a test that overrides it.
// The token is only ever set on the Authorization header — never in errors or logs.
var cfDo = func(ctx context.Context, method, path string) (cfEnvelope, error) {
	var env cfEnvelope
	req, err := http.NewRequestWithContext(ctx, method, cfBaseURL+path, nil)
	if err != nil {
		return env, &cfAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("CLOUDFLARE_API_TOKEN"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return env, &cfAPIError{msg: fmt.Sprintf("cloudflare %s %s: %v", method, path, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return env, &cfAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("cloudflare %s %s: read body: %v", method, path, err)}
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return env, &cfAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("cloudflare %s %s: decode response (HTTP %d): %v", method, path, resp.StatusCode, err)}
	}
	if !env.Success {
		return env, &cfAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("cloudflare %s %s: %s", method, path, cfErr(env))}
	}
	return env, nil
}

// cfErr renders the API's error array (or the raw status) into a message.
func cfErr(env cfEnvelope) string {
	if len(env.Errors) > 0 {
		return fmt.Sprintf("%s (code %d)", env.Errors[0].Message, env.Errors[0].Code)
	}
	return "request unsuccessful"
}

// cfGetOne fetches a single-object endpoint and unmarshals result into T.
func cfGetOne[T any](ctx context.Context, path string) (T, error) {
	var out T
	env, err := cfDo(ctx, http.MethodGet, path)
	if err != nil {
		return out, err
	}
	if len(env.Result) == 0 {
		return out, nil
	}
	return out, json.Unmarshal(env.Result, &out)
}

// cfMaxPages bounds pagination defensively — real endpoints terminate via
// total_pages, but a misreported/adversarial total_pages (always page+1) would
// otherwise drive unbounded requests.
const cfMaxPages = 10000

// cfList fetches every page of a list endpoint and flattens result into []T. It
// appends page/per_page params and loops while result_info.TotalPages exceeds the
// current page (TotalPages==0 means the endpoint is not paginated → one page).
func cfList[T any](ctx context.Context, path string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		if page > cfMaxPages {
			return nil, &cfAPIError{msg: fmt.Sprintf("cloudflare %s: pagination exceeded %d pages", path, cfMaxPages)}
		}
		env, err := cfDo(ctx, http.MethodGet, withPage(path, page))
		if err != nil {
			return nil, err
		}
		if len(env.Result) > 0 {
			var items []T
			if err := json.Unmarshal(env.Result, &items); err != nil {
				return nil, fmt.Errorf("decode %s page %d: %w", path, page, err)
			}
			all = append(all, items...)
		}
		if env.ResultInfo.TotalPages <= page { // 0 (unpaginated) or last page reached
			return all, nil
		}
	}
}

// withPage appends page + per_page query params, choosing ? or & correctly.
func withPage(path string, page int) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%spage=%d&per_page=50", path, sep, page)
}
