package vultr

import (
	"context"
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
		{"instance bare uuid", res("vultr_instance", map[string]any{"id": "uuid-1"}), "uuid-1"},
		{"dns_domain by NAME", res("vultr_dns_domain", map[string]any{"domain": "example.com"}), "example.com"},
		{"dns_record COMMA", res("vultr_dns_record", map[string]any{"domain": "example.com", "record_id": "rec-uuid"}), "example.com,rec-uuid"},
		{"firewall_rule COMMA int", res("vultr_firewall_rule", map[string]any{"firewall_group_id": "grp-uuid", "rule_id": "1"}), "grp-uuid,1"},
		{"node_pools SPACE", res("vultr_kubernetes_node_pools", map[string]any{"cluster_id": "c-uuid", "pool_id": "p-uuid"}), "c-uuid p-uuid"},
		{"vpc2 bare uuid", res("vultr_vpc2", map[string]any{"id": "v2-uuid"}), "v2-uuid"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("vultr_instance", map[string]any{"id": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func fakeVultr(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := vultrDo
	t.Cleanup(func() { vultrDo = orig })
	vultrDo = func(_ context.Context, _, path string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &vultrAPIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

func TestVultrListCursorPaginates(t *testing.T) {
	fakeVultr(t, func(path string) (string, int) {
		if strings.Contains(path, "cursor=") {
			return `{"things":[{"id":"b"},{"id":"c"}],"meta":{"links":{"next":""}}}`, 200
		}
		return `{"things":[{"id":"a"}],"meta":{"links":{"next":"CURSOR2"}}}`, 200
	})
	got, err := vultrList[vultrObj](context.Background(), "/things", "things")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "a" || got[2].ID != "c" {
		t.Errorf("expected 3 items across 2 cursor pages; got %+v", got)
	}
}

func TestConnectResolvesAccount(t *testing.T) {
	fakeVultr(t, func(path string) (string, int) {
		return `{"account":{"name":"Acme","email":"me@example.com"}}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should resolve the account, got %v", err)
	}
	// Vultr accounts have no uuid — the email is the container id.
	if run.Scope.ID != "me@example.com" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want me@example.com/tenant", run.Scope)
	}
}
