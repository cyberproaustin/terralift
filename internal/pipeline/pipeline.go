// Package pipeline runs the shared, cloud-agnostic Phases 4-6 (reconcile,
// correctness, package) over a cloud-neutral inventory + export result.
package pipeline

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/hcl"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/naming"
	"github.com/cyberproaustin/terralift/internal/provider"
	"github.com/cyberproaustin/terralift/internal/reconcile"
	"github.com/cyberproaustin/terralift/internal/tf"
)

// DryReport is the "--dry-run" output: detect and report only, write no repo.
// It produces the hygiene report and an enumeration/coverage preview from the
// inventory alone (no export, no generated HCL, no package).
func DryReport(run *core.Run, inv *model.Inventory, caps provider.Capabilities) {
	mapped := 0
	for _, r := range inv.Resources {
		if r.TFType != "" {
			mapped++
		}
	}
	hyg := reconcile.Hygiene(inv)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "hygiene.json"), hyg)
	writeMarkdown(filepath.Join(run.Paths.Reports, "hygiene.md"), hygieneMD(hyg, caps.IAM || caps.Exposure))
	run.Log.Info("DryRun", "enumerated %d resources (%d tf-mapped, %d unmapped) — no repo written",
		len(inv.Resources), mapped, len(inv.Resources)-mapped)
	run.Log.Info("DryRun", "hygiene: %d privileged (%d human), %d publicly exposed -> %s",
		hyg.PrivilegedBindings, hyg.HumanPrivileged, hyg.PubliclyExposed, filepath.Join(run.Paths.Reports, "hygiene.md"))
}

