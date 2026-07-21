package okta

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType, container string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: container, Properties: props}
}

func fakeOkta(t *testing.T, fn func(url string) (body string, status int, next string)) {
	t.Helper()
	orig := oktaDo
	t.Cleanup(func() { oktaDo = orig })
	oktaDo = func(_ context.Context, _, url string) ([]byte, int, string, error) {
		body, status, next := fn(url)
		if status >= 400 {
			return []byte(body), status, "", &oktaAPIError{Status: status, msg: "err"}
		}
		return []byte(body), status, next, nil
	}
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"user bare", res("okta_user", "o", map[string]any{"token": "00u1"}), "00u1"},
		{"app_oauth bare", res("okta_app_oauth", "o", map[string]any{"token": "0oa1"}), "0oa1"},
		{"auth_server bare", res("okta_auth_server", "o", map[string]any{"token": "aus1"}), "aus1"},
		{"policy_signon bare", res("okta_policy_signon", "o", map[string]any{"token": "p1"}), "p1"},
		{"idp_oidc bare", res("okta_idp_oidc", "o", map[string]any{"token": "idp1"}), "idp1"},
		{"auth_server_scope 2-part", res("okta_auth_server_scope", "o", map[string]any{"left": "aus1", "right": "scp1"}), "aus1/scp1"},
		{"auth_server_policy 2-part", res("okta_auth_server_policy", "o", map[string]any{"left": "aus1", "right": "pol1"}), "aus1/pol1"},
		{"policy_rule_signon 2-part", res("okta_policy_rule_signon", "o", map[string]any{"left": "p1", "right": "r1"}), "p1/r1"},
		{"auth_server_policy_rule 3-part", res("okta_auth_server_policy_rule", "o", map[string]any{"a": "aus1", "b": "pol1", "c": "rul1"}), "aus1/pol1/rul1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("okta_auth_server_scope", "o", map[string]any{"left": `${file("x")}`, "right": "scp1"})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestOktaAuthHeader(t *testing.T) {
	t.Setenv("OKTA_API_TOKEN", "abc123")
	if got := oktaAuthHeader(); got != "SSWS abc123" {
		t.Errorf("auth header = %q, want SSWS abc123 (NOT Bearer)", got)
	}
}

func TestOktaBaseConstructed(t *testing.T) {
	t.Setenv("OKTA_ORG_NAME", "dev-123")
	t.Setenv("OKTA_BASE_URL", "okta.com")
	if got := oktaBase(); got != "https://dev-123.okta.com" {
		t.Errorf("base = %q, want https://dev-123.okta.com", got)
	}
	// scheme/slashes pasted into base_url are stripped.
	t.Setenv("OKTA_BASE_URL", "https://oktapreview.com/")
	if got := oktaBase(); got != "https://dev-123.oktapreview.com" {
		t.Errorf("base = %q, want https://dev-123.oktapreview.com (cleaned)", got)
	}
}

func TestIsOktaURL(t *testing.T) {
	t.Setenv("OKTA_ORG_NAME", "dev-123")
	t.Setenv("OKTA_BASE_URL", "okta.com")
	if !isOktaURL("https://dev-123.okta.com/api/v1/users?after=x") {
		t.Error("same-host https next url should be allowed")
	}
	if isOktaURL("https://evil.example/api/v1/users") {
		t.Error("cross-host next url must be refused")
	}
	if isOktaURL("http://dev-123.okta.com/api/v1/users") {
		t.Error("plaintext http next url must be refused")
	}
}

func TestParseNextLink(t *testing.T) {
	// Multiple Link headers; only rel="next" is followed.
	links := []string{
		`<https://dev-123.okta.com/api/v1/users?limit=200>; rel="self"`,
		`<https://dev-123.okta.com/api/v1/users?after=00u9&limit=200>; rel="next"`,
	}
	if got := parseNextLink(links); got != "https://dev-123.okta.com/api/v1/users?after=00u9&limit=200" {
		t.Errorf("next = %q", got)
	}
	// self only → no next.
	if got := parseNextLink([]string{`<https://x/api>; rel="self"`}); got != "" {
		t.Errorf("expected no next, got %q", got)
	}
	// comma-joined links in one header.
	one := []string{`<https://x/a>; rel="self", <https://x/b>; rel="next"`}
	if got := parseNextLink(one); got != "https://x/b" {
		t.Errorf("comma-joined next = %q, want https://x/b", got)
	}
	// a comma INSIDE the cursor URL must not truncate the parse (bracket-aware).
	commaURL := []string{`<https://dev-123.okta.com/api/v1/users?after=a,b&limit=200>; rel="next"`}
	if got := parseNextLink(commaURL); got != "https://dev-123.okta.com/api/v1/users?after=a,b&limit=200" {
		t.Errorf("comma-in-url next = %q (comma truncated the parse)", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter(http.Header{"Retry-After": []string{"3"}}); d != 3*time.Second {
		t.Errorf("Retry-After 3 → %v, want 3s", d)
	}
	if d := parseRetryAfter(http.Header{}); d != 0 {
		t.Errorf("no Retry-After → %v, want 0", d)
	}
}

func TestOktaBackoffRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	if err := oktaBackoff(ctx, 0, time.Hour); err == nil {
		t.Error("backoff should abort immediately on a cancelled context, not sleep")
	}
}

func TestOktaListFollowsLinkNext(t *testing.T) {
	t.Setenv("OKTA_ORG_NAME", "dev-123")
	t.Setenv("OKTA_BASE_URL", "okta.com")
	fakeOkta(t, func(url string) (string, int, string) {
		if strings.Contains(url, "after=") {
			return `[{"id":"b"}]`, 200, ""
		}
		return `[{"id":"a"}]`, 200, "https://dev-123.okta.com/api/v1/users?after=00u&limit=200"
	})
	got, err := oktaList[oktaIDName](context.Background(), "/api/v1/users")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("expected 2 across 2 pages via Link next; got %+v", got)
	}
}

