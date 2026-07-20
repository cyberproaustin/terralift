// Package github implements the CloudProvider contract for GitHub — the first
// non-hyperscaler ("de-hyperscaler") provider. It has no regions, no ARNs, no
// cloud-IAM plane and no container hierarchy: the scope is a single org or user
// login and everything lands in one flat stack. Enumeration is via the `gh` CLI
// (gh api); export reuses the generic `terraform plan -generate-config-out` path
// with the integrations/github provider.
package github

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

// Provider is the GitHub implementation of provider.CloudProvider.
type Provider struct{}

func (p *Provider) Name() string { return "github" }

// Capabilities: GitHub is a flat SaaS provider — no cloud-IAM plane, no network
// exposure signals, and no sub-container hierarchy (one org/user = one stack). The
// shared hygiene/exposure reports therefore render "not applicable" rather than
// "checked, found nothing".
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
		// Keyless remote state; GitHub auth via the GITHUB_TOKEN env var, never
		// inlined into config (a token in .tf/plan files would leak).
		BackendTF: `terraform {
  backend "s3" {
    # -backend-config="bucket=..." -backend-config="key=..."
    # Any supported backend works; GitHub has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a GITHUB_TOKEN secret
# (a fine-grained PAT or app token), never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env: { GITHUB_TOKEN: '${{ secrets.TF_GITHUB_TOKEN }}' }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
