package cloudflare

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

// checkDependencies verifies terraform is present, the CLOUDFLARE_API_TOKEN is set,
// and the token is active. There is no Cloudflare CLI, so terraform is the only
// external tool.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("CLOUDFLARE_API_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "CLOUDFLARE_API_TOKEN env var")
	} else if err := verifyToken(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "CLOUDFLARE_API_TOKEN invalid or expired: "+err.Error())
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

// verifyToken confirms the API token is active (GET /user/tokens/verify).
func verifyToken(ctx context.Context) error {
	v, err := cfGetOne[struct {
		Status string `json:"status"`
	}](ctx, "/user/tokens/verify")
	if err != nil {
		return err
	}
	if v.Status != "active" {
		return fmt.Errorf("token status %q", v.Status)
	}
	return nil
}

type cfAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// connect resolves and validates the account scope. An empty scope defaults to the
// sole account the token can see; an explicit scope must be visible to the token.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	accounts, err := cfList[cfAccount](ctx, "/accounts")
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("the CLOUDFLARE_API_TOKEN has no account access")
	}

	scope := run.Scope
	if scope.ID == "" {
		if len(accounts) > 1 {
			return nil, fmt.Errorf("token can see %d accounts; pass --scope <account-id> (%s)", len(accounts), accountList(accounts))
		}
		scope.ID = accounts[0].ID
	}
	name, found := "", false
	for _, a := range accounts {
		if a.ID == scope.ID {
			name, found = a.Name, true // an account may legitimately have a blank name
		}
	}
	if !found {
		return nil, fmt.Errorf("account %q is not visible to this token; visible accounts: %s", scope.ID, accountList(accounts))
	}
	scope.Type = model.ScopeTenant // one flat container = the account
	run.Scope = scope

	run.Log.Info("Preflight", "authenticated on cloudflare account %s (%s)", scope.ID, name)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"cloudflare account " + name},
	}, nil
}

func accountList(accounts []cfAccount) string {
	parts := make([]string, 0, len(accounts))
	for _, a := range accounts {
		parts = append(parts, a.ID+" "+a.Name)
	}
	return strings.Join(parts, ", ")
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
