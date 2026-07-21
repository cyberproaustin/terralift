package honeycomb

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

// checkDependencies verifies terraform is present, HONEYCOMB_API_KEY is set, and the key
// authenticates (GET /1/auth returns 200 with a team/environment body). /1/auth also returns
// the api_key_access scope map, logged so later 403 skips are explained. No Honeycomb CLI
// dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("HONEYCOMB_API_KEY") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "HONEYCOMB_API_KEY env var")
	} else if a, err := resolveAuth(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "HONEYCOMB_API_KEY invalid: "+err.Error())
	} else if missing := missingScopes(a); missing != "" {
		run.Log.Info("Preflight", "honeycomb key is missing scopes (some lists may be skipped): %s", missing)
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

type honeycombAuth struct {
	Team struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"team"`
	Environment struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"environment"`
	APIKeyAccess map[string]bool `json:"api_key_access"`
}

// resolveAuth calls GET /1/auth — the key IS the environment, so this both validates the key
// and resolves the container.
func resolveAuth(ctx context.Context) (honeycombAuth, error) {
	a, err := honeycombGetOne[honeycombAuth](ctx, "/1/auth")
	if err != nil {
		return honeycombAuth{}, err
	}
	if a.Environment.Slug == "" && a.Team.Slug == "" {
		return honeycombAuth{}, fmt.Errorf("could not resolve the Honeycomb environment (empty /1/auth response) — check HONEYCOMB_API_KEY")
	}
	return a, nil
}

func missingScopes(a honeycombAuth) string {
	var missing []string
	for _, s := range []string{"boards", "triggers", "columns", "recipients"} {
		if !a.APIKeyAccess[s] {
			missing = append(missing, s)
		}
	}
	return strings.Join(missing, ", ")
}

// connect resolves the flat environment scope. The environment slug (fallback team slug,
// fallback host) is the container id; the key simply is the environment.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	a, err := resolveAuth(ctx)
	if err != nil {
		return nil, err
	}
	id := a.Environment.Slug
	if id == "" {
		id = a.Team.Slug
	}
	name := a.Environment.Name
	if name == "" {
		name = a.Team.Name
	}
	if name == "" {
		name = id
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on Honeycomb environment %s (%s)", name, id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"honeycomb environment " + name},
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
