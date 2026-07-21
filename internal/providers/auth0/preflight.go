package auth0

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

// checkDependencies verifies terraform is present, AUTH0_DOMAIN is set, and either a static
// AUTH0_API_TOKEN or the AUTH0_CLIENT_ID+AUTH0_CLIENT_SECRET pair is set and authenticates (the
// client-credentials exchange, then a lightweight probe). No Auth0 CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("AUTH0_DOMAIN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "AUTH0_DOMAIN env var")
	} else if auth0Base() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "AUTH0_DOMAIN is malformed (must be a bare hostname)")
	}
	hasStatic := strings.TrimSpace(os.Getenv("AUTH0_API_TOKEN")) != ""
	hasM2M := os.Getenv("AUTH0_CLIENT_ID") != "" && os.Getenv("AUTH0_CLIENT_SECRET") != ""
	if !hasStatic && !hasM2M {
		rep.OK = false
		rep.Missing = append(rep.Missing, "AUTH0_API_TOKEN, or AUTH0_CLIENT_ID + AUTH0_CLIENT_SECRET")
	}
	if os.Getenv("AUTH0_DOMAIN") != "" && (hasStatic || hasM2M) {
		if err := ensureToken(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "Auth0 authentication failed: "+err.Error())
		} else if err := validateToken(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "Auth0 token check failed: "+err.Error())
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

// ensureToken mints (or accepts) the Management API Bearer. A static AUTH0_API_TOKEN bypasses
// the client-credentials exchange.
func ensureToken(ctx context.Context) error {
	if strings.TrimSpace(os.Getenv("AUTH0_API_TOKEN")) != "" {
		return nil
	}
	if auth0AccessToken != "" {
		return nil // already minted this run (avoids a second exchange across preflight+connect)
	}
	tok, err := auth0Exchange(ctx)
	if err != nil {
		return err
	}
	auth0AccessToken = tok
	return nil
}

// validateToken makes a minimal authenticated call. /clients?per_page=1 exercises the token
// with a common read scope; a 403 (scope absent) here is tolerated by the caller.
func validateToken(ctx context.Context) error {
	_, _, err := auth0Do(ctx, http.MethodGet, "/api/v2/clients?per_page=1&include_totals=true")
	if err != nil {
		var apiErr *auth0APIError
		if errors.As(err, &apiErr) && apiErr.Status == 403 {
			return nil // token is valid; it just lacks read:clients — enumeration will skip what it can't read
		}
		return err
	}
	return nil
}

type auth0TenantSettings struct {
	FriendlyName string `json:"friendly_name"`
}

// connect runs the token exchange (or accepts a static token), validates, and resolves the flat
// tenant scope. The tenant name is the friendly_name (best-effort), falling back to AUTH0_DOMAIN.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if err := ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("auth0 authentication failed (check AUTH0_DOMAIN / AUTH0_CLIENT_ID / AUTH0_CLIENT_SECRET / AUTH0_API_TOKEN): %w", err)
	}
	name := cleanHostPart(os.Getenv("AUTH0_DOMAIN"))
	// Also self-validate the Bearer here (the static-token path is not otherwise validated by
	// ensureToken): a 401 on the probe means the token is bad; 403/404 is tolerated (scope
	// absent) and falls back to the domain name.
	ts, err := auth0GetObject[auth0TenantSettings](ctx, "/api/v2/tenants/settings")
	if err != nil {
		var apiErr *auth0APIError
		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			return nil, fmt.Errorf("auth0 token validation failed (401 on /tenants/settings) — check the credentials")
		}
	} else if ts.FriendlyName != "" {
		name = ts.FriendlyName
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: name}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on auth0 tenant %s", name)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"auth0 tenant " + name},
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
