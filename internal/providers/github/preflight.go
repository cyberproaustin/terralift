package github

import (
	"context"
	"encoding/json"
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
	// differ, so resolve which one this is.
	if isOrg(ctx, scope.ID) {
		scope.Type = model.ScopeOrganization
	} else {
		scope.Type = model.ScopeTenant // a user account: the whole account is the scope
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
