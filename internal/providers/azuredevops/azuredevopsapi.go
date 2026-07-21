package azuredevops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
)

const (
	azMaxPages = 10000
	// API versions. Azure DevOps requires an api-version on EVERY request; the distributedtask
	// (variable groups) and graph (groups) planes are still preview. VERIFY exact minor at Phase B.
	apiV         = "7.1"
	apiVPreview1 = "7.1-preview.1"
	apiVPreview2 = "7.1-preview.2"
)

// azdoAPIError carries the HTTP status so callers distinguish an absent/forbidden resource
// (404 → skip) from a fatal auth failure (401, or the 203/HTML sign-in gotcha normalized to 401)
// or a transient error (429/5xx → Warn). Status is 0 for pre-response (transport) errors.
type azdoAPIError struct {
	Status int
	msg    string
}

func (e *azdoAPIError) Error() string { return e.msg }

// azPAT reads the Personal Access Token. It rides ONLY on the Basic Authorization header (via
// basicAuth), never in a URL, error, log, body, config, or state.
func azPAT() string { return strings.TrimSpace(os.Getenv("AZDO_PERSONAL_ACCESS_TOKEN")) }

// basicAuth builds the HTTP Basic credential for a PAT: base64(":"+PAT) — an EMPTY username and the
// PAT as the password, per Azure DevOps's documented scheme.
func basicAuth() string {
	return base64.StdEncoding.EncodeToString([]byte(":" + azPAT()))
}

// azOrgURL resolves the organization base URL from AZDO_ORG_SERVICE_URL (required, no default —
// e.g. https://dev.azure.com/myorg). The scheme is respected (Azure DevOps Server on-prem may be
// http); an '@'/userinfo splice is rejected; empty on a malformed/absent value.
func azOrgURL() string {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("AZDO_ORG_SERVICE_URL")), "/")
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/")
}

// azGraphURL derives the graph/identity host (vssps.dev.azure.com) from the org URL, keeping the
// /<org> path. Only the standard dev.azure.com host is handled; other hosts (legacy
// *.visualstudio.com, on-prem) return "" so the groups enumeration skips rather than hitting a
// wrong host (VERIFY at Phase B).
func azGraphURL() string {
	u, err := neturl.Parse(azOrgURL())
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Host != "dev.azure.com" {
		return ""
	}
	return u.Scheme + "://vssps.dev.azure.com" + strings.TrimRight(u.Path, "/")
}

// azOrgName returns the <org> path segment (the scope identity).
func azOrgName() string {
	u, err := neturl.Parse(azOrgURL())
	if err != nil {
		return ""
	}
	return strings.Trim(u.Path, "/")
}

// azHost returns the org host (for the cleartext-http preflight check), from the validated base.
func azHost() string {
	u, err := neturl.Parse(azOrgURL())
	if err != nil {
		return ""
	}
	return u.Host
}

// azHTTPClient refuses redirects so the Basic (PAT) Authorization header can never be replayed to
// another host on a 3xx (Go does not strip the header on a cross-host redirect).
var azHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the PAT Authorization header must not leave the configured host)", req.URL.Host)
	},
}

// azDo performs a GET against a fully-composed URL and returns the raw body, the
// x-ms-continuationtoken response header (empty on the last page), and any error. A package var so
// tests can fake it. Auth via the Basic Authorization header; the PAT rides ONLY there. It also
// normalizes the Azure DevOps bad-PAT gotcha: an expired/under-scoped PAT yields a 203
// Non-Authoritative response with a text/html sign-in page (NOT a 401), so a 203 or an HTML
// content-type is treated as a 401 auth failure.
var azDo = func(ctx context.Context, method, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, "", &azdoAPIError{msg: err.Error()}
	}
	req.Header.Set("Authorization", "Basic "+basicAuth())
	req.Header.Set("Accept", "application/json")

	resp, err := azHTTPClient.Do(req)
	if err != nil {
		return nil, "", &azdoAPIError{msg: fmt.Sprintf("azuredevops %s: %v", redactURL(url), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", &azdoAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("azuredevops %s: read body: %v", redactURL(url), err)}
	}
	if resp.StatusCode == http.StatusNonAuthoritativeInfo || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return body, "", &azdoAPIError{Status: 401, msg: fmt.Sprintf("azuredevops %s: authentication failed (sign-in page returned — check AZDO_PERSONAL_ACCESS_TOKEN scope/expiry)", redactURL(url))}
	}
	if resp.StatusCode >= 400 {
		return body, "", &azdoAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("azuredevops %s: HTTP %d: %s", redactURL(url), resp.StatusCode, azErrMsg(body))}
	}
	return body, resp.Header.Get("x-ms-continuationtoken"), nil
}

// redactURL strips the query string (which carries api-version/continuationToken — never the PAT,
// but keep errors terse) so only the path is surfaced.
func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// azErrMsg reads Azure DevOps's error envelope {"message":...,"typeKey":...} — never echoes the
// request.
func azErrMsg(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return "request failed"
}

// azList paginates an Azure DevOps collection. Each response is the VSTS envelope
// {"count":N,"value":[...]}; more data is signaled ONLY by the x-ms-continuationtoken response
// header (fed back as &continuationToken=). api-version is appended to every request.
func azList[T any](ctx context.Context, baseURL, path, apiVersion string) ([]T, error) {
	if baseURL == "" {
		return nil, &azdoAPIError{msg: "azuredevops: base URL is empty"}
	}
	var all []T
	cont := ""
	for i := 0; ; i++ {
		if i >= azMaxPages {
			return all, &azdoAPIError{msg: fmt.Sprintf("azuredevops %s: pagination exceeded %d pages", path, azMaxPages)}
		}
		url := baseURL + path + "?api-version=" + apiVersion
		if cont != "" {
			url += "&continuationToken=" + neturl.QueryEscape(cont)
		}
		body, next, err := azDo(ctx, http.MethodGet, url)
		if err != nil {
			return all, err
		}
		var env struct {
			Value []T `json:"value"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &env); err != nil {
				return all, &azdoAPIError{msg: fmt.Sprintf("azuredevops %s: decode: %v", path, err)}
			}
		}
		all = append(all, env.Value...)
		if next == "" || next == cont {
			return all, nil
		}
		cont = next
	}
}
