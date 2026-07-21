package ns1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const ns1BaseURL = "https://api.nsone.net/v1"

// ns1APIError carries the HTTP status so callers distinguish an absent feature/
// permission (403/404 → best-effort skip) from a transient/real failure (401/429/5xx
// → surface loudly). Status is 0 for pre-response (transport) errors.
type ns1APIError struct {
	Status int
	msg    string
}

func (e *ns1APIError) Error() string { return e.msg }

// ns1Do performs a request and returns the raw body + status. A package var so tests
// can fake it. NS1 authenticates with a custom `X-NSONE-Key` header (NOT Authorization:
// Bearer); the token is only ever on that header, never in errors or logs.
var ns1Do = func(ctx context.Context, method, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, ns1BaseURL+path, nil)
	if err != nil {
		return nil, 0, &ns1APIError{msg: err.Error()}
	}
	req.Header.Set("X-NSONE-Key", os.Getenv("NS1_APIKEY"))
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, &ns1APIError{msg: fmt.Sprintf("ns1 %s: %v", path, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &ns1APIError{Status: resp.StatusCode, msg: fmt.Sprintf("ns1 %s: read body: %v", path, err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &ns1APIError{Status: resp.StatusCode, msg: fmt.Sprintf("ns1 %s: HTTP %d: %s", path, resp.StatusCode, ns1ErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func ns1ErrMsg(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return "request failed"
}

// ns1List fetches a bare-array endpoint (NS1 lists are unpaginated bare arrays) into []T.
func ns1List[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := ns1Do(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	var items []T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, &ns1APIError{msg: fmt.Sprintf("ns1 %s: decode: %v", path, err)}
		}
	}
	return items, nil
}

// ns1GetOne fetches a bare-object endpoint into T.
func ns1GetOne[T any](ctx context.Context, path string) (T, error) {
	var out T
	body, _, err := ns1Do(ctx, http.MethodGet, path)
	if err != nil {
		return out, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return out, &ns1APIError{msg: fmt.Sprintf("ns1 %s: decode: %v", path, err)}
		}
	}
	return out, nil
}
