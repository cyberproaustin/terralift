// Command terralift is a multi-cloud tool that brings existing cloud
// infrastructure under Terraform: enumerate -> born-correct export -> reconcile
// into a plan-clean module repo -> package. This is the CLI entrypoint; it wires
// flags into a run and dispatches the phase pipeline.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"

	// Blank imports register each cloud provider via its init().
	_ "github.com/cyberproaustin/terralift/internal/providers/gcp"
)

func main() {
	cloud := flag.String("cloud", "gcp", "cloud provider: "+strings.Join(sortedNames(), "|"))
	scopeType := flag.String("scope-type", "project", "scope type: project|folder|organization|subscription|account")
	scopeID := flag.String("scope", "", "scope id (project id, folder/org number, subscription id, account id)")
	artifact := flag.String("artifacts", "artifacts", "artifact output root")
	phasesArg := flag.String("phases", "1,2,3,4,5,6", "comma-separated phases to run")
	hclOnly := flag.Bool("hcl-only", false, "generate HCL only; no state/import")
	migration := flag.Bool("migration", false, "clone mode: re-targetable HCL for a new scope (implies hcl-only)")
	dryRun := flag.Bool("dry-run", false, "detect and report only; make no changes")
	verbosity := flag.String("verbosity", "info", "debug|verbose|info|warn|error")
	flag.Parse()

	log := core.NewLogger(core.ParseLevel(*verbosity))

	p, ok := provider.Get(*cloud)
	if !ok {
		log.Error("", "unknown cloud %q (registered: %s)", *cloud, strings.Join(sortedNames(), ", "))
		os.Exit(2)
	}

	cfg := core.DefaultConfig()
	cfg.Migration = *migration
	cfg.HCLOnly = *hclOnly || *migration

	run := &core.Run{
		ID:     core.NewRunID(time.Now()),
		Cloud:  *cloud,
		Scope:  model.Scope{Type: model.ScopeType(*scopeType), ID: *scopeID},
		Config: cfg,
		DryRun: *dryRun,
		Log:    log,
	}
	run.Paths = core.NewPaths(*artifact, run.ID)

	log.Info("", "TerraLift %s | cloud=%s scope=%s/%s hclOnly=%v migration=%v dryRun=%v",
		run.ID, run.Cloud, run.Scope.Type, run.Scope.ID, cfg.HCLOnly, cfg.Migration, run.DryRun)

	if err := runPipeline(context.Background(), p, run, parsePhases(*phasesArg)); err != nil {
		log.Error("", "%v", err)
		os.Exit(1)
	}
}

// runPipeline dispatches phases 1-3 to the cloud provider and 4-6 to the shared
// (provider-agnostic) reconcile/correctness/package layer. Skeleton: provider
// methods return "not implemented" until the milestones fill them in.
func runPipeline(ctx context.Context, p provider.CloudProvider, run *core.Run, phases []int) error {
	var inv *model.Inventory // carried from Phase 2 to Phase 3 in-process
	for _, n := range phases {
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
				run.Log.Warn("Enumerate", "%v", err)
				break
			}
			inv = got
			if err := core.WriteJSON(run.Paths.Inventory, inv); err != nil {
				run.Log.Warn("Enumerate", "write inventory: %v", err)
			}
			run.Log.Info("Enumerate", "inventory: %d resources -> %s", inv.Counts.Resources, run.Paths.Inventory)
		case 3:
			run.Log.Info("Export", "=== Phase 3 Export ===")
			if inv == nil { // Phase 3 run alone: load the persisted inventory
				inv = &model.Inventory{}
				if err := core.ReadJSON(run.Paths.Inventory, inv); err != nil {
					run.Log.Warn("Export", "no inventory (run Phase 2 first): %v", err)
					break
				}
			}
			res, err := p.Export(ctx, run, inv)
			if err != nil {
				run.Log.Warn("Export", "%v", err)
				break
			}
			for _, c := range res.Containers {
				run.Log.Info("Export", "container %s: %d mapped, %d unmapped (mode=%s)", c.Container, len(c.MappedIDs), len(c.UnmappedIDs), res.Mode)
			}
		case 4, 5, 6:
			run.Log.Info(fmt.Sprintf("Phase%d", n), "=== Phase %d (shared, cloud-agnostic) — not implemented yet ===", n)
		default:
			run.Log.Warn("", "unknown phase %d, skipping", n)
		}
	}
	run.Log.Info("", "skeleton pipeline complete (phase logic pending)")
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
