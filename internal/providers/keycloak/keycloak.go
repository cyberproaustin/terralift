// Package keycloak implements the CloudProvider contract for Keycloak — a flat, server-scoped
// REST provider for the self-hosted Keycloak identity server. It is realm-centric: most
// resources are enumerated via a per-realm FAN-OUT (GET /admin/realms → per-realm clients/roles/
// groups/scopes/flows/idps/federations), with a second-level per-(realm, client) fan-out for
// client roles. Three things set it apart: auth is a FORM-encoded OAuth2 token exchange with two
// grant modes (client-credentials or password) and short-lived tokens refreshed mid-run; the
// base URL is the user-supplied self-hosted server + a base_path (/auth on legacy Keycloak); and
// pagination is first/max offset on bare JSON arrays. It adopts the config core: realms, OIDC/
// SAML clients, roles (realm + client), groups, OIDC client scopes, authentication flows, OIDC/
// SAML identity providers, LDAP user federations, and required actions. Export reuses `terraform
// plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the client/idp/ldap/smtp secret scrubs, the `$`→`$$` escape,
// the list-ordering sorts) is added when live-validated. The long tail — the user plane, the
// protocol-mapper plane (the biggest deferred surface), the default-scope/group assignment
// composites, the auth sub-flow/execution depth, LDAP mappers, SAML client scopes, social IdPs —
// is deferred to later increments.
package keycloak

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "keycloak" }

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
    # Any supported backend works; Keycloak has no native state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via KEYCLOAK_URL + KEYCLOAK_CLIENT_ID
# + KEYCLOAK_CLIENT_SECRET secrets (a service-account client), never inlined in config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      KEYCLOAK_URL: '${{ vars.TF_KEYCLOAK_URL }}'
      KEYCLOAK_CLIENT_ID: '${{ secrets.TF_KEYCLOAK_CLIENT_ID }}'
      KEYCLOAK_CLIENT_SECRET: '${{ secrets.TF_KEYCLOAK_CLIENT_SECRET }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
