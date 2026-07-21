// Package okta implements the CloudProvider contract for Okta — a flat, org-scoped REST
// provider for the identity/access platform. Three things set it apart: auth is the
// distinctive `Authorization: SSWS <api-token>` header (not Bearer); the base URL is
// CONSTRUCTED from OKTA_ORG_NAME + OKTA_BASE_URL (host-from-config); and pagination is
// LINK-HEADER (RFC 5988) — bare JSON array bodies with the next-page URL in the HTTP Link
// header's rel="next", host-validated before the token is re-sent. It adopts the config core:
// users, groups + rules, user types, the signOnMode-discriminated app family, trusted origins,
// network zones, auth servers (the deepest fan-out, incl. the 3-part policy-rule composite),
// the ?type=-discriminated signon/password/mfa policies + rules, inline/event hooks, and the
// OIDC/SAML IdPs. Export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold (Okta is a 100+-resource provider; this is the config core). Curation
// (the app/idp/hook secret scrubs, the ${…} Okta-EL escaping, the default-singleton flags) is
// added when live-validated. The long tail — profile-schema, brand/theme/email, factors/
// authenticators/captcha, app-assignment + membership composites, behaviors, the other policy
// families, social IdPs — is deferred to later increments.
package okta

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "okta" }

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
    # Any supported backend works; Okta has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via OKTA_ORG_NAME + OKTA_BASE_URL
# + OKTA_API_TOKEN secrets, never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      OKTA_ORG_NAME: '${{ vars.TF_OKTA_ORG_NAME }}'
      OKTA_BASE_URL: '${{ vars.TF_OKTA_BASE_URL }}'
      OKTA_API_TOKEN: '${{ secrets.TF_OKTA_API_TOKEN }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
