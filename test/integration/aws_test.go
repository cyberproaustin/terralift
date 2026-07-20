//go:build integration

package integration

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// awsAccountID returns the account of the currently-authenticated AWS identity, or
// skips the test if the CLI is not authenticated.
func awsAccountID(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("aws", "sts", "get-caller-identity",
		"--query", "Account", "--output", "text").Output()
	if err != nil {
		t.Skipf("aws sts get-caller-identity failed (not authenticated?): %v", err)
	}
	acct := strings.TrimSpace(string(out))
	if acct == "" {
		t.Skip("empty AWS account id; skipping")
	}
	return acct
}

// TestIntegrationAWS runs the full seed -> onboard -> assert-plan-clean -> teardown
// loop against the authenticated AWS account. Resource Explorer is account-scoped,
// so onboarding sweeps the whole account; the assertions are the account-invariant
// (no drift, no failed stacks) plus proof the seed's own resource types onboarded.
func TestIntegrationAWS(t *testing.T) {
	requireTools(t, "aws", "terraform", "go")
	account := awsAccountID(t)

	// Stand up the seed and guarantee teardown (t.Cleanup destroys it).
	terraformSeed(t, "seeds/aws", nil)

	// The seed exercises networking rewiring (VPC/subnet/SG), IAM roles, a Step
	// Functions state machine and a CodeBuild project — the latter two also drive
	// the cross-stack role data-source path (their role lives in the global stack).
	// Each is keyed by its unique tl-it-* name so a pre-existing account resource of
	// the same type cannot mask a miss.
	wantSeed := map[string]string{
		"aws_vpc":               `"tl-it-vpc"`,
		"aws_iam_role":          `"tl-it-sfn-role"`,
		"aws_sfn_state_machine": `"tl-it-sm"`,
		"aws_codebuild_project": `"tl-it-cb"`,
	}

	// Resource Explorer indexes new resources asynchronously and inconsistently, so
	// pre-gate on its own broad query, then let onboardUntil re-run the sweep until
	// every seed type is picked up (or a generous deadline passes).
	waitForSeedIndexed(t, []string{
		"role/tl-it-sfn-role", "role/tl-it-cb-role",
		"stateMachine:tl-it-sm", "project/tl-it-cb",
	}, 8*time.Minute)

	deadline := time.Now().Add(15 * time.Minute)
	rep, run := onboardUntil(t, "aws", account, nil, deadline, wantSeed)

	// The account round-trips with no drift and no failed stacks...
	rep.assertClean(t)
	// ...and every seed resource was mapped (not dropped to a gap).
	assertSeedOnboarded(t, run, wantSeed)
}
