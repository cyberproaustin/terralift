//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// terraliftBin builds the terralift binary once per test run and returns its path.
func terraliftBin(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tl-it-bin")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "terralift")
		out, err := exec.Command("go", "build", "-o", binPath, "../../cmd/terralift").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build terralift: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binPath
}

// requireTools skips the test unless every named executable is on PATH.
func requireTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH; skipping integration test", tool)
		}
	}
}

// correctnessReport mirrors reports/correctness.json.
type correctnessReport struct {
	Status       string `json:"status"`
	PlanClean    int    `json:"planClean"`
	Remainder    int    `json:"remainder"`
	FailedStacks int    `json:"failedStacks"`
}

// assertClean fails the test unless the round-trip imported something with no drift
// and no failed stacks.
func (r correctnessReport) assertClean(t *testing.T) {
	t.Helper()
	if r.PlanClean == 0 {
		t.Errorf("nothing imported (planClean=0, status=%s)", r.Status)
	}
	if r.Remainder != 0 || r.FailedStacks != 0 {
		t.Errorf("not plan-clean: planClean=%d remainder=%d failedStacks=%d", r.PlanClean, r.Remainder, r.FailedStacks)
	}
}

// onboard runs `terralift onboard` for cloud/scope into a temp artifacts dir and
// returns the parsed correctness report plus the run directory (for further
// assertions on the generated repo).
func onboard(t *testing.T, cloud, scope string, extra ...string) (correctnessReport, string) {
	t.Helper()
	bin := terraliftBin(t)
	art := t.TempDir()
	args := append([]string{
		"onboard", "--cloud", cloud, "--scope", scope,
		"--artifacts", art, "--phases", "1,2,3,4,5", "--no-banner",
	}, extra...)
	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr // stream the run log for diagnosis
	if err := cmd.Run(); err != nil {
		t.Fatalf("terralift onboard %s/%s: %v", cloud, scope, err)
	}
	runs, _ := filepath.Glob(filepath.Join(art, "run-*"))
	if len(runs) == 0 {
		t.Fatal("terralift produced no run directory")
	}
	run := runs[0]
	data, err := os.ReadFile(filepath.Join(run, "reports", "correctness.json"))
	if err != nil {
		t.Fatalf("read correctness.json: %v", err)
	}
	var rep correctnessReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("parse correctness.json: %v", err)
	}
	return rep, run
}

// repoHCL concatenates every generated .tf file under a run's live tree.
func repoHCL(run string) string {
	var all strings.Builder
	_ = filepath.WalkDir(filepath.Join(run, "repo", "live"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".tf") {
			if b, e := os.ReadFile(p); e == nil {
				all.Write(b)
				all.WriteByte('\n')
			}
		}
		return nil
	})
	return all.String()
}

// repoHasAll reports whether every tfType has a resource block in the generated repo.
func repoHasAll(run string, tfTypes ...string) bool {
	s := repoHCL(run)
	for _, tf := range tfTypes {
		if !strings.Contains(s, `resource "`+tf+`"`) {
			return false
		}
	}
	return true
}

// assertOnboarded fails unless a resource of each tfType appears in the generated
// repo (proving the seed type was mapped, not dropped to a gap).
func assertOnboarded(t *testing.T, run string, tfTypes ...string) {
	t.Helper()
	s := repoHCL(run)
	for _, tf := range tfTypes {
		if !strings.Contains(s, `resource "`+tf+`"`) {
			t.Errorf("expected %s to be onboarded, but no such resource is in the generated repo", tf)
		}
	}
}

// onboardUntil re-runs onboard (with the given extra flags) until every wantType
// appears in the generated repo or the deadline passes, then returns the last
// report and run dir. Retries absorb the eventual-consistency lag every cloud's
// enumeration source has on freshly-created resources (AWS Resource Explorer, GCP
// Cloud Asset Inventory, Azure Resource Graph) — a single sweep can miss a resource
// that a later sweep indexes. The scope invariant (assertClean) is unaffected by the
// lag and is checked by the caller on whatever run is returned.
func onboardUntil(t *testing.T, cloud, scope string, extra []string, deadline time.Time, wantTypes []string) (correctnessReport, string) {
	t.Helper()
	for attempt := 1; ; attempt++ {
		rep, run := onboard(t, cloud, scope, extra...)
		if repoHasAll(run, wantTypes...) {
			t.Logf("all seed types present after %d onboard attempt(s)", attempt)
			return rep, run
		}
		if time.Now().After(deadline) {
			t.Logf("deadline reached after %d attempt(s); some of %v never indexed", attempt, wantTypes)
			return rep, run
		}
		t.Logf("attempt %d: not all of %v indexed yet; retrying onboard in 30s", attempt, wantTypes)
		time.Sleep(30 * time.Second)
	}
}

// terraformSeed copies the .tf files from seedDir into a temp dir, applies them
// with the given input variables, and registers a `terraform destroy` cleanup so
// the seed is always torn down. Pass nil vars when the seed takes none.
func terraformSeed(t *testing.T, seedDir string, vars map[string]string) {
	t.Helper()
	tmp := t.TempDir()
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		t.Fatalf("read seed dir %s: %v", seedDir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tf") {
			b, _ := os.ReadFile(filepath.Join(seedDir, e.Name()))
			if err := os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	varArgs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		varArgs = append(varArgs, "-var", k+"="+v)
	}
	tf := func(args ...string) ([]byte, error) {
		cmd := exec.Command("terraform", args...)
		cmd.Dir = tmp
		cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=1")
		return cmd.CombinedOutput()
	}
	if out, err := tf("init", "-no-color"); err != nil {
		t.Fatalf("terraform init: %v\n%s", err, out)
	}
	if out, err := tf(append([]string{"apply", "-auto-approve", "-no-color"}, varArgs...)...); err != nil {
		t.Fatalf("terraform apply: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := tf(append([]string{"destroy", "-auto-approve", "-no-color"}, varArgs...)...); err != nil {
			t.Errorf("terraform destroy (manual cleanup may be needed): %v\n%s", err, out)
		}
	})
}

// waitForSeedIndexed polls Resource Explorer with the SAME broad query terralift's
// enumeration uses (empty query string, default region) until every marker string
// is present in the returned ARNs, so the subsequent onboard is likely to see the
// whole seed. It is best-effort: on timeout it logs and returns rather than failing,
// because onboardUntil is the real safety net for RE's eventual-consistency lag.
func waitForSeedIndexed(t *testing.T, markers []string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		out, err := exec.Command("aws", "resource-explorer-2", "search",
			"--query-string", "", "--max-results", "1000",
			"--query", "Resources[].Arn", "--output", "text").Output()
		got := string(out)
		missing := ""
		for _, m := range markers {
			if err != nil || !strings.Contains(got, m) {
				missing = m
				break
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Logf("Resource Explorer still missing %q after %s; relying on onboard retries", missing, timeout)
			return
		}
		time.Sleep(15 * time.Second)
	}
}
