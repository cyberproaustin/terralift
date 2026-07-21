package vultr

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

// checkDependencies verifies terraform is present, VULTR_API_KEY is set, and the key
// authenticates (GET /account). There is no Vultr CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("VULTR_API_KEY") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "VULTR_API_KEY env var")
	} else if _, err := vultrGetOne[vultrAccount](ctx, "/account", "account"); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "VULTR_API_KEY invalid: "+err.Error())
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

type vultrAccount struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// connect resolves the flat account scope. Vultr's account has no uuid, so the email
// is the stable container id.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	a, err := vultrGetOne[vultrAccount](ctx, "/account", "account")
	if err != nil {
		return nil, err
	}
	if a.Email == "" {
		return nil, fmt.Errorf("could not resolve the vultr account (empty /account response) — check VULTR_API_KEY")
	}
	name := a.Name
	if name == "" {
		name = a.Email
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: a.Email}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on vultr account %s", name)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"vultr account " + name},
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
