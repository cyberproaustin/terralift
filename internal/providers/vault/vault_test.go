package vault

import (
	"context"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: "srv", Properties: props}
}

func fakeVault(t *testing.T, fn func(method, path string) (string, int)) {
	t.Helper()
	orig := vDo
	t.Cleanup(func() { vDo = orig })
	vDo = func(_ context.Context, method, path string) ([]byte, int, error) {
		body, status := fn(method, path)
		if status >= 400 {
			return []byte(body), status, &vaultAPIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"mount path", res("vault_mount", map[string]any{"importID": "pki"}), "pki"},
		{"auth path", res("vault_auth_backend", map[string]any{"importID": "github"}), "github"},
		{"policy name", res("vault_policy", map[string]any{"importID": "dev-team"}), "dev-team"},
		{"pki role", res("vault_pki_secret_backend_role", map[string]any{"importID": "pki/roles/web"}), "pki/roles/web"},
		{"jwt role", res("vault_jwt_auth_backend_role", map[string]any{"importID": "auth/jwt/role/ci"}), "auth/jwt/role/ci"},
		{"token role", res("vault_token_auth_backend_role", map[string]any{"importID": "auth/token/roles/nomad"}), "auth/token/roles/nomad"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("vault_policy", map[string]any{"importID": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestVAddr(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")
	if got := vAddr(); got != "https://127.0.0.1:8200" {
		t.Errorf("default = %q, want https://127.0.0.1:8200", got)
	}
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:8200") // dev-mode http scheme respected
	if got := vAddr(); got != "http://127.0.0.1:8200" {
		t.Errorf("http respected = %q, want http://127.0.0.1:8200", got)
	}
	t.Setenv("VAULT_ADDR", "vault.example.com:8200") // bare host → https
	if got := vAddr(); got != "https://vault.example.com:8200" {
		t.Errorf("bare host = %q, want https promotion", got)
	}
	t.Setenv("VAULT_ADDR", "https://user:pw@evil.example.com") // userinfo splice rejected
	if got := vAddr(); got != "" {
		t.Errorf("userinfo splice should reject, got %q", got)
	}
}

// vGetMounts prefers the {"data":{...}} wrapper and keeps only trailing-slash mount-path keys,
// filtering the envelope fields that share the legacy top level.
func TestVGetMountsShape(t *testing.T) {
	fakeVault(t, func(method, path string) (string, int) {
		return `{"request_id":"abc","lease_id":"","data":{"pki/":{"type":"pki"},"secret/":{"type":"kv"},"sys/":{"type":"system"}}}`, 200
	})
	m, err := vGetMounts(context.Background(), "/v1/sys/mounts")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 || m["pki/"].Type != "pki" || m["secret/"].Type != "kv" {
		t.Fatalf("data-wrapped decode = %+v", m)
	}
	// Legacy top-level (no data wrapper) with envelope keys mixed in — filter by trailing slash.
	fakeVault(t, func(method, path string) (string, int) {
		return `{"request_id":"abc","lease_id":"","pki/":{"type":"pki"}}`, 200
	})
	m, err = vGetMounts(context.Background(), "/v1/sys/mounts")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["pki/"].Type != "pki" {
		t.Fatalf("top-level decode should keep only pki/, got %+v", m)
	}
}

func TestVListKeys(t *testing.T) {
	fakeVault(t, func(method, path string) (string, int) {
		if !strings.Contains(path, "list=true") {
			t.Errorf("LIST should append ?list=true, got %s", path)
		}
		return `{"data":{"keys":["dev-team","admins/"]}}`, 200
	})
	keys, err := vList(context.Background(), "/v1/sys/policies/acl")
	if err != nil || len(keys) != 2 || keys[0] != "dev-team" {
		t.Fatalf("list keys = %+v err %v", keys, err)
	}
}

func TestConnectResolvesCluster(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.example.com:8200")
	fakeVault(t, func(method, path string) (string, int) {
		switch path {
		case "/v1/auth/token/lookup-self":
			return `{"data":{"display_name":"root"}}`, 200
		case "/v1/sys/health":
			return `{"cluster_name":"vault-prod"}`, 200
		}
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should validate via lookup-self, got %v", err)
	}
	if run.Scope.ID != "vault-prod" || ac.Identity != "vault-prod" {
		t.Errorf("scope/identity = %q/%q, want vault-prod", run.Scope.ID, ac.Identity)
	}
}

// A restricted token (lookup-self OK) whose sys/health is unreadable falls back to the host.
func TestConnectFallsBackToHost(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.example.com:8200")
	fakeVault(t, func(method, path string) (string, int) {
		if path == "/v1/auth/token/lookup-self" {
			return `{"data":{}}`, 200
		}
		return `{"errors":["permission denied"]}`, 403
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should succeed, got %v", err)
	}
	if run.Scope.ID != "vault.example.com:8200" {
		t.Errorf("scope id = %q, want the host fallback", run.Scope.ID)
	}
}

// The 403 taxonomy: on the sys/* backbone (core=true) a 403 is counted (systemic), on a soft list
// it is a quiet skip; 401 is always fatal; 404 is always a skip.
func TestListTaxonomy(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "audit devices", false, func() error { return &vaultAPIError{Status: 403, msg: "denied"} })
	if fails != 0 || fatal != nil {
		t.Errorf("403 on a soft list should skip; fails=%d fatal=%v", fails, fatal)
	}
	list(run, &fails, &fatal, "secret mounts", true, func() error { return &vaultAPIError{Status: 403, msg: "denied"} })
	if fails != 1 {
		t.Errorf("403 on the core backbone should count; fails=%d", fails)
	}
	list(run, &fails, &fatal, "namespaces", false, func() error { return &vaultAPIError{Status: 404, msg: "unsupported"} })
	if fails != 1 {
		t.Errorf("404 should be a skip, not counted; fails=%d", fails)
	}
	list(run, &fails, &fatal, "secret mounts", true, func() error { return &vaultAPIError{Status: 401, msg: "bad token"} })
	if fatal == nil {
		t.Error("401 should be fatal")
	}
}

// End-to-end: map-keyed mounts/auth with system-mount skips, LIST-keys policies, and the
// type-discriminated role fan-out (pki secret role + jwt/token auth roles).
func TestEnumerateMixedShapes(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.example.com:8200")
	fakeVault(t, func(method, path string) (string, int) {
		switch {
		case path == "/v1/sys/mounts":
			return `{"data":{"pki/":{"type":"pki"},"secret/":{"type":"kv"},"sys/":{"type":"system"},"identity/":{"type":"identity"}}}`, 200
		case path == "/v1/sys/auth":
			return `{"data":{"jwt/":{"type":"jwt"},"token/":{"type":"token"},"userpass/":{"type":"userpass"}}}`, 200
		case strings.HasPrefix(path, "/v1/sys/policies/acl"):
			return `{"data":{"keys":["root","default","dev-team"]}}`, 200
		case path == "/v1/sys/audit":
			return `{"data":{"file/":{"type":"file"}}}`, 200
		case strings.HasPrefix(path, "/v1/sys/namespaces"):
			return `{"errors":["unsupported path"]}`, 404 // OSS
		case strings.HasPrefix(path, "/v1/pki/roles"):
			return `{"data":{"keys":["web-server"]}}`, 200
		case strings.HasPrefix(path, "/v1/auth/jwt/role"):
			return `{"data":{"keys":["ci"]}}`, 200
		case strings.HasPrefix(path, "/v1/auth/token/roles"):
			return `{"data":{"keys":["nomad"]}}`, 200
		}
		return `{"data":{"keys":[]}}`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "vault-prod"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// System mounts skipped; kv + pki mounts adopted.
	if got := deriveImportID(mustRes(t, inv, "mount/pki")); got != "pki" {
		t.Errorf("mount import = %q, want pki", got)
	}
	mustRes(t, inv, "mount/secret")
	// token/ auth mount is not a vault_auth_backend resource; jwt + userpass are.
	mustRes(t, inv, "auth/jwt")
	mustRes(t, inv, "auth/userpass")
	if inv.Resources["auth/token"] != nil {
		t.Error("token/ auth mount should not be emitted as vault_auth_backend")
	}
	// root/default policies skipped.
	mustRes(t, inv, "policy/dev-team")
	if inv.Resources["policy/root"] != nil || inv.Resources["policy/default"] != nil {
		t.Error("built-in root/default policies should be skipped")
	}
	// Role fan-outs, path-composite import ids.
	if got := deriveImportID(mustRes(t, inv, "vault:pki_secret_backend_role/pki/roles/web-server")); got != "pki/roles/web-server" {
		t.Errorf("pki role import = %q, want pki/roles/web-server", got)
	}
	if got := deriveImportID(mustRes(t, inv, "vault:jwt_auth_backend_role/auth/jwt/role/ci")); got != "auth/jwt/role/ci" {
		t.Errorf("jwt role import = %q, want auth/jwt/role/ci", got)
	}
	if got := deriveImportID(mustRes(t, inv, "vault:token_auth_backend_role/auth/token/roles/nomad")); got != "auth/token/roles/nomad" {
		t.Errorf("token role import = %q, want auth/token/roles/nomad", got)
	}

	// mount/pki + mount/secret + auth/jwt + auth/userpass + policy/dev-team + audit/file +
	// pki role + jwt role + token role = 9.
	if len(inv.Resources) != 9 {
		t.Errorf("expected 9 resources, got %d", len(inv.Resources))
	}
}

func mustRes(t *testing.T, inv *model.Inventory, id string) *model.Resource {
	t.Helper()
	r := inv.Resources[id]
	if r == nil {
		t.Fatalf("%s missing from inventory", id)
	}
	return r
}