// Reconcile (Phase 4): coverage gap, hygiene report, reference rewire, /live layout.
func Reconcile(ctx context.Context, run *core.Run, inv *model.Inventory, export *provider.ExportResult, tmpl provider.ProviderTemplates, caps provider.Capabilities) error {
	// --- coverage (sorted enumeration for stable diffs; excluded != gap) ---
	enumIDs := make([]string, 0, len(inv.Resources))
	meta := make(map[string]reconcile.MissingResource, len(inv.Resources))
	for id, r := range inv.Resources {
		enumIDs = append(enumIDs, id)
		meta[id] = reconcile.MissingResource{ID: r.ID, Type: r.NativeType, Name: r.Name, Container: r.Container}
	}
	sort.Strings(enumIDs)
	var exported, excluded []string
	for _, c := range export.Containers {
		exported = append(exported, c.MappedIDs...)
		excluded = append(excluded, c.ExcludedIDs...)
	}
	cov := reconcile.Coverage(enumIDs, exported, excluded, meta)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "coverage.json"), cov)
	writeMarkdown(filepath.Join(run.Paths.Reports, "coverage.md"), coverageMD(cov))
	run.Log.Info("Reconcile", "coverage: %d/%d considered exported (%.1f%%), %d excluded, %d gap",
		cov.Covered, cov.Considered, cov.CoveragePct, cov.Excluded, cov.Gap)

	// --- hygiene (only meaningful where the provider has an IAM/exposure plane) ---
	hyg := reconcile.Hygiene(inv)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "hygiene.json"), hyg)
	writeMarkdown(filepath.Join(run.Paths.Reports, "hygiene.md"), hygieneMD(hyg, caps.IAM || caps.Exposure))
	run.Log.Info("Reconcile", "hygiene: %d privileged (%d human), %d publicly exposed",
		hyg.PrivilegedBindings, hyg.HumanPrivileged, hyg.PubliclyExposed)

	// --- layout: one plan-clean stack per container under repo/live/<container>/.
	// Flat live-stacks (no shared /modules extraction) is intentional: an onboarding
	// repo should mirror reality 1:1 so the plan round-trip is provable; factoring
	// common patterns into modules is a later, human refactoring step. ---
	stacks := 0
	usedDirs := map[string]bool{}
	for _, c := range export.Containers {
		// Distinct containers can sanitize to the same directory name (e.g. RG
		// names differing only by case or punctuation); de-collide so one stack
		// never overwrites another.
		leaf := uniqueDir(usedDirs, naming.Sanitize(c.Container))
		dst := filepath.Join(run.Paths.Repo, "live", leaf)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := copyTF(c.Dir, dst); err != nil {
			// A half-copied stack is worse than an absent one: fmt/Correctness/Package
			// would run against a truncated tree and could report it as fine. Drop the
			// partial dir and skip this stack rather than shipping something broken.
			run.Log.Error("Reconcile", "copy HCL for %s failed — skipping stack: %v", naming.Sanitize(c.Container), err)
			_ = os.RemoveAll(dst)
			continue
		}
		// The reconciled backend.tf is authoritative; strip any backend block the
		// export tool emitted (aztfexport writes `backend "local" {}`), else
		// terraform rejects two backend blocks.
		stripBackends(dst)
		// Rewire literal cloud-ids in the generated HCL to azurerm/google_x.y.id
		// references (dependency ordering + portability for a clean rebuild).
		if n := rewireStack(dst, c.ConfigFiles, c.AddressByID); n > 0 {
			run.Log.Info("Reconcile", "rewired %d reference(s) in %s", n, filepath.Base(dst))
		}
		_ = os.WriteFile(filepath.Join(dst, "backend.tf"), []byte(tmpl.BackendTF), 0o644)
		// Migration (clone) mode: re-target scope-pinning attributes to variables,
		// drop the source import blocks, and emit variables.tf + tfvars example.
		if run.Config.Migration {
			migrateStack(run, dst, c.ConfigFiles, tmpl.MigrationAttrs)
		}
		stacks++
	}
	run.Log.Info("Reconcile", "layout: %d live stack(s) -> %s", stacks, filepath.Join(run.Paths.Repo, "live"))

	// --- secrets review: app config SHIPS (it is the point of onboarding to IaC),
	// so flag secret-looking entries for the operator to relocate to a managed
	// store rather than wiping them. ---
	sr := reconcile.ScanSecrets(run.Paths.Repo)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "secrets-review.json"), sr)
	writeMarkdown(filepath.Join(run.Paths.Reports, "secrets-review.md"), secretsReviewMD(sr))
	if len(sr.Findings) > 0 {
		run.Log.Warn("Reconcile", "secrets review: %d config value(s) look like secrets across %d file(s) — see reports/secrets-review.md",
			len(sr.Findings), sr.Files)
	} else {
		run.Log.Info("Reconcile", "secrets review: no secret-looking config values in %d file(s)", sr.Files)
	}

	// --- redactions report: the flip side of the secrets review. Where we DID
	// scrub an unambiguous secret value (control-plane-only guarantee), the value
	// is now GONE from the repo — record exactly what, so anyone cutting over to
	// IaC knows to supply it and is never blindsided by a missing/blank value. ---
	red := collectRedactions(export)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "redactions.json"), red)
	writeMarkdown(filepath.Join(run.Paths.Reports, "redactions.md"), redactionsMD(red))
	if len(red) > 0 {
		run.Log.Warn("Reconcile", "redactions: scrubbed %d secret value(s) from the repo — you MUST supply these before cutover; see reports/redactions.md", len(red))
	}

	// --- .gitignore: keep state, plan files, and .tfvars OUT of version control.
	// The onboarding guarantee is control-plane-only; a later `git add` of the
	// operator's own terraform.tfstate (which DOES contain data-plane secrets) would
	// undo that, so ship the guard with the repo. ---
	_ = os.WriteFile(filepath.Join(run.Paths.Repo, ".gitignore"), []byte(repoGitignore), 0o644)

	// --- README: orient whoever receives the repo (what it is, how to adopt it,
	// which reports to read, how to clone it elsewhere). ---
	_ = os.WriteFile(filepath.Join(run.Paths.Repo, "README.md"), []byte(repoReadme), 0o644)

	// --- CI pipeline starter (plan-on-PR + gated apply, keyless OIDC/WIF) ---
	if tmpl.Pipeline != "" {
		p := filepath.Join(run.Paths.Repo, "ci-pipeline.yml")
		if err := os.WriteFile(p, []byte(tmpl.Pipeline), 0o644); err == nil {
			run.Log.Info("Reconcile", "CI pipeline starter -> %s (place per your CI: .github/workflows/ or azure-pipelines.yml)", p)
		}
	}

	// --- terraform fmt: canonically format the generated repo so the output is
	// idiomatic HCL a human would accept in review. Best-effort (no-op if the
	// terraform binary is absent). ---
	if out, err := tf.New(run.Paths.Repo).Fmt(ctx); err != nil {
		run.Log.Verbose("Reconcile", "terraform fmt skipped: %v", err)
		run.Log.Verbose("Reconcile", "%s", tailStr(out, 10))
	} else {
		run.Log.Info("Reconcile", "terraform fmt: repo formatted")
	}
	return nil
}

