package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies the tool chain the GitHub phases need: the gh CLI
// (enumeration) and terraform (export + plan round-trip), and that gh is
// authenticated. Missing tools are reported, not fatal.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["gh"] = ghVersion(ctx)
	if rep.Tools["gh"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "gh (GitHub CLI)")
	}
	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}

	if _, err := authedUser(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "gh not authenticated: run `gh auth login`")
	}

	for name, ver := range rep.Tools {
		if ver != "" {
			run.Log.Info("Preflight", "%s %s", name, ver)
		}
	}
	if len(rep.Missing) > 0 {
		run.Log.Warn("Preflight", "missing dependencies: %s", strings.Join(rep.Missing, ", "))
	}
	return rep, nil
}

// connect resolves the authenticated identity and the scope (org or user login).
// An empty scope defaults to the authenticated user's own account.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	login, err := authedUser(ctx)
	if err != nil {
		return nil, err
	}
	scope := run.Scope
	if scope.ID == "" {
		scope.ID = login
	}
	// An org and a user login share the same namespace; the enumeration endpoints
	// differ, so resolve which one this is. Critically, a USER scope is enumerated
	// via `user/repos` — the AUTHENTICATED user's repos — which ignores scope.ID, so
	// a typo or another user's login would silently enumerate THIS account's repos
	// under the wrong owner. Guard against that.
	requestedOrg := scope.Type == model.ScopeOrganization // explicit --scope-type organization
	switch {
	case isOrg(ctx, scope.ID):
		scope.Type = model.ScopeOrganization
	case requestedOrg:
		return nil, fmt.Errorf("--scope-type organization was set, but %q is not an organization visible to this token (check the login, read:org scope, and any SSO authorization)", scope.ID)
	case strings.EqualFold(scope.ID, login):
		scope.Type = model.ScopeTenant // the authenticated user's own account
	default:
		return nil, fmt.Errorf("scope %q is neither an organization visible to this token nor the authenticated user %q; a user scope can only enumerate the authenticated account", scope.ID, login)
	}
	run.Scope = scope

	// The Terraform github provider authenticates via GITHUB_TOKEN. Export
	// (generate-config-out) and the correctness oracle both shell out to terraform,
	// which inherits this process's environment, so publish gh's token here (unless
	// the operator already set one) rather than inlining it into config.
	if os.Getenv("GITHUB_TOKEN") == "" {
		if tok, err := ghToken(ctx); err == nil && tok != "" {
			_ = os.Setenv("GITHUB_TOKEN", tok)
		}
	}

	run.Log.Info("Preflight", "authenticated as %s on %s/%s", login, scope.Type, scope.ID)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: login,
		Notes:    []string{"gh user " + login},
	}, nil
}

// authedUser returns the login of the gh-authenticated user.
func authedUser(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := ghAPI(ctx, &u, "user"); err != nil {
		return "", err
	}
	return u.Login, nil
}

// isOrg reports whether login is a GitHub organization (vs a user).
func isOrg(ctx context.Context, login string) bool {
	var o struct {
		Login string `json:"login"`
	}
	return ghAPI(ctx, &o, "orgs/"+login) == nil && o.Login != ""
}

// terraformVersion returns the terraform version, or "" if not on PATH.
func terraformVersion(ctx context.Context) string {
	if _, err := exec.LookPath("terraform"); err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, "terraform", "version", "-json").Output()
	if err != nil {
		return ""
	}
	var v struct {
		TerraformVersion string `json:"terraform_version"`
	}
	if json.Unmarshal(out, &v) == nil {
		return v.TerraformVersion
	}
	return ""
}
