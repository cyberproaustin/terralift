// Package gcp implements the CloudProvider contract for Google Cloud:
// Cloud Asset Inventory enumeration (search-all-resources --read-mask="*" gives
// metadata AND full config in one sweep; search-all-iam-policies for IAM), and a
// born-correct export built on native Terraform import blocks (we author the
// `to` address; import IDs are derived per-type from CAI asset names).
package gcp

import (
	"context"
	"errors"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

// Provider is the GCP implementation of provider.CloudProvider.
type Provider struct{}

func (p *Provider) Name() string { return "gcp" }

func (p *Provider) CheckDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	// M1: detect gcloud (gcloud.cmd on Windows) + terraform; verify
	// cloudasset.googleapis.com is enabled; validate both auth planes.
	return nil, notImplemented("CheckDependencies")
}

func (p *Provider) Connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	// M1: validate gcloud CLI auth AND ADC (gcloud auth application-default);
	// optional impersonation; resolve scope + descendant projects.
	return nil, notImplemented("Connect")
}

func (p *Provider) Enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	// search-all-resources --read-mask="*" (floor+truth in one), search-all-iam-policies
	// (IAM join), + public-exposure enrichers -> cloud-neutral Inventory.
	return enumerate(ctx, run)
}

func (p *Provider) Export(ctx context.Context, run *core.Run, inv *model.Inventory) (*provider.ExportResult, error) {
	// per-type import-ID derivation -> born-correct `import{}` blocks ->
	// terraform plan -generate-config-out (draft) -> scrub secrets.
	return export(ctx, run, inv)
}

func (p *Provider) Templates() provider.ProviderTemplates {
	// M4: google/google-beta provider.tf, gcs backend.tf (keyless via ADC/WIF),
	// GitHub Actions + Azure DevOps WIF pipelines.
	return provider.ProviderTemplates{}
}

func notImplemented(method string) error {
	return errors.New("gcp: " + method + " not implemented yet")
}
