// Package gitlab implements the CloudProvider contract for GitLab — a flat, instance-scoped REST
// provider driven by a two-ROOT fan-out: the groups and projects a token can manage, then their
// durable config children (CI/CD variable shells, labels, webhooks, deploy keys, protected
// branches/tags, memberships, milestones, group LDAP links, and project share-group links). Auth is
// the PRIVATE-TOKEN header (a Personal/Project/Group access token); the base is GITLAB_BASE_URL,
// which already carries the /api/v4 suffix (unlike Vault's /v1/). Collections page via ?page= with
// the X-Next-Page response header. The defining hazard is the import id: four colon-composite
// shapes (bare / 2-part / 3-part env-scope / 4-part ldap), precomputed per resource. Export reuses
// `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the field-level secret scrubs — the CI/CD variable VALUE, which
// the API returns on read, plus hook tokens and project runners_token) is added when live-validated.
// Access-token resources (personal/project/group/deploy tokens) are HARD-EXCLUDED and never
// enumerated (they mint live credentials); admin-only planes (system hooks, GET /users) and the
// group-milestone / share-group edge cases are deferred to later increments.
package gitlab

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "gitlab" }

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
    # Any supported backend works; GitLab also offers a native http state backend.
  }
}
`,
		Pipeline: `# GitLab/GitHub CI: plan-on-PR + gated apply. Auth via a GITLAB_TOKEN secret,
# never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      GITLAB_TOKEN: '${{ secrets.TF_GITLAB_TOKEN }}'
      # GITLAB_BASE_URL: '${{ vars.TF_GITLAB_BASE_URL }}'  # self-managed only
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
