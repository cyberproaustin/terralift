package aws

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

// checkDependencies verifies the tool chain the AWS phases need (aws CLI for
// enumeration, terraform for born-correct export + the plan round-trip) and that
// the caller is authenticated.
func checkDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	rep := &provider.DependencyReport{OK: true, Tools: map[string]string{}}

	rep.Tools["aws"] = awsVersion(ctx)
	if rep.Tools["aws"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "aws (AWS CLI v2)")
	}
	rep.Tools["terraform"] = terraformVersion(ctx)
	if rep.Tools["terraform"] == "" {
		rep.OK = false
		rep.Missing = append(rep.Missing, "terraform")
	}

	if arn, err := stsCallerARN(ctx); err != nil {
		rep.OK = false
		rep.Notes = append(rep.Notes, "aws not authenticated: run `aws configure` (or `aws sso login`)")
	} else {
		rep.Notes = append(rep.Notes, "identity "+arn)
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

// connect validates AWS auth and resolves the active account + identity.
func connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	account, err := stsAccount(ctx)
	if err != nil {
		return nil, err // already prefixed with the failing aws command
	}
	arn, _ := stsCallerARN(ctx)
	scope := run.Scope
	if scope.ID == "" { // default the scope to the authenticated account
		scope = model.Scope{Type: model.ScopeAccount, ID: account}
	}
	run.Log.Info("Preflight", "authenticated as %s on account %s", arn, account)
	return &provider.AuthContext{
		Scopes:   []model.Scope{scope},
		Identity: arn,
		Notes:    []string{"account " + account},
	}, nil
}

func awsVersion(ctx context.Context) string {
	if _, err := exec.LookPath(awsBin()); err != nil {
		return ""
	}
	// `aws --version` prints e.g. "aws-cli/2.36.1 Python/... " on stderr/stdout.
	out, err := exec.CommandContext(ctx, awsBin(), "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		return strings.TrimPrefix(fields[0], "aws-cli/")
	}
	return ""
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
