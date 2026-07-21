package newrelic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	nrBaseUS     = "https://api.newrelic.com/graphql"
	nrBaseEU     = "https://api.eu.newrelic.com/graphql"
	nrMaxPages   = 10000
	nrMaxRetries = 4
)

// nerdgraphError carries the failure taxonomy for NerdGraph. Two axes: Status is the HTTP
// status (>=400) for a transport/HTTP-layer failure (e.g. a bad key → 401/403), and
// ErrorClass is the GraphQL errors[].extensions.errorClass for a 200-with-errors response
// (e.g. UNAUTHORIZED/FORBIDDEN → the product/permission is absent). Callers classify on
// these to tell a fatal auth failure from a best-effort per-product skip.
type nerdgraphError struct {
	Status     int
	ErrorClass string
	msg        string
}

func (e *nerdgraphError) Error() string { return e.msg }

// nrBase resolves the region endpoint. NerdGraph has exactly two data-center bases; the
// region is read from NEW_RELIC_REGION (US default, EU). There is only ever this one URL —
// the probe, every list, and every cursor follow all POST here.
func nrBase() string {
	if strings.EqualFold(os.Getenv("NEW_RELIC_REGION"), "EU") {
		return nrBaseEU
	}
	return nrBaseUS
}

// nrAccountID parses NEW_RELIC_ACCOUNT_ID as an int (NerdGraph wants an Int, not a string).
func nrAccountID() (int, error) {
	raw := os.Getenv("NEW_RELIC_ACCOUNT_ID")
	if raw == "" {
		return 0, &nerdgraphError{msg: "NEW_RELIC_ACCOUNT_ID is not set"}
	}
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, &nerdgraphError{msg: "NEW_RELIC_ACCOUNT_ID is not a valid integer"}
	}
	return id, nil
}

// nrHTTPClient refuses to follow redirects so the API-Key header can never be replayed to
// another host (Go does not strip custom headers on a cross-host 3xx). NerdGraph answers
// 200 directly, so a redirect is treated as a hard error.
var nrHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the API-Key header must not leave the configured host)", req.URL.Host)
	},
}

// nerdgraph POSTs a GraphQL query + variables and returns the raw `data` tree, with bounded
// exponential backoff on NerdGraph's aggressive rate limiting (429) and transient 5xx/
// TIMEOUT/SERVER_ERROR. A package var so tests substitute a fake (the fake bypasses the
// retry loop, which is fine — the loop is not the thing under test).
var nerdgraph = func(ctx context.Context, query string, vars map[string]any) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		data, err := nerdgraphOnce(ctx, query, vars)
		if err == nil {
			return data, nil
		}
		var ngErr *nerdgraphError
		if attempt < nrMaxRetries && errors.As(err, &ngErr) && nrRetryable(ngErr) {
			if berr := nrBackoff(ctx, attempt); berr != nil {
				return nil, berr
			}
			continue
		}
		return nil, err
	}
}

// nrRetryable reports whether an error is worth a bounded retry: HTTP 429 / 5xx, or a
// GraphQL TIMEOUT/SERVER_ERROR errorClass. An auth failure (401/403, UNAUTHORIZED/FORBIDDEN)
// is NOT retried — it will not resolve on its own.
func nrRetryable(e *nerdgraphError) bool {
	if e.Status == 429 || e.Status >= 500 {
		return true
	}
	switch e.ErrorClass {
	case "TIMEOUT", "SERVER_ERROR", "INTERNAL_SERVER_ERROR":
		return true
	}
	return false
}

// nrBackoff sleeps an exponentially growing, capped interval, aborting early if the context
// is cancelled.
func nrBackoff(ctx context.Context, attempt int) error {
	d := time.Duration(200*(1<<attempt)) * time.Millisecond // 200, 400, 800, 1600 ms
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// nerdgraphOnce is a single POST of a GraphQL query + variables to the region endpoint. The
// single most important rule: a 200 with a non-empty `errors` array is a FAILURE — NerdGraph
// answers 200 for query-level and even partial errors, so `data` is never trusted when
// `errors` is present. The NEW_RELIC_API_KEY rides ONLY on the API-Key header, never in the
// URL, body, error, or log (the query text is static GraphQL and the variables carry no
// secret).
func nerdgraphOnce(ctx context.Context, query string, vars map[string]any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, &nerdgraphError{msg: "encode request: " + err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nrBase(), bytes.NewReader(payload))
	if err != nil {
		return nil, &nerdgraphError{msg: err.Error()}
	}
	req.Header.Set("API-Key", os.Getenv("NEW_RELIC_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := nrHTTPClient.Do(req)
	if err != nil {
		return nil, &nerdgraphError{msg: fmt.Sprintf("nerdgraph %s: %v", nrBase(), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &nerdgraphError{Status: resp.StatusCode, msg: fmt.Sprintf("nerdgraph: read body: %v", err)}
	}
	if resp.StatusCode >= 400 {
		return nil, &nerdgraphError{Status: resp.StatusCode, msg: fmt.Sprintf("nerdgraph: HTTP %d: %s", resp.StatusCode, snippet(body))}
	}

	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				ErrorClass string `json:"errorClass"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, &nerdgraphError{Status: resp.StatusCode, msg: "nerdgraph: decode: " + err.Error()}
	}
	if len(env.Errors) > 0 {
		e := env.Errors[0]
		return nil, &nerdgraphError{
			ErrorClass: e.Extensions.ErrorClass,
			msg:        fmt.Sprintf("nerdgraph error (%s): %s", nonEmpty(e.Extensions.ErrorClass, "GRAPHQL_ERROR"), e.Message),
		}
	}
	return env.Data, nil
}

// nrPaged runs a NerdGraph cursor-paginated query. The cursor is echoed back to the one
// fixed endpoint (never a server-supplied follow-URL), so the auth header never travels to
// another host. extract pulls the page's items + the nextCursor from wherever they live in
// the response tree (the depth differs per query). Terminates on an empty nextCursor.
func nrPaged[T any](ctx context.Context, query string, baseVars map[string]any, extract func(json.RawMessage) ([]T, string, error)) ([]T, error) {
	var all []T
	var cursor any // nil on the first call → GraphQL null → first page
	for i := 0; ; i++ {
		if i >= nrMaxPages {
			return nil, &nerdgraphError{msg: "nerdgraph: pagination exceeded max pages"}
		}
		vars := make(map[string]any, len(baseVars)+1)
		for k, v := range baseVars {
			vars[k] = v
		}
		vars["cursor"] = cursor
		data, err := nerdgraph(ctx, query, vars)
		if err != nil {
			return nil, err
		}
		items, next, err := extract(data)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		cursor = next
	}
}

// nrOnce runs a single (unpaged) NerdGraph query and returns the raw data tree.
func nrOnce(ctx context.Context, query string, vars map[string]any) (json.RawMessage, error) {
	return nerdgraph(ctx, query, vars)
}

func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	if s == "" {
		return "request failed"
	}
	return s
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
