package newrelic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies terraform is present, NEW_RELIC_API_KEY + NEW_RELIC_ACCOUNT_ID
// are set (the account id parseable as an int), and the key + account authenticate via the
// combined probe. There is no New Relic CLI dependency.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}
	if os.Getenv("NEW_RELIC_API_KEY") == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "NEW_RELIC_API_KEY env var")
	}
	if _, err := nrAccountID(); err != nil {
		rep.OK = false
		rep.Missing = append(rep.Missing, "NEW_RELIC_ACCOUNT_ID env var (integer)")
	}
	if os.Getenv("NEW_RELIC_API_KEY") != "" {
		if _, err := nrAccountID(); err == nil {
			if _, err := probe(ctx); err != nil {
				rep.OK = false
				rep.Notes = append(rep.Notes, "New Relic credentials invalid: "+err.Error())
			}
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

// probe runs the combined user-email + account-name query. It confirms three things at
// once: the key is a valid User key (actor.user.email non-empty), the key can SEE the
// account (actor.account non-null — the `account: null`-with-no-error subtlety is treated
// as fatal), and it returns the account name for the container. Returns that name.
func probe(ctx context.Context) (string, error) {
	acct, err := nrAccountID()
	if err != nil {
		return "", err
	}
	data, err := nerdgraph(ctx, qProbe, map[string]any{"acct": acct})
	if err != nil {
		return "", err
	}
	var pr nrProbe
	if err := json.Unmarshal(data, &pr); err != nil {
		return "", &nerdgraphError{msg: "decode probe: " + err.Error()}
	}
	if pr.Actor.User.Email == "" {
		return "", fmt.Errorf("NEW_RELIC_API_KEY is not a valid User key (no user resolved)")
	}
	if pr.Actor.Account == nil {
		return "", fmt.Errorf("key is valid but has no access to account %d (account resolved to null)", acct)
	}
	name := pr.Actor.Account.Name
	if name == "" {
		name = strconv.Itoa(acct)
	}
	return name, nil
}

// connect resolves the flat account scope: the NEW_RELIC_ACCOUNT_ID simply IS the scope.
// Confirm the probe and set the single flat container (id = account id, name = the resolved
// account name).
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	acct, err := nrAccountID()
	if err != nil {
		return nil, err
	}
	name, err := probe(ctx)
	if err != nil {
		return nil, err
	}
	id := strconv.Itoa(acct)
	scope := model.Scope{Type: model.ScopeTenant, ID: id}
	run.Scope = scope
	run.Log.Info("Preflight", "authenticated on New Relic account %s (%s)", name, id)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: name,
		Notes:    []string{"newrelic account " + name + " (" + id + ")"},
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
