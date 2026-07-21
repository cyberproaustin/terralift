// Package digitalocean implements the CloudProvider contract for DigitalOcean — a
// flat, token-scoped provider (the token is the account). Enumeration is via the
// DigitalOcean REST API v2 (no CLI); export reuses `terraform plan
// -generate-config-out` with the digitalocean/digitalocean provider. Auth is the
// DIGITALOCEAN_TOKEN env var, which the Terraform provider reads directly.
//
// NOTE: Phase-A scaffold — enumeration + import IDs + wiring, mirrored on the
// Cloudflare/GitHub providers. Curation (docs/v2-specs/digitalocean.md) is added when
// live-validated.
package digitalocean

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "digitalocean" }

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
    # Any supported backend works; DigitalOcean has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a DIGITALOCEAN_TOKEN secret,
# never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env: { DIGITALOCEAN_TOKEN: '${{ secrets.TF_DIGITALOCEAN_TOKEN }}' }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
