package newrelic

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

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func fakeNerdgraph(t *testing.T, fn func(query string, vars map[string]any) (json.RawMessage, error)) {
	t.Helper()
	orig := nerdgraph
	t.Cleanup(func() { nerdgraph = orig })
	nerdgraph = func(_ context.Context, query string, vars map[string]any) (json.RawMessage, error) {
		return fn(query, vars)
	}
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"dashboard guid", res("newrelic_one_dashboard", map[string]any{"guid": "GUID1"}), "GUID1"},
		{"alert_policy account SECOND (reversed)", res("newrelic_alert_policy", map[string]any{"policy_id": "123", "account_id": "456"}), "123:456"},
		{"nrql_condition 3-part, type lowercased", res("newrelic_nrql_alert_condition", map[string]any{"policy_id": "123", "condition_id": "789", "condition_type": "BASELINE"}), "123:789:baseline"},
		{"muting_rule account FIRST", res("newrelic_alert_muting_rule", map[string]any{"account_id": "456", "id": "55"}), "456:55"},
		{"workload 3-part account FIRST", res("newrelic_workload", map[string]any{"account_id": "456", "workload_id": "1456", "guid": "GUID2"}), "456:1456:GUID2"},
		{"synthetics script guid", res("newrelic_synthetics_script_monitor", map[string]any{"guid": "GUID3"}), "GUID3"},
		{"key_transaction guid", res("newrelic_key_transaction", map[string]any{"guid": "GUID4"}), "GUID4"},
		{"workflow bare id", res("newrelic_workflow", map[string]any{"id": "wf1"}), "wf1"},
		{"notif_channel bare id", res("newrelic_notification_channel", map[string]any{"id": "ch1"}), "ch1"},
		{"obfuscation bare numeric id", res("newrelic_obfuscation_rule", map[string]any{"id": "42"}), "42"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("newrelic_one_dashboard", map[string]any{"guid": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestNRBaseRegion(t *testing.T) {
	t.Setenv("NEW_RELIC_REGION", "")
	if nrBase() != nrBaseUS {
		t.Errorf("default region = %q, want US base", nrBase())
	}
	t.Setenv("NEW_RELIC_REGION", "EU")
	if nrBase() != nrBaseEU {
		t.Errorf("EU region = %q, want EU base", nrBase())
	}
	t.Setenv("NEW_RELIC_REGION", "eu") // case-insensitive
	if nrBase() != nrBaseEU {
		t.Errorf("lowercase eu = %q, want EU base", nrBase())
	}
}

func TestSyntheticsNativeSplit(t *testing.T) {
	cases := map[string]string{
		"SIMPLE":         "newrelic:synthetics_monitor",
		"BROWSER":        "newrelic:synthetics_monitor",
		"SCRIPT_API":     "newrelic:synthetics_script_monitor",
		"SCRIPT_BROWSER": "newrelic:synthetics_script_monitor",
		"CERT_CHECK":     "newrelic:synthetics_cert_check_monitor",
		"BROKEN_LINKS":   "newrelic:synthetics_broken_links_monitor",
		"STEP_MONITOR":   "newrelic:synthetics_step_monitor",
		"NONSENSE":       "",
	}
	for mt, want := range cases {
		if got := syntheticsNative(mt); got != want {
			t.Errorf("monitorType %q → %q, want %q", mt, got, want)
		}
	}
}

// nrPaged must follow nextCursor across pages and stop when it is empty.
func TestNRPagedFollowsCursor(t *testing.T) {
	fakeNerdgraph(t, func(query string, vars map[string]any) (json.RawMessage, error) {
		if vars["cursor"] == nil {
			return raw(`{"actor":{"entitySearch":{"results":{"entities":[{"guid":"A"}],"nextCursor":"CUR2"}}}}`), nil
		}
		return raw(`{"actor":{"entitySearch":{"results":{"entities":[{"guid":"B"},{"guid":"C"}],"nextCursor":""}}}}`), nil
	})
	got, err := nrPaged(context.Background(), qEntitySearch, map[string]any{"query": "x"}, extractEntities)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].GUID != "A" || got[2].GUID != "C" {
		t.Errorf("expected 3 entities across 2 cursor pages; got %+v", got)
	}
}

// The end-to-end enumerate exercises: the dashboard parent filter, the synthetics
// monitorType split, the workload workloadId follow-up + 3-part composite, and numeric-id
// decode (policy/condition/workload/obfuscation ids arrive as JSON numbers).
func TestEnumerateSplitsFiltersAndComposites(t *testing.T) {
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "456")
	fakeNerdgraph(t, func(query string, vars map[string]any) (json.RawMessage, error) {
		switch query {
		case qEntitySearch:
			q, _ := vars["query"].(string)
			switch {
			case strings.Contains(q, "DASHBOARD"):
				return raw(`{"actor":{"entitySearch":{"results":{"entities":[` +
					`{"guid":"D1","name":"dash","dashboardParentGuid":""},` +
					`{"guid":"D1P","name":"page","dashboardParentGuid":"D1"}` +
					`],"nextCursor":""}}}}`), nil
			case strings.Contains(q, "SYNTH"):
				return raw(`{"actor":{"entitySearch":{"results":{"entities":[` +
					`{"guid":"S1","name":"simple","monitorType":"SIMPLE"},` +
					`{"guid":"S2","name":"scripted","monitorType":"SCRIPT_API"},` +
					`{"guid":"S3","name":"cert","monitorType":"CERT_CHECK"}` +
					`],"nextCursor":""}}}}`), nil
			case strings.Contains(q, "WORKLOAD"):
				return raw(`{"actor":{"entitySearch":{"results":{"entities":[{"guid":"W1","name":"wl"}],"nextCursor":""}}}}`), nil
			}
			return raw(`{"actor":{"entitySearch":{"results":{"entities":[],"nextCursor":""}}}}`), nil
		case qWorkloadID:
			return raw(`{"actor":{"entity":{"workloadId":1456}}}`), nil
		case qAlertPolicies:
			return raw(`{"actor":{"account":{"alerts":{"policiesSearch":{"policies":[{"id":100,"name":"p"}],"nextCursor":""}}}}}`), nil
		case qNRQLConditions:
			return raw(`{"actor":{"account":{"alerts":{"nrqlConditionsSearch":{"nrqlConditions":[{"id":200,"name":"c","policyId":100,"type":"BASELINE"}],"nextCursor":""}}}}}`), nil
		case qMutingRules:
			return raw(`{"actor":{"account":{"alerts":{"mutingRules":[{"id":300,"name":"m"}]}}}}`), nil
		case qDestinations:
			return raw(`{"actor":{"account":{"aiNotifications":{"destinations":{"entities":[{"id":"d1","name":"dest","type":"WEBHOOK"}],"nextCursor":""}}}}}`), nil
		case qChannels:
			return raw(`{"actor":{"account":{"aiNotifications":{"channels":{"entities":[{"id":"c1","name":"chan","destinationId":"d1"}],"nextCursor":""}}}}}`), nil
		case qWorkflows:
			return raw(`{"actor":{"account":{"aiWorkflows":{"workflows":{"entities":[{"id":"wf1","name":"flow"}],"nextCursor":""}}}}}`), nil
		case qObfuscation:
			return raw(`{"actor":{"account":{"logConfigurations":{"obfuscationRules":[{"id":10,"name":"r"}],"obfuscationExpressions":[{"id":11,"name":"e"}]}}}}`), nil
		}
		return raw(`{}`), nil
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "456"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// Dashboard parent filter: D1 kept, D1P (page child) dropped.
	if _, ok := inv.Resources["dashboard/D1"]; !ok {
		t.Error("parent dashboard D1 should be adopted")
	}
	if _, ok := inv.Resources["dashboard/D1P"]; ok {
		t.Error("dashboard page child D1P must NOT be adopted (owned by parent)")
	}

	// Synthetics split by monitorType.
	assertType(t, inv, "synthetics/S1", "newrelic_synthetics_monitor")
	assertType(t, inv, "synthetics/S2", "newrelic_synthetics_script_monitor")
	assertType(t, inv, "synthetics/S3", "newrelic_synthetics_cert_check_monitor")

	// Workload 3-part composite, workloadId from the follow-up (numeric → "1456").
	wl := inv.Resources["workload/W1"]
	if wl == nil {
		t.Fatal("workload W1 should be adopted")
	}
	if got := deriveImportID(wl); got != "456:1456:W1" {
		t.Errorf("workload import id = %q, want 456:1456:W1", got)
	}

	// Numeric ids decoded and composed correctly.
	if got := deriveImportID(inv.Resources["alert_policy/100"]); got != "100:456" {
		t.Errorf("alert_policy import id = %q, want 100:456 (account SECOND)", got)
	}
	if got := deriveImportID(inv.Resources["nrql_condition/200"]); got != "100:200:baseline" {
		t.Errorf("nrql condition import id = %q, want 100:200:baseline", got)
	}
	if got := deriveImportID(inv.Resources["muting_rule/300"]); got != "456:300" {
		t.Errorf("muting rule import id = %q, want 456:300", got)
	}
	if got := deriveImportID(inv.Resources["obfuscation_rule/10"]); got != "10" {
		t.Errorf("obfuscation rule import id = %q, want 10", got)
	}

	// Sanity: expected total (1 dash + 3 synth + 1 wl + 1 policy + 1 cond + 1 mute +
	// 1 dest + 1 chan + 1 wf + 1 obf-rule + 1 obf-expr = 13; key_transaction empty).
	if len(inv.Resources) != 13 {
		t.Errorf("expected 13 resources, got %d", len(inv.Resources))
	}
}

func assertType(t *testing.T, inv *model.Inventory, id, want string) {
	t.Helper()
	r := inv.Resources[id]
	if r == nil {
		t.Errorf("%s missing from inventory", id)
		return
	}
	if r.TFType != want {
		t.Errorf("%s: TFType = %q, want %q", id, r.TFType, want)
	}
}

// A GraphQL UNAUTHORIZED/FORBIDDEN errorClass on a per-product list is a quiet skip.
func TestListSkipsForbiddenErrorClass(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "x", func() error { return &nerdgraphError{ErrorClass: "FORBIDDEN", msg: "no access"} })
	if fatal != nil {
		t.Errorf("FORBIDDEN errorClass should not be fatal, got %v", fatal)
	}
	if fails != 0 {
		t.Errorf("FORBIDDEN errorClass should be a quiet skip (fails=0), got %d", fails)
	}
}

// An HTTP 401 mid-enumeration (key revoked) is fatal.
func TestListHTTP401Fatal(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "x", func() error { return &nerdgraphError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("HTTP 401 during enumeration should be fatal")
	}
}

func TestConnectResolvesAccount(t *testing.T) {
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "456")
	fakeNerdgraph(t, func(query string, vars map[string]any) (json.RawMessage, error) {
		return raw(`{"actor":{"user":{"email":"me@example.com"},"account":{"name":"Acme"}}}`), nil
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should resolve the account, got %v", err)
	}
	if run.Scope.ID != "456" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want 456/tenant", run.Scope)
	}
	if ac.Identity != "Acme" {
		t.Errorf("identity = %q, want Acme", ac.Identity)
	}
}

// account resolving to null (with NO errors entry) is fatal — the key is valid but cannot
// see the requested account.
func TestConnectNullAccountFatal(t *testing.T) {
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "456")
	fakeNerdgraph(t, func(query string, vars map[string]any) (json.RawMessage, error) {
		return raw(`{"actor":{"user":{"email":"me@example.com"},"account":null}}`), nil
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err == nil {
		t.Error("a null account must be fatal (key valid but no access to the account)")
	}
}

// A key that resolves no user (e.g. a License/Ingest key, not a User key) is rejected.
func TestConnectRejectsNonUserKey(t *testing.T) {
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "456")
	fakeNerdgraph(t, func(query string, vars map[string]any) (json.RawMessage, error) {
		return raw(`{"actor":{"user":{"email":""},"account":{"name":"Acme"}}}`), nil
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err == nil {
		t.Error("connect should reject a key that resolves no User (not a User key)")
	}
}

func TestClientRefusesRedirects(t *testing.T) {
	if nrHTTPClient.CheckRedirect == nil {
		t.Fatal("nrHTTPClient must set CheckRedirect to refuse redirects")
	}
}

// Rate limiting (429) and transient 5xx/TIMEOUT are retryable; auth failures are not.
func TestNRRetryable(t *testing.T) {
	retry := []*nerdgraphError{
		{Status: 429},
		{Status: 500},
		{Status: 503},
		{ErrorClass: "TIMEOUT"},
		{ErrorClass: "SERVER_ERROR"},
	}
	for _, e := range retry {
		if !nrRetryable(e) {
			t.Errorf("expected retryable: %+v", e)
		}
	}
	noRetry := []*nerdgraphError{
		{Status: 401},
		{Status: 403},
		{Status: 400},
		{ErrorClass: "UNAUTHORIZED"},
		{ErrorClass: "FORBIDDEN"},
		{},
	}
	for _, e := range noRetry {
		if nrRetryable(e) {
			t.Errorf("expected NOT retryable: %+v", e)
		}
	}
}
