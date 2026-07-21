package logzio

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType, container string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: container, Properties: props}
}

func fakeLogzio(t *testing.T, fn func(method, path string) (string, int)) {
	t.Helper()
	orig := lzDo
	t.Cleanup(func() { lzDo = orig })
	lzDo = func(_ context.Context, method, path string, _ []byte) ([]byte, int, error) {
		body, status := fn(method, path)
		if status >= 400 {
			return []byte(body), status, &logzioAPIError{Status: status, msg: "err"}
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
		{"alert numeric id", res("logzio_alert_v2", "a", map[string]any{"token": "123"}), "123"},
		{"endpoint numeric id", res("logzio_endpoint", "a", map[string]any{"token": "456"}), "456"},
		{"drop_filter STRING id", res("logzio_drop_filter", "a", map[string]any{"token": "filter-hash-abc"}), "filter-hash-abc"},
		{"auth groups singleton", res("logzio_authentication_groups", "a", map[string]any{"token": "authentication_groups"}), "authentication_groups"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("logzio_alert_v2", "a", map[string]any{"token": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestLZBase(t *testing.T) {
	t.Setenv("LOGZIO_BASE_URL", "")
	t.Setenv("LOGZIO_REGION", "")
	if got := lzBase(); got != "https://api.logz.io" {
		t.Errorf("default = %q, want US api.logz.io", got)
	}
	t.Setenv("LOGZIO_REGION", "eu")
	if got := lzBase(); got != "https://api-eu.logz.io" {
		t.Errorf("eu = %q, want api-eu.logz.io", got)
	}
	t.Setenv("LOGZIO_REGION", "xx") // unknown → general rule api-<region>.logz.io
	if got := lzBase(); got != "https://api-xx.logz.io" {
		t.Errorf("unknown region = %q, want api-xx.logz.io", got)
	}
	t.Setenv("LOGZIO_BASE_URL", "")
	t.Setenv("LOGZIO_REGION", "eu/../evil.example.com") // charset guard → fall back to us
	if got := lzBase(); got != "https://api.logz.io" {
		t.Errorf("malformed region = %q, want us fallback https://api.logz.io", got)
	}
	t.Setenv("LOGZIO_REGION", "")
	t.Setenv("LOGZIO_BASE_URL", "http://custom.internal") // override + http upgraded
	if got := lzBase(); got != "https://custom.internal" {
		t.Errorf("base_url override = %q, want https://custom.internal", got)
	}
}

// decodeList tolerates the three GET-list shapes across endpoints.
func TestDecodeListShapes(t *testing.T) {
	// Bare array.
	got, err := decodeList[lzObj]("/p", []byte(`[{"id":1},{"id":2}]`))
	if err != nil || len(got) != 2 {
		t.Fatalf("bare array: got %+v err %v", got, err)
	}
	// Object wrapping one named array (beside scalar metadata).
	got, err = decodeList[lzObj]("/p", []byte(`{"endpoints":[{"id":3}],"total":1}`))
	if err != nil || len(got) != 1 || got[0].id() != "3" {
		t.Fatalf("named-array wrapper: got %+v err %v", got, err)
	}
	// Single resource object (settings singleton) → one element.
	got, err = decodeList[lzObj]("/p", []byte(`{"id":7,"name":"archive"}`))
	if err != nil || len(got) != 1 || got[0].id() != "7" {
		t.Fatalf("single object: got %+v err %v", got, err)
	}
	// Empty / null bodies → empty, no error.
	for _, body := range []string{``, `null`} {
		if got, err := decodeList[lzObj]("/p", []byte(body)); err != nil || len(got) != 0 {
			t.Errorf("body %q: got %+v err %v", body, got, err)
		}
	}
}

func TestLZIDFlexDecode(t *testing.T) {
	var n lzObj
	if err := json.Unmarshal([]byte(`{"id":123,"title":"num"}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.id() != "123" {
		t.Errorf("numeric id = %q, want 123", n.id())
	}
	var s lzObj
	if err := json.Unmarshal([]byte(`{"id":"df-hash","name":"str"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.id() != "df-hash" {
		t.Errorf("string id = %q, want df-hash", s.id())
	}
	// id/label field fallbacks: alertId + title, accountId + accountName.
	var a lzObj
	json.Unmarshal([]byte(`{"alertId":789,"title":"My Alert"}`), &a)
	if a.id() != "789" || a.label() != "My Alert" {
		t.Errorf("alert fallback: id=%q label=%q", a.id(), a.label())
	}
	var sa lzObj
	json.Unmarshal([]byte(`{"accountId":9,"accountName":"Sub1"}`), &sa)
	if sa.id() != "9" || sa.label() != "Sub1" {
		t.Errorf("subaccount fallback: id=%q label=%q", sa.id(), sa.label())
	}
}

func TestLZSearchEnvelope(t *testing.T) {
	fakeLogzio(t, func(method, path string) (string, int) {
		if method != "POST" {
			t.Errorf("search should POST, got %s", method)
		}
		return `{"results":[{"id":5,"name":"tok1"}],"total":1}`, 200
	})
	got, err := lzSearch[lzObj](context.Background(), "/v1/log-shipping/tokens/retrieve", "results")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].id() != "5" {
		t.Errorf("expected 1 token from the results envelope; got %+v", got)
	}
}

func TestConnectResolvesAccount(t *testing.T) {
	t.Setenv("LOGZIO_REGION", "")
	t.Setenv("LOGZIO_BASE_URL", "")
	fakeLogzio(t, func(method, path string) (string, int) { return `[]`, 200 })
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should validate via /v1/endpoints, got %v", err)
	}
	if run.Scope.ID != "api.logz.io" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want api.logz.io/tenant", run.Scope)
	}
	if ac.Identity != "api.logz.io" {
		t.Errorf("identity = %q, want api.logz.io", ac.Identity)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "metrics", func() error { return &logzioAPIError{Status: 403, msg: "plan absent"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "endpoints", func() error { return &logzioAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the mixed list shapes (GET bare-list, POST-search, singleton), the flex id/label,
// and the string-id drop_filter.
func TestEnumerateMixedShapes(t *testing.T) {
	fakeLogzio(t, func(method, path string) (string, int) {
		switch {
		case strings.Contains(path, "/v2/alerts"):
			return `[{"alertId":1,"title":"Alert1"}]`, 200
		case strings.Contains(path, "/v1/endpoints"):
			return `[{"id":2,"title":"Slack"}]`, 200
		case strings.Contains(path, "/v1/drop-filters"):
			return `[{"id":"df-hash","logType":"nginx"}]`, 200
		case strings.Contains(path, "time-based-accounts"):
			return `[{"accountId":3,"accountName":"Sub1"}]`, 200
		case strings.Contains(path, "metrics-accounts"):
			return `[{"accountId":8,"accountName":"Metrics1"}]`, 200
		case strings.Contains(path, "/v1/user-management/users"):
			return `[{"id":4,"username":"bob"}]`, 200
		case strings.Contains(path, "log-shipping/tokens/retrieve"):
			return `{"results":[{"id":5,"name":"tok1"}]}`, 200
		case strings.Contains(path, "/v1/s3-fetcher"):
			return `[{"id":6,"name":"elb-logs"}]`, 200
		case strings.Contains(path, "/v1/archive/settings"):
			return `[{"id":7}]`, 200
		case strings.Contains(path, "/v1/authentication-groups"):
			return `[{"group":"admins","userRole":"USER_ROLE_ACCOUNT_ADMIN"}]`, 200
		}
		return `[]`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "api.logz.io"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// GET bare-list (alerts) with alertId flex.
	if got := deriveImportID(mustRes(t, inv, "alert/1")); got != "1" {
		t.Errorf("alert import = %q, want 1", got)
	}
	// String-id drop_filter preserved.
	if got := deriveImportID(mustRes(t, inv, "drop_filter/df-hash")); got != "df-hash" {
		t.Errorf("drop_filter import = %q, want df-hash", got)
	}
	// POST-search reached the log-shipping token.
	mustRes(t, inv, "log_shipping_token/5")
	// Singleton auth-groups emitted.
	if got := deriveImportID(mustRes(t, inv, "authentication_groups")); got != "authentication_groups" {
		t.Errorf("auth-groups singleton import = %q", got)
	}
	// accountId/accountName fallbacks.
	if r := mustRes(t, inv, "sub_account/3"); r.Name != "Sub1" {
		t.Errorf("sub_account name = %q, want Sub1", r.Name)
	}

	// alert(1)+endpoint(1)+drop_filter(1)+sub_account(1)+user(1)+log_shipping_token(1)+
	// s3_fetcher(1)+archive(1)+metrics_account(1)+authentication_groups(1) = 10.
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
