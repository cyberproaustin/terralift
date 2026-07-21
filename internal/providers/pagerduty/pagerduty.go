// Package pagerduty implements the CloudProvider contract for PagerDuty — a flat,
// account-scoped REST provider. Two things set it apart: auth is the distinctive
// `Authorization: Token token=<token>` header (not Bearer), and import IDs mix bare P-prefixed
// ids with composites that use DIFFERENT separators (dot for service_integration/ruleset_rule,
// colon for the team_membership/user_* composites). It adopts the on-call config plane:
// services + integrations, escalation policies, schedules, teams + memberships, users +
// contact methods + notification rules, business services, maintenance windows, extensions,
// webhook subscriptions, tags, response plays, and the legacy ruleset plane. Enumeration is
// via the PagerDuty REST API (keyed offset/`more` pager, five per-parent fan-outs); export
// reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the integration/webhook/extension secret scrubs, the
// schedule timestamp drift) is added when live-validated. Event Orchestration (unverified
// cursor pager), automation_actions (feature-gated), and slack_connection (different host +
// OAuth) are deferred to later increments.
package pagerduty

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "pagerduty" }

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
    # Any supported backend works; PagerDuty has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a PAGERDUTY_TOKEN secret,
# never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env: { PAGERDUTY_TOKEN: '${{ secrets.TF_PAGERDUTY_TOKEN }}' }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