func TestOktaListRefusesForeignNext(t *testing.T) {
	t.Setenv("OKTA_ORG_NAME", "dev-123")
	t.Setenv("OKTA_BASE_URL", "okta.com")
	fakeOkta(t, func(url string) (string, int, string) {
		return `[{"id":"a"}]`, 200, "https://evil.example/steal?after=x"
	})
	if _, err := oktaList[oktaIDName](context.Background(), "/api/v1/users"); err == nil {
		t.Error("oktaList must refuse to follow a Link next pointing at a foreign host")
	}
}

func TestAppNativeDiscriminator(t *testing.T) {
	cases := []struct{ mode, name, want string }{
		{"OPENID_CONNECT", "x", "okta:app_oauth"},
		{"SAML_2_0", "x", "okta:app_saml"},
		{"AUTO_LOGIN", "x", "okta:app_auto_login"},
		{"BOOKMARK", "x", "okta:app_bookmark"},
		{"BASIC_AUTH", "x", "okta:app_basic_auth"},
		{"BROWSER_PLUGIN", "some_swa", "okta:app_swa"},
		{"BROWSER_PLUGIN", "template_swa3field", "okta:app_three_field"},
		{"SECURE_PASSWORD_STORE", "x", "okta:app_secure_password_store"},
		{"WS_FEDERATION", "x", ""},
	}
	for _, c := range cases {
		if got := appNative(c.mode, c.name); got != c.want {
			t.Errorf("appNative(%q,%q) = %q, want %q", c.mode, c.name, got, c.want)
		}
	}
}

