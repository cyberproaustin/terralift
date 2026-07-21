package opsgenie

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

func fakeOpsgenie(t *testing.T, fn func(url string) (string, int)) {
	t.Helper()
	orig := ogDo
	t.Cleanup(func() { ogDo = orig })
	ogDo = func(_ context.Context, _, url string) ([]byte, int, error) {
		body, status := fn(url)
		if status >= 400 {
			return []byte(body), status, &opsgenieAPIError{Status: status, msg: "err"}
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
		{"team bare", res("opsgenie_team", "a", map[string]any{"token": "t1"}), "t1"},
		{"user bare (id)", res("opsgenie_user", "a", map[string]any{"token": "u1"}), "u1"},
		{"heartbeat bare NAME", res("opsgenie_heartbeat", "a", map[string]any{"token": "my-heartbeat"}), "my-heartbeat"},
		{"team_routing_rule slash", res("opsgenie_team_routing_rule", "a", map[string]any{"left": "t1", "right": "rr1"}), "t1/rr1"},
		{"schedule_rotation slash", res("opsgenie_schedule_rotation", "a", map[string]any{"left": "s1", "right": "rot1"}), "s1/rot1"},
		{"service_incident_rule slash", res("opsgenie_service_incident_rule", "a", map[string]any{"left": "svc1", "right": "ir1"}), "svc1/ir1"},
		{"notification_rule slash (user_id parent)", res("opsgenie_notification_rule", "a", map[string]any{"left": "u1", "right": "nr1"}), "u1/nr1"},
		{"user_contact slash (USERNAME parent)", res("opsgenie_user_contact", "a", map[string]any{"left": "jane@corp.com", "right": "c1"}), "jane@corp.com/c1"},
		{"notification_policy slash", res("opsgenie_notification_policy", "a", map[string]any{"left": "t1", "right": "np1"}), "t1/np1"},
		{"alert_policy GLOBAL bare", res("opsgenie_alert_policy", "a", map[string]any{"token": "ap1", "team": ""}), "ap1"},
		{"alert_policy TEAM slash", res("opsgenie_alert_policy", "a", map[string]any{"token": "ap2", "team": "t1"}), "t1/ap2"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("opsgenie_user_contact", "a", map[string]any{"left": `${file("x")}`, "right": "c1"})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestOGAuthHeader(t *testing.T) {
	t.Setenv("OPSGENIE_API_KEY", "abc123")
	if got := ogAuthHeader(); got != "GenieKey abc123" {
		t.Errorf("auth header = %q, want GenieKey abc123 (NOT Bearer)", got)
	}
}

func TestOGBase(t *testing.T) {
	t.Setenv("OPSGENIE_API_URL", "")
	if got := ogBase(); got != ogBaseUS {
		t.Errorf("default = %q, want US", got)
	}
	t.Setenv("OPSGENIE_API_URL", "api.eu.opsgenie.com") // bare host, provider-style
	if got := ogBase(); got != ogBaseEU {
		t.Errorf("bare EU host = %q, want %q", got, ogBaseEU)
	}
	t.Setenv("OPSGENIE_API_URL", "http://proxy.local")
	if got := ogBase(); got != "https://proxy.local" {
		t.Errorf("http = %q, want https upgrade", got)
	}
}

func TestIsOpsgenieURL(t *testing.T) {
	t.Setenv("OPSGENIE_API_URL", "") // US base → host api.opsgenie.com
	if !isOpsgenieURL("https://api.opsgenie.com/v2/teams?offset=100") {
		t.Error("same-host https next url should be allowed")
	}
	if isOpsgenieURL("https://evil.example/v2/teams") {
		t.Error("cross-host next url must be refused")
	}
	if isOpsgenieURL("http://api.opsgenie.com/v2/teams") {
		t.Error("plaintext http next url must be refused")
	}
}

func TestOGListFollowsPagingNext(t *testing.T) {
	t.Setenv("OPSGENIE_API_URL", "")
	fakeOpsgenie(t, func(url string) (string, int) {
		if strings.Contains(url, "offset=100") {
			return `{"data":[{"id":"b","name":"B"}]}`, 200
		}
		return `{"data":[{"id":"a","name":"A"}],"paging":{"next":"https://api.opsgenie.com/v2/teams?limit=100&offset=100"}}`, 200
	})
	got, err := ogList[ogIDName](context.Background(), "/v2/teams")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("expected 2 across 2 pages via paging.next; got %+v", got)
	}
}

func TestOGListRefusesForeignNext(t *testing.T) {
	t.Setenv("OPSGENIE_API_URL", "")
	fakeOpsgenie(t, func(url string) (string, int) {
		return `{"data":[{"id":"a"}],"paging":{"next":"https://evil.example/steal?limit=100"}}`, 200
	})
	if _, err := ogList[ogIDName](context.Background(), "/v2/teams"); err == nil {
		t.Error("ogList must refuse to follow a paging.next pointing at a foreign host")
	}
}

func TestOGListHeartbeatsNestedEnvelope(t *testing.T) {
	fakeOpsgenie(t, func(url string) (string, int) {
		return `{"data":{"heartbeats":[{"name":"hb1"},{"name":"hb2"}]}}`, 200
	})
	hs, err := ogListHeartbeats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 || hs[0].Name != "hb1" {
		t.Errorf("expected 2 heartbeats from data.heartbeats; got %+v", hs)
	}
}

func TestConnectResolvesAccount(t *testing.T) {
	fakeOpsgenie(t, func(url string) (string, int) { return `{"data":{"name":"Acme"}}`, 200 })
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should resolve the account, got %v", err)
	}
	if run.Scope.ID != "Acme" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want Acme/tenant", run.Scope)
	}
	if ac.Identity != "Acme" {
		t.Errorf("identity = %q, want Acme", ac.Identity)
	}
}

// When /v2/account is gated (404), connect falls back to a /v2/users probe and uses the API
// host as the display id — a valid key must not be rejected just because /v2/account is gated.
func TestConnectFallsBackToUsersWhenAccountGated(t *testing.T) {
	t.Setenv("OPSGENIE_API_URL", "")
	fakeOpsgenie(t, func(url string) (string, int) {
		if strings.Contains(url, "/v2/account") {
			return `{"message":"not found"}`, 404
		}
		return `{"data":[]}`, 200 // /v2/users probe succeeds
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should succeed via the /v2/users fallback, got %v", err)
	}
	if run.Scope.ID != ogBaseHost() {
		t.Errorf("scope.ID = %q, want the API host %q (fallback)", run.Scope.ID, ogBaseHost())
	}
}

// A 401 on /v2/account is a real auth failure and must NOT be masked by the fallback.
func TestConnectRejectsBadKey(t *testing.T) {
	fakeOpsgenie(t, func(url string) (string, int) { return `{"message":"unauthorized"}`, 401 })
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err == nil {
		t.Error("connect should fail on a 401 (real auth failure), not fall back")
	}
}

func TestListSkips404AndFatal403(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "heartbeats", func() error { return &opsgenieAPIError{Status: 404, msg: "absent"} })
	if fatal != nil || fails != 0 {
		t.Errorf("404 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "teams", func() error { return &opsgenieAPIError{Status: 403, msg: "forbidden"} })
	if fatal == nil {
		t.Error("403 during enumeration should be fatal")
	}
}

// End-to-end: the fan-outs, the alert-policy global-vs-team flip, the two different per-user
// parents (username vs user_id), the integration type discriminator, and the nested-envelope
// heartbeat.
func TestEnumerateFanOutsPoliciesAndDiscriminator(t *testing.T) {
	t.Setenv("OPSGENIE_API_URL", "")
	fakeOpsgenie(t, func(url string) (string, int) {
		switch {
		case strings.Contains(url, "/v2/teams/"): // routing rules (before /v2/teams)
			return `{"data":[{"id":"rr1","name":"rr"}]}`, 200
		case strings.Contains(url, "/v2/teams"):
			return `{"data":[{"id":"t1","name":"Team"}]}`, 200
		case strings.Contains(url, "/v2/policies/alert") && strings.Contains(url, "teamId"):
			return `{"data":[{"id":"ap2","name":"tp","teamId":"t1"}]}`, 200
		case strings.Contains(url, "/v2/policies/alert"):
			return `{"data":[{"id":"ap1","name":"gp"}]}`, 200 // global (no teamId)
		case strings.Contains(url, "/v2/policies/notification"):
			return `{"data":[{"id":"np1","name":"np"}]}`, 200
		case strings.Contains(url, "/v2/users/") && strings.Contains(url, "contacts"):
			return `{"data":[{"id":"c1"}]}`, 200
		case strings.Contains(url, "/v2/users/") && strings.Contains(url, "notification-rules"):
			return `{"data":[{"id":"nr1","name":"nr"}]}`, 200
		case strings.Contains(url, "/v2/users"):
			return `{"data":[{"id":"u1","username":"jane@corp.com","fullName":"Jane"}]}`, 200
		case strings.Contains(url, "/v2/schedules"):
			return `{"data":[{"id":"s1","name":"sch","rotations":[{"id":"rot1","name":"rot"}]}]}`, 200
		case strings.Contains(url, "/v2/escalations"):
			return `{"data":[{"id":"e1","name":"esc"}]}`, 200
		case strings.Contains(url, "/v1/services/"): // incident rules (before /v2/services)
			return `{"data":[{"id":"ir1","name":"ir"}]}`, 200
		case strings.Contains(url, "/v2/services"):
			return `{"data":[{"id":"svc1","name":"svc"}]}`, 200
		case strings.Contains(url, "/v2/integrations"):
			return `{"data":[{"id":"api1","name":"apiint","type":"API"},{"id":"em1","name":"emint","type":"Email"},{"id":"v1","name":"dd","type":"Datadog"}]}`, 200
		case strings.Contains(url, "/v2/maintenance"):
			return `{"data":[{"id":"m1","name":"maint"}]}`, 200
		case strings.Contains(url, "/v2/heartbeats"):
			return `{"data":{"heartbeats":[{"name":"hb1"}]}}`, 200
		}
		return `{"data":[]}`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "acct"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// Alert policy: global bare vs team-scoped slash.
	if got := deriveImportID(mustRes(t, inv, "alert_policy/ap1")); got != "ap1" {
		t.Errorf("global alert policy import = %q, want bare ap1", got)
	}
	if got := deriveImportID(mustRes(t, inv, "alert_policy/ap2")); got != "t1/ap2" {
		t.Errorf("team alert policy import = %q, want t1/ap2", got)
	}
	// The two per-user parents: contact keys on username, notification_rule on user_id.
	if got := deriveImportID(mustRes(t, inv, "user_contact/u1/c1")); got != "jane@corp.com/c1" {
		t.Errorf("user_contact import = %q, want jane@corp.com/c1 (username parent)", got)
	}
	if got := deriveImportID(mustRes(t, inv, "notification_rule/u1/nr1")); got != "u1/nr1" {
		t.Errorf("notification_rule import = %q, want u1/nr1 (user_id parent)", got)
	}
	// schedule_rotation from the embedded rotations.
	if got := deriveImportID(mustRes(t, inv, "schedule_rotation/s1/rot1")); got != "s1/rot1" {
		t.Errorf("schedule_rotation import = %q, want s1/rot1", got)
	}
	// Integration discriminator; vendor type skipped.
	if r := mustRes(t, inv, "integration/api1"); r.TFType != "opsgenie_api_integration" {
		t.Errorf("api1 TFType = %q, want opsgenie_api_integration", r.TFType)
	}
	if r := mustRes(t, inv, "integration/em1"); r.TFType != "opsgenie_email_integration" {
		t.Errorf("em1 TFType = %q, want opsgenie_email_integration", r.TFType)
	}
	if _, ok := inv.Resources["integration/v1"]; ok {
		t.Error("vendor (Datadog) integration must be skipped")
	}
	// heartbeat by name from the nested envelope.
	if got := deriveImportID(mustRes(t, inv, "heartbeat/hb1")); got != "hb1" {
		t.Errorf("heartbeat import = %q, want hb1", got)
	}

	// team(1)+routing_rule(1)+ap1(1)+ap2(1)+np1(1)+user(1)+contact(1)+notif_rule(1)+
	// schedule(1)+rotation(1)+escalation(1)+service(1)+incident_rule(1)+api_int(1)+
	// email_int(1)+maintenance(1)+heartbeat(1) = 17.
	if len(inv.Resources) != 17 {
		t.Errorf("expected 17 resources, got %d", len(inv.Resources))
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