// migrateStack turns an adopted stack into a portable clone: scope-pinning
// attributes (location / project / resource_group_name / region / …) become
// migration variables, resource names get an optional prefix/suffix wrap for
// global-uniqueness in the target, the source import blocks are dropped (a clone
// creates fresh, it does not adopt the source ids), and variables.tf +
// terraform.tfvars.example are emitted seeded with the source values.
func migrateStack(run *core.Run, dst string, configFiles []string, attrs map[string]string) {
	if len(attrs) == 0 {
		return
	}
	rule := reconcile.MigrationRule{AttrToVar: attrs, WrapName: true}
	defaults := map[string]string{} // migration var -> source value (for the tfvars example)

	// Re-target both the resource config and the provider config (region/project).
	// De-dup: a provider's config file may already be main.tf (Azure), and
	// re-processing a file would double-wrap resource names.
	files := dedup(append(append([]string{}, configFiles...), "providers.tf", "provider.tf", "main.tf"))
	for _, name := range files {
		p := filepath.Join(dst, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		hcl := string(data)
		for attr, v := range attrs {
			if def := reconcile.FirstAttr(hcl, attr); def != "" && defaults[v] == "" {
				defaults[v] = def
			}
		}
		_ = os.WriteFile(p, []byte(reconcile.ToMigration(hcl, rule)), 0o644)
	}
	_ = os.Remove(filepath.Join(dst, "import.tf")) // a clone does not adopt source ids

	// Stable-ordered variable list.
	seen := map[string]bool{}
	var vars []string
	for _, v := range attrs {
		if !seen[v] {
			seen[v] = true
			vars = append(vars, v)
		}
	}
	sort.Strings(vars)

	var vb, tb strings.Builder
	vb.WriteString("# Generated by TerraLift — migration (clone) variables. Override for the TARGET.\n\n")
	tb.WriteString("# TerraLift migration — source values shown; edit for the target environment.\n\n")
	for _, v := range vars {
		fmt.Fprintf(&vb, "variable %q {\n  type    = string\n", v)
		if d := defaults[v]; d != "" {
			fmt.Fprintf(&vb, "  default = %q\n", d)
		}
		vb.WriteString("}\n\n")
		fmt.Fprintf(&tb, "%s = %q\n", v, defaults[v])
	}
	// name uniqueness knobs (empty => names unchanged; set to avoid clashing with source).
	vb.WriteString("variable \"name_prefix\" {\n  type    = string\n  default = \"\"\n}\n\n")
	vb.WriteString("variable \"name_suffix\" {\n  type    = string\n  default = \"\"\n}\n")
	tb.WriteString("name_prefix = \"\"\nname_suffix = \"\"\n")

	_ = os.WriteFile(filepath.Join(dst, "variables.tf"), []byte(vb.String()), 0o644)
	_ = os.WriteFile(filepath.Join(dst, "terraform.tfvars.example"), []byte(tb.String()), 0o644)
	run.Log.Info("Reconcile", "migration: %s re-targeted to variables; import blocks dropped", filepath.Base(dst))
}

// rewireStack rewires literal ids -> references in each provider-declared config
// file (generated.tf for GCP, main.tf for Azure), returning the total count.
// Only these files are touched — never import.tf, whose `id = "..."` literals
// must stay literal.
func rewireStack(stackDir string, configFiles []string, addrByID map[string]string) int {
	if len(addrByID) == 0 || len(configFiles) == 0 {
		return 0
	}
	total := 0
	for _, name := range configFiles {
		p := filepath.Join(stackDir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out, n := reconcile.Rewire(string(data), addrByID)
		if n > 0 {
			_ = os.WriteFile(p, []byte(out), 0o644)
			total += n
		}
	}
	return total
}

// Correctness (Phase 5): terraform plan round-trip oracle per live stack. In
// hcl-only/discovery mode there is no state to round-trip -> n/a (honest).
func Correctness(ctx context.Context, run *core.Run) error {
	if run.Config.HCLOnly {
		rep := map[string]any{
			"status": "n/a-hcl-only",
			"note":   "Discovery mode: no state imported, so there is nothing to round-trip. Run in import mode (apply the born-correct import blocks) to enable the plan round-trip oracle.",
		}
		_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "correctness.json"), rep)
		writeMarkdown(filepath.Join(run.Paths.Reports, "correctness.md"), "# Correctness (round-trip)\n\nStatus: **n/a (hcl-only)** — no state to round-trip. Apply the import blocks (import mode) to enable the oracle.\n")
		run.Log.Info("Correctness", "status=n/a (hcl-only; no state to round-trip)")
		return nil
	}
	liveRoot := filepath.Join(run.Paths.Repo, "live")
	entries, _ := os.ReadDir(liveRoot)
	clean, remainder, failed := 0, 0, 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Plan in a backend-free scratch copy: the oracle checks plan-cleanliness,
		// not state storage, so the reconciled backend.tf (which would demand a
		// -backend-config) must not block it.
		scratch, err := oracleStackCopy(filepath.Join(liveRoot, e.Name()))
		if err != nil {
			failed++
			continue
		}
		r := tf.New(scratch)
		if out, err := r.InitLocal(ctx); err != nil {
			run.Log.Warn("Correctness", "init failed in %s: %v", e.Name(), err)
			run.Log.Verbose("Correctness", "%s", tailStr(out, 20))
			failed++
			os.RemoveAll(scratch)
			continue
		}
		planFile := filepath.Join(scratch, "tl.plan")
		if out, err := r.Plan(ctx, planFile); err != nil {
			run.Log.Warn("Correctness", "plan failed in %s: %v", e.Name(), err)
			run.Log.Verbose("Correctness", "%s", tailStr(out, 25))
			failed++
			os.RemoveAll(scratch)
			continue
		}
		if js, err := r.ShowJSON(ctx, planFile); err != nil {
			run.Log.Warn("Correctness", "show failed in %s: %v", e.Name(), err)
			failed++
		} else if rt, err := tf.ParseRoundTrip([]byte(js)); err != nil {
			run.Log.Warn("Correctness", "plan JSON parse failed in %s: %v", e.Name(), err)
			failed++
		} else {
			clean += len(rt.Clean)
			remainder += len(rt.Drift)
		}
		os.RemoveAll(scratch)
	}
	status := "ran"
	if failed > 0 {
		status = "partial"
	}
	rep := map[string]any{"status": status, "planClean": clean, "remainder": remainder, "failedStacks": failed}
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "correctness.json"), rep)
	writeMarkdown(filepath.Join(run.Paths.Reports, "correctness.md"),
		fmt.Sprintf("# Correctness (round-trip)\n\n- Status: **%s**\n- Plan-clean: **%d**\n- Remainder (drift): **%d**\n- Failed stacks: **%d**\n", status, clean, remainder, failed))
	run.Log.Info("Correctness", "status=%s plan-clean=%d remainder=%d failed=%d", status, clean, remainder, failed)
	return nil
}

