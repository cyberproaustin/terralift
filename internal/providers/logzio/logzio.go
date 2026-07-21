// Package logzio implements the CloudProvider contract for Logz.io — a flat, account-scoped
// REST provider for the observability platform. It is the simplest shape among the recent
// providers: no fan-out, one flat account container. Two things set it apart: auth is a custom
// X-API-TOKEN header, and the base URL is region-specific (LOGZIO_REGION → api[-<region>].logz.io,
// or a LOGZIO_BASE_URL override). Its enumeration SHAPE varies per resource — GET bare-list,
// POST …/search|/retrieve with a body, or a singleton GET. It adopts the native config plane:
// alerts (v2), notification endpoints, drop filters, sub-accounts, users, log-shipping tokens,
// s3 fetchers, archive settings, metrics accounts, and the auth-groups singleton. Export reuses
// `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the field-level secret scrubs — endpoint credentials, token
// values, AWS/storage keys) is added when live-validated. The grafana_* embedded-Grafana plane
// (five resources that mirror the standalone grafana provider — UID imports + a second API
// shape) and the Kibana data-view are deferred to later increments; logzio_alert (legacy) is
// dropped in favour of logzio_alert_v2.
package logzio

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "logzio" }

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
    # Any supported backend works; Logz.io has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a LOGZIO_API_TOKEN secret,
# never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      LOGZIO_API_TOKEN: '${{ secrets.TF_LOGZIO_API_TOKEN }}'
      LOGZIO_REGION: '${{ vars.TF_LOGZIO_REGION }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
