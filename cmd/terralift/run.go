package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/pipeline"
	"github.com/cyberproaustin/terralift/internal/provider"
	"github.com/cyberproaustin/terralift/internal/util"
)

// runOpts holds the flags shared by `onboard` and `clone`.
type runOpts struct {
	cloud, scopeType, scopeID string
	artifacts, resourceGroups string
	phases, verbosity         string
	hclOnly, dryRun, noBanner bool
}

func onboardCmd() *cobra.Command {
	o := &runOpts{}
	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Adopt live infrastructure into a plan-clean Terraform repo (import blocks).",
		Long: `Enumerate the live infrastructure in a scope and generate a plan-clean Terraform
repo that ADOPTS it via born-correct import blocks — running terraform plan on the
result is a clean import (no create/destroy).`,
		Args:    cobra.NoArgs,
		Example: "  terralift onboard --cloud gcp --scope my-project-id",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOnboard(cmd.Context(), o, false)
		},
	}
	bindRunFlags(cmd, o)
	return cmd
}

func cloneCmd() *cobra.Command {
	o := &runOpts{}
	cmd := &cobra.Command{
		Use:   "clone",
		Short: "Generate re-targetable Terraform to recreate the infra in a NEW scope (migration mode).",
		Long: `Like onboard, but the output is a portable CLONE: scope-pinning attributes become
variables and import blocks are dropped, so you can stand the same infrastructure up
in a different project/subscription/account. Implies --hcl-only.`,
		Args:    cobra.NoArgs,
		Example: "  terralift clone --cloud gcp --scope my-project-id",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOnboard(cmd.Context(), o, true)
		},
	}
	bindRunFlags(cmd, o)
	return cmd
}

func bindRunFlags(cmd *cobra.Command, o *runOpts) {
	f := cmd.Flags()
	f.StringVar(&o.cloud, "cloud", "", "cloud provider: "+strings.Join(sortedNames(), "|")+" (required)")
	f.StringVar(&o.scopeID, "scope", "", "scope id: project id / folder|org number / subscription id / account id (required)")
	f.StringVar(&o.scopeType, "scope-type", "", "scope type: project|folder|organization|subscription|account (default: per cloud)")
	f.StringVar(&o.artifacts, "artifacts", "artifacts", "artifact output root")
	f.StringVar(&o.resourceGroups, "resource-groups", "", "restrict to these containers/resource groups (comma-separated; empty = all)")
	f.StringVar(&o.phases, "phases", "1,2,3,4,5,6", "comma-separated phases to run")
	f.StringVar(&o.verbosity, "verbosity", "info", "log level: debug|verbose|info|warn|error")
	f.BoolVar(&o.hclOnly, "hcl-only", false, "generate HCL only; no state/import round-trip")
	f.BoolVar(&o.dryRun, "dry-run", false, "detect and report only; make no changes")
	f.BoolVar(&o.noBanner, "no-banner", false, "suppress the startup banner")
	_ = cmd.MarkFlagRequired("cloud")
	_ = cmd.MarkFlagRequired("scope")
}

func runOnboard(ctx context.Context, o *runOpts, migration bool) error {
	printBanner(o.noBanner)

	log := core.NewLogger(core.ParseLevel(o.verbosity))

	p, ok := provider.Get(o.cloud)
	if !ok {
		return fmt.Errorf("unknown cloud %q (registered: %s)", o.cloud, strings.Join(sortedNames(), ", "))
	}

	scopeType := o.scopeType
	if scopeType == "" {
		scopeType = defaultScopeType(o.cloud)
	}

	cfg := core.DefaultConfig()
	cfg.Migration = migration
	cfg.HCLOnly = o.hclOnly || migration
	cfg.Containers = util.SplitCSV([]string{o.resourceGroups})

	run := &core.Run{
		ID:     core.NewRunID(time.Now()),
		Cloud:  o.cloud,
		Scope:  model.Scope{Type: model.ScopeType(scopeType), ID: o.scopeID},
		Config: cfg,
		DryRun: o.dryRun,
		Log:    log,
	}
	run.Paths = core.NewPaths(o.artifacts, run.ID)
	// Owner-only (0700) run root: generated HCL is written at 0644 and redacted a
	// moment later, so an owner-only parent closes the brief window where another
	// user on a shared host could read a not-yet-scrubbed file.
	if err := os.MkdirAll(run.Paths.Root, 0o700); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}

	log.Info("", "TerraLift %s | cloud=%s scope=%s/%s hclOnly=%v migration=%v dryRun=%v",
		run.ID, run.Cloud, run.Scope.Type, run.Scope.ID, cfg.HCLOnly, cfg.Migration, run.DryRun)

	return runPipeline(ctx, p, run, parsePhases(o.phases))
}

