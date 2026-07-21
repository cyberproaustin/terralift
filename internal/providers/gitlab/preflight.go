package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, GITLAB_TOKEN is set, and the token authenticates
// against the resolved base (a lightweight GET /user — always available to a valid token; a 200
// confirms the token, a 401 is a bad token). No GitLab CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if glBase() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "GITLAB_BASE_URL (valid http/https URL)")
	}
	if os.Getenv("GITLAB_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "GITLAB_TOKEN env var")
	} else if _, err := fetchUser(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "GITLAB_TOKEN invalid: "+err.Error())
	}
	// Warn if the token would traverse cleartext to a non-loopback host (parity with the vault/
	// keycloak preflights; GITLAB_BASE_URL permits http for a self-managed dev instance).
	if a := glBase(); strings.HasPrefix(a, "http://") && !isLoopback(glHost()) {
		rep.Notes = append(rep.Notes, "GITLAB_BASE_URL is http:// to a non-loopback host — the token would be sent in cleartext")
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

// fetchUser calls GET /user — always available to a valid token, so a 200 confirms it. It decodes
// only the username (for a friendly note); no secret is present.
func fetchUser(ctx context.Context) (string, error) {
	body, _, err := glDo(ctx, http.MethodGet, "/user")
	if err != nil {
		return "", err
	}
	var u struct {
		Username string `json:"username"`
	}
	if json.Unmarshal(body, &u) != nil {
		return "", nil
	}
	return u.Username, nil
}

// connect resolves the flat instance scope (the base host) after validating the token via GET /user.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if glBase() == "" {
		return nil, fmt.Errorf("gitlab: GITLAB_BASE_URL is malformed (must be an http/https URL)")
	}
	user, err := fetchUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("gitlab token validation failed (GET /user) — check GITLAB_TOKEN / GITLAB_BASE_URL: %w", err)
	}
	host := glHost()
	scope := model.Scope{Type: model.ScopeTenant, ID: host}
	run.Scope = scope
	note := "gitlab instance " + host
	if user != "" {
		note += " (authenticated as @" + user + ")"
	}
	run.Log.Info("Preflight", "authenticated on %s", note)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: host,
		Notes:    []string{note},
	}, nil
}

func isLoopback(host string) bool {
	h := host
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return h == "127.0.0.1" || h == "localhost" || h == "::1" || h == "[::1]"
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
