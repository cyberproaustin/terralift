package gitlab

import (
	"context"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: "gitlab.com", Properties: props}
}

func fakeGitlab(t *testing.T, fn func(method, path string) (body, next string, status int)) {
	t.Helper()
	orig := glDo
	t.Cleanup(func() { glDo = orig })
	glDo = func(_ context.Context, method, path string) ([]byte, string, error) {
		body, next, status := fn(method, path)
		if status >= 400 {
			return []byte(body), "", &gitlabAPIError{Status: status, msg: "err"}
		}
		return []byte(body), next, nil
	}
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"group bare id", res("gitlab_group", map[string]any{"importID": "42"}), "42"},
		{"project bare id", res("gitlab_project", map[string]any{"importID": "7"}), "7"},
		{"hook 2-part", res("gitlab_project_hook", map[string]any{"importID": "7:99"}), "7:99"},
		{"variable 3-part env-scope", res("gitlab_project_variable", map[string]any{"importID": "7:DEPLOY:prod"}), "7:DEPLOY:prod"},
		{"ldap 4-part cn", res("gitlab_group_ldap_link", map[string]any{"importID": "42:ldapmain:cn=x:"}), "42:ldapmain:cn=x:"},
		{"branch protection name leaf", res("gitlab_branch_protection", map[string]any{"importID": "7:main"}), "7:main"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("gitlab_project_variable", map[string]any{"importID": `7:${x}:*`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestGLBase(t *testing.T) {
	t.Setenv("GITLAB_BASE_URL", "")
	if got := glBase(); got != "https://gitlab.com/api/v4" {
		t.Errorf("default = %q, want https://gitlab.com/api/v4", got)
	}
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com") // self-managed bare host → append /api/v4
	if got := glBase(); got != "https://gitlab.example.com/api/v4" {
		t.Errorf("bare host = %q, want /api/v4 appended", got)
	}
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com/api/v4/") // already has /api/v4 → keep, no double-append
	if got := glBase(); got != "https://gitlab.example.com/api/v4" {
		t.Errorf("full base = %q, want no double /api/v4", got)
	}
	t.Setenv("GITLAB_BASE_URL", "https://user:pw@evil.example.com") // userinfo splice rejected
	if got := glBase(); got != "" {
		t.Errorf("userinfo splice should reject, got %q", got)
	}
}

// glList follows the X-Next-Page header across pages.
func TestGLListPaginates(t *testing.T) {
	fakeGitlab(t, func(method, path string) (string, string, int) {
		if strings.HasSuffix(path, "page=1") {
			return `[{"id":1}]`, "2", 200
		}
		return `[{"id":2}]`, "", 200
	})
	gs, err := glList[glGroup](context.Background(), "/groups")
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 || gs[0].ID != 1 || gs[1].ID != 2 {
		t.Fatalf("expected 2 paged groups, got %+v", gs)
	}
}

func TestConnectResolvesInstance(t *testing.T) {
	t.Setenv("GITLAB_BASE_URL", "")
	fakeGitlab(t, func(method, path string) (string, string, int) {
		if path != "/user" {
			t.Errorf("connect should hit /user, got %s", path)
		}
		return `{"username":"alice"}`, "", 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should validate via /user, got %v", err)
	}
	if run.Scope.ID != "gitlab.com" || ac.Identity != "gitlab.com" {
		t.Errorf("scope/identity = %q/%q, want gitlab.com", run.Scope.ID, ac.Identity)
	}
}

func TestListSkips403AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "groups", func() error { return &gitlabAPIError{Status: 403, msg: "forbidden"} })
	if fatal != nil || fails != 0 {
		t.Errorf("403 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "projects", func() error { return &gitlabAPIError{Status: 401, msg: "unauthorized"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the two-root fan-out (group + project children), the composite import ids, the
// share-group derived from the project object, and the LDAP-links 404 skip.
func TestEnumerateFanOut(t *testing.T) {
	t.Setenv("GITLAB_BASE_URL", "")
	fakeGitlab(t, func(method, path string) (string, string, int) {
		switch {
		case strings.HasPrefix(path, "/groups?"):
			return `[{"id":1,"full_path":"acme"}]`, "", 200
		case strings.HasPrefix(path, "/projects?"):
			return `[{"id":2,"path_with_namespace":"acme/web","name":"web","shared_with_groups":[{"group_id":3,"group_name":"ops"}]}]`, "", 200
		case strings.HasPrefix(path, "/groups/1/variables"):
			return `[{"key":"TOKEN","environment_scope":"*"}]`, "", 200
		case strings.HasPrefix(path, "/groups/1/labels"):
			return `[{"id":10,"name":"bug"}]`, "", 200
		case strings.HasPrefix(path, "/groups/1/hooks"):
			return `[{"id":11,"url":"https://h"}]`, "", 200
		case strings.HasPrefix(path, "/groups/1/members"):
			return `[{"id":12,"username":"alice"}]`, "", 200
		case strings.HasPrefix(path, "/groups/1/ldap_group_links"):
			return `{"message":"404 Not Found"}`, "", 404 // CE/SaaS
		case strings.HasPrefix(path, "/projects/2/variables"):
			return `[{"key":"DEPLOY","environment_scope":"prod"}]`, "", 200
		case strings.HasPrefix(path, "/projects/2/labels"):
			return `[{"id":20,"name":"ci"}]`, "", 200
		case strings.HasPrefix(path, "/projects/2/hooks"):
			return `[{"id":21,"url":"https://ph"}]`, "", 200
		case strings.HasPrefix(path, "/projects/2/deploy_keys"):
			return `[{"id":22,"title":"key1"}]`, "", 200
		case strings.HasPrefix(path, "/projects/2/protected_branches"):
			return `[{"name":"main"}]`, "", 200
		case strings.HasPrefix(path, "/projects/2/protected_tags"):
			return `[{"name":"v*"}]`, "", 200
		case strings.HasPrefix(path, "/projects/2/members"):
			return `[{"id":23,"username":"bob"}]`, "", 200
		case strings.HasPrefix(path, "/projects/2/milestones"):
			return `[{"id":24,"title":"M1"}]`, "", 200
		}
		return `[]`, "", 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "gitlab.com"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"group/1":                        "1",
		"project/2":                      "2",
		"project_share_group/2/3":        "2:3",
		"group_variable/1/TOKEN/*":       "1:TOKEN:*",
		"project_variable/2/DEPLOY/prod": "2:DEPLOY:prod",
		"deploy_key/2/22":                "2:22",
		"branch_protection/2/main":       "2:main",
		"project_membership/2/23":        "2:23",
	}
	for id, want := range checks {
		if got := deriveImportID(mustRes(t, inv, id)); got != want {
			t.Errorf("%s import = %q, want %q", id, got, want)
		}
	}

	// group(1)+project(1)+share_group(1)+group{var,label,hook,member}(4)+
	// project{var,label,hook,deploy_key,branch,tag,member,milestone}(8) = 15. LDAP link 404-skipped.
	if len(inv.Resources) != 15 {
		t.Errorf("expected 15 resources, got %d", len(inv.Resources))
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
