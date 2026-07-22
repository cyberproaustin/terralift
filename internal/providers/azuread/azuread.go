// Package azuread implements the CloudProvider contract for Microsoft Entra ID (formerly Azure AD)
// over the Microsoft Graph API — a flat, tenant-scoped provider. Auth is an OAuth2
// client-credentials exchange (a form-encoded POST to the tenant's login.microsoftonline.com token
// endpoint → a short-lived Graph Bearer refreshed on a mid-run 401, the Keycloak analogue). Lists
// are the OData {"value":[...],"@odata.nextLink":"<abs-url>"} envelope, paged by following the
// server-supplied nextLink after host-validating it stays on graph.microsoft.com (the token-exfil
// guard). The defining hazard is the import id: the azuread v3 provider changed every object import
// from a bare UUID to a Graph-PATH prefix (/groups/<id>, /servicePrincipals/<id>, …), with two
// relationship composites (<group>/member/<id>, /servicePrincipals/<sp>/appRoleAssignedTo/<id>) and
// a bare-id directory-role assignment. Export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation is minimal (Graph returns credential METADATA, not secret values,
// on read; the dedicated credential resources are not enumerated) — mostly computed-field pruning
// at Phase B. Users (PII + tenant scale) are deferred/off by default; the credential resources
// (azuread_application_password/_certificate, SP secrets), azuread_directory_role (not importable),
// the decomposed azuread_application_* planes, owners/PIM/entitlement RBAC, and /beta surfaces are
// deferred to later increments. Pinned to hashicorp/azuread ~> 3.x (the v3 import-id shape).
package azuread

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "azuread" }

func (p *Provider) Capabilities() provider.Capabilities {
	// IAM is false (mirroring Keycloak): Entra's access control is adopted as ordinary resources
	// (group memberships, role/app-role assignments), not a separate IAM plane the engine analyzes.
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
  backend "azurerm" {
    # -backend-config="storage_account_name=..." -backend-config="container_name=..."
    # Any supported backend works; Entra ID has no native Terraform state store.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via the ARM_* client-credentials
# secrets, never a client secret in config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      ARM_TENANT_ID: '${{ vars.TF_ARM_TENANT_ID }}'
      ARM_CLIENT_ID: '${{ vars.TF_ARM_CLIENT_ID }}'
      ARM_CLIENT_SECRET: '${{ secrets.TF_ARM_CLIENT_SECRET }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
