package keycloak

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

func fakeKC(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := kcDo
	t.Cleanup(func() { kcDo = orig; kcAccessToken = "" })
	kcDo = func(_ context.Context, _, path string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &keycloakAPIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

func fakeKCExchange(t *testing.T, fn func() (string, error)) {
	t.Helper()
	orig := kcExchange
	t.Cleanup(func() { kcExchange = orig; kcAccessToken = "" })
	kcExchange = func(_ context.Context) (string, error) { return fn() }
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"realm bare", res("keycloak_realm", "s", map[string]any{"token": "my-realm"}), "my-realm"},
		{"openid_client 2-part uuid", res("keycloak_openid_client", "s", map[string]any{"left": "my-realm", "right": "cuuid1"}), "my-realm/cuuid1"},
		{"role 2-part (realm or client)", res("keycloak_role", "s", map[string]any{"left": "my-realm", "right": "role1"}), "my-realm/role1"},
		{"group 2-part", res("keycloak_group", "s", map[string]any{"left": "my-realm", "right": "grp1"}), "my-realm/grp1"},
		{"oidc_idp 2-part alias", res("keycloak_oidc_identity_provider", "s", map[string]any{"left": "my-realm", "right": "my-idp"}), "my-realm/my-idp"},
		{"required_action 2-part alias", res("keycloak_required_action", "s", map[string]any{"left": "my-realm", "right": "CONFIGURE_TOTP"}), "my-realm/CONFIGURE_TOTP"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("keycloak_role", "s", map[string]any{"left": "my-realm", "right": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestKCBase(t *testing.T) {
	t.Setenv("KEYCLOAK_BASE_PATH", "")
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com/")
	if got := kcBase(); got != "https://kc.example.com" {
		t.Errorf("base = %q, want https://kc.example.com", got)
	}
	// legacy Wildfly base_path.
	t.Setenv("KEYCLOAK_BASE_PATH", "/auth")
	if got := kcBase(); got != "https://kc.example.com/auth" {
		t.Errorf("base = %q, want https://kc.example.com/auth", got)
	}
	// http allowed (self-hosted dev).
	t.Setenv("KEYCLOAK_BASE_PATH", "")
	t.Setenv("KEYCLOAK_URL", "http://localhost:8080")
	if got := kcBase(); got != "http://localhost:8080" {
		t.Errorf("base = %q, want http://localhost:8080", got)
	}
	// userinfo '@' splice rejected.
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com@evil.com")
	if got := kcBase(); got != "" {
		t.Errorf("userinfo splice = %q, want empty", got)
	}
}

func TestRefreshTokenSetsBearer(t *testing.T) {
	fakeKCExchange(t, func() (string, error) { return "tok123", nil })
	if err := refreshToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if kcAccessToken != "tok123" {
		t.Errorf("access token = %q, want tok123 (from the exchange)", kcAccessToken)
	}
}

func TestKCListPaginates(t *testing.T) {
	fakeKC(t, func(path string) (string, int) {
		if strings.Contains(path, "first=100") {
			return `[{"id":"z","name":"last"}]`, 200
		}
		var b strings.Builder
		b.WriteString("[")
		for i := 0; i < kcPageSize; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":"x","name":"n"}`)
		}
		b.WriteString("]")
		return b.String(), 200
	})
	got, err := kcList[kcIDName](context.Background(), "/admin/realms/r/roles")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != kcPageSize+1 {
		t.Errorf("expected %d across 2 pages via first/max; got %d", kcPageSize+1, len(got))
	}
}

func TestConnectExchangesAndResolvesServer(t *testing.T) {
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com")
	fakeKCExchange(t, func() (string, error) { return "tok", nil })
	fakeKC(t, func(path string) (string, int) { return `[]`, 200 }) // /admin/realms probe
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should exchange + resolve the server, got %v", err)
	}
	if run.Scope.ID != "kc.example.com" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want kc.example.com/tenant", run.Scope)
	}
	if ac.Identity != "kc.example.com" {
		t.Errorf("identity = %q, want kc.example.com", ac.Identity)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "realms", func() error { return &keycloakAPIError{Status: 403, msg: "forbidden"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "clients", func() error { return &keycloakAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration (after refresh) should be fatal")
	}
}

// End-to-end: the realm fan-out (skip master), the protocol/providerId discriminators + built-in
// skips, the two-level client-roles fan-out (2-part import, NOT 3-part), and the group-tree
// flatten.
func TestEnumerateFanOutDiscriminatorsAndClientRoles(t *testing.T) {
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com")
	fakeKC(t, func(path string) (string, int) {
		switch {
		case path == "/admin/realms":
			return `[{"id":"r1","realm":"my-realm"},{"id":"rm","realm":"master"}]`, 200
		case strings.Contains(path, "/clients/") && strings.Contains(path, "/roles"):
			if strings.Contains(path, "cuuid1") {
				return `[{"id":"crole1","name":"app-role"}]`, 200
			}
			return `[]`, 200
		case strings.Contains(path, "/client-scopes"):
			return `[{"id":"sc1","name":"profile","protocol":"openid-connect"},{"id":"scsaml","name":"saml-scope","protocol":"saml"}]`, 200
		case strings.Contains(path, "/clients"):
			return `[{"id":"cuuid1","clientId":"my-app","protocol":"openid-connect"},{"id":"csaml","clientId":"saml-app","protocol":"saml"},{"id":"cacc","clientId":"account","protocol":"openid-connect"}]`, 200
		case strings.Contains(path, "/roles"):
			return `[{"id":"rrole1","name":"realm-role"}]`, 200
		case strings.Contains(path, "/groups"):
			return `[{"id":"g1","name":"parent","subGroups":[{"id":"g2","name":"child","subGroups":[]}]}]`, 200
		case strings.Contains(path, "/authentication/flows"):
			return `[{"id":"f1","alias":"custom-flow","builtIn":false},{"id":"fb","alias":"browser","builtIn":true}]`, 200
		case strings.Contains(path, "/identity-provider/instances"):
			return `[{"alias":"my-oidc","providerId":"oidc"},{"alias":"my-saml","providerId":"saml"},{"alias":"my-google","providerId":"google"}]`, 200
		case strings.Contains(path, "/components"):
			return `[{"id":"comp1","name":"my-ldap","providerId":"ldap"},{"id":"comp2","name":"kerb","providerId":"kerberos"}]`, 200
		case strings.Contains(path, "/authentication/required-actions"):
			return `[{"alias":"CONFIGURE_TOTP","name":"Configure OTP"}]`, 200
		}
		return `[]`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "server"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// master realm skipped.
	if _, ok := inv.Resources["realm/master"]; ok {
		t.Error("master realm must be skipped")
	}
	// client protocol discriminator + built-in (account) skip.
	if c := mustRes(t, inv, "client/my-realm/cuuid1"); c.TFType != "keycloak_openid_client" {
		t.Errorf("cuuid1 TFType = %q, want keycloak_openid_client", c.TFType)
	}
	if c := mustRes(t, inv, "client/my-realm/csaml"); c.TFType != "keycloak_saml_client" {
		t.Errorf("csaml TFType = %q, want keycloak_saml_client", c.TFType)
	}
	for id := range inv.Resources {
		if strings.HasPrefix(id, "client/") && strings.Contains(id, "cacc") {
			t.Error("built-in account client must be skipped")
		}
	}
	// client role: two-level fan-out, but 2-part import (NOT 3-part).
	if got := deriveImportID(mustRes(t, inv, "role/my-realm/crole1")); got != "my-realm/crole1" {
		t.Errorf("client role import = %q, want 2-part my-realm/crole1", got)
	}
	mustRes(t, inv, "role/my-realm/rrole1") // realm role
	// group tree flattened.
	mustRes(t, inv, "group/my-realm/g1")
	mustRes(t, inv, "group/my-realm/g2")
	// idp providerId discriminator; social skipped.
	if i := mustRes(t, inv, "idp/my-realm/my-oidc"); i.TFType != "keycloak_oidc_identity_provider" {
		t.Errorf("my-oidc TFType = %q, want keycloak_oidc_identity_provider", i.TFType)
	}
	if i := mustRes(t, inv, "idp/my-realm/my-saml"); i.TFType != "keycloak_saml_identity_provider" {
		t.Errorf("my-saml TFType = %q, want keycloak_saml_identity_provider", i.TFType)
	}
	if _, ok := inv.Resources["idp/my-realm/my-google"]; ok {
		t.Error("social (google) idp must be skipped")
	}
	// ldap federation filter; kerberos skipped. builtIn flow skipped. saml scope skipped.
	mustRes(t, inv, "federation/my-realm/comp1")
	if _, ok := inv.Resources["flow/my-realm/fb"]; ok {
		t.Error("builtIn flow must be skipped")
	}
	if _, ok := inv.Resources["client_scope/my-realm/scsaml"]; ok {
		t.Error("saml client scope must be skipped (OIDC only in Phase A)")
	}

	// realm(1)+clients(2)+roles(2: crole1+rrole1)+groups(2)+scope(1)+flow(1)+idp(2)+
	// federation(1)+required_action(1) = 13.
	if len(inv.Resources) != 13 {
		t.Errorf("expected 13 resources, got %d", len(inv.Resources))
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
