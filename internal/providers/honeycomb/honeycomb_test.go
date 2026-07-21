package honeycomb

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

func fakeHoneycomb(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := honeycombDo
	t.Cleanup(func() { honeycombDo = orig })
	honeycombDo = func(_ context.Context, _, path string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &honeycombAPIError{Status: status, msg: "err"}
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
		{"dataset bare slug", res("honeycombio_dataset", "env", map[string]any{"token": "my-dataset"}), "my-dataset"},
		{"column ds/key_name", res("honeycombio_column", "env", map[string]any{"dataset": "my-dataset", "token": "duration_ms"}), "my-dataset/duration_ms"},
		{"derived_column ds/alias", res("honeycombio_derived_column", "env", map[string]any{"dataset": "my-dataset", "token": "any_error"}), "my-dataset/any_error"},
		{"derived_column __all__ BARE", res("honeycombio_derived_column", "env", map[string]any{"dataset": "__all__", "token": "any_error"}), "any_error"},
		{"trigger ds/id", res("honeycombio_trigger", "env", map[string]any{"dataset": "my-dataset", "token": "trg1"}), "my-dataset/trg1"},
		{"trigger __all__ BARE", res("honeycombio_trigger", "env", map[string]any{"dataset": "__all__", "token": "trg1"}), "trg1"},
		{"slo ds/id", res("honeycombio_slo", "env", map[string]any{"dataset": "my-dataset", "token": "slo1"}), "my-dataset/slo1"},
		{"burn_alert ds/id", res("honeycombio_burn_alert", "env", map[string]any{"dataset": "my-dataset", "token": "ba1"}), "my-dataset/ba1"},
		{"query_annotation ds/id", res("honeycombio_query_annotation", "env", map[string]any{"dataset": "my-dataset", "token": "qa1"}), "my-dataset/qa1"},
		{"flexible_board bare", res("honeycombio_flexible_board", "env", map[string]any{"token": "AobW9oAZX71"}), "AobW9oAZX71"},
		{"email_recipient bare", res("honeycombio_email_recipient", "env", map[string]any{"token": "nx2zsegA0dZ"}), "nx2zsegA0dZ"},
		{"pagerduty_recipient bare", res("honeycombio_pagerduty_recipient", "env", map[string]any{"token": "r2"}), "r2"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("honeycombio_column", "env", map[string]any{"dataset": "my-dataset", "token": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestHoneycombBase(t *testing.T) {
	t.Setenv("HONEYCOMB_API_ENDPOINT", "")
	t.Setenv("HONEYCOMB_API_HOST", "")
	t.Setenv("HONEYCOMB_API_URL", "")
	if got := honeycombBase(); got != honeycombBaseUS {
		t.Errorf("default = %q, want US", got)
	}
	t.Setenv("HONEYCOMB_API_ENDPOINT", honeycombBaseEU)
	if got := honeycombBase(); got != honeycombBaseEU {
		t.Errorf("endpoint = %q, want EU", got)
	}
	t.Setenv("HONEYCOMB_API_ENDPOINT", "")
	t.Setenv("HONEYCOMB_API_URL", "https://api.honeycomb.io/") // Terraformer-era fallback, trailing slash trimmed
	if got := honeycombBase(); got != honeycombBaseUS {
		t.Errorf("URL fallback = %q, want %q", got, honeycombBaseUS)
	}
}

func TestRecipientNative(t *testing.T) {
	cases := map[string]string{
		"email":            "honeycomb:email_recipient",
		"pagerduty":        "honeycomb:pagerduty_recipient",
		"slack":            "honeycomb:slack_recipient",
		"webhook":          "honeycomb:webhook_recipient",
		"msteams":          "honeycomb:msteams_recipient",
		"msteams_workflow": "honeycomb:msteams_workflow_recipient",
		"nonsense":         "",
	}
	for typ, want := range cases {
		if got := recipientNative(typ); got != want {
			t.Errorf("type %q → %q, want %q", typ, got, want)
		}
	}
}

func TestConnectResolvesEnvironment(t *testing.T) {
	fakeHoneycomb(t, func(path string) (string, int) {
		return `{"environment":{"name":"Production","slug":"prod"},"team":{"name":"Acme","slug":"acme"},"api_key_access":{"boards":true}}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should resolve the environment, got %v", err)
	}
	if run.Scope.ID != "prod" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want prod/tenant", run.Scope)
	}
	if ac.Identity != "Production" {
		t.Errorf("identity = %q, want Production", ac.Identity)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "boards", func() error { return &honeycombAPIError{Status: 403, msg: "scope absent"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "datasets", func() error { return &honeycombAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the dataset fan-out, the second-level per-SLO burn-alert fan-out, the __all__
// env-wide pass (bare import), the classic-board skip, and the recipient type split.
func TestEnumerateFanOutAndAllVariants(t *testing.T) {
	fakeHoneycomb(t, func(path string) (string, int) {
		switch {
		case path == "/1/datasets":
			return `[{"name":"My Dataset","slug":"my-dataset"}]`, 200
		case strings.HasPrefix(path, "/1/columns/"):
			return `[{"key_name":"duration_ms"}]`, 200
		case strings.HasPrefix(path, "/1/query_annotations/"):
			return `[{"id":"qa1","name":"anno"}]`, 200
		case strings.HasPrefix(path, "/1/derived_columns/"):
			if strings.Contains(path, "__all__") {
				return `[{"alias":"env_wide_dc"}]`, 200
			}
			return `[{"alias":"any_error"}]`, 200
		case strings.HasPrefix(path, "/1/triggers/"):
			if strings.Contains(path, "__all__") {
				return `[{"id":"mdtrg","name":"md"}]`, 200
			}
			return `[{"id":"trg1","name":"trig"}]`, 200
		case strings.HasPrefix(path, "/1/slos/"):
			if strings.Contains(path, "__all__") {
				return `[]`, 200
			}
			return `[{"id":"slo1","name":"slo"}]`, 200
		case strings.HasPrefix(path, "/1/burn_alerts/"):
			return `[{"id":"ba1","name":"burn"}]`, 200
		case path == "/1/boards":
			return `[{"id":"brd1","name":"Board","type":"flexible"},{"id":"cls1","name":"Old","type":"classic"}]`, 200
		case path == "/1/recipients":
			return `[{"id":"rc1","type":"email"},{"id":"rc2","type":"pagerduty"},{"id":"rc3","type":"unknown"}]`, 200
		}
		return `[]`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "prod"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// __all__ env-wide variants import BARE.
	if got := deriveImportID(mustRes(t, inv, "derived_column/__all__/env_wide_dc")); got != "env_wide_dc" {
		t.Errorf("env-wide derived column import = %q, want bare env_wide_dc", got)
	}
	if got := deriveImportID(mustRes(t, inv, "trigger/__all__/mdtrg")); got != "mdtrg" {
		t.Errorf("MD trigger import = %q, want bare mdtrg", got)
	}
	// Real dataset-scoped resources carry the composite.
	if got := deriveImportID(mustRes(t, inv, "column/my-dataset/duration_ms")); got != "my-dataset/duration_ms" {
		t.Errorf("column import = %q, want my-dataset/duration_ms", got)
	}
	// Second-level per-SLO burn-alert fan-out worked.
	mustRes(t, inv, "burn_alert/my-dataset/ba1")
	// Classic board skipped; flexible kept as honeycombio_flexible_board.
	if _, ok := inv.Resources["board/cls1"]; ok {
		t.Error("classic board must be skipped")
	}
	if b := mustRes(t, inv, "board/brd1"); b.TFType != "honeycombio_flexible_board" {
		t.Errorf("board TFType = %q, want honeycombio_flexible_board", b.TFType)
	}
	// Recipient type split; unknown type skipped.
	if r := mustRes(t, inv, "recipient/rc2"); r.TFType != "honeycombio_pagerduty_recipient" {
		t.Errorf("rc2 TFType = %q, want honeycombio_pagerduty_recipient", r.TFType)
	}
	if _, ok := inv.Resources["recipient/rc3"]; ok {
		t.Error("unknown recipient type must be skipped")
	}

	// 1 dataset + 1 column + 1 qa + 1 dc(real) + 1 dc(__all__) + 1 trg(real) + 1 trg(md) +
	// 1 slo + 1 burn_alert + 1 board + 2 recipients = 12.
	if len(inv.Resources) != 12 {
		t.Errorf("expected 12 resources, got %d", len(inv.Resources))
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
