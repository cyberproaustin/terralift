package mackerel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, a Mackerel API key is set, and the key
// authenticates against the resolved base (a lightweight GET /api/v0/services — always present,
// cheap; a 200/403 confirms the key authenticates, a 401 is a bad key). No Mackerel CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if mkKey() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "MACKEREL_APIKEY env var")
	} else if err := validateKey(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "MACKEREL_APIKEY invalid: "+err.Error())
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

// validateKey probes GET /api/v0/services — the always-available scoped endpoint. A 200 confirms
// the key; a 401 is a genuinely bad/revoked key; a 403 means a restricted key that still
// authenticates (Mackerel scopes permissions per resource, so a 403 is NOT an auth failure) and is
// treated as valid. Other errors (network/5xx) surface as-is. The key never appears in the error.
func validateKey(ctx context.Context) error {
	_, _, err := mkDo(ctx, http.MethodGet, "/api/v0/services")
	if err == nil {
		return nil
	}
	var apiErr *mackerelAPIError
	if errors.As(err, &apiErr) && apiErr.Status == 403 {
		return nil
	}
	return err
}

// fetchOrg calls GET /api/v0/org — best-effort for the org name (the scope identity). A restricted
// key may lack org read (403), so callers fall back to the base host on any error. It decodes ONLY
// the name; no secret is present in the response.
func fetchOrg(ctx context.Context) (string, error) {
	body, _, err := mkDo(ctx, http.MethodGet, "/api/v0/org")
	if err != nil {
		return "", err
	}
	var o struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &o) != nil {
		return "", nil
	}
	return o.Name, nil
}

// connect resolves the flat org scope. It gates on validateKey (GET /api/v0/services) so a
// restricted-but-valid key is accepted, then reads the org name (GET /api/v0/org) BEST-EFFORT for
// the container id — falling back to the base host on any error or an empty name so the scope is
// never blank and a missing org-read permission never aborts the export.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if err := validateKey(ctx); err != nil {
		return nil, fmt.Errorf("mackerel key validation failed (GET /api/v0/services) — check MACKEREL_APIKEY / MACKEREL_API_BASE: %w", err)
	}
	id, err := fetchOrg(ctx)
	if err != nil || id == "" {
		id = strings.TrimPrefix(strings.TrimPrefix(mkBase(), "https://"), "http://")
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on mackerel org (%s)", id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{"mackerel org " + id},
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
