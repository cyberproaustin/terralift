package datadog

import (
	"context"
	"net/http"
	"net/url"
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
		{"monitor by id", res("datadog_monitor", map[string]any{"id": "12345"}), "12345"},
		{"dashboard by id", res("datadog_dashboard", map[string]any{"id": "abc-def-ghi"}), "abc-def-ghi"},
		{"synthetics by PUBLIC_ID", res("datadog_synthetics_test", map[string]any{"public_id": "abc-def-ghi"}), "abc-def-ghi"},
		{"logs_index by NAME", res("datadog_logs_index", map[string]any{"name": "main"}), "main"},
		{"logs_metric by id(name)", res("datadog_logs_metric", map[string]any{"id": "my.custom.metric"}), "my.custom.metric"},
		{"role by uuid", res("datadog_role", map[string]any{"id": "11111111-2222-3333-4444-555555555555"}), "11111111-2222-3333-4444-555555555555"},
		{"downtime by uuid", res("datadog_downtime_schedule", map[string]any{"id": "aaaa-bbbb"}), "aaaa-bbbb"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("datadog_logs_index", map[string]any{"name": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func fakeDatadog(t *testing.T, fn func(url string) (string, int)) {
	t.Helper()
	orig := datadogDo
	t.Cleanup(func() { datadogDo = orig })
	datadogDo = func(_ context.Context, _, url string) ([]byte, int, error) {
		body, status := fn(url)
		if status >= 400 {
			return []byte(body), status, &datadogAPIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

// v1 bare array, 0-based page/page_size — must page until a short page.
func TestListArrayPagedPaginates(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		if strings.Contains(url, "page=1") {
			return `[{"id":3,"name":"c"}]`, 200
		}
		// page 0 returns a full page (page_size=1000) → the pager must fetch page 1.
		var b strings.Builder
		b.WriteString("[")
		for i := 0; i < datadogPageSize; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":1,"name":"x"}`)
		}
		b.WriteString("]")
		return b.String(), 200
	})
	got, err := datadogListArrayPaged[ddMonitor](context.Background(), "/api/v1/monitor")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != datadogPageSize+1 {
		t.Errorf("expected %d items across 2 pages; got %d", datadogPageSize+1, len(got))
	}
}

// v1 keyed "data" (flat) with limit/offset — SLO.
func TestListKeyedOffsetPaginates(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		if strings.Contains(url, "offset=1000") {
			return `{"data":[{"id":"z","name":"last"}]}`, 200
		}
		var b strings.Builder
		b.WriteString(`{"data":[`)
		for i := 0; i < datadogPageSize; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":"a","name":"x"}`)
		}
		b.WriteString(`]}`)
		return b.String(), 200
	})
	got, err := datadogListKeyedOffset[ddSLO](context.Background(), "/api/v1/slo", "data")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != datadogPageSize+1 {
		t.Errorf("expected %d SLOs across 2 pages; got %d", datadogPageSize+1, len(got))
	}
}

// v2 JSON:API page[number]/page[size], bounded by meta.page.total_count.
func TestListJSONAPIPaginates(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		if strings.Contains(url, "page[number]=1") {
			return `{"data":[{"id":"r2","type":"roles","attributes":{"name":"two"}}],"meta":{"page":{"total_count":1001}}}`, 200
		}
		var b strings.Builder
		b.WriteString(`{"data":[`)
		for i := 0; i < datadogPageSize; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":"r1","type":"roles","attributes":{"name":"one"}}`)
		}
		b.WriteString(`],"meta":{"page":{"total_count":1001}}}`)
		return b.String(), 200
	})
	got, err := datadogListJSONAPI(context.Background(), "/api/v2/roles")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != datadogPageSize+1 {
		t.Errorf("expected %d roles across 2 pages; got %d", datadogPageSize+1, len(got))
	}
	if got[len(got)-1].attr("name") != "two" {
		t.Errorf("expected last role name=two; got %q", got[len(got)-1].attr("name"))
	}
}

// v2 JSON:API downtimes use the offset-style page[offset]/page[limit] quirk.
func TestListJSONAPIOffsetUsesOffsetParams(t *testing.T) {
	var sawOffset bool
	fakeDatadog(t, func(url string) (string, int) {
		if strings.Contains(url, "page[offset]=") && strings.Contains(url, "page[limit]=") {
			sawOffset = true
		}
		if strings.Contains(url, "page[number]") {
			t.Errorf("downtimes must NOT use page[number]: %s", url)
		}
		return `{"data":[{"id":"dt1","type":"downtime"}]}`, 200
	})
	got, err := datadogListJSONAPIOffset(context.Background(), "/api/v2/downtimes")
	if err != nil {
		t.Fatal(err)
	}
	if !sawOffset {
		t.Error("expected page[offset]/page[limit] params on the downtimes request")
	}
	if len(got) != 1 || got[0].ID != "dt1" {
		t.Errorf("expected 1 downtime dt1; got %+v", got)
	}
}

// A 403 on a resource list is best-effort skipped (feature/permission absent), not fatal.
func TestListBestEffortSkipsForbidden(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		return `{"errors":["forbidden"]}`, 403
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "roles", func() error {
		_, err := datadogListJSONAPI(context.Background(), "/api/v2/roles")
		return err
	})
	if fatal != nil {
		t.Errorf("403 should not be fatal, got %v", fatal)
	}
	if fails != 0 {
		t.Errorf("403 should be a quiet skip (fails=0), got %d", fails)
	}
}