// Package (Phase 6): zip repo/ + reports/ into package/onboarding.zip. Close
// errors are propagated so a truncated archive is never reported as success.
func Package(run *core.Run) (string, error) {
	if err := os.MkdirAll(run.Paths.Package, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(run.Paths.Package, "onboarding.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)

	addTree := func(root, prefix string) error {
		if _, err := os.Stat(root); err != nil {
			return nil
		}
		return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			w, err := zw.Create(filepath.Join(prefix, rel))
			if err != nil {
				return err
			}
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			defer src.Close()
			_, err = io.Copy(w, src)
			return err
		})
	}
	werr := addTree(run.Paths.Repo, "repo")
	if werr == nil {
		werr = addTree(run.Paths.Reports, "reports")
	}
	if cerr := zw.Close(); werr == nil {
		werr = cerr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", fmt.Errorf("package: %w", werr)
	}
	run.Log.Info("Package", "onboarding package -> %s", zipPath)
	return zipPath, nil
}

// --- helpers ---

func copyTF(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		// .tf config plus asset files the export emits alongside it (e.g. a Lambda
		// placeholder .zip that a `filename = "…"` reference needs to apply).
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".tf") && !strings.HasSuffix(e.Name(), ".zip")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeMarkdown(path, content string) { _ = os.WriteFile(path, []byte(content), 0o644) }

