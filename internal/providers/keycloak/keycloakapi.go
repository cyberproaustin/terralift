package keycloak

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
	kcMaxPages = 100000
	kcPageSize = 100
)

// keycloakAPIError carries the HTTP status so callers distinguish an absent role/feature
// (403/404 → best-effort skip) from an auth failure (401 → refresh + retry) or a transient
// error (429/5xx → Warn). Status is 0 for pre-response (transport) errors.
type keycloakAPIError struct {
	Status int
	msg    string
}

func (e *keycloakAPIError) Error() string { return e.msg }

// kcAccessToken is the short-lived Admin API Bearer minted by the token exchange. Keycloak
// tokens are often only ~60s, so kcDo refreshes it on a mid-run 401.
var kcAccessToken string

// kcBase resolves the server base URL from KEYCLOAK_URL (a full URL, e.g.
// https://sso.example.com) plus KEYCLOAK_BASE_PATH (/auth on legacy Wildfly, empty on Quarkus
// 17+). https is preferred but http is allowed (self-hosted dev — preflight warns on http to a
// non-loopback host). An '@'/userinfo splice is rejected. Empty if malformed.
func kcBase() string {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("KEYCLOAK_URL")), "/")
	if raw == "" {
		return ""
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	base := u.Scheme + "://" + u.Host
	// base_path may come from KEYCLOAK_BASE_PATH, or (fallback) a path in KEYCLOAK_URL itself
	// (e.g. https://sso.example.com/auth) — don't silently drop it.
	bp := strings.TrimSpace(os.Getenv("KEYCLOAK_BASE_PATH"))
	if bp == "" {
		bp = u.Path
	}
	bp = "/" + strings.Trim(bp, "/")
	if bp == "/" {
		bp = ""
	}
	return base + bp
}

// kcHost returns the server host, derived from the ALREADY-VALIDATED base (so a rejected
// userinfo-splice URL yields "" here too, not the attacker host).
func kcHost() string {
	u, err := neturl.Parse(kcBase())
	if err != nil {
		return ""
	}
	return u.Host
}

func kcAuthRealm() string {
	if r := strings.TrimSpace(os.Getenv("KEYCLOAK_REALM")); r != "" {
		return r
	}
	return "master"
}

// kcHTTPClient refuses redirects so neither the form-body secret nor the Bearer can be replayed
// to another host on a 3xx.
var kcHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (auth secrets must not leave the configured host)", req.URL.Host)
	},
}

// kcExchange performs the OAuth2 token exchange against the auth realm's token endpoint with a
// FORM-encoded body (NOT JSON). Two grant modes: client_credentials (a service-account client)
// preferred when KEYCLOAK_CLIENT_ID+SECRET are set, else the password grant via admin-cli. The
// POST is UNAUTHENTICATED; the client_secret/password ride ONLY in the form body, never in the
// URL, an error, or a log (the error carries only the server's error text). A package var so
// tests can fake it.
var kcExchange = func(ctx context.Context) (string, error) {
	form := neturl.Values{}
	if os.Getenv("KEYCLOAK_CLIENT_ID") != "" && os.Getenv("KEYCLOAK_CLIENT_SECRET") != "" {
		form.Set("grant_type", "client_credentials")
		form.Set("client_id", os.Getenv("KEYCLOAK_CLIENT_ID"))
		form.Set("client_secret", os.Getenv("KEYCLOAK_CLIENT_SECRET"))
	} else {
		form.Set("grant_type", "password")
		form.Set("client_id", "admin-cli")
		form.Set("username", os.Getenv("KEYCLOAK_USER"))
		form.Set("password", os.Getenv("KEYCLOAK_PASSWORD"))
	}
	tokenURL := kcBase() + "/realms/" + neturl.PathEscape(kcAuthRealm()) + "/protocol/openid-connect/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", &keycloakAPIError{msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := kcHTTPClient.Do(req)
	if err != nil {
		return "", &keycloakAPIError{msg: fmt.Sprintf("keycloak /token: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", &keycloakAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("keycloak token exchange failed: HTTP %d: %s", resp.StatusCode, kcErrMsg(body))}
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", &keycloakAPIError{Status: resp.StatusCode, msg: "keycloak token exchange returned no access_token"}
	}
	return tok.AccessToken, nil
}

// refreshToken re-mints the Bearer (Keycloak access tokens are short-lived; a multi-realm
// enumeration outlives one).
func refreshToken(ctx context.Context) error {
	tok, err := kcExchange(ctx)
	if err != nil {
		return err
	}
	kcAccessToken = tok
	return nil
}

// kcDo performs an Admin API GET against base+path. A package var so tests can fake it. On a
// mid-run 401 (the short-lived token expired) it re-mints once and retries. The access_token
// rides ONLY on the Authorization header, never in the URL, errors, or logs.
var kcDo = func(ctx context.Context, method, path string) ([]byte, int, error) {
	body, status, err := kcDoOnce(ctx, method, path)
	if status == 401 {
		if rerr := refreshToken(ctx); rerr == nil {
			return kcDoOnce(ctx, method, path)
		}
	}
	return body, status, err
}

func kcDoOnce(ctx context.Context, method, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, kcBase()+path, nil)
	if err != nil {
		return nil, 0, &keycloakAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+kcAccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := kcHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &keycloakAPIError{msg: fmt.Sprintf("keycloak %s: %v", redactURL(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &keycloakAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("keycloak %s: read body: %v", redactURL(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &keycloakAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("keycloak %s: HTTP %d: %s", redactURL(path), resp.StatusCode, kcErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func kcErrMsg(body []byte) string {
	var e struct {
		ErrorMessage     string `json:"errorMessage"`
		ErrorDescription string `json:"error_description"`
		Error            string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil {
		switch {
		case e.ErrorMessage != "":
			return e.ErrorMessage
		case e.ErrorDescription != "":
			return e.ErrorDescription
		case e.Error != "":
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

// kcGet fetches a bare-array endpoint (unpaged: realms, client-scopes, flows, idps, components,
// required-actions) into []T.
func kcGet[T any](ctx context.Context, path string) ([]T, error) {
	body, _, err := kcDo(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	var items []T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, &keycloakAPIError{msg: fmt.Sprintf("keycloak %s: decode: %v", redactURL(path), err)}
		}
	}
	return items, nil
}

// kcList paginates a bare-array endpoint with ?first=&max= (offset/limit), stopping on a short/
// empty page. Used for the potentially-large lists (clients, roles, groups).
func kcList[T any](ctx context.Context, path string) ([]T, error) {
	var all []T
	for first := 0; ; first += kcPageSize {
		if first/kcPageSize >= kcMaxPages {
			return all, &keycloakAPIError{msg: fmt.Sprintf("keycloak %s: pagination exceeded %d pages", redactURL(path), kcMaxPages)}
		}
		url := fmt.Sprintf("%s%sfirst=%d&max=%d", path, sep(path), first, kcPageSize)
		body, _, err := kcDo(ctx, http.MethodGet, url)
		if err != nil {
			return all, err
		}
		var items []T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &items); err != nil {
				return all, &keycloakAPIError{msg: fmt.Sprintf("keycloak %s: decode: %v", redactURL(path), err)}
			}
		}
		all = append(all, items...)
		if len(items) < kcPageSize {
			return all, nil
		}
	}
}
