package ns1

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
		{"zone by name", res("ns1_zone", map[string]any{"zone": "example.com"}), "example.com"},
		{"record 3-part slash", res("ns1_record", map[string]any{"zone": "example.com", "domain": "www.example.com", "type": "CNAME"}), "example.com/www.example.com/CNAME"},
		{"datafeed slash", res("ns1_datafeed", map[string]any{"datasource_id": "src1", "datafeed_id": "feed1"}), "src1/feed1"},
		{"user by username", res("ns1_user", map[string]any{"username": "alice"}), "alice"},
		{"tsigkey by name", res("ns1_tsigkey", map[string]any{"name": "key1"}), "key1"},
		{"monitoringjob bare id", res("ns1_monitoringjob", map[string]any{"id": "mj1"}), "mj1"},
		{"team bare id", res("ns1_team", map[string]any{"id": "t1"}), "t1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("ns1_zone", map[string]any{"zone": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestExcludedReason(t *testing.T) {
	if excludedReason(&model.Resource{NativeType: "ns1:tsigkey"}) == "" {
		t.Error("tsigkey (write-only secret) should be excluded")
	}
	if excludedReason(&model.Resource{NativeType: "ns1:apikey"}) == "" {
		t.Error("apikey (credential material) should be excluded")
	}
	if excludedReason(&model.Resource{NativeType: "ns1:zone"}) != "" {
		t.Error("zone should not be excluded")
	}
}

func fakeNS1(t *testing.T, fn func(path string) (string, int)) {
	t.Helper()
	orig := ns1Do
	t.Cleanup(func() { ns1Do = orig })
	ns1Do = func(_ context.Context, _, path string) ([]byte, int, error) {
		body, status := fn(path)
		if status >= 400 {
			return []byte(body), status, &ns1APIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

func TestEnumerateSkipsLinkedZoneRecords(t *testing.T) {
	fakeNS1(t, func(path string) (string, int) {
		switch {
		case path == "/zones":
			return `[{"zone":"a.com"},{"zone":"b.com"}]`, 200
		case path == "/zones/a.com":
			return `{"zone":"a.com","records":[{"domain":"www.a.com","type":"A"}]}`, 200
		case path == "/zones/b.com":
			return `{"zone":"b.com","link":"a.com"}`, 200 // linked zone — records managed on the primary
		default:
			return `[]`, 200
		}
	})
	run := &core.Run{Scope: model.Scope{ID: ns1Container}, Log: core.NewLogger(core.ParseLevel("error"))}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Resources["zone/a.com"] == nil || inv.Resources["zone/b.com"] == nil {
		t.Fatal("both zones should be adopted")
	}
	if inv.Resources["record/a.com/www.a.com/A"] == nil {
		t.Error("normal zone's record should be adopted")
	}
	for id := range inv.Resources {
		if strings.HasPrefix(id, "record/b.com/") {
			t.Errorf("linked zone b.com must not contribute records, found %q", id)
		}
	}
}

func TestConnectValidates(t *testing.T) {
	fakeNS1(t, func(path string) (string, int) {
		return `[]`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should succeed on a valid key, got %v", err)
	}
	if run.Scope.ID != ns1Container || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want %s/tenant", run.Scope, ns1Container)
	}
}
