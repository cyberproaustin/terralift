package azuread

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
	adMaxPages   = 10000
	adGraphBase  = "https://graph.microsoft.com/v1.0"
	adLoginBase  = "https://login.microsoftonline.com"
	adGraphScope = "https://graph.microsoft.com/.default"
)

// azureadAPIError carries the HTTP status so callers distinguish an absent/forbidden resource
// (403/404 → best-effort skip — Graph is permission-scoped, so 403s are common) from a fatal auth
// failure (401, after a token refresh) or a transient error (429/5xx → Warn). Status is 0 for
// transport errors.
type azureadAPIError struct {
	Status int
	msg    string
}

func (e *azureadAPIError) Error() string { return e.msg }

// adAccessToken is the short-lived Graph Bearer minted by the client-credentials exchange. Graph
// tokens last ~60-90 min, so adDo refreshes it on a mid-run 401.
var adAccessToken string

func adTenant() string       { return strings.TrimSpace(os.Getenv("ARM_TENANT_ID")) }
func adClientID() string     { return strings.TrimSpace(os.Getenv("ARM_CLIENT_ID")) }
func adClientSecret() string { return strings.TrimSpace(os.Getenv("ARM_CLIENT_SECRET")) }

// graphHost is the Graph host that a server-supplied @odata.nextLink must stay on before the Bearer
// is re-sent to it (token-exfil guard).
func graphHost() string {
	u, err := neturl.Parse(adGraphBase)
	if err != nil {
		return ""
	}
	return u.Host
}

// adHTTPClient refuses redirects so neither the client_secret (in the token-exchange form body) nor
// the Bearer can be replayed to another host on a 3xx (Go does not strip a custom header / re-POST
// body safely across hosts).
var adHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (auth credentials must not leave the configured host)", req.URL.Host)
	},
}

// adExchange performs the OAuth2 client-credentials token exchange against the tenant's token
// endpoint with a FORM-encoded body. The client_secret rides ONLY in that body — never in a URL,
// an error, or a log. A package var so tests can fake it.
var adExchange = func(ctx context.Context) (string, error) {
	form := neturl.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", adClientID())
	form.Set("client_secret", adClientSecret())
	form.Set("scope", adGraphScope)

	tokenURL := adLoginBase + "/" + neturl.PathEscape(adTenant()) + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", &azureadAPIError{msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := adHTTPClient.Do(req)
	if err != nil {
		return "", &azureadAPIError{msg: fmt.Sprintf("azuread /token: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", &azureadAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("azuread token exchange failed: HTTP %d: %s", resp.StatusCode, oauthErrMsg(body))}
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", &azureadAPIError{Status: resp.StatusCode, msg: "azuread token exchange returned no access_token"}
	}
	return tok.AccessToken, nil
}

// refreshToken re-mints the Bearer (Graph access tokens are short-lived; a multi-list enumeration
// can outlive one).
func refreshToken(ctx context.Context) error {
	tok, err := adExchange(ctx)
	if err != nil {
		return err
	}
	adAccessToken = tok
	return nil
}

// adDo performs a Graph request against a fully-composed URL. A package var so tests can fake it.
// On a mid-run 401 (the short-lived token expired) it re-mints once and retries. The Bearer rides
// ONLY on the Authorization header, never in the URL, errors, or logs.
var adDo = func(ctx context.Context, method, url string) ([]byte, error) {
	body, status, err := adDoOnce(ctx, method, url)
	if status == 401 {
		if rerr := refreshToken(ctx); rerr == nil {
			body, _, err = adDoOnce(ctx, method, url)
		}
	}
	return body, err
}

func adDoOnce(ctx context.Context, method, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, 0, &azureadAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+adAccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := adHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &azureadAPIError{msg: fmt.Sprintf("azuread %s: %v", redactURL(url), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &azureadAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("azuread %s: read body: %v", redactURL(url), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &azureadAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("azuread %s: HTTP %d: %s", redactURL(url), resp.StatusCode, adErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

// redactURL strips the query string (which may carry $skiptoken continuation state) so only the
// path is surfaced; the Bearer is never in the URL regardless.
func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	// Also collapse an absolute graph URL to its path for terse errors.
	if u, err := neturl.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}

// adErrMsg reads Graph's error envelope {"error":{"code","message"}} — never echoes the request.
func adErrMsg(body []byte) string {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Error.Message != "" {
			return e.Error.Message
		}
		if e.Error.Code != "" {
			return e.Error.Code
		}
	}
	return "request failed"
}

// oauthErrMsg reads the OAuth token-endpoint error {"error","error_description"} — the description
// is diagnostic (e.g. AADSTS codes) and carries no secret.
func oauthErrMsg(body []byte) string {
	var e struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.ErrorDescription != "" {
			// Trim to the first line — AADSTS descriptions are multi-line with trace ids.
			if i := strings.IndexByte(e.ErrorDescription, '\n'); i >= 0 {
				return e.ErrorDescription[:i]
			}
			return e.ErrorDescription
		}
		if e.Error != "" {
			return e.Error
		}
	}
	return "token exchange failed"
}

// isGraphURL host-validates a server-supplied @odata.nextLink: it must be https and stay on the
// Graph host before the Bearer is re-sent to it (the Fastly/Opsgenie next-link exfil lesson — a
// nextLink is not an HTTP redirect, so Go would send the Authorization header to whatever host it
// names).
func isGraphURL(raw string) bool {
	u, err := neturl.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}
	return u.Host == graphHost()
}

// gGraphList follows the OData collection envelope {"value":[...],"@odata.nextLink":"<abs-url>"},
// decoding .value and following the SERVER-SUPPLIED absolute nextLink after host-validating it.
func gGraphList[T any](ctx context.Context, path string) ([]T, error) {
	var all []T
	url := adGraphBase + path
	for i := 0; ; i++ {
		if i >= adMaxPages {
			return all, &azureadAPIError{msg: fmt.Sprintf("azuread %s: pagination exceeded %d pages", redactURL(url), adMaxPages)}
		}
		body, err := adDo(ctx, http.MethodGet, url)
		if err != nil {
			return all, err
		}
		var env struct {
			Value []T    `json:"value"`
			Next  string `json:"@odata.nextLink"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &env); err != nil {
				return all, &azureadAPIError{msg: fmt.Sprintf("azuread %s: decode: %v", redactURL(url), err)}
			}
		}
		all = append(all, env.Value...)
		if env.Next == "" {
			return all, nil
		}
		if !isGraphURL(env.Next) {
			return all, &azureadAPIError{msg: fmt.Sprintf("azuread %s: refusing to follow @odata.nextLink to a non-Graph host", redactURL(url))}
		}
		url = env.Next
	}
}
