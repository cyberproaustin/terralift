// Package auth0 implements the CloudProvider contract for Auth0 — a flat, tenant-scoped REST
// provider for the identity/CIAM platform. It is distinguished by its auth: unlike every prior
// provider's static header token, Auth0's Management API uses a short-lived Bearer minted at
// connect time by an OAuth2 client-credentials EXCHANGE (POST /oauth/token with an M2M
// client_id/secret), with a static AUTH0_API_TOKEN as a bypass. The base URL is the tenant
// domain (host-from-config); pagination is page/per_page + include_totals keyed envelopes (plus
// a bare-array log-streams endpoint, a fixed-name email-template fan-out, and single-object
// settings singletons). It adopts the config core: clients, resource servers, connections,
// roles, actions, organizations, client grants, log streams, email templates, and the six
// tenant-wide settings singletons. Export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold (Auth0 is a 60+-resource provider; this is the config core). Curation
// (the connection strategy tree + the app/connection/resource-server/action/email/guardian/
// log-stream secret scrubs) is added when live-validated. The long tail — the user plane, the
// :: relationship/membership resources, deprecated rules/hooks, custom domains, the singleton
// sub-settings, and the Forms/Flows/key-management planes — is deferred to later increments.
package auth0

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "auth0" }

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
    # Any supported backend works; Auth0 has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via AUTH0_DOMAIN + AUTH0_CLIENT_ID
# + AUTH0_CLIENT_SECRET secrets (an M2M app), never inlined in config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      AUTH0_DOMAIN: '${{ vars.TF_AUTH0_DOMAIN }}'
      AUTH0_CLIENT_ID: '${{ secrets.TF_AUTH0_CLIENT_ID }}'
      AUTH0_CLIENT_SECRET: '${{ secrets.TF_AUTH0_CLIENT_SECRET }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
