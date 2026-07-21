package cloudflare

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Properties: props}
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"zone", res("cloudflare_zone", map[string]any{"zone_id": "z1"}), "z1"},
		{"zone_settings", res("cloudflare_zone_settings_override", map[string]any{"zone_id": "z1"}), "z1"},
		{"record", res("cloudflare_record", map[string]any{"zone_id": "z1", "record_id": "r1"}), "z1/r1"},
		{"page_rule", res("cloudflare_page_rule", map[string]any{"zone_id": "z1", "page_rule_id": "p1"}), "z1/p1"},
		{"ruleset zone", res("cloudflare_ruleset", map[string]any{"scope": "zone", "parent_id": "z1", "ruleset_id": "rs1"}), "zone/z1/rs1"},
		{"ruleset account", res("cloudflare_ruleset", map[string]any{"scope": "account", "parent_id": "a1", "ruleset_id": "rs1"}), "account/a1/rs1"},
		{"access_rule zone", res("cloudflare_access_rule", map[string]any{"scope": "zone", "parent_id": "z1", "rule_id": "ar1"}), "zone/z1/ar1"},
		{"lb_pool account-scoped", res("cloudflare_load_balancer_pool", map[string]any{"account_id": "a1", "pool_id": "pl1"}), "a1/pl1"},
		{"access_application NO prefix", res("cloudflare_access_application", map[string]any{"account_id": "a1", "app_id": "ap1"}), "a1/ap1"},
		{"access_policy WITH prefix", res("cloudflare_access_policy", map[string]any{"account_id": "a1", "app_id": "ap1", "policy_id": "po1"}), "account/a1/ap1/po1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	// Defense-in-depth: an id embedding a ${ } sequence must be neutralized.
	r := res("cloudflare_record", map[string]any{"zone_id": "z1", "record_id": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestExcludedReason(t *testing.T) {
	if excludedReason(&model.Resource{NativeType: "cloudflare:custom_ssl"}) == "" {
		t.Error("custom_ssl (write-only private key) should be excluded")
	}
	if excludedReason(&model.Resource{NativeType: "cloudflare:zone"}) != "" {
		t.Error("zone should not be excluded")
	}
}

// fakeCF substitutes cfDo, returning a canned envelope (result + total_pages) per
// the first path that matches.
func fakeCF(t *testing.T, pages map[string]cfEnvelope) {
	t.Helper()
	orig := cfDo
	t.Cleanup(func() { cfDo = orig })
	cfDo = func(_ context.Context, _, path string) (cfEnvelope, error) {
		for key, env := range pages {
			if strings.Contains(path, key) {
				return env, nil
			}
		}
		return cfEnvelope{Success: true}, nil
	}
}

func env(t *testing.T, v any, page, totalPages int) cfEnvelope {
	t.Helper()
	raw, _ := json.Marshal(v)
	e := cfEnvelope{Result: raw, Success: true}
	e.ResultInfo.Page = page
	e.ResultInfo.TotalPages = totalPages
	return e
}

func TestCfListPaginates(t *testing.T) {
	// Two pages; withPage appends page=N so we key on that.
	fakeCF(t, map[string]cfEnvelope{
		"page=1": env(t, []cfNamed{{ID: "a"}}, 1, 2),
		"page=2": env(t, []cfNamed{{ID: "b"}, {ID: "c"}}, 2, 2),
	})
	got, err := cfList[cfNamed](context.Background(), "/things")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "a" || got[2].ID != "c" {
		t.Errorf("expected 3 flattened items a,b,c; got %+v", got)
	}
}

func TestEnumZoneAccessRuleScope(t *testing.T) {
	// The zone access-rules endpoint returns inherited rules too; only a zone-owned
	// rule must be adopted (as zone-scoped), not account/organization rules.
	rules := []map[string]any{
		{"id": "r-zone", "scope": map[string]any{"type": "zone"}},
		{"id": "r-acct", "scope": map[string]any{"type": "account"}},
		{"id": "r-org", "scope": map[string]any{"type": "organization"}},
	}
	fakeCF(t, map[string]cfEnvelope{
		"/firewall/access_rules/rules": env(t, rules, 1, 1),
	})
	inv := &model.Inventory{Resources: map[string]*model.Resource{}}
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	enumZone(context.Background(), run, inv, "acct1", cfZone{ID: "z1", Name: "example.com"})

	got := inv.Resources["access_rule/r-zone"]
	if got == nil {
		t.Fatal("zone-owned access rule not adopted")
	}
	if got.Properties["scope"] != "zone" || got.Properties["parent_id"] != "z1" {
		t.Errorf("wrong scope/parent: %+v", got.Properties)
	}
	if inv.Resources["access_rule/r-acct"] != nil {
		t.Error("account-owned rule must not be adopted by the zone loop")
	}
	if inv.Resources["access_rule/r-org"] != nil {
		t.Error("organization rule must not be adopted")
	}
}

func TestConnectResolvesAndValidatesAccount(t *testing.T) {
	fakeCF(t, map[string]cfEnvelope{
		"/accounts": env(t, []cfAccount{{ID: "acct1", Name: "Prod"}}, 1, 1),
	})
	// Empty scope -> defaults to the sole account, tenant scope.
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect with a single account should resolve, got %v", err)
	}
	if run.Scope.ID != "acct1" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want acct1/tenant", run.Scope)
	}
	// An explicit scope the token cannot see is rejected.
	run2 := &core.Run{Scope: model.Scope{ID: "nope"}, Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run2); err == nil {
		t.Error("connect should reject an account not visible to the token")
	}
}