// A 401 mid-enumeration is fatal — every remaining list would fail too.
func TestList401IsFatal(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		return `{"errors":["auth"]}`, 401
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "roles", func() error {
		_, err := datadogListJSONAPI(context.Background(), "/api/v2/roles")
		return err
	})
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// Notebook ids arrive as JSON NUMBERS (v1-shaped JSON:API); the flex ddID must decode
// them instead of failing the whole page (which silently dropped all notebooks).
func TestNotebookNumericIDDecodes(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		return `{"data":[{"id":123456,"type":"notebooks","attributes":{"name":"My NB"}}],"meta":{"page":{"total_filtered_count":1}}}`, 200
	})
	got, err := datadogListJSONAPIStartCount(context.Background(), "/api/v1/notebooks")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 notebook, got %d", len(got))
	}
	if got[0].id() != "123456" {
		t.Errorf("numeric id = %q, want 123456", got[0].id())
	}
	if got[0].attr("name") != "My NB" {
		t.Errorf("name = %q, want My NB", got[0].attr("name"))
	}
}

// /api/v2/security_monitoring/rules returns FLAT rule objects under `data` (no attributes
// wrapper); attr/attrBool must read id/name/isDefault from the item root so default rules
// are actually skipped.
func TestFlatItemAttrsReadFromRoot(t *testing.T) {
	body := `{"data":[` +
		`{"id":"rule-1","name":"custom","isDefault":false},` +
		`{"id":"rule-2","name":"builtin","isDefault":true}` +
		`],"meta":{"page":{"total_count":2}}}`
	fakeDatadog(t, func(url string) (string, int) { return body, 200 })
	got, err := datadogListJSONAPI(context.Background(), "/api/v2/security_monitoring/rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(got))
	}
	if got[0].id() != "rule-1" || got[0].attr("name") != "custom" {
		t.Errorf("flat id/name not read from root: id=%q name=%q", got[0].id(), got[0].attr("name"))
	}
	if got[0].attrBool("isDefault") {
		t.Error("rule-1 isDefault should be false")
	}
	if !got[1].attrBool("isDefault") {
		t.Error("rule-2 isDefault should be true (read from flat item root) — default rules would not be skipped otherwise")
	}
}

// The HTTP client must refuse redirects — the two secret headers must never be re-sent to
// a redirect target (Go does not strip custom headers on a cross-host 3xx).
func TestClientRefusesRedirects(t *testing.T) {
	u, _ := url.Parse("https://evil.example/x")
	if err := datadogHTTPClient.CheckRedirect(&http.Request{URL: u}, nil); err == nil {
		t.Error("datadogHTTPClient must refuse to follow redirects")
	}
}

// DD_HOST must never yield a plaintext http:// base (keys ride on headers); an explicit
// http:// is upgraded and a bare host is promoted to https://.
func TestDatadogBaseForcesHTTPS(t *testing.T) {
	t.Setenv("DATADOG_HOST", "")
	t.Setenv("DD_HOST", "http://api.datadoghq.eu")
	if b := datadogBase(); b != "https://api.datadoghq.eu" {
		t.Errorf("http:// base = %q, want https upgrade", b)
	}
	t.Setenv("DD_HOST", "datadoghq.eu")
	if b := datadogBase(); b != "https://datadoghq.eu" {
		t.Errorf("bare host = %q, want https://datadoghq.eu", b)
	}
	t.Setenv("DD_HOST", "")
	if b := datadogBase(); b != datadogDefaultBase {
		t.Errorf("unset = %q, want default %q", b, datadogDefaultBase)
	}
}

func TestConnectResolvesOrg(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		switch {
		case strings.Contains(url, "/api/v1/validate"):
			return `{"valid":true}`, 200
		case strings.Contains(url, "/api/v2/permissions"):
			return `{"data":[]}`, 200
		case strings.Contains(url, "/api/v1/org"):
			return `{"orgs":[{"public_id":"abc123","name":"Acme"}]}`, 200
		}
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should resolve the org, got %v", err)
	}
	if run.Scope.ID != "abc123" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want abc123/tenant", run.Scope)
	}
}

// A valid API key but a rejected app key must fail connect (the /validate blind spot).
func TestConnectRejectsBadAppKey(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		if strings.Contains(url, "/api/v1/validate") {
			return `{"valid":true}`, 200
		}
		if strings.Contains(url, "/api/v2/permissions") {
			return `{"errors":["forbidden"]}`, 403
		}
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err == nil {
		t.Error("connect should fail when the app key is rejected (403 on the app-key check)")
	}
}

// The org lookup falls back to the API host when /api/v1/org is not readable.
func TestConnectFallsBackToHost(t *testing.T) {
	fakeDatadog(t, func(url string) (string, int) {
		switch {
		case strings.Contains(url, "/api/v1/validate"):
			return `{"valid":true}`, 200
		case strings.Contains(url, "/api/v2/permissions"):
			return `{"data":[]}`, 200
		case strings.Contains(url, "/api/v1/org"):
			return `{"errors":["forbidden"]}`, 403
		}
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should succeed on host fallback, got %v", err)
	}
	if run.Scope.ID != "api.datadoghq.com" {
		t.Errorf("scope.ID = %q, want api.datadoghq.com (host fallback)", run.Scope.ID)
	}
}
