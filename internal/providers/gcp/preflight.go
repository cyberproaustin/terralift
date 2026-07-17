package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies the external tool chain the GCP phases need (gcloud
// for Cloud Asset Inventory enumeration, terraform for the born-correct export +
// plan round-trip) and that gcloud is authenticated. Missing tools are reported,
// not fatal — the caller decides.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["gcloud"] = gcloudVersion(ctx)
	if rep.Tools["gcloud"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "gcloud (Google Cloud SDK)")
	}
	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}

	if _, err := activeAccount(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "gcloud not authenticated: run `gcloud auth login`")
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

// connect validates gcloud auth and resolves the active identity + project scope.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	account, err := activeAccount(ctx)
	if err != nil {
		return nil, err
	}
	scope := run.Scope
	if scope.ID == "" { // default the scope to the gcloud config project
		if p := configProject(ctx); p != "" {
			scope = model.Scope{Type: model.ScopeProject, ID: p}
			run.Scope = scope
		}
	}
	run.Log.Info("Preflight", "authenticated as %s on %s/%s", account, scope.Type, scope.ID)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: account,
		Notes:    []string{"active account " + account},
	}, nil
}

func gcloudVersion(ctx context.Context) string {
	if _, err := exec.LookPath(gcloudBin()); err != nil {
		return ""
	}
	var v map[string]any
	if err := runGcloudJSON(ctx, &v, "version"); err != nil {
		return ""
	}
	if s, ok := v["Google Cloud SDK"].(string); ok {
		return s
	}
	return ""
}

// activeAccount returns the currently-active gcloud account, or an error when
// none is authenticated.
func activeAccount(ctx context.Context) (string, error) {
	var accts []struct {
		Account string `json:"account"`
		Status  string `json:"status"`
	}
	if err := runGcloudJSON(ctx, &accts, "auth", "list"); err != nil {
		return "", err
	}
	for _, a := range accts {
		if strings.EqualFold(a.Status, "ACTIVE") {
			return a.Account, nil
		}
	}
	return "", fmt.Errorf("no active gcloud account")
}

// configProject returns the gcloud config project (empty if unset).
func configProject(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, gcloudBin(), "config", "get-value", "project", "--quiet").Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "" || p == "(unset)" {
		return ""
	}
	return p
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
