package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, GRAFANA_URL is set and valid, GRAFANA_AUTH
// is set, and the pair authenticates (GET /api/org returns 200). There is no /validate
// endpoint — a 200 from /api/org exercises both the URL and the auth in one call. No Grafana
// CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	urlErr := validateGrafanaURL()
	if urlErr != nil {
		rep.OK = false
		rep.Missing = append(rep.Missing, urlErr.Error())
	}
	if os.Getenv("GRAFANA_AUTH") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "GRAFANA_AUTH env var")
	}
	if urlErr == nil && os.Getenv("GRAFANA_AUTH") != "" {
		warnPlaintextBasic(run)
		if _, err := resolveOrg(ctx); err != nil {
			rep.OK = false
			rep.Notes = append(rep.Notes, "Grafana auth/URL check failed: "+err.Error())
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

// warnPlaintextBasic warns when GRAFANA_URL is plaintext http:// to a NON-loopback host
// while GRAFANA_AUTH is Basic (user:pass) — trivially reversible credentials would traverse
// the network in cleartext. http:// to localhost (a dev instance) is fine. Grafana can't be
// forced to https (self-hosted), so this is a warning, not a hard failure.
func warnPlaintextBasic(run *core.Run) {
	if !strings.Contains(os.Getenv("GRAFANA_AUTH"), ":") {
		return // Bearer token, not Basic — reversibility does not apply
	}
	u, err := neturl.Parse(strings.TrimSpace(os.Getenv("GRAFANA_URL")))
	if err != nil || u.Scheme != "http" {
		return
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1", "":
		return
	}
	run.Log.Warn("Preflight", "GRAFANA_URL is plaintext http:// to %s with Basic auth — credentials will traverse the network in cleartext; prefer https", u.Hostname())
}

type grafanaOrg struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// resolveOrg fetches the current org via GET /api/org. The numeric id is load-bearing: it is
// the orgID prefix on every composite import ID.
func resolveOrg(ctx context.Context) (grafanaOrg, error) {
	o, err := grafanaGetOne[grafanaOrg](ctx, "/api/org")
	if err != nil {
		return grafanaOrg{}, err
	}
	if o.ID == 0 {
		return grafanaOrg{}, fmt.Errorf("could not resolve the current Grafana org (empty /api/org response) — check GRAFANA_URL/GRAFANA_AUTH")
	}
	return o, nil
}

// connect resolves the flat org scope. The org id (as a string) is the container ID and is
// reused as the orgID prefix in every import ID; the org name is the display name.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	o, err := resolveOrg(ctx)
	if err != nil {
		return nil, err
	}
	id := strconv.FormatInt(o.ID, 10)
	name := o.Name
	if name == "" {
		name = id
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on Grafana org %s (id %s)", name, id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"grafana org " + name + " (" + id + ")"},
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
