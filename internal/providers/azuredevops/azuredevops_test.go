package azuredevops

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

func res(tfType string, props map[string]any) *model.Resource {
	return &model.Resource{TFType: tfType, Container: "myorg", Properties: props}
}

func fakeAzdo(t *testing.T, fn func(method, url string) (body, cont string, status int)) {
	t.Helper()
	orig := azDo
	t.Cleanup(func() { azDo = orig })
	azDo = func(_ context.Context, method, url string) ([]byte, string, error) {
		body, cont, status := fn(method, url)
		if status == 203 || status == 401 {
			return []byte(body), "", &azdoAPIError{Status: 401, msg: "auth"}
		}
		if status >= 400 {
			return []byte(body), "", &azdoAPIError{Status: status, msg: "err"}
		}
		return []byte(body), cont, nil
	}
}

func TestDeriveImportIDs(t *testing.T) {
	cases := []struct {
		name string
		r    *model.Resource
		want string
	}{
		{"project bare GUID", res("azuredevops_project", map[string]any{"importID": "PG-uuid"}), "PG-uuid"},
		{"agent_pool bare int", res("azuredevops_agent_pool", map[string]any{"importID": "5"}), "5"},
		{"group bare descriptor", res("azuredevops_group", map[string]any{"importID": "vssgp.abc"}), "vssgp.abc"},
		{"repo 2-part uuid leaf", res("azuredevops_git_repository", map[string]any{"importID": "PG/RG"}), "PG/RG"},
		{"build def 2-part int leaf", res("azuredevops_build_definition", map[string]any{"importID": "PG/10"}), "PG/10"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.r); got != c.want {
			t.Errorf("%s: import id = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDeriveImportIDEscapesTemplates(t *testing.T) {
	r := res("azuredevops_project", map[string]any{"importID": `${x}`})
	if got := deriveImportID(r); !strings.Contains(got, "$${") {
		t.Errorf("template sequence not escaped: %q", got)
	}
}

func TestAzOrgURL(t *testing.T) {
	t.Setenv("AZDO_ORG_SERVICE_URL", "")
	if got := azOrgURL(); got != "" {
		t.Errorf("unset should be empty (required, no default), got %q", got)
	}
	t.Setenv("AZDO_ORG_SERVICE_URL", "dev.azure.com/myorg") // bare → https
	if got := azOrgURL(); got != "https://dev.azure.com/myorg" {
		t.Errorf("bare = %q, want https promotion", got)
	}
	if got := azOrgName(); got != "myorg" {
		t.Errorf("org name = %q, want myorg", got)
	}
	t.Setenv("AZDO_ORG_SERVICE_URL", "https://user:pw@evil.example.com/o") // userinfo splice rejected
	if got := azOrgURL(); got != "" {
		t.Errorf("userinfo splice should reject, got %q", got)
	}
}

func TestAzGraphURL(t *testing.T) {
	t.Setenv("AZDO_ORG_SERVICE_URL", "https://dev.azure.com/myorg")
	if got := azGraphURL(); got != "https://vssps.dev.azure.com/myorg" {
		t.Errorf("graph host = %q, want vssps variant", got)
	}
	t.Setenv("AZDO_ORG_SERVICE_URL", "https://myorg.visualstudio.com") // legacy host → skip (empty)
	if got := azGraphURL(); got != "" {
		t.Errorf("non-standard host should skip groups, got %q", got)
	}
}

func TestBasicAuth(t *testing.T) {
	t.Setenv("AZDO_PERSONAL_ACCESS_TOKEN", "sekret")
	want := base64.StdEncoding.EncodeToString([]byte(":sekret"))
	if got := basicAuth(); got != want {
		t.Errorf("basicAuth = %q, want base64 of :sekret", got)
	}
}

// azList decodes the {count,value} envelope and follows the x-ms-continuationtoken header.
func TestAzListPaginates(t *testing.T) {
	fakeAzdo(t, func(method, url string) (string, string, int) {
		if strings.Contains(url, "continuationToken=") {
			return `{"count":1,"value":[{"id":"b"}]}`, "", 200
		}
		return `{"count":1,"value":[{"id":"a"}]}`, "n1", 200
	})
	rs, err := azList[azRepo](context.Background(), "https://dev.azure.com/o", "/x/_apis/git/repositories", apiV)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].ID != "a" || rs[1].ID != "b" {
		t.Fatalf("expected 2 paged repos, got %+v", rs)
	}
}

// The real azDo normalizes the Azure DevOps bad-PAT gotcha (203 + text/html sign-in page) to a 401.
func TestAzDo203SignInIsAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNonAuthoritativeInfo)
		_, _ = w.Write([]byte("<html>Sign In</html>"))
	}))
	defer srv.Close()
	t.Setenv("AZDO_PERSONAL_ACCESS_TOKEN", "pat")
	_, _, err := azDo(context.Background(), http.MethodGet, srv.URL)
	var apiErr *azdoAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Fatalf("203/HTML should normalize to a 401 auth failure, got %v", err)
	}
}

