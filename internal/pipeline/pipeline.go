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
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/naming"
	"github.com/cyberproaustin/terralift/internal/provider"
	"github.com/cyberproaustin/terralift/internal/reconcile"
	"github.com/cyberproaustin/terralift/internal/tf"
)

// DryReport is the "--dry-run" output: detect and report only, write no repo.
// It produces the hygiene report and an enumeration/coverage preview from the
// inventory alone (no export, no generated HCL, no package).
func DryReport(run *core.Run, inv *model.Inventory) {
	mapped := 0
	for _, r := range inv.Resources {
		if r.TFType != "" {
			mapped++
		}
	}
	hyg := reconcile.Hygiene(inv)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "hygiene.json"), hyg)
	writeMarkdown(filepath.Join(run.Paths.Reports, "hygiene.md"), hygieneMD(hyg))
	run.Log.Info("DryRun", "enumerated %d resources (%d tf-mapped, %d unmapped) — no repo written",
		len(inv.Resources), mapped, len(inv.Resources)-mapped)
	run.Log.Info("DryRun", "hygiene: %d privileged (%d human), %d publicly exposed -> %s",
		hyg.PrivilegedBindings, hyg.HumanPrivileged, hyg.PubliclyExposed, filepath.Join(run.Paths.Reports, "hygiene.md"))
}

// Reconcile (Phase 4): coverage gap, hygiene report, reference rewire, /live layout.
func Reconcile(run *core.Run, inv *model.Inventory, export *provider.ExportResult, tmpl provider.ProviderTemplates) error {
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

	// --- hygiene ---
	hyg := reconcile.Hygiene(inv)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "hygiene.json"), hyg)
	writeMarkdown(filepath.Join(run.Paths.Reports, "hygiene.md"), hygieneMD(hyg))
	run.Log.Info("Reconcile", "hygiene: %d privileged (%d human), %d publicly exposed",
		hyg.PrivilegedBindings, hyg.HumanPrivileged, hyg.PubliclyExposed)

	// --- layout: one plan-clean stack per container under repo/live/<container>/.
	// Flat live-stacks (no shared /modules extraction) is intentional: an onboarding
	// repo should mirror reality 1:1 so the plan round-trip is provable; factoring
	// common patterns into modules is a later, human refactoring step. ---
	stacks := 0
	for _, c := range export.Containers {
		dst := filepath.Join(run.Paths.Repo, "live", naming.Sanitize(c.Container))
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := copyTF(c.Dir, dst); err != nil {
			run.Log.Warn("Reconcile", "copy HCL: %v", err)
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

	// --- CI pipeline starter (plan-on-PR + gated apply, keyless OIDC/WIF) ---
	if tmpl.Pipeline != "" {
		p := filepath.Join(run.Paths.Repo, "ci-pipeline.yml")
		if err := os.WriteFile(p, []byte(tmpl.Pipeline), 0o644); err == nil {
			run.Log.Info("Reconcile", "CI pipeline starter -> %s (place per your CI: .github/workflows/ or azure-pipelines.yml)", p)
		}
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
	files := append(append([]string{}, configFiles...), "providers.tf", "provider.tf", "main.tf")
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
		} else if rt, err := tf.ParseRoundTrip([]byte(js)); err == nil {
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
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

func stripBackendBlocks(hcl string) (string, int) {
	lines := strings.Split(hcl, "\n")
	out := make([]string, 0, len(lines))
	removed, depth, inBlock := 0, 0, false
	for _, l := range lines {
		if !inBlock {
			if backendStart.MatchString(l) {
				removed++
				if d := strings.Count(l, "{") - strings.Count(l, "}"); d > 0 {
					inBlock = true
					depth = d
				}
				continue
			}
			out = append(out, l)
			continue
		}
		depth += strings.Count(l, "{") - strings.Count(l, "}")
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

func hygieneMD(h reconcile.HygieneReport) string {
	var b strings.Builder
	b.WriteString("# Hygiene / Lockdown Report\n\n## Actions\n")
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
