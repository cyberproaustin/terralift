package launchdarkly

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

// checkDependencies verifies terraform is present, LAUNCHDARKLY_ACCESS_TOKEN is set, the API
// host is well-formed, and the token authenticates (GET /api/v2/members/me returns 200 — any
// token can read its own member). No LaunchDarkly CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("LAUNCHDARKLY_ACCESS_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "LAUNCHDARKLY_ACCESS_TOKEN env var")
	}
	if ldBase() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "LAUNCHDARKLY_API_HOST is malformed (must be a bare hostname)")
	}
	if os.Getenv("LAUNCHDARKLY_ACCESS_TOKEN") != "" && ldBase() != "" {
		if _, err := resolveMember(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "LAUNCHDARKLY_ACCESS_TOKEN invalid: "+err.Error())
		}
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

type ldMember struct {
	Email string `json:"email"`
}

// resolveMember calls GET /api/v2/members/me — validates the token and yields the caller's
// email (there is no account-name endpoint).
func resolveMember(ctx context.Context) (ldMember, error) {
	return ldGetObject[ldMember](ctx, "/api/v2/members/me")
}

// connect resolves the flat account scope. The token IS the account; the container id is the
// caller's email (best-effort), falling back to the API host.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	m, err := resolveMember(ctx)
	if err != nil {
		return nil, fmt.Errorf("launchdarkly token validation failed (GET /api/v2/members/me) — check LAUNCHDARKLY_ACCESS_TOKEN: %w", err)
	}
	id := m.Email
	if id == "" {
		id = ldBaseHost()
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on launchdarkly account (%s)", id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{"launchdarkly account " + id},
	}, nil
}

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
