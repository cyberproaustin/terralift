package opsgenie

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, OPSGENIE_API_KEY is set, and the key
// authenticates (GET /v2/account returns 200). /v2/account doubles as the account-name/plan
// probe. No Opsgenie CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("OPSGENIE_API_KEY") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "OPSGENIE_API_KEY env var")
	} else if _, err := resolveAccount(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "OPSGENIE_API_KEY invalid: "+err.Error())
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

type ogAccount struct {
	Name string `json:"name"`
}

// resolveAccount validates the key and resolves the account name. It prefers GET /v2/account
// (which also yields the name), but that endpoint is gated on some plans (→ 404); on any
// non-auth error it falls back to a lightweight GET /v2/users?limit=1 probe and returns an
// empty name (connect then uses the API host as the display id). A 401/403 is a genuine auth
// failure and is surfaced, not masked by the fallback.
func resolveAccount(ctx context.Context) (ogAccount, error) {
	a, err := ogGetData[ogAccount](ctx, "/v2/account")
	if err == nil {
		return a, nil
	}
	var apiErr *opsgenieAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403) {
		return ogAccount{}, err // real auth failure — do not fall back
	}
	// /v2/account gated/absent → validate the key with a minimal users probe (single call,
	// no pagination follow).
	if _, _, uerr := ogDo(ctx, http.MethodGet, ogBase()+"/v2/users?limit=1"); uerr != nil {
		return ogAccount{}, uerr
	}
	return ogAccount{}, nil // key valid; name unknown (connect falls back to the host)
}

// connect resolves the flat account scope. The key IS the account; the container id is the
// account name (fallback to the API host), no id lookup required.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	a, err := resolveAccount(ctx)
	if err != nil {
		return nil, fmt.Errorf("opsgenie account validation failed (GET /v2/account) — check OPSGENIE_API_KEY: %w", err)
	}
	id := a.Name
	if id == "" {
		id = ogBaseHost()
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on opsgenie account %s", id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{"opsgenie account " + id},
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
