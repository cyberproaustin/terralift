// Package grafana implements the CloudProvider contract for Grafana — a flat, org-scoped
// REST provider. Two things set it apart from the other REST providers: the API host is the
// operator's OWN instance (self-hosted or Grafana Cloud), read from GRAFANA_URL; and almost
// every import ID is an org-scoped composite ({{orgID}}:{{token}}), built from the org id
// resolved in Connect. It adopts the config plane: dashboards, folders, data sources, the
// unified-alerting provisioning family (contact points, notification policy, message
// templates, mute timings, rule groups), teams, service accounts, playlists, library panels,
// and (best-effort, Enterprise) custom roles and reports. Enumeration is via the Grafana HTTP
// API (no CLI); export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (grafana_dashboard's model JSON is the heaviest surface of
// any provider — see docs/v2-specs/grafana.md) is added when live-validated. Permissions
// (dashboard/folder), annotations, and instance-scoped grafana_organization are deferred;
// grafana_service_account_token, data_source secure fields, and the Grafana Cloud stack plane
// are excluded (write-only secrets / separate creds).
package grafana

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "grafana" }

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
    # Any supported backend works; Grafana has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via GRAFANA_URL + GRAFANA_AUTH
# secrets, never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      GRAFANA_URL: '${{ vars.TF_GRAFANA_URL }}'
      GRAFANA_AUTH: '${{ secrets.TF_GRAFANA_AUTH }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
