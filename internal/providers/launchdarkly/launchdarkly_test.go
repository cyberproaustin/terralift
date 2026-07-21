package launchdarkly

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

func fakeLD(t *testing.T, fn func(url string) (string, int)) {
	t.Helper()
	orig := ldDo
	t.Cleanup(func() { ldDo = orig })
	ldDo = func(_ context.Context, _, url string) ([]byte, int, error) {
		body, status := fn(url)
		if status >= 400 {
			return []byte(body), status, &launchdarklyAPIError{Status: status, msg: "err"}
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
		{"project bare", res("launchdarkly_project", "a", map[string]any{"token": "proj1"}), "proj1"},
		{"webhook bare _id", res("launchdarkly_webhook", "a", map[string]any{"token": "wh_abc"}), "wh_abc"},
		{"team bare", res("launchdarkly_team", "a", map[string]any{"token": "team1"}), "team1"},
		{"environment 2-part", res("launchdarkly_environment", "a", map[string]any{"left": "proj1", "right": "production"}), "proj1/production"},
		{"feature_flag 2-part", res("launchdarkly_feature_flag", "a", map[string]any{"left": "proj1", "right": "flag1"}), "proj1/flag1"},
		{"metric 2-part", res("launchdarkly_metric", "a", map[string]any{"left": "proj1", "right": "m1"}), "proj1/m1"},
		{"segment 3-part", res("launchdarkly_segment", "a", map[string]any{"a": "proj1", "b": "production", "c": "seg1"}), "proj1/production/seg1"},
		{"destination 3-part _id leaf", res("launchdarkly_destination", "a", map[string]any{"a": "proj1", "b": "production", "c": "dst1"}), "proj1/production/dst1"},
		{"flag_environment 3-part ENV MIDDLE", res("launchdarkly_feature_flag_environment", "a", map[string]any{"a": "proj1", "b": "production", "c": "flag1"}), "proj1/production/flag1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("launchdarkly_segment", "a", map[string]any{"a": `${file("x")}`, "b": "production", "c": "seg1"})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestLDBase(t *testing.T) {
	t.Setenv("LAUNCHDARKLY_API_HOST", "")
	if got := ldBase(); got != "https://app.launchdarkly.com" {
		t.Errorf("default = %q, want app.launchdarkly.com", got)
	}
	t.Setenv("LAUNCHDARKLY_API_HOST", "https://app.launchdarkly.us/") // federal, cleaned
	if got := ldBase(); got != "https://app.launchdarkly.us" {
		t.Errorf("federal = %q, want https://app.launchdarkly.us", got)
	}
	t.Setenv("LAUNCHDARKLY_API_HOST", "evil.com@attacker.example") // '@' rejected
	if got := ldBase(); got != "" {
		t.Errorf("malformed host = %q, want empty", got)
	}
}

func TestResolveNextAndHostValidation(t *testing.T) {
	t.Setenv("LAUNCHDARKLY_API_HOST", "")
	// relative path resolves against base + validates same-host.
	if u, ok := resolveNext("/api/v2/flags/p?offset=20"); !ok || u != "https://app.launchdarkly.com/api/v2/flags/p?offset=20" {
		t.Errorf("relative next = %q,%v", u, ok)
	}
	// full same-host URL is allowed.
	if _, ok := resolveNext("https://app.launchdarkly.com/api/v2/x"); !ok {
		t.Error("same-host full next should be allowed")
	}
	// foreign host refused.
	if _, ok := resolveNext("https://evil.example/steal"); ok {
		t.Error("foreign-host next must be refused")
	}
	// plaintext http refused.
	if isLaunchDarklyURL("http://app.launchdarkly.com/x") {
		t.Error("http next must be refused")
	}
}

func TestLDListFollowsLinksNext(t *testing.T) {
	t.Setenv("LAUNCHDARKLY_API_HOST", "")
	fakeLD(t, func(url string) (string, int) {
		if strings.Contains(url, "offset=50") {
			return `{"items":[{"key":"b"}]}`, 200 // no _links.next → last page
		}
		return `{"items":[{"key":"a"}],"_links":{"next":{"href":"/api/v2/projects?limit=50&offset=50"}}}`, 200
	})
	got, err := ldList[ldKeyName](context.Background(), "/api/v2/projects")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Errorf("expected 2 across 2 pages via _links.next; got %+v", got)
	}
}

func TestLDListRefusesForeignNext(t *testing.T) {
	t.Setenv("LAUNCHDARKLY_API_HOST", "")
	fakeLD(t, func(url string) (string, int) {
		return `{"items":[{"key":"a"}],"_links":{"next":{"href":"https://evil.example/steal"}}}`, 200
	})
	if _, err := ldList[ldKeyName](context.Background(), "/api/v2/projects"); err == nil {
		t.Error("ldList must refuse a _links.next pointing at a foreign host")
	}
}

func TestConnectResolvesMember(t *testing.T) {
	t.Setenv("LAUNCHDARKLY_API_HOST", "")
	fakeLD(t, func(url string) (string, int) { return `{"email":"me@example.com"}`, 200 })
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should resolve the member, got %v", err)
	}
	if run.Scope.ID != "me@example.com" || run.Scope.Type != model.ScopeTenant {
		t.Errorf("scope = %+v, want me@example.com/tenant", run.Scope)
	}
	if ac.Identity != "me@example.com" {
		t.Errorf("identity = %q, want me@example.com", ac.Identity)
	}
}

func TestListSkips404AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "teams", func() error { return &launchdarklyAPIError{Status: 404, msg: "enterprise only"} })
	if fatal != nil || fails != 0 {
		t.Errorf("404 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "projects", func() error { return &launchdarklyAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the project fan-out, the two-level project×env fan-out (segments/destinations),
// the flag×env derivation (feature_flag_environment, env in the middle), and the account-wide
// plane.
func TestEnumerateFanOutAndFlagEnv(t *testing.T) {
	t.Setenv("LAUNCHDARKLY_API_HOST", "")
	fakeLD(t, func(url string) (string, int) {
		switch {
		case strings.Contains(url, "/environments"):
			return `{"items":[{"key":"production","name":"Prod"},{"key":"staging","name":"Stg"}]}`, 200
		case strings.Contains(url, "/api/v2/projects"):
			return `{"items":[{"key":"proj1","name":"P1"}]}`, 200
		case strings.Contains(url, "/api/v2/flags/"):
			return `{"items":[{"key":"flag1","name":"F1","environments":{"production":{},"staging":{}}}]}`, 200
		case strings.Contains(url, "/api/v2/metrics/"):
			return `{"items":[{"key":"m1","name":"M1"}]}`, 200
		case strings.Contains(url, "/api/v2/segments/") && strings.Contains(url, "/production"):
			return `{"items":[{"key":"seg1","name":"S1"}]}`, 200
		case strings.Contains(url, "/api/v2/segments/"):
			return `{"items":[]}`, 200
		case strings.Contains(url, "/api/v2/destinations/") && strings.Contains(url, "/production"):
			return `{"items":[{"_id":"dst1","name":"D1"}]}`, 200
		case strings.Contains(url, "/api/v2/destinations/"):
			return `{"items":[]}`, 200
		case strings.Contains(url, "/api/v2/webhooks"):
			return `{"items":[{"_id":"wh1","name":"W1"}]}`, 200
		case strings.Contains(url, "/api/v2/teams"):
			return `{"items":[{"key":"team1","name":"T1"}]}`, 200
		case strings.Contains(url, "/api/v2/roles"):
			return `{"items":[{"key":"role1","name":"R1"}]}`, 200
		}
		return `{"items":[]}`, 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "acct"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	// flag×env: one feature_flag_environment per env, env in the middle.
	if got := deriveImportID(mustRes(t, inv, "flag_env/proj1/production/flag1")); got != "proj1/production/flag1" {
		t.Errorf("flag_env import = %q, want proj1/production/flag1 (env middle)", got)
	}
	mustRes(t, inv, "flag_env/proj1/staging/flag1")
	// environment 2-part.
	if got := deriveImportID(mustRes(t, inv, "environment/proj1/production")); got != "proj1/production" {
		t.Errorf("environment import = %q, want proj1/production", got)
	}
	// segment 3-part (two-level fan-out).
	if got := deriveImportID(mustRes(t, inv, "segment/proj1/production/seg1")); got != "proj1/production/seg1" {
		t.Errorf("segment import = %q, want proj1/production/seg1", got)
	}
	// destination 3-part with the _id leaf.
	mustRes(t, inv, "destination/proj1/production/dst1")
	// account-wide bare webhook.
	if got := deriveImportID(mustRes(t, inv, "webhook/wh1")); got != "wh1" {
		t.Errorf("webhook import = %q, want wh1", got)
	}

	// project(1)+env(2)+flag(1)+flag_env(2)+metric(1)+segment(1)+destination(1)+webhook(1)+
	// team(1)+role(1) = 12.
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