// defaultScopeType is the natural top-level scope for each cloud when --scope-type
// is omitted.
func defaultScopeType(cloud string) string {
	switch cloud {
	case "aws":
		return "account"
	case "azure":
		return "subscription"
	default: // gcp
		return "project"
	}
}

// runPipeline dispatches phases 1-3 to the cloud provider and 4-6 to the shared
// (provider-agnostic) reconcile/correctness/package layer. In --dry-run it detects
// and reports (phases 1-2 + hygiene) and writes no repo.
func runPipeline(ctx context.Context, p provider.CloudProvider, run *core.Run, phases []int) error {
	var inv *model.Inventory          // carried from Phase 2 into later phases
	var export *provider.ExportResult // carried from Phase 3 into Phase 4
	for _, n := range phases {
		// Dry-run stops before any generating phase: report from the inventory only.
		if run.DryRun && n >= 3 {
			if inv != nil {
				pipeline.DryReport(run, inv, p.Capabilities())
			}
			run.Log.Info("", "dry-run complete — detection + reports only, no repo written")
			return nil
		}
		switch n {
		case 1:
			run.Log.Info("Preflight", "=== Phase 1 Preflight ===")
			if _, err := p.CheckDependencies(ctx, run); err != nil {
				run.Log.Warn("Preflight", "%v", err)
			}
			if _, err := p.Connect(ctx, run); err != nil {
				run.Log.Warn("Preflight", "%v", err)
			}
		case 2:
			run.Log.Info("Enumerate", "=== Phase 2 Enumerate ===")
			got, err := p.Enumerate(ctx, run)
			if err != nil {
				return fmt.Errorf("phase 2 enumerate: %w", err) // fatal: never proceed on empty inventory
			}
			inv = got
			if err := core.WriteJSON(run.Paths.Inventory, inv); err != nil {
				return fmt.Errorf("write inventory: %w", err)
			}
			run.Log.Info("Enumerate", "inventory: %d resources -> %s", inv.Counts.Resources, run.Paths.Inventory)
		case 3:
			run.Log.Info("Export", "=== Phase 3 Export ===")
			if inv == nil { // Phase 3 run alone: load the persisted inventory
				inv = &model.Inventory{}
				if err := core.ReadJSON(run.Paths.Inventory, inv); err != nil {
					return fmt.Errorf("no inventory (run Phase 2 first): %w", err)
				}
			}
			res, err := p.Export(ctx, run, inv)
			if err != nil {
				return fmt.Errorf("phase 3 export: %w", err)
			}
			export = res
			for _, c := range res.Containers {
				run.Log.Info("Export", "container %s: %d mapped, %d excluded, %d gap (mode=%s)",
					c.Container, len(c.MappedIDs), len(c.ExcludedIDs), len(c.GapIDs), res.Mode)
			}
		case 4:
			run.Log.Info("Reconcile", "=== Phase 4 Reconcile ===")
			if inv == nil || export == nil {
				return fmt.Errorf("phase 4 needs Phase 2+3 output in-process; run 2,3,4 together")
			}
			if err := pipeline.Reconcile(ctx, run, inv, export, p.Templates(), p.Capabilities()); err != nil {
				return fmt.Errorf("phase 4 reconcile: %w", err)
			}
		case 5:
			run.Log.Info("Correctness", "=== Phase 5 Correctness ===")
			if err := pipeline.Correctness(ctx, run); err != nil {
				return fmt.Errorf("phase 5 correctness: %w", err)
			}
		case 6:
			run.Log.Info("Package", "=== Phase 6 Package ===")
			if _, err := pipeline.Package(run); err != nil {
				return fmt.Errorf("phase 6 package: %w", err)
			}
		default:
			run.Log.Warn("", "unknown phase %d, skipping", n)
		}
	}
	run.Log.Info("", "run complete")
	return nil
}

func parsePhases(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

func sortedNames() []string {
	n := provider.Names()
	sort.Strings(n)
	return n
}
