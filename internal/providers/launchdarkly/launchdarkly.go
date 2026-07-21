// Package launchdarkly implements the CloudProvider contract for LaunchDarkly — a flat,
// account-scoped REST provider for the feature-flag / experimentation platform. It is
// project-centric: most resources are enumerated via a per-project FAN-OUT (GET /api/v2/projects
// → per-project environments/flags/metrics), with a second-level per-(project, environment)
// fan-out for segments/destinations and a flag×env derivation for the per-env flag targeting.
// Three things set it apart: auth is a RAW token on the Authorization header (NO scheme prefix
// — the inverse of the GenieKey/SSWS/Bearer providers); pagination is HATEOAS _links.next.href
// (a server-supplied URL, host-validated before the token is re-sent); and import IDs are `/`
// composites at 1/2/3-part depth (the env-scoped 3-part ids put the env in the MIDDLE).
//
// NOTE: Phase-A scaffold. Curation (the environment SDK-key / destination-config / webhook-secret
// scrubs, the include_in_snippet prune) is added when live-validated. The flag×env plane is the
// largest inventory this provider produces (tens of thousands on a big account). The long tail —
// access_token (write-only), relay proxy config, flag triggers/approvals/workflows, context
// kinds, inline project environments — is deferred to later increments.
package launchdarkly

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "launchdarkly" }

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
    # Any supported backend works; LaunchDarkly has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via a LAUNCHDARKLY_ACCESS_TOKEN
# secret, never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env: { LAUNCHDARKLY_ACCESS_TOKEN: '${{ secrets.TF_LAUNCHDARKLY_ACCESS_TOKEN }}' }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
