package linode

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
		{"instance bare id", res("linode_instance", map[string]any{"id": "123"}), "123"},
		{"domain_record COMMA-2", res("linode_domain_record", map[string]any{"domain_id": "10", "record_id": "20"}), "10,20"},
		{"nb_config COMMA-2", res("linode_nodebalancer_config", map[string]any{"nodebalancer_id": "1", "config_id": "2"}), "1,2"},
		{"nb_node COMMA-3", res("linode_nodebalancer_node", map[string]any{"nodebalancer_id": "1", "config_id": "2", "node_id": "3"}), "1,2,3"},
		{"vpc_subnet COMMA-2", res("linode_vpc_subnet", map[string]any{"vpc_id": "5", "subnet_id": "6"}), "5,6"},
		{"bucket COLON", res("linode_object_storage_bucket", map[string]any{"region": "us-east", "label": "my-bucket"}), "us-east:my-bucket"},
		{"image private prefix", res("linode_image", map[string]any{"id": "private/12345"}), "private/12345"},
		{"rdns by address", res("linode_rdns", map[string]any{"address": "1.2.3.4"}), "1.2.3.4"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("linode_instance", map[string]any{"id": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func fakeLinode(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := linodeDo
	t.Cleanup(func() { linodeDo = orig })
	linodeDo = func(_ context.Context, _, path, _ string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &linodeAPIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

func TestLinodeListPaginates(t *testing.T) {
	fakeLinode(t, func(path string) (string, int) {
		if strings.Contains(path, "page=2") {
			return `{"data":[{"id":2}],"pages":2}`, 200
		}
		return `{"data":[{"id":1}],"pages":2}`, 200
	})
	got, err := linodeList[linodeObj](context.Background(), "/things", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("expected 2 items across 2 pages; got %+v", got)
	}
}

func TestEnumerateSkipsPublicImageAndDefaultRDNS(t *testing.T) {
	fakeLinode(t, func(path string) (string, int) {
		switch {
		case strings.Contains(path, "/images"):
			return `{"data":[{"id":"private/1","is_public":false},{"id":"linode/debian","is_public":true}],"pages":1}`, 200
		case strings.Contains(path, "/networking/ips"):
			return `{"data":[{"address":"1.2.3.4","rdns":"custom.example.com"},{"address":"5.6.7.8","rdns":"5-6-7-8.ip.linodeusercontent.com"}],"pages":1}`, 200
		default:
			return `{"data":[],"pages":1}`, 200
		}
	})
	run := &core.Run{Scope: model.Scope{ID: "acct"}, Log: core.NewLogger(core.ParseLevel("error"))}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Resources["image/private/1"] == nil {
		t.Error("private (account) image should be adopted")
	}
	if inv.Resources["image/linode/debian"] != nil {
		t.Error("public/distribution image must be skipped")
	}
	if inv.Resources["rdns/1.2.3.4"] == nil {
		t.Error("customized rDNS should be adopted")
	}
	if inv.Resources["rdns/5.6.7.8"] != nil {
		t.Error("default *.ip.linodeusercontent.com PTR must be skipped")
	}
}

func TestConnectResolvesAccount(t *testing.T) {
	fakeLinode(t, func(path string) (string, int) {
		if strings.Contains(path, "/account") {
			return `{"euuid":"acct-uuid","email":"me@example.com"}`, 200
		}
		return `{}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should resolve the account, got %v", err)
	}
	if run.Scope.ID != "acct-uuid" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want acct-uuid/tenant", run.Scope)
	}
}
