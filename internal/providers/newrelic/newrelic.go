// Package newrelic implements the CloudProvider contract for New Relic — a flat,
// account-scoped provider whose entire API is NerdGraph, a GraphQL endpoint (NOT a family
// of REST list endpoints like every other provider). It adopts the observability config
// plane: dashboards, alert policies + NRQL conditions + muting rules, the notification
// stack (destinations/channels/workflows), synthetics monitors, workloads, key
// transactions, and log obfuscation. Enumeration POSTs GraphQL queries and paginates via
// nextCursor; export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (newrelic_one_dashboard's recursive page/widget/NRQL
// tree is the heaviest surface of any provider — see docs/v2-specs/newrelic.md) is added
// when live-validated. service_level, synthetics_private_location, entity_tags, and the
// deprecated nrql_drop_rule are deferred to later increments; secure_credential and
// api_access_key are excluded (write-only secrets).
package newrelic

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "newrelic" }

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
    # Any supported backend works; New Relic has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via NEW_RELIC_API_KEY (a User
# key) + NEW_RELIC_ACCOUNT_ID + NEW_RELIC_REGION secrets, never a key in config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      NEW_RELIC_API_KEY: '${{ secrets.TF_NEW_RELIC_API_KEY }}'
      NEW_RELIC_ACCOUNT_ID: '${{ secrets.TF_NEW_RELIC_ACCOUNT_ID }}'
      NEW_RELIC_REGION: '${{ vars.TF_NEW_RELIC_REGION }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
