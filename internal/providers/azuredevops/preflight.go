package azuredevops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, AZDO_ORG_SERVICE_URL + AZDO_PERSONAL_ACCESS_TOKEN
// are set, and the PAT authenticates against the org (a lightweight GET /_apis/projects?$top=1 — a
// 200 confirms the PAT; a 401, or the 203/HTML sign-in gotcha normalized to 401, is a bad/expired
// PAT). No Azure DevOps CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if azOrgURL() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "AZDO_ORG_SERVICE_URL (e.g. https://dev.azure.com/<org>)")
	}
	if azPAT() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "AZDO_PERSONAL_ACCESS_TOKEN env var")
	} else if azOrgURL() != "" {
		if err := validatePAT(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "AZDO_PERSONAL_ACCESS_TOKEN invalid: "+err.Error())
		}
	}
	// Warn if the PAT would traverse cleartext to a non-loopback host (parity with the vault/
	// gitlab preflights; AZDO_ORG_SERVICE_URL permits http for on-prem Azure DevOps Server).
	if a := azOrgURL(); strings.HasPrefix(a, "http://") && !isLoopback(azHost()) {
		rep.Notes = append(rep.Notes, "AZDO_ORG_SERVICE_URL is http:// to a non-loopback host — the PAT would be sent in cleartext")
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

// validatePAT makes a lightweight org-scoped call (GET /_apis/projects?$top=1). A 200 confirms the
// PAT; the 203/HTML sign-in gotcha is normalized to a 401 by azDo.
func validatePAT(ctx context.Context) error {
	_, _, err := azDo(ctx, http.MethodGet, azOrgURL()+"/_apis/projects?api-version="+apiV+"&$top=1")
	return err
}

// connect resolves the flat org scope (the <org> path segment) after validating the PAT.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if azOrgURL() == "" {
		return nil, fmt.Errorf("azuredevops: AZDO_ORG_SERVICE_URL is malformed or unset (need e.g. https://dev.azure.com/<org>)")
	}
	if err := validatePAT(ctx); err != nil {
		return nil, fmt.Errorf("azuredevops PAT validation failed (GET /_apis/projects) — check AZDO_ORG_SERVICE_URL / AZDO_PERSONAL_ACCESS_TOKEN: %w", err)
	}
	org := azOrgName()
	if org == "" {
		org = azOrgURL()
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: org}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on azure devops org (%s)", org)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: org,
		Notes:    []string{"azure devops org " + org},
	}, nil
}

func isLoopback(host string) bool {
	h := host
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return h == "127.0.0.1" || h == "localhost" || h == "::1" || h == "[::1]"
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
