// Package opsgenie implements the CloudProvider contract for Opsgenie — a flat,
// account-scoped REST provider and the closest sibling to PagerDuty. Two things set it apart:
// auth is the distinctive `Authorization: GenieKey <api-key>` header (not Bearer), and
// pagination is a data/paging.next envelope whose next-link is a SERVER-SUPPLIED full URL,
// host-validated before the key is re-sent. It adopts the on-call config plane: teams +
// routing rules, users + contacts + notification rules, schedules + rotations, escalations,
// services + incident rules, API/Email integrations, alert/notification policies, maintenance,
// and heartbeats. Enumeration is via the Opsgenie REST API; export reuses `terraform plan
// -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the api_integration api_key scrub, the schedule/rotation
// timestamp drift, the {{…}} placeholder escaping) is added when live-validated. The
// no-documented-import resources (integration_action, custom_role), the vendor-integration
// plane (type != API/Email), and incident_template are deferred to later increments.
package opsgenie

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "opsgenie" }

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
    # Any supported backend works; Opsgenie has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via an OPSGENIE_API_KEY secret,
# never a key committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env: { OPSGENIE_API_KEY: '${{ secrets.TF_OPSGENIE_API_KEY }}' }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
