package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, PAGERDUTY_TOKEN is set, and the token
// authenticates (GET /abilities returns 200). There is no GET /users/me on a general-access
// token, so /abilities is the validation probe; it also doubles as a capability list. No
// PagerDuty CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("PAGERDUTY_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "PAGERDUTY_TOKEN env var")
	} else if err := validateToken(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "PAGERDUTY_TOKEN invalid: "+err.Error())
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

// validateToken calls GET /abilities — a lightweight, always-present endpoint that exercises
// the token in one call (there is no cheap account-identity endpoint on a REST token).
func validateToken(ctx context.Context) error {
	_, _, err := pdDo(ctx, http.MethodGet, "/abilities", "")
	return err
}

// connect resolves the flat account scope. The token IS the account — there is no id lookup —
// so the container id is the API host string best-effort (REST does not hand back the account
// subdomain cheaply).
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if err := validateToken(ctx); err != nil {
		return nil, fmt.Errorf("pagerduty token validation failed (GET /abilities) — check PAGERDUTY_TOKEN: %w", err)
	}
	id := strings.TrimPrefix(strings.TrimPrefix(pdBase(), "https://"), "http://")
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on pagerduty account (%s)", id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{"pagerduty account on " + id},
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
