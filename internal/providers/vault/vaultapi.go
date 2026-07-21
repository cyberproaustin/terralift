package vault

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

// vaultAPIError carries the HTTP status so callers distinguish an absent path/empty LIST
// (404 → skip), a permission-denied leaf (403 → skip, unless on the sys/* backbone) from a fatal
// auth failure (401) or a transient error (429/5xx → Warn). Status is 0 for transport errors.
type vaultAPIError struct {
	Status int
	msg    string
}

func (e *vaultAPIError) Error() string { return e.msg }

// vAddr resolves the Vault server base URL from VAULT_ADDR (default https://127.0.0.1:8200). Unlike
// the SaaS providers, the scheme is RESPECTED as given (Vault dev mode is http://127.0.0.1:8200) —
// a bare host defaults to https, but an explicit http:// is honored (preflight warns on http to a
// non-loopback host). An '@'/userinfo splice is rejected; empty on a malformed value. TLS is
// verified against the system trust store — the scaffold does NOT disable verification.
func vAddr() string {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("VAULT_ADDR")), "/")
	if raw == "" {
		return "https://127.0.0.1:8200"
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// vHost returns the server host, derived from the ALREADY-VALIDATED base (so a rejected
// userinfo-splice URL yields "" here too, not the attacker host).
func vHost() string {
	u, err := neturl.Parse(vAddr())
	if err != nil {
		return ""
	}
	return u.Host
}

// vHTTPClient refuses redirects so the X-Vault-Token header can never be replayed to another host
// on a 3xx (Vault issues 307 standby→active redirects; Go does not strip a custom header on a
// cross-host redirect). TLS is verified via the default transport's system trust store.
var vHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing to follow redirect to %s (the X-Vault-Token header must not leave the configured host)", req.URL.Host)
	},
}

// vDo performs a request against base+path and returns the raw body + status. A package var so
// tests can fake it. Auth via the X-Vault-Token header (+ optional X-Vault-Namespace); the token
// rides ONLY on that header, never in the URL, errors, or logs. Config enumeration is entirely GET
// (LIST is expressed as GET ...?list=true), so there is no request body.
var vDo = func(ctx context.Context, method, path string) ([]byte, int, error) {
	base := vAddr()
	if base == "" {
		return nil, 0, &vaultAPIError{msg: "VAULT_ADDR is malformed (must be an http/https URL)"}
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return nil, 0, &vaultAPIError{msg: err.Error()}
	}
	req.Header.Set("X-Vault-Token", os.Getenv("VAULT_TOKEN"))
	if ns := strings.TrimSpace(os.Getenv("VAULT_NAMESPACE")); ns != "" {
		req.Header.Set("X-Vault-Namespace", ns)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := vHTTPClient.Do(req)
	if err != nil {
		return nil, 0, &vaultAPIError{msg: fmt.Sprintf("vault %s: %v", redactURL(path), err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &vaultAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("vault %s: read body: %v", redactURL(path), err)}
	}
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, &vaultAPIError{Status: resp.StatusCode, msg: fmt.Sprintf("vault %s: HTTP %d: %s", redactURL(path), resp.StatusCode, vErrMsg(body))}
	}
	return body, resp.StatusCode, nil
}

func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

// vErrMsg reads Vault's error envelope {"errors":["..."]} — never echoes the request.
func vErrMsg(body []byte) string {
	var e struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 {
		return strings.Join(e.Errors, "; ")
	}
	return "request failed"
}

// vMount is one entry of a map-keyed sys/mounts, sys/auth, or sys/audit response. Only the type is
// decoded (it drives the role fan-out + the system-mount skip); NO secret config field is pulled.
type vMount struct {
	Type string `json:"type"`
}

// vGetMounts decodes a map-keyed response — sys/mounts, sys/auth, sys/audit return
// {"data":{"<path>/":{"type":...}}} where the KEYS are mount paths (trailing '/'). It prefers the
// "data" wrapper and falls back to the legacy top-level duplicate; either way it keeps ONLY keys
// ending in '/' (mount paths), which naturally excludes the envelope fields (request_id, lease_id,
// warnings, …) that share the top level.
func vGetMounts(ctx context.Context, path string) (map[string]vMount, error) {
	body, _, err := vDo(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	raw := map[string]json.RawMessage{}
	var wrap struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &wrap) == nil && len(wrap.Data) > 0 {
		raw = wrap.Data
	} else if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &vaultAPIError{msg: fmt.Sprintf("vault %s: decode: %v", redactURL(path), err)}
	}
	out := make(map[string]vMount, len(raw))
	for k, v := range raw {
		if !strings.HasSuffix(k, "/") {
			continue // envelope key (request_id/lease_id/…) or non-path — skip
		}
		var m vMount
		if json.Unmarshal(v, &m) == nil {
			out[k] = m
		}
	}
	return out, nil
}

// vList performs a Vault LIST (expressed as GET ...?list=true) and returns the child keys from the
// {"data":{"keys":[...]}} envelope. A trailing '/' on a key marks a sub-path (kept verbatim; the
// caller trims it). NB: an empty Vault LIST directory returns 404 — callers treat that as "no
// items", not an error.
func vList(ctx context.Context, path string) ([]string, error) {
	body, _, err := vDo(ctx, http.MethodGet, path+"?list=true")
	if err != nil {
		return nil, err
	}
	// Prefer the {"data":{"keys":[...]}} envelope; fall back to a top-level {"keys":[...]} for
	// symmetry with vGetMounts' tolerant decode (older/proxied LIST responses).
	var wrap struct {
		Keys []string `json:"keys"`
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, &vaultAPIError{msg: fmt.Sprintf("vault %s: decode: %v", redactURL(path), err)}
	}
	if len(wrap.Data.Keys) > 0 {
		return wrap.Data.Keys, nil
	}
	return wrap.Keys, nil
}
