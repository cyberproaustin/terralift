package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies the external tool chain the Azure phases need
// (az CLI for enumeration, aztfexport for export, terraform for the plan
// round-trip) and that az is authenticated. Missing tools are reported, not
// fatal — the caller decides.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["az"] = azVersion(ctx)
	if rep.Tools["az"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "az (Azure CLI)")
	}
	rep.Tools["aztfexport"] = aztfexportVersion(ctx)
	if rep.Tools["aztfexport"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "aztfexport")
	}
	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}

	// Auth check: `az account show` fails when not logged in.
	if _, err := azAccountShow(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "az not authenticated: run `az login`")
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

// connect validates Azure auth and resolves the active identity + scope.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	acct, err := azAccountShow(ctx)
	if err != nil {
		return nil, fmt.Errorf("az account show: %w", err)
	}
	identity := acct.User.Name
	if identity == "" {
		identity = acct.User.Type
	}
	notes := []string{fmt.Sprintf("subscription %q (%s), tenant %s", acct.Name, acct.ID, acct.TenantID)}
	run.Log.Info("Preflight", "authenticated as %s on subscription %s", identity, acct.ID)
	return &provider.AuthContext{
		Scopes:   []model.Scope{run.Scope},
		Identity: identity,
		Notes:    notes,
	}, nil
}

// --- version + account helpers ---

func azVersion(ctx context.Context) string {
	var v map[string]any
	if err := runAz(ctx, &v, "version"); err != nil {
		return ""
	}
	if s, ok := v["azure-cli"].(string); ok {
		return s
	}
	return ""
}

var aztfexportVer = regexp.MustCompile(`v?\d+\.\d+\.\d+\S*`)

func aztfexportVersion(ctx context.Context) string {
	if _, err := exec.LookPath(aztfexportBin()); err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, aztfexportBin(), "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return aztfexportVer.FindString(string(out))
}

func terraformBin() string { return "terraform" }

func terraformVersion(ctx context.Context) string {
	if _, err := exec.LookPath(terraformBin()); err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, terraformBin(), "version", "-json").Output()
	if err != nil {
		return ""
	}
	var v struct {
		TerraformVersion string `json:"terraform_version"`
	}
	if json.Unmarshal(out, &v) == nil && v.TerraformVersion != "" {
		return v.TerraformVersion
	}
	return ""
}

type azAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenantId"`
	User     struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"user"`
}

func azAccountShow(ctx context.Context) (*azAccount, error) {
	var a azAccount
	if err := runAz(ctx, &a, "account", "show"); err != nil {
		return nil, err
	}
	return &a, nil
}
