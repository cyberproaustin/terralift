package digitalocean

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

// checkDependencies verifies terraform is present, DIGITALOCEAN_TOKEN is set, and the
// token authenticates (GET /v2/account). There is no DigitalOcean CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("DIGITALOCEAN_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "DIGITALOCEAN_TOKEN env var")
	} else if _, err := fetchAccount(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "DIGITALOCEAN_TOKEN invalid or account inaccessible: "+err.Error())
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

type doAccount struct {
	UUID   string `json:"uuid"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

// fetchAccount validates the token and returns the account (its uuid is the scope).
func fetchAccount(ctx context.Context) (doAccount, error) {
	a, err := doGetOne[doAccount](ctx, "/account", "account")
	if err != nil {
		return a, err
	}
	if a.Status != "active" {
		return a, fmt.Errorf("account status %q", a.Status)
	}
	return a, nil
}

// connect resolves the flat account scope. The token IS the account, so there is no
// multi-account resolution — the account uuid is the single container.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	a, err := fetchAccount(ctx)
	if err != nil {
		return nil, err
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: a.UUID}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on digitalocean account %s (%s)", a.UUID, a.Email)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: a.Email,
		Notes:    []string{"digitalocean account " + a.Email},
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
