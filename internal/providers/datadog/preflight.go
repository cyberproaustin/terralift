package datadog

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

// checkDependencies verifies terraform is present, both DD_API_KEY and DD_APP_KEY are set,
// and the pair authenticates. Two checks are needed because GET /api/v1/validate only
// exercises the API key (it returns valid:true even with a bogus app key), so a second
// app-key-scoped call confirms the app key. There is no Datadog CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if datadogAPIKey() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "DD_API_KEY env var")
	}
	if datadogAppKey() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "DD_APP_KEY env var")
	}
	if datadogAPIKey() != "" && datadogAppKey() != "" {
		if valid, err := datadogValidate(ctx); err != nil || !valid {
			rep.OK = false
			if err != nil {
				rep.Notes = append(rep.Notes, "DD_API_KEY invalid: "+err.Error())
			} else {
				rep.Notes = append(rep.Notes, "DD_API_KEY failed validation (/api/v1/validate)")
			}
		} else if err := datadogAppKeyCheck(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "DD_APP_KEY problem: "+err.Error())
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

// datadogValidate calls GET /api/v1/validate. This checks only the API key (it needs just
// DD-API-KEY); the app key is confirmed separately by datadogAppKeyCheck.
func datadogValidate(ctx context.Context) (bool, error) {
	body, _, err := datadogDo(ctx, http.MethodGet, datadogBase()+"/api/v1/validate")
	if err != nil {
		return false, err
	}
	var v struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return false, &datadogAPIError{msg: "decode /api/v1/validate: " + err.Error()}
	}
	return v.Valid, nil
}

// datadogAppKeyCheck makes one lightweight app-key-scoped call (GET /api/v2/permissions).
// A 401/403 here means the app key is absent or lacks permission — the case /validate
// cannot catch. Other errors (network/5xx) are surfaced as-is.
func datadogAppKeyCheck(ctx context.Context) error {
	_, _, err := datadogDo(ctx, http.MethodGet, datadogBase()+"/api/v2/permissions")
	if err == nil {
		return nil
	}
	var apiErr *datadogAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403) {
		return fmt.Errorf("app-key call rejected (invalid or insufficient permission): %w", err)
	}
	return err
}

// connect resolves the flat org scope. The DD_API_KEY + DD_APP_KEY pair simply IS the org
// — there is no sub-account resolution — so this validates the pair and derives a
// best-effort container id/name (the org public_id if readable, else the API host).
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	valid, err := datadogValidate(ctx)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("datadog credentials failed validation (/api/v1/validate) — check DD_API_KEY")
	}
	if err := datadogAppKeyCheck(ctx); err != nil {
		return nil, fmt.Errorf("datadog app key check failed — check DD_APP_KEY: %w", err)
	}

	id, name := datadogOrg(ctx)
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on datadog org %s", name)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"datadog org " + name},
	}, nil
}

// datadogOrg best-effort resolves the current org's public id + name via GET /api/v1/org.
// The org id/name is cosmetic here (exactly one flat container), so any failure falls back
// to the API host string, which is always non-empty and stable.
func datadogOrg(ctx context.Context) (id, name string) {
	host := strings.TrimPrefix(strings.TrimPrefix(datadogBase(), "https://"), "http://")
	body, _, err := datadogDo(ctx, http.MethodGet, datadogBase()+"/api/v1/org")
	if err == nil {
		var env struct {
			Orgs []struct {
				PublicID string `json:"public_id"`
				Name     string `json:"name"`
			} `json:"orgs"`
		}
		if json.Unmarshal(body, &env) == nil && len(env.Orgs) > 0 {
			o := env.Orgs[0]
			if o.PublicID != "" {
				id = o.PublicID
				name = o.Name
				if name == "" {
					name = o.PublicID
				}
				return id, name
			}
		}
	}
	return host, host
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
