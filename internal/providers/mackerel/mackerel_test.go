package mackerel

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

func fakeMackerel(t *testing.T, fn func(method, path string) (string, int)) {
	t.Helper()
	orig := mkDo
	t.Cleanup(func() { mkDo = orig })
	mkDo = func(_ context.Context, method, path string) ([]byte, int, error) {
		body, status := fn(method, path)
		if status >= 400 {
			return []byte(body), status, &mackerelAPIError{Status: status, msg: "err"}
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
		{"service bare name", res("mackerel_service", "org", map[string]any{"token": "web"}), "web"},
		{"monitor opaque id", res("mackerel_monitor", "org", map[string]any{"token": "2qtozU21abc"}), "2qtozU21abc"},
		{"dashboard opaque id", res("mackerel_dashboard", "org", map[string]any{"token": "ABCDEFG"}), "ABCDEFG"},
		{"role colon composite", res("mackerel_role", "org", map[string]any{"service": "web", "role": "app"}), "web:app"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("mackerel_monitor", "org", map[string]any{"token": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestMKBase(t *testing.T) {
	t.Setenv("MACKEREL_API_BASE", "")
	t.Setenv("MACKEREL_APIURL", "")
	if got := mkBase(); got != "https://api.mackerelio.com" {
		t.Errorf("default = %q, want https://api.mackerelio.com", got)
	}
	t.Setenv("MACKEREL_API_BASE", "http://kcps-mackerel.io") // override + http upgraded
	if got := mkBase(); got != "https://kcps-mackerel.io" {
		t.Errorf("base override = %q, want https://kcps-mackerel.io", got)
	}
	t.Setenv("MACKEREL_API_BASE", "")
	t.Setenv("MACKEREL_APIURL", "internal.example.com") // alias + bare host promoted to https
	if got := mkBase(); got != "https://internal.example.com" {
		t.Errorf("apiurl alias = %q, want https://internal.example.com", got)
	}
}

func TestMKKey(t *testing.T) {
	t.Setenv("MACKEREL_APIKEY", "")
	t.Setenv("MACKEREL_API_KEY", "aliaskey")
	if got := mkKey(); got != "aliaskey" {
		t.Errorf("alias key = %q, want aliaskey", got)
	}
	t.Setenv("MACKEREL_APIKEY", "primarykey")
	if got := mkKey(); got != "primarykey" {
		t.Errorf("primary key = %q, want primarykey", got)
	}
}

// decodeEnvelope tolerates both the {<key>:[...]} envelope and a bare array.
func TestDecodeEnvelope(t *testing.T) {
	got, err := decodeEnvelope[mkObj]("/p", "services", []byte(`{"services":[{"name":"web"},{"name":"db"}]}`))
	if err != nil || len(got) != 2 || got[0].label() != "web" {
		t.Fatalf("named-array envelope: got %+v err %v", got, err)
	}
	// camelCase key (notificationGroups) still resolves when passed explicitly.
	got, err = decodeEnvelope[mkObj]("/p", "notificationGroups", []byte(`{"notificationGroups":[{"id":"g1"}]}`))
	if err != nil || len(got) != 1 || got[0].ID != "g1" {
		t.Fatalf("camelCase key: got %+v err %v", got, err)
	}
	// Bare-array fallback if the response is not wrapped.
	got, err = decodeEnvelope[mkObj]("/p", "services", []byte(`[{"name":"web"}]`))
	if err != nil || len(got) != 1 {
		t.Fatalf("bare-array fallback: got %+v err %v", got, err)
	}
	// Empty body → empty, no error.
	if got, err := decodeEnvelope[mkObj]("/p", "services", nil); err != nil || len(got) != 0 {
		t.Errorf("empty body: got %+v err %v", got, err)
	}
}

func TestConnectResolvesOrg(t *testing.T) {
	fakeMackerel(t, func(method, path string) (string, int) {
		switch path {
		case "/api/v0/services": // validateKey probe
			return `{"services":[]}`, 200
		case "/api/v0/org": // best-effort identity
			return `{"name":"my-org"}`, 200
		}
		t.Errorf("unexpected path %s", path)
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should validate via /api/v0/services, got %v", err)
	}
	if run.Scope.ID != "my-org" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want my-org/tenant", run.Scope)
	}
	if ac.Identity != "my-org" {
		t.Errorf("identity = %q, want my-org", ac.Identity)
	}
}

// A restricted key authenticates (services 200/403) but lacks org read (403 on /org) — connect
// must NOT abort; it falls back to the base host for the scope id.
func TestConnectRestrictedKeyFallsBackToHost(t *testing.T) {
	t.Setenv("MACKEREL_API_BASE", "")
	t.Setenv("MACKEREL_APIURL", "")
	fakeMackerel(t, func(method, path string) (string, int) {
		switch path {
		case "/api/v0/services":
			return `{"services":[]}`, 200
		case "/api/v0/org":
			return `{"error":{"message":"forbidden"}}`, 403
		}
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("restricted key should still connect, got %v", err)
	}
	if run.Scope.ID != "api.mackerelio.com" {
		t.Errorf("scope id = %q, want the base host fallback api.mackerelio.com", run.Scope.ID)
	}
}

// validateKey: 200/403 on the services probe are both "authenticated"; only 401 is a bad key.
func TestValidateKeyTaxonomy(t *testing.T) {
	fakeMackerel(t, func(method, path string) (string, int) { return `{"error":{"message":"forbidden"}}`, 403 })
	if err := validateKey(context.Background()); err != nil {
		t.Errorf("403 on services should be a valid (restricted) key, got %v", err)
	}
	fakeMackerel(t, func(method, path string) (string, int) { return `{"error":{"message":"unauthorized"}}`, 401 })
	if err := validateKey(context.Background()); err == nil {
		t.Error("401 on services should be a bad key")
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "aws integrations", func() error { return &mackerelAPIError{Status: 403, msg: "not permitted"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "monitors", func() error { return &mackerelAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the service→role fan-out (colon composite) plus the flat org-level lists.
func TestEnumerateMixedShapes(t *testing.T) {
	fakeMackerel(t, func(method, path string) (string, int) {
		switch {
		case path == "/api/v0/services":
			return `{"services":[{"name":"web"}]}`, 200
		case strings.HasPrefix(path, "/api/v0/services/web/roles"):
			return `{"roles":[{"name":"app"},{"name":"db"}]}`, 200
		case path == "/api/v0/monitors":
			return `{"monitors":[{"id":"2mon","name":"cpu","type":"host"}]}`, 200
		case path == "/api/v0/channels":
			return `{"channels":[{"id":"3chan","name":"slack","type":"slack"}]}`, 200
		case path == "/api/v0/notification-groups":
			return `{"notificationGroups":[{"id":"4grp","name":"oncall"}]}`, 200
		case path == "/api/v0/dashboards":
			return `{"dashboards":[{"id":"5dash","title":"Overview"}]}`, 200
		case path == "/api/v0/aws-integrations":
			return `{"aws_integrations":[{"id":"6aws","name":"prod-aws"}]}`, 200
		case path == "/api/v0/downtimes":
			return `{"downtimes":[{"id":"7dt","name":"maint"}]}`, 200
		case path == "/api/v0/alert-group-settings":
			return `{"alertGroupSettings":[{"id":"8ags","name":"web-alerts"}]}`, 200
		}
		return `{}`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "my-org"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// Service imports by bare name.
	if got := deriveImportID(mustRes(t, inv, "service/web")); got != "web" {
		t.Errorf("service import = %q, want web", got)
	}
	// Roles fan out under the service as colon composites.
	if got := deriveImportID(mustRes(t, inv, "role/web/app")); got != "web:app" {
		t.Errorf("role import = %q, want web:app", got)
	}
	if got := deriveImportID(mustRes(t, inv, "role/web/db")); got != "web:db" {
		t.Errorf("role import = %q, want web:db", got)
	}
	// Dashboard labels by title.
	if r := mustRes(t, inv, "dashboard/5dash"); r.Name != "Overview" {
		t.Errorf("dashboard name = %q, want Overview", r.Name)
	}
	// aws_integration (snake-case envelope key) reached.
	mustRes(t, inv, "aws_integration/6aws")

	// service(1)+role(2)+monitor(1)+channel(1)+notification_group(1)+dashboard(1)+
	// aws_integration(1)+downtime(1)+alert_group_setting(1) = 10.
	if len(inv.Resources) != 10 {
		t.Errorf("expected 10 resources, got %d", len(inv.Resources))
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
