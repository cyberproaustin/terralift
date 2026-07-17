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
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/naming"
	"github.com/cyberproaustin/terralift/internal/provider"
	"github.com/cyberproaustin/terralift/internal/reconcile"
	"github.com/cyberproaustin/terralift/internal/tf"
)

// Reconcile (Phase 4): coverage gap, hygiene report, and the /live layout.
func Reconcile(run *core.Run, inv *model.Inventory, export *provider.ExportResult, tmpl provider.ProviderTemplates) error {
	// --- coverage ---
	enumIDs := make([]string, 0, len(inv.Resources))
	meta := make(map[string]reconcile.MissingResource, len(inv.Resources))
	for id, r := range inv.Resources {
		enumIDs = append(enumIDs, id)
		meta[id] = reconcile.MissingResource{ID: r.ID, Type: r.NativeType, Name: r.Name, Container: r.Container}
	}
	var exported []string
	for _, c := range export.Containers {
		exported = append(exported, c.MappedIDs...)
	}
	cov := reconcile.Coverage(enumIDs, exported, meta)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "coverage.json"), cov)
	writeMarkdown(filepath.Join(run.Paths.Reports, "coverage.md"), coverageMD(cov))
	run.Log.Info("Reconcile", "coverage: %d/%d exported (%.1f%%), %d in gap, %d extra captured",
		cov.Covered, cov.Enumerated, cov.CoveragePct, len(cov.Missing), cov.ExtraExported)

	// --- hygiene ---
	hyg := reconcile.Hygiene(inv)
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "hygiene.json"), hyg)
	writeMarkdown(filepath.Join(run.Paths.Reports, "hygiene.md"), hygieneMD(hyg))
	run.Log.Info("Reconcile", "hygiene: %d privileged (%d human), %d publicly exposed",
		hyg.PrivilegedBindings, hyg.HumanPrivileged, hyg.PubliclyExposed)

	// --- layout: repo/live/<container>/ (imported HCL + backend) ---
	stacks := 0
	for _, c := range export.Containers {
		dst := filepath.Join(run.Paths.Repo, "live", naming.Sanitize(c.Container))
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := copyTF(c.Dir, dst); err != nil {
			run.Log.Warn("Reconcile", "copy HCL: %v", err)
		}
		_ = os.WriteFile(filepath.Join(dst, "backend.tf"), []byte(tmpl.BackendTF), 0o644)
		stacks++
	}
	run.Log.Info("Reconcile", "layout: %d live stack(s) -> %s", stacks, filepath.Join(run.Paths.Repo, "live"))
	return nil
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
	clean, remainder := 0, 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sd := filepath.Join(liveRoot, e.Name())
		r := tf.New(sd)
		if _, err := r.Init(ctx); err != nil {
			continue
		}
		planFile := filepath.Join(sd, "tl.plan")
		if _, err := r.Plan(ctx, planFile); err != nil {
			continue
		}
		js, err := r.ShowJSON(ctx, planFile)
		if err != nil {
			continue
		}
		if rt, err := tf.ParseRoundTrip([]byte(js)); err == nil {
			clean += len(rt.Clean)
			remainder += len(rt.Drift)
		}
	}
	rep := map[string]any{"status": "ran", "planClean": clean, "remainder": remainder}
	_ = core.WriteJSON(filepath.Join(run.Paths.Reports, "correctness.json"), rep)
	writeMarkdown(filepath.Join(run.Paths.Reports, "correctness.md"),
		fmt.Sprintf("# Correctness (round-trip)\n\n- Plan-clean: **%d**\n- Remainder (drift): **%d**\n", clean, remainder))
	run.Log.Info("Correctness", "plan-clean=%d remainder=%d", clean, remainder)
	return nil
}

// Package (Phase 6): zip repo/ + reports/ into package/onboarding.zip.
func Package(run *core.Run) (string, error) {
	if err := os.MkdirAll(run.Paths.Package, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(run.Paths.Package, "onboarding.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	addTree := func(root, prefix string) {
		if _, err := os.Stat(root); err != nil {
			return
		}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
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
	addTree(run.Paths.Repo, "repo")
	addTree(run.Paths.Reports, "reports")
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

func coverageMD(c reconcile.CoverageReport) string {
	var b strings.Builder
	b.WriteString("# Coverage Gap Report (control-plane only)\n\n")
	fmt.Fprintf(&b, "- Enumerated: **%d**\n- Of those, exported: **%d** (**%.1f%%**)\n- In gap (unsupported/skipped): **%d**\n- Extra captured beyond floor: **%d**\n\n",
		c.Enumerated, c.Covered, c.CoveragePct, len(c.Missing), c.ExtraExported)
	if len(c.Missing) > 0 {
		b.WriteString("## Gap detail\n\n")
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
