package azuread

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

// checkDependencies verifies terraform is present, the ARM_* client-credentials env vars are set,
// and the credentials mint a Graph token (the client-credentials exchange itself is the check — a
// success confirms tenant/client/secret; a failure is a bad credential). No Azure CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	for _, e := range []struct{ name, val string }{
		{"ARM_TENANT_ID", adTenant()}, {"ARM_CLIENT_ID", adClientID()}, {"ARM_CLIENT_SECRET", adClientSecret()},
	} {
		if e.val == "" {
			rep.OK = false
			rep.Missing = append(rep.Missing, e.name+" env var")
		}
	}
	if adTenant() != "" && adClientID() != "" && adClientSecret() != "" {
		if err := refreshToken(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "azuread client credentials invalid: "+err.Error())
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

// connect mints the Graph token and resolves the flat tenant scope. The tenant display name is
// best-effort (GET /organization); the container id falls back to ARM_TENANT_ID.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if adTenant() == "" || adClientID() == "" || adClientSecret() == "" {
		return nil, fmt.Errorf("azuread: set ARM_TENANT_ID, ARM_CLIENT_ID, and ARM_CLIENT_SECRET")
	}
	if err := refreshToken(ctx); err != nil {
		return nil, fmt.Errorf("azuread token exchange failed — check ARM_TENANT_ID/ARM_CLIENT_ID/ARM_CLIENT_SECRET and the app's admin consent: %w", err)
	}
	name := fetchOrgName(ctx)
	id := adTenant()
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	note := "entra tenant " + id
	if name != "" {
		note += " (" + name + ")"
	}
	run.Log.Info("Preflight", "authenticated on %s", note)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{note},
	}, nil
}

// fetchOrgName reads GET /organization for the tenant display name (best-effort; the token is
// already validated by the exchange). It decodes only the display name.
func fetchOrgName(ctx context.Context) string {
	body, err := adDo(ctx, http.MethodGet, adGraphBase+"/organization")
	if err != nil {
		return ""
	}
	var env struct {
		Value []struct {
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if json.Unmarshal(body, &env) == nil && len(env.Value) > 0 {
		return env.Value[0].DisplayName
	}
	return ""
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
