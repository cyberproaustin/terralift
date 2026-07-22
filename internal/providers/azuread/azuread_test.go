package azuread

import (
	"context"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: "tid", Properties: props}
}

func fakeExchange(t *testing.T) {
	t.Helper()
	orig := adExchange
	t.Cleanup(func() { adExchange = orig })
	adExchange = func(_ context.Context) (string, error) { return "tok", nil }
}

func fakeAdDo(t *testing.T, fn func(method, url string) (body string, status int)) {
	t.Helper()
	orig := adDo
	t.Cleanup(func() { adDo = orig })
	adDo = func(_ context.Context, method, url string) ([]byte, error) {
		body, status := fn(method, url)
		if status >= 400 {
			return []byte(body), &azureadAPIError{Status: status, msg: "err"}
		}
		return []byte(body), nil
	}
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"group path prefix", res("azuread_group", map[string]any{"importID": "/groups/g1"}), "/groups/g1"},
		{"sp path prefix", res("azuread_service_principal", map[string]any{"importID": "/servicePrincipals/s1"}), "/servicePrincipals/s1"},
		{"group_member no leading slash", res("azuread_group_member", map[string]any{"importID": "g1/member/m1"}), "g1/member/m1"},
		{"app_role_assignment composite", res("azuread_app_role_assignment", map[string]any{"importID": "/servicePrincipals/s1/appRoleAssignedTo/ar1"}), "/servicePrincipals/s1/appRoleAssignedTo/ar1"},
		{"directory_role_assignment bare id", res("azuread_directory_role_assignment", map[string]any{"importID": "ePROZI_iKE-1"}), "ePROZI_iKE-1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("azuread_group", map[string]any{"importID": `/groups/${x}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestIsGraphURL(t *testing.T) {
	if !isGraphURL("https://graph.microsoft.com/v1.0/groups?$skiptoken=abc") {
		t.Error("on-host https nextLink should validate")
	}
	if isGraphURL("https://evil.example.com/v1.0/groups") {
		t.Error("off-host nextLink must be rejected (token-exfil guard)")
	}
	if isGraphURL("http://graph.microsoft.com/v1.0/groups") {
		t.Error("http nextLink must be rejected")
	}
}

// gGraphList follows an on-host @odata.nextLink and stops when it's absent.
func TestGGraphListPaginates(t *testing.T) {
	fakeAdDo(t, func(method, url string) (string, int) {
		if strings.Contains(url, "skiptoken") {
			return `{"value":[{"id":"g2"}]}`, 200
		}
		return `{"value":[{"id":"g1"}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/groups?$skiptoken=abc"}`, 200
	})
	got, err := gGraphList[adObj](context.Background(), "/groups")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "g1" || got[1].ID != "g2" {
		t.Fatalf("expected 2 paged objects, got %+v", got)
	}
}

// An off-host @odata.nextLink is refused (the Bearer must not be sent to another host); page-1
// items are still returned.
func TestGGraphListRefusesOffHostNextLink(t *testing.T) {
	fakeAdDo(t, func(method, url string) (string, int) {
		return `{"value":[{"id":"g1"}],"@odata.nextLink":"https://evil.example.com/v1.0/groups?$skiptoken=x"}`, 200
	})
	got, err := gGraphList[adObj](context.Background(), "/groups")
	if err == nil {
		t.Fatal("expected a host-mismatch error on the off-host nextLink")
	}
	if len(got) != 1 {
		t.Errorf("page-1 items should still be returned, got %d", len(got))
	}
}

func TestConnectResolvesTenant(t *testing.T) {
	t.Setenv("ARM_TENANT_ID", "tid")
	t.Setenv("ARM_CLIENT_ID", "cid")
	t.Setenv("ARM_CLIENT_SECRET", "sec")
	fakeExchange(t)
	fakeAdDo(t, func(method, url string) (string, int) {
		if !strings.Contains(url, "/organization") {
			t.Errorf("connect should read /organization, got %s", url)
		}
		return `{"value":[{"displayName":"Contoso"}]}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should succeed, got %v", err)
	}
	if run.Scope.ID != "tid" || ac.Identity != "tid" {
		t.Errorf("scope/identity = %q/%q, want tid", run.Scope.ID, ac.Identity)
	}
}

func TestListTaxonomy(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	// A top-level 403 is a non-fatal skip but is COUNTED, so an all-forbidden run trips the
	// systemic guard rather than returning a silent empty inventory.
	list(run, &fails, &fatal, "conditional access policies", func() error { return &azureadAPIError{Status: 403, msg: "insufficient privileges"} })
	if fatal != nil || fails != 1 {
		t.Errorf("403 should be a counted skip; fatal=%v fails=%d (want fails=1)", fatal, fails)
	}
	// A 404 (feature absent) is a quiet skip and NOT counted.
	list(run, &fails, &fatal, "administrative units", func() error { return &azureadAPIError{Status: 404, msg: "not found"} })
	if fails != 1 {
		t.Errorf("404 should not be counted; fails=%d (want 1)", fails)
	}
	list(run, &fails, &fatal, "groups", func() error { return &azureadAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the top-level lists + the two relationship fan-outs (group members, app-role
// assignments), the v3 path-prefix imports, and the bare-id directory-role assignment.
func TestEnumerateFanOut(t *testing.T) {
	fakeAdDo(t, func(method, url string) (string, int) {
		switch {
		case strings.Contains(url, "/members"):
			return `{"value":[{"id":"m1","displayName":"Member"}]}`, 200
		case strings.Contains(url, "appRoleAssignedTo"):
			return `{"value":[{"id":"ar1","principalDisplayName":"Prin"}]}`, 200
		case strings.Contains(url, "/applications"):
			return `{"value":[{"id":"a1","displayName":"App"}]}`, 200
		case strings.Contains(url, "/servicePrincipals"):
			return `{"value":[{"id":"s1","displayName":"SP"}]}`, 200
		case strings.Contains(url, "namedLocations"):
			return `{"value":[{"id":"n1","displayName":"NL"}]}`, 200
		case strings.Contains(url, "conditionalAccess/policies"):
			return `{"value":[{"id":"p1","displayName":"CA"}]}`, 200
		case strings.Contains(url, "administrativeUnits"):
			return `{"value":[{"id":"u1","displayName":"AU"}]}`, 200
		case strings.Contains(url, "roleAssignments"):
			return `{"value":[{"id":"r1"}]}`, 200
		case strings.Contains(url, "/groups"):
			return `{"value":[{"id":"g1","displayName":"Grp"}]}`, 200
		}
		return `{"value":[]}`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "tid"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"group/g1":                     "/groups/g1",
		"application/a1":               "/applications/a1",
		"service_principal/s1":         "/servicePrincipals/s1",
		"named_location/n1":            "/identity/conditionalAccess/namedLocations/n1",
		"directory_role_assignment/r1": "r1",
		"group_member/g1/m1":           "g1/member/m1",
		"app_role_assignment/s1/ar1":   "/servicePrincipals/s1/appRoleAssignedTo/ar1",
	}
	for id, want := range checks {
		if got := deriveImportID(mustRes(t, inv, id)); got != want {
			t.Errorf("%s import = %q, want %q", id, got, want)
		}
	}

	// group(1)+app(1)+sp(1)+named_loc(1)+ca_policy(1)+admin_unit(1)+role_assignment(1)+
	// group_member(1)+app_role_assignment(1) = 9.
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
