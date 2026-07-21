package fastly

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, FASTLY_API_KEY is set, and the
// token authenticates (GET /tokens/self). There is no Fastly CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("FASTLY_API_KEY") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "FASTLY_API_KEY env var")
	} else if _, err := fastlyGetOne[fastlyToken](ctx, "/tokens/self"); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "FASTLY_API_KEY invalid or expired: "+err.Error())
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

type fastlyToken struct {
	CustomerID string `json:"customer_id"`
}

type fastlyCustomer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// connect resolves the flat customer scope. The token IS the customer, so there is no
// multi-account resolution — the customer id is the single container.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	c, err := fastlyGetOne[fastlyCustomer](ctx, "/current_customer")
	if err != nil {
		return nil, err
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: c.ID}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on fastly customer %s (%s)", c.ID, c.Name)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: c.Name,
		Notes:    []string{"fastly customer " + c.Name},
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
