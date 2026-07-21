package auth0

import (
	"context"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType, container string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: container, Properties: props}
}

func fakeAuth0Do(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := auth0Do
	t.Cleanup(func() { auth0Do = orig; auth0AccessToken = "" })
	auth0Do = func(_ context.Context, _, path string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &auth0APIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

func fakeAuth0Exchange(t *testing.T, fn func() (string, error)) {
	t.Helper()
	orig := auth0Exchange
	t.Cleanup(func() { auth0Exchange = orig; auth0AccessToken = "" })
	auth0Exchange = func(_ context.Context) (string, error) { return fn() }
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"client bare", res("auth0_client", "t", map[string]any{"token": "cid1"}), "cid1"},
		{"resource_server bare", res("auth0_resource_server", "t", map[string]any{"token": "rs1"}), "rs1"},
		{"email_template by name", res("auth0_email_template", "t", map[string]any{"token": "welcome_email"}), "welcome_email"},
		{"tenant singleton sentinel", res("auth0_tenant", "t", map[string]any{"token": "tenant"}), "tenant"},
		{"guardian singleton sentinel", res("auth0_guardian", "t", map[string]any{"token": "guardian"}), "guardian"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("auth0_email_template", "t", map[string]any{"token": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestAuth0BaseConstructed(t *testing.T) {
	t.Setenv("AUTH0_DOMAIN", "mytenant.us.auth0.com")
	if got := auth0Base(); got != "https://mytenant.us.auth0.com" {
		t.Errorf("base = %q, want https://mytenant.us.auth0.com", got)
	}
	// pasted scheme/slashes stripped.
	t.Setenv("AUTH0_DOMAIN", "https://mytenant.eu.auth0.com/")
	if got := auth0Base(); got != "https://mytenant.eu.auth0.com" {
		t.Errorf("base = %q, want cleaned https://mytenant.eu.auth0.com", got)
	}
}

func TestEnsureTokenStaticBypass(t *testing.T) {
	t.Setenv("AUTH0_API_TOKEN", "static-tok")
	called := false
	fakeAuth0Exchange(t, func() (string, error) { called = true; return "x", nil })
	if err := ensureToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("a static AUTH0_API_TOKEN must bypass the client-credentials exchange")
	}
	if auth0BearerToken() != "static-tok" {
		t.Errorf("bearer = %q, want static-tok", auth0BearerToken())
	}
}

func TestEnsureTokenExchange(t *testing.T) {
	t.Setenv("AUTH0_API_TOKEN", "")
	fakeAuth0Exchange(t, func() (string, error) { return "minted-tok", nil })
	if err := ensureToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if auth0BearerToken() != "minted-tok" {
		t.Errorf("bearer = %q, want minted-tok (from the exchange)", auth0BearerToken())
	}
}

func TestAuth0ListPaginates(t *testing.T) {
	fakeAuth0Do(t, func(path string) (string, int) {
		// anchor on "page=1&" — "page=1" alone is a substring of "per_page=100".
		if strings.Contains(path, "page=1&") {
			return `{"clients":[{"client_id":"c"}],"total":3,"start":2,"length":1}`, 200
		}
		return `{"clients":[{"client_id":"a"},{"client_id":"b"}],"total":3,"start":0,"length":2}`, 200
	})
	got, err := auth0List[auth0Client](context.Background(), "/api/v2/clients", "clients")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ClientID != "a" || got[2].ClientID != "c" {
		t.Errorf("expected 3 clients across 2 pages (start+length>=total); got %+v", got)
	}
}

func TestConnectExchangesAndResolvesTenant(t *testing.T) {
	t.Setenv("AUTH0_DOMAIN", "mytenant.us.auth0.com")
	t.Setenv("AUTH0_API_TOKEN", "")
	fakeAuth0Exchange(t, func() (string, error) { return "tok", nil })
	fakeAuth0Do(t, func(path string) (string, int) {
		if strings.Contains(path, "/tenants/settings") {
			return `{"friendly_name":"Acme Corp"}`, 200
		}
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should exchange + resolve the tenant, got %v", err)
	}
	if run.Scope.ID != "Acme Corp" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want Acme Corp/tenant", run.Scope)
	}
	if ac.Identity != "Acme Corp" {
		t.Errorf("identity = %q, want Acme Corp", ac.Identity)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "clients", func() error { return &auth0APIError{Status: 403, msg: "insufficient_scope"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "roles", func() error { return &auth0APIError{Status: 401, msg: "invalid token"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the system-object skips, the doubled actions path, the bare-array log streams,
// the email-template name fan-out, and the settings singletons (email_provider 404 → skipped).
func TestEnumerateShapesAndSkips(t *testing.T) {
	fakeAuth0Do(t, func(path string) (string, int) {
		switch {
		case strings.Contains(path, "/clients"):
			return `{"clients":[{"client_id":"cid1","name":"App"},{"client_id":"cidG","name":"Global","global":true}],"total":2,"start":0,"length":2}`, 200
		case strings.Contains(path, "/resource-servers"):
			return `{"resource_servers":[{"id":"rs1","name":"API"},{"id":"rsSys","name":"Mgmt","is_system":true}],"total":2,"start":0,"length":2}`, 200
		case strings.Contains(path, "/connections"):
			return `{"connections":[{"id":"con1","name":"db"}],"total":1,"start":0,"length":1}`, 200
		case strings.Contains(path, "/roles"):
			return `{"roles":[{"id":"rol1","name":"admin"}],"total":1,"start":0,"length":1}`, 200
		case strings.Contains(path, "/actions/actions"):
			return `{"actions":[{"id":"act1","name":"login"}],"total":1,"start":0,"length":1}`, 200
		case strings.Contains(path, "/organizations"):
			return `{"organizations":[{"id":"org1","name":"acme","display_name":"Acme"}],"total":1,"start":0,"length":1}`, 200
		case strings.Contains(path, "/client-grants"):
			return `{"client_grants":[{"id":"cgr1"}],"total":1,"start":0,"length":1}`, 200
		case strings.Contains(path, "/log-streams"):
			return `[{"id":"lst1","name":"stream"}]`, 200
		case strings.Contains(path, "/email-templates/welcome_email"):
			return `{"template":"welcome_email"}`, 200
		case strings.Contains(path, "/email-templates/"):
			return `{"error":"not found"}`, 404
		case strings.Contains(path, "/emails/provider"):
			return `{"error":"not configured"}`, 404
		case strings.Contains(path, "/tenants/settings"),
			strings.Contains(path, "/branding"),
			strings.Contains(path, "/attack-protection/"),
			strings.Contains(path, "/prompts"),
			strings.Contains(path, "/guardian/factors"):
			return `{}`, 200
		}
		return `{}`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "tenant"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// System-object skips.
	if _, ok := inv.Resources["client/cidG"]; ok {
		t.Error("global all-applications client must be skipped")
	}
	if _, ok := inv.Resources["resource_server/rsSys"]; ok {
		t.Error("is_system resource server must be skipped")
	}
	// Doubled actions path + bare-array log streams reached.
	mustRes(t, inv, "action/act1")
	mustRes(t, inv, "log_stream/lst1")
	// Email-template name fan-out: welcome adopted, others 404-skipped.
	if got := deriveImportID(mustRes(t, inv, "email_template/welcome_email")); got != "welcome_email" {
		t.Errorf("email_template import = %q, want welcome_email", got)
	}
	if _, ok := inv.Resources["email_template/verify_email"]; ok {
		t.Error("unconfigured email template (404) must be skipped")
	}
	// Singletons: present ones adopted with sentinel imports; email_provider (404) skipped.
	if got := deriveImportID(mustRes(t, inv, "tenant")); got != "tenant" {
		t.Errorf("tenant singleton import = %q, want tenant sentinel", got)
	}
	mustRes(t, inv, "attack_protection")
	if _, ok := inv.Resources["email_provider"]; ok {
		t.Error("email_provider (404, unconfigured) must be skipped")
	}

	// client(1)+resource_server(1)+connection(1)+role(1)+action(1)+organization(1)+
	// client_grant(1)+log_stream(1)+email_template(1)+singletons(tenant/branding/
	// attack_protection/prompt/guardian = 5) = 14.
	if len(inv.Resources) != 14 {
		t.Errorf("expected 14 resources, got %d", len(inv.Resources))
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
