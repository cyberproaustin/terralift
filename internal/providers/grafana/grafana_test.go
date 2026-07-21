package grafana

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType, container string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: container, Properties: props}
}

func fakeGrafana(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := grafanaDo
	t.Cleanup(func() { grafanaDo = orig })
	grafanaDo = func(_ context.Context, _, path string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &grafanaAPIError{Status: status, msg: "err"}
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
		{"dashboard orgID:uid", res("grafana_dashboard", "1", map[string]any{"token": "dash1"}), "1:dash1"},
		{"folder orgID:uid", res("grafana_folder", "1", map[string]any{"token": "f1"}), "1:f1"},
		{"data_source orgID:uid", res("grafana_data_source", "1", map[string]any{"token": "d1"}), "1:d1"},
		{"rule_group 3-part", res("grafana_rule_group", "1", map[string]any{"folder_uid": "f1", "title": "g1"}), "1:f1:g1"},
		{"notification_policy singleton", res("grafana_notification_policy", "1", map[string]any{}), "1:policy"},
		{"contact_point by name", res("grafana_contact_point", "1", map[string]any{"name": "oncall"}), "1:oncall"},
		{"message_template by name", res("grafana_message_template", "1", map[string]any{"name": "tmpl"}), "1:tmpl"},
		{"mute_timing by name", res("grafana_mute_timing", "1", map[string]any{"name": "weekends"}), "1:weekends"},
		{"team numeric id", res("grafana_team", "1", map[string]any{"token": "10"}), "1:10"},
		{"service_account numeric id", res("grafana_service_account", "1", map[string]any{"token": "20"}), "1:20"},
		{"playlist orgID:uid", res("grafana_playlist", "1", map[string]any{"token": "p1"}), "1:p1"},
		{"library_panel orgID:uid", res("grafana_library_panel", "1", map[string]any{"token": "lp1"}), "1:lp1"},
		{"role orgID:uid", res("grafana_role", "1", map[string]any{"token": "custom1"}), "1:custom1"},
		{"report numeric id", res("grafana_report", "1", map[string]any{"token": "30"}), "1:30"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	// A rule-group title (free text) can carry template markers; the finished composite must
	// be escaped.
	r := res("grafana_rule_group", "1", map[string]any{"folder_uid": "f1", "title": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestValidateGrafanaURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://myorg.grafana.net", true},
		{"http://grafana.local:3000", true},
		{"https://grafana.mycorp.internal/grafana", true}, // sub-path allowed
		{"", false},
		{"ftp://grafana.local", false},        // bad scheme
		{"https://grafana.local?x=1", false},  // query rejected
		{"https://grafana.local#frag", false}, // fragment rejected
		{"https://", false},                   // no host
	}
	for _, c := range cases {
		t.Setenv("GRAFANA_URL", c.url)
		err := validateGrafanaURL()
		if c.ok && err != nil {
			t.Errorf("%q should be valid, got %v", c.url, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%q should be invalid", c.url)
		}
	}
}

func TestGrafanaBaseStripsQueryAndSlash(t *testing.T) {
	t.Setenv("GRAFANA_URL", "https://g.example.com/grafana/?x=1#f")
	if got := grafanaBase(); got != "https://g.example.com/grafana" {
		t.Errorf("base = %q, want https://g.example.com/grafana", got)
	}
}

func TestGrafanaAuthHeader(t *testing.T) {
	t.Setenv("GRAFANA_AUTH", "glsa_sometoken")
	if h, ok := grafanaAuthHeader(); !ok || h != "Bearer glsa_sometoken" {
		t.Errorf("token → %q,%v; want Bearer glsa_sometoken", h, ok)
	}
	t.Setenv("GRAFANA_AUTH", "admin:s3cret")
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:s3cret"))
	if h, ok := grafanaAuthHeader(); !ok || h != want {
		t.Errorf("basic → %q,%v; want %q", h, ok, want)
	}
	t.Setenv("GRAFANA_AUTH", "")
	if _, ok := grafanaAuthHeader(); ok {
		t.Error("empty GRAFANA_AUTH should yield no header")
	}
}

func TestListArrayPagedPaginates(t *testing.T) {
	fakeGrafana(t, func(path string) (string, int) {
		if strings.Contains(path, "page=2") {
			return `[{"uid":"z","title":"last"}]`, 200
		}
		var b strings.Builder
		b.WriteString("[")
		for i := 0; i < grafanaPerPage; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"uid":"x","title":"t"}`)
		}
		b.WriteString("]")
		return b.String(), 200
	})
	got, err := grafanaListArrayPaged[grafanaFolder](context.Background(), "/api/folders", grafanaPerPage)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != grafanaPerPage+1 {
		t.Errorf("expected %d folders across 2 pages, got %d", grafanaPerPage+1, len(got))
	}
}

func TestListKeyedPagedTotalCount(t *testing.T) {
	fakeGrafana(t, func(path string) (string, int) {
		return `{"teams":[{"id":1,"name":"a"},{"id":2,"name":"b"}],"totalCount":2}`, 200
	})
	got, err := grafanaListKeyedPaged[grafanaIDName](context.Background(), "/api/teams/search", "teams", "perpage", grafanaPerPage)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 teams (bounded by totalCount), got %d", len(got))
	}
}

func TestConnectResolvesOrg(t *testing.T) {
	fakeGrafana(t, func(path string) (string, int) {
		return `{"id":7,"name":"Main Org."}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should resolve the org, got %v", err)
	}
	if run.Scope.ID != "7" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want 7/tenant", run.Scope)
	}
	if ac.Identity != "Main Org." {
		t.Errorf("identity = %q, want Main Org.", ac.Identity)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "reports", func() error { return &grafanaAPIError{Status: 403, msg: "enterprise only"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "folders", func() error { return &grafanaAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: contact-point name dedup, rule-group grouping by (folderUID, ruleGroup), the
// General-folder skip, the fixed/global-role skip, and the singleton notification policy.
func TestEnumerateDedupGroupingAndSkips(t *testing.T) {
	fakeGrafana(t, func(path string) (string, int) {
		switch {
		case strings.HasPrefix(path, "/api/search"):
			return `[{"uid":"dash1","title":"Dash","type":"dash-db"}]`, 200
		case strings.HasPrefix(path, "/api/folders"):
			return `[{"uid":"f1","title":"Team"},{"uid":"","title":"General"}]`, 200
		case strings.HasPrefix(path, "/api/datasources"):
			return `[{"uid":"d1","name":"prom","type":"prometheus"}]`, 200
		case strings.HasPrefix(path, "/api/v1/provisioning/contact-points"):
			return `[{"name":"oncall"},{"name":"oncall"},{"name":"email"}]`, 200
		case strings.HasPrefix(path, "/api/v1/provisioning/policies"):
			return `{"receiver":"oncall","routes":[{}]}`, 200
		case strings.HasPrefix(path, "/api/v1/provisioning/templates"):
			return `[{"name":"tmpl1"}]`, 200
		case strings.HasPrefix(path, "/api/v1/provisioning/mute-timings"):
			return `[{"name":"weekends"}]`, 200
		case strings.HasPrefix(path, "/api/v1/provisioning/alert-rules"):
			return `[{"uid":"r1","title":"a","folderUID":"f1","ruleGroup":"g1"},{"uid":"r2","title":"b","folderUID":"f1","ruleGroup":"g1"},{"uid":"r3","title":"c","folderUID":"f2","ruleGroup":"g2"}]`, 200
		case strings.HasPrefix(path, "/api/teams/search"):
			return `{"teams":[{"id":10,"name":"t"}],"totalCount":1}`, 200
		case strings.HasPrefix(path, "/api/serviceaccounts/search"):
			return `{"serviceAccounts":[{"id":20,"name":"sa"}],"totalCount":1}`, 200
		case strings.HasPrefix(path, "/api/playlists"):
			return `[{"uid":"p1","name":"pl"}]`, 200
		case strings.HasPrefix(path, "/api/library-elements"):
			return `{"result":{"elements":[{"uid":"lp1","name":"panel"}],"totalCount":1}}`, 200
		case strings.HasPrefix(path, "/api/access-control/roles"):
			return `[{"uid":"custom1","name":"Custom","global":false},{"uid":"fixed:dashboards:reader","name":"fixed","global":true}]`, 200
		case strings.HasPrefix(path, "/api/reports"):
			return `[{"id":30,"name":"rpt"}]`, 200
		}
		return `[]`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "1"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// Contact points deduped by name: oncall + email = 2 (not 3).
	if _, ok := inv.Resources["contact_point/oncall"]; !ok {
		t.Error("contact_point/oncall missing")
	}
	if _, ok := inv.Resources["contact_point/email"]; !ok {
		t.Error("contact_point/email missing")
	}
	// Rule groups: 2 distinct (f1/g1, f2/g2).
	rg := 0
	for id := range inv.Resources {
		if strings.HasPrefix(id, "rule_group/") {
			rg++
		}
	}
	if rg != 2 {
		t.Errorf("expected 2 rule groups, got %d", rg)
	}
	// General folder skipped.
	if _, ok := inv.Resources["folder/f1"]; !ok {
		t.Error("folder/f1 should be adopted")
	}
	for id := range inv.Resources {
		if id == "folder/" {
			t.Error("General folder (empty uid) must be skipped")
		}
	}
	// Fixed/global role skipped; custom kept.
	if _, ok := inv.Resources["role/custom1"]; !ok {
		t.Error("custom role should be adopted")
	}
	if _, ok := inv.Resources["role/fixed:dashboards:reader"]; ok {
		t.Error("fixed/global role must be skipped")
	}
	// Singleton notification policy emitted.
	if _, ok := inv.Resources["notification_policy"]; !ok {
		t.Error("notification_policy singleton should be emitted")
	}

	// Composite import IDs use the org id from the scope (Container).
	if got := deriveImportID(inv.Resources["dashboard/dash1"]); got != "1:dash1" {
		t.Errorf("dashboard import id = %q, want 1:dash1", got)
	}
	if got := deriveImportID(inv.Resources["team/10"]); got != "1:10" {
		t.Errorf("team import id = %q, want 1:10", got)
	}
	if got := deriveImportID(inv.Resources["notification_policy"]); got != "1:policy" {
		t.Errorf("policy import id = %q, want 1:policy", got)
	}

	// 1 dash + 1 folder + 1 ds + 2 cp + 1 policy + 1 tmpl + 1 mute + 2 rg + 1 team +
	// 1 sa + 1 playlist + 1 lib + 1 role + 1 report = 16.
	if len(inv.Resources) != 16 {
		t.Errorf("expected 16 resources, got %d", len(inv.Resources))
	}
}