// oracleStackCopy copies a stack's .tf files (EXCEPT backend.tf) into a fresh
// temp dir so the correctness oracle can init+plan with local state.
func oracleStackCopy(stackDir string) (string, error) {
	tmp, err := os.MkdirTemp("", "tl-oracle-")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") || e.Name() == "backend.tf" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(stackDir, e.Name())); err == nil {
			_ = os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644)
		}
	}
	return tmp, nil
}

func tailStr(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

var backendStart = regexp.MustCompile(`^\s*backend\s+"[^"]+"\s*\{`)

// stripBackends removes every `backend "..." { ... }` block from the .tf files in
// dir (brace-balanced; handles single-line `backend "local" {}` and multi-line
// forms) so only the reconciled backend.tf configures state.
func stripBackends(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if out, n := stripBackendBlocks(string(data)); n > 0 {
			_ = os.WriteFile(p, []byte(out), 0o644)
		}
	}
}

func stripBackendBlocks(src string) (string, int) {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	removed, depth, inBlock := 0, 0, false
	for _, l := range lines {
		if !inBlock {
			if backendStart.MatchString(l) {
				removed++
				if d := hcl.BraceDelta(l); d > 0 { // string/comment-aware
					inBlock = true
					depth = d
				}
				continue
			}
			out = append(out, l)
			continue
		}
		depth += hcl.BraceDelta(l)
		if depth <= 0 {
			inBlock = false
		}
	}
	return strings.Join(out, "\n"), removed
}

func coverageMD(c reconcile.CoverageReport) string {
	var b strings.Builder
	b.WriteString("# Coverage Gap Report (control-plane only)\n\n")
	fmt.Fprintf(&b, "- Enumerated: **%d**\n- Considered (enumerated − excluded): **%d**\n- Of those, exported: **%d** (**%.1f%%**)\n- Intentionally excluded (managed/default/noise): **%d**\n- Gap (unsupported type): **%d**\n\n",
		c.Enumerated, c.Considered, c.Covered, c.CoveragePct, c.Excluded, c.Gap)
	if len(c.Missing) > 0 {
		b.WriteString("## Gap detail (unsupported types)\n\n")
		for _, m := range c.Missing {
			fmt.Fprintf(&b, "- `%s` %s\n", m.Type, m.Name)
		}
	}
	return b.String()
}