func TestConnectResolvesOrg(t *testing.T) {
	t.Setenv("OKTA_ORG_NAME", "dev-123")
	t.Setenv("OKTA_BASE_URL", "okta.com")
	fakeOkta(t, func(url string) (string, int, string) {
		if strings.Contains(url, "/api/v1/org") {
			return `{"subdomain":"dev-123","companyName":"Acme Inc"}`, 200, ""
		}
		return `[]`, 200, "" // /api/v1/users?limit=1 probe
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should resolve the org, got %v", err)
	}
	if run.Scope.ID != "Acme Inc" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want Acme Inc/tenant", run.Scope)
	}
	if ac.Identity != "Acme Inc" {
		t.Errorf("identity = %q, want Acme Inc", ac.Identity)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "apps", func() error { return &oktaAPIError{Status: 403, msg: "role absent"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "users", func() error { return &oktaAPIError{Status: 401, msg: "invalid token"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the app/idp discriminators + Okta-own-app skip + group filter, the ?type= policy
// loop with its rule fan-out, and the auth-server NESTED fan-out (the 3-part composite).
func TestEnumerateDiscriminatorsAndNestedFanOut(t *testing.T) {
	t.Setenv("OKTA_ORG_NAME", "dev-123")
	t.Setenv("OKTA_BASE_URL", "okta.com")
	fakeOkta(t, func(url string) (string, int, string) {
		switch {
		case strings.Contains(url, "/authorizationServers/") && strings.Contains(url, "/rules"):
			return `[{"id":"arl1","name":"arule"}]`, 200, ""
		case strings.Contains(url, "/authorizationServers/") && strings.Contains(url, "/scopes"):
			return `[{"id":"scp1","name":"scope"}]`, 200, ""
		case strings.Contains(url, "/authorizationServers/") && strings.Contains(url, "/claims"):
			return `[{"id":"clm1","name":"claim"}]`, 200, ""
		case strings.Contains(url, "/authorizationServers/") && strings.Contains(url, "/policies"):
			return `[{"id":"apl1","name":"apolicy"}]`, 200, ""
		case strings.Contains(url, "/authorizationServers"):
			return `[{"id":"aus1","name":"default"}]`, 200, ""
		case strings.Contains(url, "/policies/") && strings.Contains(url, "/rules"):
			return `[{"id":"pr1","name":"prule"}]`, 200, ""
		case strings.Contains(url, "type=OKTA_SIGN_ON"):
			return `[{"id":"pso1","name":"signon"}]`, 200, ""
		case strings.Contains(url, "type=PASSWORD"):
			return `[{"id":"ppw1","name":"pwd"}]`, 200, ""
		case strings.Contains(url, "type=MFA_ENROLL"):
			return `[{"id":"pmf1","name":"mfa"}]`, 200, ""
		case strings.Contains(url, "/apps"):
			return `[{"id":"0oa1","label":"OIDC","name":"oidc_client","signOnMode":"OPENID_CONNECT"},{"id":"0oa2","label":"SAML","name":"saml_app","signOnMode":"SAML_2_0"},{"id":"0oaX","label":"Admin","name":"saasure","signOnMode":"OPENID_CONNECT"}]`, 200, ""
		case strings.Contains(url, "/users"):
			return `[{"id":"00u1"}]`, 200, ""
		case strings.Contains(url, "/groups/rules"):
			return `[{"id":"0pr1","name":"grule"}]`, 200, ""
		case strings.Contains(url, "/groups"):
			return `[{"id":"00g1","type":"OKTA_GROUP","profile":{"name":"Eng"}},{"id":"00g2","type":"APP_GROUP","profile":{"name":"AppG"}}]`, 200, ""
		case strings.Contains(url, "/meta/types/user"):
			return `[{"id":"oty1","name":"default"}]`, 200, ""
		case strings.Contains(url, "/trustedOrigins"):
			return `[{"id":"tos1","name":"origin"}]`, 200, ""
		case strings.Contains(url, "/zones"):
			return `[{"id":"nzo1","name":"zone"}]`, 200, ""
		case strings.Contains(url, "/inlineHooks"):
			return `[{"id":"cal1","name":"ihook"}]`, 200, ""
		case strings.Contains(url, "/eventHooks"):
			return `[{"id":"who1","name":"ehook"}]`, 200, ""
		case strings.Contains(url, "/idps"):
			return `[{"id":"0oi1","name":"OIDC IdP","type":"OIDC"},{"id":"0oi2","name":"Google","type":"GOOGLE"}]`, 200, ""
		}
		return `[]`, 200, ""
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "org"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// App discriminator + Okta-own skip.
	if a := mustRes(t, inv, "app/0oa1"); a.TFType != "okta_app_oauth" {
		t.Errorf("0oa1 TFType = %q, want okta_app_oauth", a.TFType)
	}
	if a := mustRes(t, inv, "app/0oa2"); a.TFType != "okta_app_saml" {
		t.Errorf("0oa2 TFType = %q, want okta_app_saml", a.TFType)
	}
	if _, ok := inv.Resources["app/0oaX"]; ok {
		t.Error("saasure (Okta-own app) must be skipped")
	}
	// Group filter guard.
	if _, ok := inv.Resources["group/00g2"]; ok {
		t.Error("APP_GROUP must be skipped")
	}
	// IdP discriminator.
	if i := mustRes(t, inv, "idp/0oi1"); i.TFType != "okta_idp_oidc" {
		t.Errorf("0oi1 TFType = %q, want okta_idp_oidc", i.TFType)
	}
	if _, ok := inv.Resources["idp/0oi2"]; ok {
		t.Error("social (Google) IdP must be skipped")
	}
	// The 3-part auth-server-policy-rule composite.
	if got := deriveImportID(mustRes(t, inv, "auth_server_policy_rule/aus1/apl1/arl1")); got != "aus1/apl1/arl1" {
		t.Errorf("auth_server_policy_rule import = %q, want aus1/apl1/arl1 (3-part)", got)
	}
	// 2-part auth-server scope + top-level policy rule.
	if got := deriveImportID(mustRes(t, inv, "auth_server_scope/aus1/scp1")); got != "aus1/scp1" {
		t.Errorf("auth_server_scope import = %q, want aus1/scp1", got)
	}
	if got := deriveImportID(mustRes(t, inv, "policy_rule/pso1/pr1")); got != "pso1/pr1" {
		t.Errorf("policy_rule import = %q, want pso1/pr1", got)
	}

	// user(1)+group(1)+group_rule(1)+user_type(1)+apps(2)+trusted_origin(1)+zone(1)+
	// inline_hook(1)+event_hook(1)+idp(1)+policies(3)+policy_rules(3)+auth_server(1)+
	// as_scope(1)+as_claim(1)+as_policy(1)+as_policy_rule(1) = 22.
	if len(inv.Resources) != 22 {
		t.Errorf("expected 22 resources, got %d", len(inv.Resources))
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