func TestConnectResolvesOrg(t *testing.T) {
	t.Setenv("AZDO_ORG_SERVICE_URL", "https://dev.azure.com/myorg")
	fakeAzdo(t, func(method, url string) (string, string, int) {
		if !strings.Contains(url, "/_apis/projects") {
			t.Errorf("connect should validate via /_apis/projects, got %s", url)
		}
		return `{"count":0,"value":[]}`, "", 200
	})
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	ac, err := connect(context.Background(), run)
	if err != nil {
		t.Fatalf("connect should succeed, got %v", err)
	}
	if run.Scope.ID != "myorg" || ac.Identity != "myorg" {
		t.Errorf("scope/identity = %q/%q, want myorg", run.Scope.ID, ac.Identity)
	}
}

func TestListSkips404AndFatal401(t *testing.T) {
	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
	fails := 0
	var fatal error
	list(run, &fails, &fatal, "groups", func() error { return &azdoAPIError{Status: 404, msg: "absent"} })
	if fatal != nil || fails != 0 {
		t.Errorf("404 should be a quiet skip; fatal=%v fails=%d", fatal, fails)
	}
	list(run, &fails, &fatal, "projects", func() error { return &azdoAPIError{Status: 401, msg: "bad pat"} })
	if fatal == nil {
		t.Error("401 during enumeration should be fatal")
	}
}

// End-to-end: the org→project fan-out, the org-level pools (hosted skipped) + graph groups, the
// {count,value} envelope, and the composite import ids.
func TestEnumerateFanOut(t *testing.T) {
	t.Setenv("AZDO_ORG_SERVICE_URL", "https://dev.azure.com/myorg")
	fakeAzdo(t, func(method, url string) (string, string, int) {
		switch {
		case strings.Contains(url, "/_apis/projects?"):
			return `{"count":1,"value":[{"id":"PG","name":"proj"}]}`, "", 200
		case strings.Contains(url, "/_apis/distributedtask/pools"):
			return `{"value":[{"id":5,"name":"pool1","isHosted":false},{"id":6,"name":"Azure Pipelines","isHosted":true}]}`, "", 200
		case strings.Contains(url, "/_apis/graph/groups"):
			return `{"value":[{"descriptor":"vssgp.abc","displayName":"Team A","principalName":"[proj]\\Team A"}]}`, "", 200
		case strings.Contains(url, "/git/repositories"):
			return `{"value":[{"id":"RG","name":"repo1"}]}`, "", 200
		case strings.Contains(url, "/build/definitions"):
			return `{"value":[{"id":10,"name":"build1"}]}`, "", 200
		case strings.Contains(url, "/distributedtask/variablegroups"):
			return `{"value":[{"id":11,"name":"vg1"}]}`, "", 200
		case strings.Contains(url, "/distributedtask/queues"):
			return `{"value":[{"id":12,"name":"q1"}]}`, "", 200
		case strings.Contains(url, "/teams"):
			return `{"value":[{"id":"TG","name":"team1"}]}`, "", 200
		case strings.Contains(url, "/distributedtask/environments"):
			return `{"value":[{"id":13,"name":"env1"}]}`, "", 200
		}
		return `{"value":[]}`, "", 200
	})

	run := &core.Run{Log: core.NewLogger(core.ParseLevel("error")), Scope: model.Scope{Type: model.ScopeTenant, ID: "myorg"}}
	inv, err := enumerate(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"project/PG":             "PG",
		"agent_pool/5":           "5",
		"group/vssgp.abc":        "vssgp.abc",
		"git_repository/PG/RG":   "PG/RG",
		"build_definition/PG/10": "PG/10",
		"team/PG/TG":             "PG/TG",
		"environment/PG/13":      "PG/13",
	}
	for id, want := range checks {
		if got := deriveImportID(mustRes(t, inv, id)); got != want {
			t.Errorf("%s import = %q, want %q", id, got, want)
		}
	}
	// The Azure-hosted pool (id 6) is skipped.
	if inv.Resources["agent_pool/6"] != nil {
		t.Error("hosted agent pool should be skipped")
	}

	// project(1)+pool(1)+group(1)+repo(1)+build(1)+vargroup(1)+queue(1)+team(1)+env(1) = 9.
	if len(inv.Resources) != 9 {
		t.Errorf("expected 9 resources, got %d", len(inv.Resources))
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
