// Package provider defines the per-cloud contract (the seam) and a registry.
// Phases 1-3 dispatch to a CloudProvider; Phases 4-6 are cloud-agnostic and
// consume the results.
package provider

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// CloudProvider is implemented once per cloud (azure, gcp, aws, ...).
type CloudProvider interface {
	// Name is the cloud key: "azure" | "gcp" | "aws".
	Name() string

	// CheckDependencies detects/installs required tools and validates API access.
	CheckDependencies(ctx context.Context, run *core.Run) (*DependencyReport, error)

	// Connect validates this cloud's credential plane(s) and resolves scope.
	Connect(ctx context.Context, run *core.Run) (*AuthContext, error)

	// Enumerate builds the canonical inventory (floor + truth + enrichers).
	Enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error)

	// Export produces born-correct Terraform: import blocks/mapping + generated HCL.
	Export(ctx context.Context, run *core.Run, inv *model.Inventory) (*ExportResult, error)

	// Templates returns provider.tf / backend.tf / pipeline for the package phase.
	Templates() ProviderTemplates
}

// DependencyReport is the outcome of the preflight tool/API check.
type DependencyReport struct {
	OK      bool
	Missing []string
	Tools   map[string]string // name -> version
	Notes   []string
}

// AuthContext is the resolved auth + scope after Connect.
type AuthContext struct {
	Scopes   []model.Scope
	Identity string
	Notes    []string
}

// ExportResult summarizes per-container export for the reconcile phase.
type ExportResult struct {
	Mode       string // "import" | "hcl-only"
	Simulated  bool
	Containers []ContainerExport
}

type ContainerExport struct {
	Container   string
	Dir         string
	MappedIDs   []string          // exported (import blocks written)
	ExcludedIDs []string          // intentionally skipped: managed/default/sub-resource/noise
	GapIDs      []string          // genuinely unsupported types (no TF mapping)
	AddressByID map[string]string // canonical resource id -> tf address (for reference rewire)
	Renames     int
}

// ProviderTemplates are the cloud-specific packaging templates.
type ProviderTemplates struct {
	ProviderTF string
	BackendTF  string
	Pipeline   string
}

// --- registry -------------------------------------------------------------

var registry = map[string]CloudProvider{}

// Register adds a provider implementation (called from each provider's init()).
func Register(p CloudProvider) { registry[p.Name()] = p }

// Get returns the provider for a cloud key.
func Get(name string) (CloudProvider, bool) {
	p, ok := registry[name]
	return p, ok
}

// Names lists registered cloud keys.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
