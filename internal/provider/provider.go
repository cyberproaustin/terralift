// Package provider defines the per-cloud contract (the seam) and a registry.
// Phases 1-3 dispatch to a CloudProvider; Phases 4-6 are cloud-agnostic and
// consume the results.
package provider

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/hcl"
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

	// Capabilities declares which cross-cutting analyses are meaningful for this
	// cloud, so the shared reports don't misrepresent a provider that has no such
	// plane (e.g. a flat SaaS provider with no IAM/network-exposure plane).
	Capabilities() Capabilities
}

// Capabilities declares which cross-cutting, hyperscaler-shaped analyses apply to
// a provider. The built-in clouds set all true; a flat SaaS/platform provider
// (Datadog, GitHub, ...) sets the ones it lacks to false so hygiene/exposure are
// reported as "not applicable" rather than "checked, found nothing."
type Capabilities struct {
	// IAM: the provider populates a distinct access-control plane
	// (inv.IAM / per-resource IAM) that the hygiene report reasons about.
	IAM bool
	// Exposure: the provider populates per-resource public-reachability signals.
	Exposure bool
	// Hierarchy: the scope has meaningful sub-containers (regions / resource
	// groups / projects). False means a single flat stack.
	Hierarchy bool
}

// HyperscalerCapabilities is the all-true set the built-in clouds (AWS/GCP/Azure)
// declare: they have an IAM plane, network exposure, and a container hierarchy.
func HyperscalerCapabilities() Capabilities {
	return Capabilities{IAM: true, Exposure: true, Hierarchy: true}
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
	Containers []ContainerExport
}

type ContainerExport struct {
	Container   string
	Dir         string
	MappedIDs   []string          // exported (import blocks written)
	ExcludedIDs []string          // intentionally skipped: managed/default/sub-resource/noise
	GapIDs      []string          // genuinely unsupported types (no TF mapping)
	AddressByID map[string]string // canonical resource id -> FULL tf reference expression (…​.id/.self_link/.email); consumed verbatim by reconcile.Rewire
	ConfigFiles []string          // generated HCL file names safe to rewire (e.g. generated.tf, main.tf) — NOT import.tf
	Redactions  []hcl.Redaction   // secret values scrubbed from this container (for the operator-facing report)
}

// ProviderTemplates are the cloud-specific packaging templates. Each provider
// authors its own provider block during export (writeProviderTF etc.), so only
// the backend and CI pipeline are templated here.
type ProviderTemplates struct {
	BackendTF string
	Pipeline  string
	// MigrationAttrs maps HCL attributes that pin a resource to its source scope
	// (e.g. location, resource_group_name, project) to the migration variable they
	// become in clone mode, so generated config re-targets to a new environment.
	MigrationAttrs map[string]string
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
