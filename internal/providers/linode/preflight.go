package linode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// isPermissionErr reports whether err is a 401/403 (token lacks a scope/permission).
func isPermissionErr(err error) bool {
	var apiErr *linodeAPIError
	return errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403)
}

// checkDependencies verifies terraform is present, LINODE_TOKEN is set, and the token
// authenticates (GET /profile — readable by any valid token). There is no CLI.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("LINODE_TOKEN") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "LINODE_TOKEN env var")
	} else if _, err := linodeGetOne[linodeProfile](ctx, "/profile"); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "LINODE_TOKEN invalid: "+err.Error())
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

type linodeProfile struct {
	Username string `json:"username"`
}

type linodeAccount struct {
	EUUID string `json:"euuid"`
	Email string `json:"email"`
}

// connect resolves the flat account scope. Prefer /account (euuid + email); fall back
// to /profile (readable by any token) only when the token lacks account:read_only
// (401/403), so a transient /account error is surfaced rather than silently masked.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	id, name := "", ""
	a, aerr := linodeGetOne[linodeAccount](ctx, "/account")
	switch {
	case aerr == nil && a.EUUID != "":
		id, name = a.EUUID, a.Email
	case aerr == nil, isPermissionErr(aerr):
		// account readable-but-empty (odd), or the token lacks account scope — use the
		// profile identity, which any valid token can read.
		p, perr := linodeGetOne[linodeProfile](ctx, "/profile")
		if perr != nil {
			return nil, perr
		}
		id, name = p.Username, p.Username
	default:
		return nil, aerr // a transient/real /account error — surface it
	}
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on linode account %s (%s)", id, name)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"linode account " + name},
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
