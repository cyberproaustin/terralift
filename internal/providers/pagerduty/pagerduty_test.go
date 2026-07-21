package pagerduty

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

func fakePagerduty(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := pdDo
	t.Cleanup(func() { pdDo = orig })
	pdDo = func(_ context.Context, _, path, _ string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &pagerdutyAPIError{Status: status, msg: "err"}
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
		{"service bare", res("pagerduty_service", "a", map[string]any{"token": "PSVC1"}), "PSVC1"},
		{"service_integration DOT", res("pagerduty_service_integration", "a", map[string]any{"left": "PSVC1", "right": "PINT1"}), "PSVC1.PINT1"},
		{"escalation_policy bare", res("pagerduty_escalation_policy", "a", map[string]any{"token": "PEP1"}), "PEP1"},
		{"team_membership COLON user-first", res("pagerduty_team_membership", "a", map[string]any{"left": "PUSR1", "right": "PTEAM1"}), "PUSR1:PTEAM1"},
		{"user_contact_method COLON", res("pagerduty_user_contact_method", "a", map[string]any{"left": "PUSR1", "right": "PCM1"}), "PUSR1:PCM1"},
		{"user_notification_rule COLON", res("pagerduty_user_notification_rule", "a", map[string]any{"left": "PUSR1", "right": "PNR1"}), "PUSR1:PNR1"},
		{"ruleset_rule DOT", res("pagerduty_ruleset_rule", "a", map[string]any{"left": "PRS1", "right": "PRULE1"}), "PRS1.PRULE1"},
		{"tag bare", res("pagerduty_tag", "a", map[string]any{"token": "PTAG1"}), "PTAG1"},
		{"extension_servicenow bare", res("pagerduty_extension_servicenow", "a", map[string]any{"token": "PEXT1"}), "PEXT1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("pagerduty_service_integration", "a", map[string]any{"left": `${file("x")}`, "right": "PINT1"})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestPDAuthHeader(t *testing.T) {
	t.Setenv("PAGERDUTY_TOKEN", "abc123")
	if got := pdAuthHeader(); got != "Token token=abc123" {
		t.Errorf("auth header = %q, want Token token=abc123 (NOT Bearer)", got)
	}
}

func TestPDBase(t *testing.T) {
	t.Setenv("PAGERDUTY_API_URL", "")
	t.Setenv("PAGERDUTY_SERVICE_REGION", "")
	if got := pdBase(); got != pdBaseUS {
		t.Errorf("default = %q, want US", got)
	}
	t.Setenv("PAGERDUTY_SERVICE_REGION", "eu")
	if got := pdBase(); got != pdBaseEU {
		t.Errorf("region eu = %q, want EU", got)
	}
	t.Setenv("PAGERDUTY_SERVICE_REGION", "")
	t.Setenv("PAGERDUTY_API_URL", "http://proxy.internal/pd/") // http upgraded, trailing slash trimmed
	if got := pdBase(); got != "https://proxy.internal/pd" {
		t.Errorf("api_url = %q, want https://proxy.internal/pd", got)
	}
}

func TestExtensionNative(t *testing.T) {
	snow := pdExtension{ID: "PEXT1", ExtensionSchema: pdIDName{Summary: "ServiceNow (v7)"}}
	if got := extensionNative(snow); got != "pagerduty:extension_servicenow" {
		t.Errorf("ServiceNow schema → %q, want extension_servicenow", got)
	}
	generic := pdExtension{ID: "PEXT2", ExtensionSchema: pdIDName{Summary: "Generic V2 Webhook"}}
	if got := extensionNative(generic); got != "pagerduty:extension" {
		t.Errorf("generic schema → %q, want extension", got)
	}
}

func TestPDListPagedOffsetMore(t *testing.T) {
	fakePagerduty(t, func(path string) (string, int) {
		if strings.Contains(path, "offset=100") {
			return `{"escalation_policies":[{"id":"P2"}],"more":false}`, 200
		}
		return `{"escalation_policies":[{"id":"P1"}],"more":true}`, 200
	})
	got, err := pdListPaged[pdIDName](context.Background(), "/escalation_policies", "escalation_policies", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "P1" || got[1].ID != "P2" {
		t.Errorf("expected 2 across 2 pages via `more`; got %+v", got)
	}
}

func TestConnectResolvesAccount(t *testing.T) {
	fakePagerduty(t, func(path string) (string, int) { return `{"abilities":["sso"]}`, 200 })
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should validate via /abilities, got %v", err)
	}
	if run.Scope.Type != model.ScopeTenant || run.Scope.ID == "" {
		t.Errorf("scope = %+v, want a non-empty tenant id", run.Scope)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "tags", func() error { return &pagerdutyAPIError{Status: 403, msg: "scope absent"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "services", func() error { return &pagerdutyAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the include-integrations fan-out (DOT composite), the team/user/ruleset
// per-parent fan-outs (COLON + DOT composites), the extension discriminator, and the
// From-header-gated response plays (users listed first supply the email).
func TestEnumerateFanOutsAndComposites(t *testing.T) {
	fakePagerduty(t, func(path string) (string, int) {
		switch {
		case strings.HasPrefix(path, "/services"):
			return `{"services":[{"id":"PSVC1","name":"web","integrations":[{"id":"PINT1","name":"ig","summary":"Datadog"}]}],"more":false}`, 200
		case strings.HasPrefix(path, "/escalation_policies"):
			return `{"escalation_policies":[{"id":"PEP1","name":"ep"}],"more":false}`, 200
		case strings.HasPrefix(path, "/schedules"):
			return `{"schedules":[{"id":"PSCH1","name":"sch"}],"more":false}`, 200
		case strings.HasPrefix(path, "/teams/"): // members fan-out (before /teams)
			return `{"members":[{"user":{"id":"PUSR1","summary":"Jane"},"role":"manager"}],"more":false}`, 200
		case strings.HasPrefix(path, "/teams"):
			return `{"teams":[{"id":"PTEAM1","name":"team"}],"more":false}`, 200
		case strings.HasPrefix(path, "/users/") && strings.Contains(path, "contact_methods"):
			return `{"contact_methods":[{"id":"PCM1","summary":"email"}],"more":false}`, 200
		case strings.HasPrefix(path, "/users/") && strings.Contains(path, "notification_rules"):
			return `{"notification_rules":[{"id":"PNR1","summary":"rule"}],"more":false}`, 200
		case strings.HasPrefix(path, "/users"):
			return `{"users":[{"id":"PUSR1","name":"Jane","email":"jane@x.com"}],"more":false}`, 200
		case strings.HasPrefix(path, "/business_services"):
			return `{"business_services":[{"id":"PBS1","name":"bs"}],"more":false}`, 200
		case strings.HasPrefix(path, "/maintenance_windows"):
			return `{"maintenance_windows":[{"id":"PMW1","summary":"mw"}],"more":false}`, 200
		case strings.HasPrefix(path, "/extensions"):
			return `{"extensions":[{"id":"PEXT1","name":"snow","extension_schema":{"summary":"ServiceNow (v7)"}},{"id":"PEXT2","name":"generic","extension_schema":{"summary":"Generic V2 Webhook"}}],"more":false}`, 200
		case strings.HasPrefix(path, "/webhook_subscriptions"):
			return `{"webhook_subscriptions":[{"id":"PWH1","summary":"wh"}],"more":false}`, 200
		case strings.HasPrefix(path, "/tags"):
			return `{"tags":[{"id":"PTAG1","label":"prod"}],"more":false}`, 200
		case strings.HasPrefix(path, "/response_plays"):
			return `{"response_plays":[{"id":"PRP1","name":"play"}],"more":false}`, 200
		case strings.HasPrefix(path, "/rulesets/"): // rules fan-out (before /rulesets)
			return `{"rules":[{"id":"PRULE1"}],"more":false}`, 200
		case strings.HasPrefix(path, "/rulesets"):
			return `{"rulesets":[{"id":"PRS1","name":"rs"}],"more":false}`, 200
		}
		return `{"more":false}`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "acct"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// Composites with the right separators + parent order.
	if got := deriveImportID(mustRes(t, inv, "service_integration/PSVC1/PINT1")); got != "PSVC1.PINT1" {
		t.Errorf("service_integration import = %q, want PSVC1.PINT1 (dot)", got)
	}
	if got := deriveImportID(mustRes(t, inv, "team_membership/PUSR1/PTEAM1")); got != "PUSR1:PTEAM1" {
		t.Errorf("team_membership import = %q, want PUSR1:PTEAM1 (colon, user-first)", got)
	}
	if got := deriveImportID(mustRes(t, inv, "user_contact_method/PUSR1/PCM1")); got != "PUSR1:PCM1" {
		t.Errorf("contact_method import = %q, want PUSR1:PCM1", got)
	}
	if got := deriveImportID(mustRes(t, inv, "ruleset_rule/PRS1/PRULE1")); got != "PRS1.PRULE1" {
		t.Errorf("ruleset_rule import = %q, want PRS1.PRULE1 (dot)", got)
	}
	// Extension discriminator.
	if e := mustRes(t, inv, "extension/PEXT1"); e.TFType != "pagerduty_extension_servicenow" {
		t.Errorf("PEXT1 TFType = %q, want pagerduty_extension_servicenow", e.TFType)
	}
	if e := mustRes(t, inv, "extension/PEXT2"); e.TFType != "pagerduty_extension" {
		t.Errorf("PEXT2 TFType = %q, want pagerduty_extension", e.TFType)
	}
	// Response plays reached (From header supplied from the first user's email).
	mustRes(t, inv, "response_play/PRP1")

	// 1 svc + 1 svc_int + 1 ep + 1 sch + 1 team + 1 membership + 1 user + 1 cm + 1 nr +
	// 1 bs + 1 mw + 2 ext + 1 wh + 1 tag + 1 rp + 1 ruleset + 1 ruleset_rule = 18.
	if len(inv.Resources) != 18 {
		t.Errorf("expected 18 resources, got %d", len(inv.Resources))
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
