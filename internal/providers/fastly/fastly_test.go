package fastly

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
		{"service_vcl bare", res("fastly_service_vcl", map[string]any{"service_id": "svc1"}), "svc1"},
		{"service_compute bare", res("fastly_service_compute", map[string]any{"service_id": "svc1"}), "svc1"},
		{"dictionary_items SLASH", res("fastly_service_dictionary_items", map[string]any{"service_id": "svc1", "dictionary_id": "d1"}), "svc1/d1"},
		{"acl_entries SLASH", res("fastly_service_acl_entries", map[string]any{"service_id": "svc1", "acl_id": "a1"}), "svc1/a1"},
		{"snippet SLASH", res("fastly_service_dynamic_snippet_content", map[string]any{"service_id": "svc1", "snippet_id": "s1"}), "svc1/s1"},
		{"tls_subscription bare", res("fastly_tls_subscription", map[string]any{"id": "t1"}), "t1"},
		{"user bare", res("fastly_user", map[string]any{"id": "u1"}), "u1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("fastly_service_vcl", map[string]any{"service_id": `${file("x")}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestExcludedReason(t *testing.T) {
	if excludedReason(&model.Resource{NativeType: "fastly:tls_private_key"}) == "" {
		t.Error("tls_private_key (write-only key_pem) should be excluded")
	}
	if excludedReason(&model.Resource{NativeType: "fastly:service_vcl"}) != "" {
		t.Error("service_vcl should not be excluded")
	}
}

func TestActiveVersion(t *testing.T) {
	active := fastlyService{Versions: []struct {
		Number int  `json:"number"`
		Active bool `json:"active"`
	}{{1, false}, {2, true}, {3, false}}}
	if got := activeVersion(active); got != 2 {
		t.Errorf("activeVersion = %d, want the active version 2", got)
	}
	latest := fastlyService{Versions: []struct {
		Number int  `json:"number"`
		Active bool `json:"active"`
	}{{1, false}, {2, false}}}
	if got := activeVersion(latest); got != 2 {
		t.Errorf("activeVersion = %d, want the latest (2) when none active", got)
	}
}

func fakeFastly(t *testing.T, fn func(url string) (string, int)) {
	t.Helper()
	orig := fastlyDo
	t.Cleanup(func() { fastlyDo = orig })
	fastlyDo = func(_ context.Context, _, url string) ([]byte, int, error) {
		body, status := fn(url)
		if status >= 400 {
			return []byte(body), status, &fastlyAPIError{Status: status, msg: "err"}
		}
		return []byte(body), status, nil
	}
}

func TestFastlyGetBareArray(t *testing.T) {
	fakeFastly(t, func(url string) (string, int) {
		return `[{"id":"a"},{"id":"b"}]`, 200
	})
	got, err := fastlyGet[fastlyItem](context.Background(), "/things")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("bare-array decode wrong: %+v", got)
	}
}

func TestFastlyListJSONAPIFollowsNext(t *testing.T) {
	fakeFastly(t, func(url string) (string, int) {
		if strings.Contains(url, "number]=2") || strings.Contains(url, "page=2") {
			return `{"data":[{"id":"b"},{"id":"c"}]}`, 200
		}
		return `{"data":[{"id":"a"}],"links":{"next":"https://api.fastly.com/tls/x?page[number]=2"}}`, 200
	})
	got, err := fastlyListJSONAPI[fastlyItem](context.Background(), "/tls/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "a" || got[2].ID != "c" {
		t.Errorf("JSON:API pagination wrong: %+v", got)
	}
}

func TestFastlyListJSONAPIRejectsForeignNext(t *testing.T) {
	fakeFastly(t, func(url string) (string, int) {
		return `{"data":[{"id":"a"}],"links":{"next":"https://evil.example/steal"}}`, 200
	})
	if _, err := fastlyListJSONAPI[fastlyItem](context.Background(), "/tls/x"); err == nil {
		t.Error("must refuse to follow a next-page url to a non-Fastly host")
	}
}

func TestConnectResolvesCustomer(t *testing.T) {
	fakeFastly(t, func(url string) (string, int) {
		return `{"id":"cust1","name":"Acme"}`, 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	if _, err := connect(context.Background(), run); err != nil {
		t.Fatalf("connect should resolve the customer, got %v", err)
	}
	if run.Scope.ID != "cust1" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want cust1/tenant", run.Scope)
	}
}
