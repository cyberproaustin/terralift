package logzio

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

// checkDependencies verifies terraform is present, LOGZIO_API_TOKEN is set, and the token
// authenticates against the resolved region (a lightweight GET /v1/endpoints — always present,
// cheap; a 200 confirms the token + region, a 401 is a bad token). No Logz.io CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("LOGZIO_API_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "LOGZIO_API_TOKEN env var")
	} else if err := validateToken(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "LOGZIO_API_TOKEN invalid: "+err.Error())
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

// validateToken makes a lightweight account-scoped call (GET /v1/endpoints) — always present,
// so a 200 confirms the token + region and a 401 is a genuine bad-token failure.
func validateToken(ctx context.Context) error {
	_, _, err := lzDo(ctx, http.MethodGet, "/v1/endpoints", nil)
	return err
}

// connect resolves the flat account scope. The token IS the account (no name endpoint), so the
// container id is the region base host.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if err := validateToken(ctx); err != nil {
		return nil, fmt.Errorf("logzio token validation failed (GET /v1/endpoints) — check LOGZIO_API_TOKEN / LOGZIO_REGION: %w", err)
	}
	host := strings.TrimPrefix(strings.TrimPrefix(lzBase(), "https://"), "http://")
	scope := model.Scope{Type: model.ScopeTenant, ID: host}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on logzio account (%s)", host)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: host,
		Notes:    []string{"logzio account on " + host},
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
