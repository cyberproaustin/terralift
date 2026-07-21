// Package datadog implements the CloudProvider contract for Datadog — a flat, org-scoped
// provider whose scope is simply the DD_API_KEY + DD_APP_KEY pair (no sub-account). It
// adopts the observability config plane (monitors, dashboards, SLOs, synthetics, logs
// config, notebooks, security rules, downtimes) plus IAM-ish breadth (roles, users).
// Enumeration is via the Datadog REST API (no CLI) — TWO auth headers, a site-configurable
// base URL, and three response families across API v1 and v2; export reuses `terraform
// plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (datadog_dashboard is the heaviest surface of any
// provider — the recursive widget tree, see docs/v2-specs/datadog.md) is added when
// live-validated.
package datadog

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "datadog" }

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
    # Any supported backend works; Datadog has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via DD_API_KEY + DD_APP_KEY
# secrets, never keys committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      DD_API_KEY: '${{ secrets.TF_DD_API_KEY }}'
      DD_APP_KEY: '${{ secrets.TF_DD_APP_KEY }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
