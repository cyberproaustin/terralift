// Package mackerel implements the CloudProvider contract for Mackerel (mackerel.io, by Hatena) —
// a flat, org-scoped REST provider for the monitoring platform. Two things set it apart: auth is a
// custom X-Api-Key header, and services act as a fan-out KEY for roles (GET /api/v0/services/
// <svc>/roles) rather than a container tree. Every list endpoint returns a {"<key>":[...]}
// named-array envelope over an opaque string id (services/roles identify by name). It adopts the
// native config plane: services + roles, monitors (7 polymorphic kinds → one mackerel_monitor
// type), channels, notification groups, dashboards, AWS integrations, downtimes, and alert-group
// settings. Export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the field-level secret scrubs — channel webhook tokens,
// aws_integration secret_key, external-monitor auth headers) is added when live-validated. Hosts
// (agent-registered, no TF resource), users (invite-only), the *_metadata resources, and the
// default_notification_group singleton are deferred to later increments; the provider source is
// mackerelio-labs/mackerel (pre-1.0 — pin ~> 0.9).
package mackerel

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "mackerel" }

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
    # Any supported backend works; Mackerel has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a MACKEREL_APIKEY secret,
# never a key committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      MACKEREL_APIKEY: '${{ secrets.TF_MACKEREL_APIKEY }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
