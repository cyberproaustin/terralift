package vault

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

// checkDependencies verifies terraform is present, VAULT_ADDR + VAULT_TOKEN are set, and the token
// authenticates (a lightweight GET auth/token/lookup-self — any VALID token can self-lookup
// regardless of privileges, so a 200 confirms the token and a 403 is a genuinely bad token). No
// Vault CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if vAddr() == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "VAULT_ADDR (valid http/https URL)")
	}
	if os.Getenv("VAULT_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "VAULT_TOKEN env var")
	} else if err := validateToken(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "VAULT_TOKEN invalid: "+err.Error())
	}
	// Warn if the token would traverse cleartext to a non-loopback host.
	if a := vAddr(); strings.HasPrefix(a, "http://") && !isLoopback(vHost()) {
		rep.Notes = append(rep.Notes, "VAULT_ADDR is http:// to a non-loopback host — the token would be sent in cleartext")
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

// validateToken calls GET auth/token/lookup-self — a valid token can always self-lookup, so this
// disambiguates a bad token (403) from a merely under-privileged one. It decodes nothing sensitive.
func validateToken(ctx context.Context) error {
	_, _, err := vDo(ctx, http.MethodGet, "/v1/auth/token/lookup-self")
	return err
}

// connect resolves the flat server scope. It gates on validateToken, then reads the cluster name
// (GET sys/health) BEST-EFFORT for the container id — falling back to the VAULT_ADDR host on any
// error (a standby/sealed node or a restricted token) so the scope is never blank.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	if vAddr() == "" {
		return nil, fmt.Errorf("vault: VAULT_ADDR is malformed (must be an http/https URL)")
	}
	if err := validateToken(ctx); err != nil {
		return nil, fmt.Errorf("vault token validation failed (auth/token/lookup-self) — check VAULT_ADDR / VAULT_TOKEN: %w", err)
	}
	id := clusterName(ctx)
	if id == "" {
		id = vHost()
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on vault server (%s)", id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: id,
		Notes:    []string{"vault server " + id},
	}, nil
}

// clusterName reads GET sys/health for the cluster_name (best-effort scope identity). sys/health
// returns non-200 for standby/sealed nodes; on any error the caller falls back to the host.
func clusterName(ctx context.Context) string {
	body, _, err := vDo(ctx, http.MethodGet, "/v1/sys/health")
	if err != nil {
		return ""
	}
	var h struct {
		ClusterName string `json:"cluster_name"`
	}
	if json.Unmarshal(body, &h) == nil {
		return h.ClusterName
	}
	return ""
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
