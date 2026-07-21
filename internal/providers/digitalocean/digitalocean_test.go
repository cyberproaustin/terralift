package digitalocean

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
		{"droplet", res("digitalocean_droplet", map[string]any{"droplet_id": "123"}), "123"},
		{"domain", res("digitalocean_domain", map[string]any{"name": "example.com"}), "example.com"},
		{"record COMMA", res("digitalocean_record", map[string]any{"domain": "example.com", "record_id": "42"}), "example.com,42"},
		{"vpc", res("digitalocean_vpc", map[string]any{"vpc_id": "v-1"}), "v-1"},
		{"reserved_ip", res("digitalocean_reserved_ip", map[string]any{"ip": "1.2.3.4"}), "1.2.3.4"},
		{"certificate by NAME", res("digitalocean_certificate", map[string]any{"name": "mycert"}), "mycert"},
		{"container_registry by NAME", res("digitalocean_container_registry", map[string]any{"name": "myreg"}), "myreg"},
		{"k8s node pool BARE id", res("digitalocean_kubernetes_node_pool", map[string]any{"pool_id": "np-1"}), "np-1"},
		{"database_db COMMA", res("digitalocean_database_db", map[string]any{"cluster_id": "c-1", "name": "appdb"}), "c-1,appdb"},
		{"database_user COMMA", res("digitalocean_database_user", map[string]any{"cluster_id": "c-1", "name": "app"}), "c-1,app"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("digitalocean_domain", map[string]any{"name": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestExcludedReason(t *testing.T) {
	custom := &model.Resource{NativeType: "digitalocean:certificate", Properties: map[string]any{"cert_type": "custom"}}
	if excludedReason(custom) == "" {
		t.Error("custom certificate (write-only key) should be excluded")
	}
	le := &model.Resource{NativeType: "digitalocean:certificate", Properties: map[string]any{"cert_type": "lets_encrypt"}}
	if excludedReason(le) != "" {
		t.Error("lets_encrypt certificate should be adopted, not excluded")
	}
	if excludedReason(&model.Resource{NativeType: "digitalocean:droplet"}) != "" {
		t.Error("droplet should not be excluded")
	}
}

func TestDoListFollowsNextAndNestingKey(t *testing.T) {
	// DO nests the array under a per-endpoint key and paginates via links.pages.next.
	orig := doDo
	t.Cleanup(func() { doDo = orig })
	doDo = func(_ context.Context, _, url string) ([]byte, int, error) {
		// "?page=2" distinguishes page 2 (the first url is "...?per_page=200", which
		// contains the substring "page=2" inside "per_page" — hence the "?" anchor).
		if strings.Contains(url, "?page=2") {
			return []byte(`{"things":[{"id":"b"},{"id":"c"}]}`), 200, nil
		}
		return []byte(`{"things":[{"id":"a"}],"links":{"pages":{"next":"https://api.digitalocean.com/v2/things?page=2&per_page=200"}}}`), 200, nil
	}
	got, err := doList[doUUIDNamed](context.Background(), "/things", "things")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "a" || got[2].ID != "c" {
		t.Errorf("expected 3 flattened items a,b,c; got %+v", got)
	}
}

func TestConnectResolvesAccount(t *testing.T) {
	orig := doDo
	t.Cleanup(func() { doDo = orig })
	doDo = func(_ context.Context, _, url string) ([]byte, int, error) {
		return []byte(`{"account":{"uuid":"acct-uuid","email":"me@example.com","status":"active"}}`), 200, nil
	}
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should resolve the account, got %v", err)
	}
	if run.Scope.ID != "acct-uuid" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want acct-uuid/tenant", run.Scope)
	}
}

func TestHasTag(t *testing.T) {
	if !hasTag([]string{"a", "terraform:default-node-pool"}, "terraform:default-node-pool") {
		t.Error("hasTag should find the default-node-pool tag")
	}
	if hasTag([]string{"a"}, "terraform:default-node-pool") {
		t.Error("hasTag false positive")
	}
}
