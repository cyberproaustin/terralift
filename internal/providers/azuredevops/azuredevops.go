// Package azuredevops implements the CloudProvider contract for Azure DevOps Services
// (dev.azure.com) — a flat, org-scoped REST provider driven by an org→project fan-out plus two
// org-level roots (agent pools; graph groups on the separate vssps.dev.azure.com host). Several
// mechanics set it apart: auth is a Personal Access Token sent over HTTP Basic
// (base64(":"+PAT)); every request requires an ?api-version= query param; list responses are the
// VSTS {"count":N,"value":[...]} envelope; pagination is signaled by the x-ms-continuationtoken
// response header; and a bad/expired PAT returns a 203 Non-Authoritative HTML sign-in page rather
// than a 401 (normalized to a 401 auth failure). Import ids are four shapes (bare GUID/int/
// descriptor, or <projectGUID>/<child>). Export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the field-level secret scrubs — variable-group secret values,
// is_secret pipeline variables) is added when live-validated. The service-endpoint / service-
// connection family (whose authorization blobs are live credentials) is never enumerated; service
// hooks, the policy plane, and the entitlement/PAT admin planes are deferred to later increments.
package azuredevops

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "azuredevops" }

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
  backend "azurerm" {
    # -backend-config="storage_account_name=..." -backend-config="container_name=..."
    # Any supported backend works; Azure DevOps has no native Terraform state store.
  }
}
`,
		Pipeline: `# Azure Pipelines / GitHub Actions: plan-on-PR + gated apply. Auth via
# AZDO_ORG_SERVICE_URL + AZDO_PERSONAL_ACCESS_TOKEN secrets, never a PAT in config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      AZDO_ORG_SERVICE_URL: '${{ vars.TF_AZDO_ORG_SERVICE_URL }}'
      AZDO_PERSONAL_ACCESS_TOKEN: '${{ secrets.TF_AZDO_PERSONAL_ACCESS_TOKEN }}'
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