func secretsReviewMD(sr reconcile.SecretsReview) string {
	var b strings.Builder
	b.WriteString("# Secrets Review\n\n")
	b.WriteString("TerraLift **ships your application configuration** (app settings, env vars, ")
	b.WriteString("connection settings) — wiping it would break your apps, so it is preserved ")
	b.WriteString("verbatim in the generated Terraform. Unambiguous single secrets (passwords, ")
	b.WriteString("private keys, `*_access_key`, `SecureString` params) are already removed.\n\n")
	b.WriteString("The entries below are config values that **look like secrets**. Before you ")
	b.WriteString("make this repo the source of truth, move each real secret into a managed store ")
	b.WriteString("(Azure Key Vault / AWS Secrets Manager / GCP Secret Manager) and replace the ")
	b.WriteString("literal with a reference. This is manual, judgement-based work — TerraLift ")
	b.WriteString("flags, it does not decide.\n\n")
	if len(sr.Findings) == 0 {
		fmt.Fprintf(&b, "_No secret-looking config values found across %d scanned file(s)._\n", sr.Files)
		return b.String()
	}
	fmt.Fprintf(&b, "**%d** entr%s to review across %d file(s):\n\n", len(sr.Findings), plural(len(sr.Findings), "y", "ies"), sr.Files)
	b.WriteString("| File | Line | Resource | Key | Preview | Why |\n")
	b.WriteString("|------|-----:|----------|-----|---------|-----|\n")
	for _, f := range sr.Findings {
		res := f.Resource
		if res == "" {
			res = "—"
		}
		fmt.Fprintf(&b, "| `%s` | %d | `%s` | `%s` | `%s` | %s |\n",
			f.File, f.Line, res, f.Key, f.Preview, f.Reason)
	}
	return b.String()
}

// uniqueDir returns base, or base-2, base-3, … if base is already taken, and
// records the chosen name in used.
func uniqueDir(used map[string]bool, base string) string {
	if base == "" {
		base = "stack"
	}
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	used[name] = true
	return name
}

// repoGitignore keeps state and secret-bearing artifacts out of the onboarding
// repo. terraform.tfstate holds data-plane values; .tfvars commonly holds the
// secrets the operator supplies per reports/redactions.md — neither belongs in git.
const repoGitignore = `# TerraLift — keep state and secrets out of version control.
*.tfstate
*.tfstate.*
.terraform/
.terraform.lock.hcl
crash.log
*.tfplan
*.tfplan.*

# Local variable files may hold the secrets you supply (see reports/redactions.md).
# The committed example is the template; real values stay local.
*.tfvars
*.auto.tfvars
!*.tfvars.example
`

// repoReadme orients whoever receives the onboarding repo.
const repoReadme = "# Onboarding repository (generated by TerraLift)\n\n" +
	"A born-correct, plan-clean, **control-plane-only** Terraform representation of your\n" +
	"live cloud infrastructure — ready to adopt so you can switch to IaC.\n\n" +
	"## Layout\n" +
	"- `live/<container>/` — one stack per container (region / resource group / project):\n" +
	"  - `generated.tf` — resource configuration, curated from the live state.\n" +
	"  - `import.tf` — `import {}` blocks that adopt the existing resources into state.\n" +
	"  - `iam.tf` — access control captured as code (where applicable).\n" +
	"  - `providers.tf` / `backend.tf` — provider + remote state (keyless OIDC/WIF).\n" +
	"- `reports/` — READ THESE before you cut over (see below).\n" +
	"- `ci-pipeline.yml` — a plan-on-PR + gated-apply starter for your CI.\n\n" +
	"## Adopt it (switch to IaC)\n" +
	"1. Configure `backend.tf` and run `terraform init`.\n" +
	"2. `terraform plan` — the import blocks adopt the live resources; the plan should be\n" +
	"   clean (no create/destroy). Review any in-place changes.\n" +
	"3. `terraform apply` to bring the resources under management.\n" +
	"4. Once adopted, delete `import.tf` — import blocks are one-time.\n\n" +
	"## Read the reports first\n" +
	"- `reports/secrets-review.md` — shipped config values that LOOK like secrets; relocate\n" +
	"  real secrets to a managed store and reference them.\n" +
	"- `reports/redactions.md` — secret values TerraLift removed (NOT in this repo); supply\n" +
	"  them before cutover.\n" +
	"- `reports/hygiene.md` — publicly-exposed / over-privileged resources to lock down.\n" +
	"- `reports/coverage.md` — onboarded vs. excluded vs. gaps.\n" +
	"- `reports/correctness.md` — the plan round-trip result.\n\n" +
	"## Recreate elsewhere (clone / DR)\n" +
	"Re-run TerraLift with `--migration` for a re-targetable copy (scope attributes become\n" +
	"variables, import blocks dropped).\n\n" +
	"_Review before making this repo the source of truth._\n"

