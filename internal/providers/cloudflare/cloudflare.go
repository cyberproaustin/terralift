// Package cloudflare implements the CloudProvider contract for Cloudflare — a flat,
// account-scoped provider with no regions, no cloud-IAM plane and no container
// hierarchy. Enumeration is via the Cloudflare REST API v4 (no CLI); export reuses
// the generic `terraform plan -generate-config-out` path with the cloudflare/cloudflare
// v4 provider. Auth is the CLOUDFLARE_API_TOKEN env var, which the Terraform provider
// reads directly.
//
// NOTE: Phase-A scaffold — enumeration + import IDs + wiring, mirrored on the GitHub
// provider. Curation (see docs/v2-specs/cloudflare.md) is added when live-validated.
package cloudflare

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

// Provider is the Cloudflare implementation of provider.CloudProvider.
type Provider struct{}

func (p *Provider) Name() string { return "cloudflare" }

func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{IAM: false, Exposure: false, Hierarchy: false}
}

func (p *Provider) CheckDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	return checkDependencies(ctx, run)
}

func (p *Provider) Connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	return connect(ctx, run)
}

func (p *Provider) Enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	return enumerate(ctx, run)
}

func (p *Provider) Export(ctx context.Context, run *core.Run, inv *model.Inventory) (*provider.ExportResult, error) {
	return export(ctx, run, inv)
}

func (p *Provider) Templates() provider.ProviderTemplates {
	return provider.ProviderTemplates{
		BackendTF: `terraform {
  backend "s3" {
    # -backend-config="bucket=..." -backend-config="key=..."
    # Any supported backend works; Cloudflare has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a CLOUDFLARE_API_TOKEN secret,
# never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env: { CLOUDFLARE_API_TOKEN: '${{ secrets.TF_CLOUDFLARE_API_TOKEN }}' }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
