// Package honeycomb implements the CloudProvider contract for Honeycomb — a flat,
// environment-scoped REST provider. It is dataset-centric: most resources are enumerated via a
// per-dataset FAN-OUT (parent GET /1/datasets → per-dataset sub-lists), exactly like Fastly's
// per-service fan-out, with a second-level per-SLO burn-alert fan-out and a synthetic
// "__all__" pass for environment-wide (non-Classic) derived columns and multi-dataset
// triggers/SLOs. It adopts the config plane: datasets, columns, derived columns, query
// annotations, flexible boards, triggers, SLOs, burn alerts, and the typed recipients.
// Enumeration is via the Honeycomb v1 config API (X-Honeycomb-Team header, bare JSON arrays,
// no pagination); export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation is added when live-validated. Not-importable resources
// (dataset_definition, marker, marker_setting, board_view), the no-list-endpoint
// honeycombio_query, and the v2 management plane (environment, api_key) are deferred;
// honeycombio_api_key and recipient secrets are excluded / scrubbed (write-only).
package honeycomb

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "honeycomb" }

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
    # Any supported backend works; Honeycomb has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a HONEYCOMB_API_KEY secret,
# never a key committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env: { HONEYCOMB_API_KEY: '${{ secrets.TF_HONEYCOMB_API_KEY }}' }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
