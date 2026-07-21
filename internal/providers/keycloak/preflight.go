package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, KEYCLOAK_URL is set and well-formed, an auth
// mode is configured (client-credentials or password), and the token exchange + a GET
// /admin/realms probe succeed. No Keycloak CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("KEYCLOAK_URL") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "KEYCLOAK_URL env var")
	} else if kcBase() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "KEYCLOAK_URL is malformed (must be an http/https URL)")
	}
	hasClient := os.Getenv("KEYCLOAK_CLIENT_ID") != "" && os.Getenv("KEYCLOAK_CLIENT_SECRET") != ""
	hasPassword := os.Getenv("KEYCLOAK_USER") != "" && os.Getenv("KEYCLOAK_PASSWORD") != ""
	if !hasClient && !hasPassword {
		rep.OK = false
		rep.Missing = append(rep.Missing, "KEYCLOAK_CLIENT_ID+KEYCLOAK_CLIENT_SECRET, or KEYCLOAK_USER+KEYCLOAK_PASSWORD")
	}
	warnPlaintext(run)
	if kcBase() != "" && (hasClient || hasPassword) {
		if err := refreshToken(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "Keycloak authentication failed: "+err.Error())
		} else if _, _, err := kcDo(ctx, http.MethodGet, "/admin/realms"); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "Keycloak admin check failed (GET /admin/realms): "+err.Error())
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

// warnPlaintext warns when KEYCLOAK_URL is http:// to a non-loopback host — the token/Bearer
// would traverse the network in cleartext. http to localhost (a dev server) is fine.
func warnPlaintext(run *core.Run) {
	u, err := neturl.Parse(strings.TrimSpace(os.Getenv("KEYCLOAK_URL")))
	if err != nil || u.Scheme != "http" {
		return
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1", "":
		return
	}
	run.Log.Warn("Preflight", "KEYCLOAK_URL is plaintext http:// to %s — the admin token traverses the network in cleartext; prefer https", u.Hostname())
}

// connect runs the token exchange, validates GET /admin/realms, and sets the flat server scope
// (id = the KEYCLOAK_URL host).
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	warnPlaintext(run) // re-warn if Connect is invoked without CheckDependencies
	if err := refreshToken(ctx); err != nil {
		return nil, fmt.Errorf("keycloak authentication failed (check KEYCLOAK_URL / KEYCLOAK_CLIENT_ID / KEYCLOAK_CLIENT_SECRET / KEYCLOAK_USER / KEYCLOAK_PASSWORD): %w", err)
	}
	if _, _, err := kcDo(ctx, http.MethodGet, "/admin/realms"); err != nil {
		return nil, fmt.Errorf("keycloak admin API not reachable (GET /admin/realms) — the token lacks admin scope: %w", err)
	}
	id := kcHost()
	if id == "" {
		id = "keycloak"
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on keycloak server %s", id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{"keycloak server " + id},
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