// dedup returns names with duplicates removed, order preserved.
func dedup(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// ContainerRedaction is one scrubbed secret plus the stack it came from, for the
// operator-facing redactions report.
type ContainerRedaction struct {
	Container string
	hcl.Redaction
}

// collectRedactions flattens every container's scrubbed-secret list, tagging each
// with its container, in a stable order.
func collectRedactions(export *provider.ExportResult) []ContainerRedaction {
	var out []ContainerRedaction
	for _, c := range export.Containers {
		for _, r := range c.Redactions {
			out = append(out, ContainerRedaction{Container: c.Container, Redaction: r})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Container != out[j].Container {
			return out[i].Container < out[j].Container
		}
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		return out[i].Attr < out[j].Attr
	})
	return out
}

func redactionsMD(red []ContainerRedaction) string {
	var b strings.Builder
	b.WriteString("# Redactions — secret values removed from this repo\n\n")
	b.WriteString("To honor the **control-plane-only** guarantee, TerraLift scrubbed the ")
	b.WriteString("unambiguous secret values below out of the generated Terraform. **These ")
	b.WriteString("values are NOT in your repo.** Before you make this repo the source of ")
	b.WriteString("truth and apply it, you must supply each one (from your existing secret ")
	b.WriteString("store, a password manager, or by rotating it) — otherwise the resource ")
	b.WriteString("will come up with a missing or empty value.\n\n")
	if len(red) == 0 {
		b.WriteString("_Nothing was redacted — no unambiguous secret values were present in the generated config._\n")
		return b.String()
	}
	b.WriteString("**Action legend**\n")
	b.WriteString("- `removed` — the attribute was deleted. It is optional/write-only, so the ")
	b.WriteString("live value stays as-is (unmanaged); set it in your config when you want ")
	b.WriteString("Terraform to manage it.\n")
	b.WriteString("- `blanked` — the attribute is required, so it was set to `\"\"` and protected ")
	b.WriteString("with `lifecycle { ignore_changes }` so an apply won't overwrite the real ")
	b.WriteString("value. Populate it (ideally from a secret store) before removing the guard.\n")
	b.WriteString("- `placeholder` — the value could not be retrieved from the cloud at all ")
	b.WriteString("(e.g. Lambda code bytes, a sensitive SSM parameter value). A placeholder + ")
	b.WriteString("`ignore_changes` lets the resource import and plan clean; **you must supply ")
	b.WriteString("the real value/code before applying from scratch.**\n\n")
	fmt.Fprintf(&b, "**%d** secret value(s) scrubbed:\n\n", len(red))
	b.WriteString("| Stack | Resource type | Attribute | Action |\n")
	b.WriteString("|-------|---------------|-----------|--------|\n")
	for _, r := range red {
		res := r.Resource
		if res == "" {
			res = "—"
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s |\n", r.Container, res, r.Attr, r.Action)
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func hygieneMD(h reconcile.HygieneReport, applicable bool) string {
	var b strings.Builder
	b.WriteString("# Hygiene / Lockdown Report\n\n")
	if !applicable {
		b.WriteString("_Not applicable for this provider: it has no cloud IAM plane or " +
			"network-exposure surface to assess. Access control for this provider is " +
			"captured as ordinary resources in the generated Terraform._\n")
		return b.String()
	}
	b.WriteString("## Actions\n")
	if len(h.Actions) == 0 {
		b.WriteString("- None — already locked down.\n")
	}
	for _, a := range h.Actions {
		fmt.Fprintf(&b, "- [ ] %s\n", a)
	}
	fmt.Fprintf(&b, "\n## Findings (%d privileged, %d human, %d public)\n\n", h.PrivilegedBindings, h.HumanPrivileged, h.PubliclyExposed)
	for _, f := range h.Findings {
		fmt.Fprintf(&b, "- **%s** — %s (`%s`)\n", f.Kind, f.Detail, f.Resource)
	}
	return b.String()
}
