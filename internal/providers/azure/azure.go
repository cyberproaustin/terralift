// Package azure implements the CloudProvider contract for Microsoft Azure:
// Azure Resource Graph enumeration (KQL floor + RBAC/policy/exposure enrichers)
// and a born-correct export driven by aztfexport (generate-mapping -> rewrite ->
// import). Scope is a subscription; the per-resource container is the resource group.
package azure

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

// Provider is the Azure implementation of provider.CloudProvider.
type Provider struct{}

func (p *Provider) Name() string { return "azure" }

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
		ProviderTF: `terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
  }
}

provider "azurerm" {
  features {}
}
`,
		// Remote state on Azure Storage, keyless: OIDC + Entra (use_azuread_auth),
		// never a storage key (which would leak into .terraform / plan files).
		BackendTF: `terraform {
  backend "azurerm" {
    use_oidc         = true
    use_azuread_auth = true
    # resource_group_name / storage_account_name / container_name / key
    # supplied at init via -backend-config.
  }
}
`,
		Pipeline: `# Azure DevOps: plan-on-PR + gated apply via Workload Identity Federation (no keys).
trigger:
  branches: { include: [ main ] }
  paths: { include: [ 'live/**' ] }
pr:
  branches: { include: [ main ] }
stages:
  - stage: plan
    jobs:
      - job: plan
        steps:
          - task: AzureCLI@2
            inputs:
              azureSubscription: '$(PLAN_SERVICE_CONNECTION)'   # WIF, Reader
              scriptType: bash
              addSpnToEnvironment: true
              inlineScript: |
                export ARM_USE_OIDC=true ARM_USE_AZUREAD=true
                terraform init && terraform validate && terraform plan
`,
	}
}
