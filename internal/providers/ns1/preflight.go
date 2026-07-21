package ns1

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

// ns1Container is the synthetic flat container id: NS1 has no whoami/account-id
// endpoint (an API key is not tied to a discoverable account object), so the single
// container is a constant.
const ns1Container = "account"

// checkDependencies verifies terraform is present, NS1_APIKEY is set, and the key
// authenticates. It probes GET /zones (permission-safe for any DNS key) rather than
// /account/* (which 403s on DNS-only keys). There is no NS1 CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("NS1_APIKEY") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "NS1_APIKEY env var")
	} else if _, err := ns1List[ns1ZoneSummary](ctx, "/zones"); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "NS1_APIKEY invalid or lacks zone access: "+err.Error())
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

// connect validates the key and sets the synthetic flat scope.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if _, err := ns1List[ns1ZoneSummary](ctx, "/zones"); err != nil {
		return nil, err
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: ns1Container}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated to NS1")
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: "ns1",
		Notes:    []string{"ns1 api key"},
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
