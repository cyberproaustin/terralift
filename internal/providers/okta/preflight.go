package okta

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

// checkDependencies verifies terraform is present, OKTA_ORG_NAME + OKTA_BASE_URL +
// OKTA_API_TOKEN are set, and the token authenticates (GET /api/v1/users?limit=1 returns 200 —
// any admin token can read it). No Okta CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("OKTA_ORG_NAME") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "OKTA_ORG_NAME env var")
	}
	if os.Getenv("OKTA_BASE_URL") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "OKTA_BASE_URL env var")
	}
	if os.Getenv("OKTA_API_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "OKTA_API_TOKEN env var")
	}
	if oktaBaseHost() != "" && os.Getenv("OKTA_API_TOKEN") != "" {
		if err := validateToken(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "Okta credentials invalid: "+err.Error())
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

// validateToken makes a minimal authenticated call (GET /api/v1/users?limit=1). Any admin
// token can read users, so a 200 confirms both the constructed org URL and the token.
func validateToken(ctx context.Context) error {
	_, _, _, err := oktaDo(ctx, http.MethodGet, oktaBase()+"/api/v1/users?limit=1")
	return err
}

type oktaOrg struct {
	Subdomain   string `json:"subdomain"`
	CompanyName string `json:"companyName"`
}

// connect resolves the flat org scope. The token IS the org; the container id is the org
// display name (best-effort via GET /api/v1/org), falling back to OKTA_ORG_NAME / the host.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if err := validateToken(ctx); err != nil {
		return nil, fmt.Errorf("okta token validation failed (GET /api/v1/users) — check OKTA_ORG_NAME/OKTA_BASE_URL/OKTA_API_TOKEN: %w", err)
	}
	id := os.Getenv("OKTA_ORG_NAME")
	if org, err := oktaGetObject[oktaOrg](ctx, "/api/v1/org"); err == nil {
		if org.CompanyName != "" {
			id = org.CompanyName
		} else if org.Subdomain != "" {
			id = org.Subdomain
		}
	}
	if id == "" {
		id = oktaBaseHost()
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on okta org %s", id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{"okta org " + id},
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
