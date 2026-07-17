// Package gcp implements the CloudProvider contract for Google Cloud:
// Cloud Asset Inventory enumeration (search-all-resources --read-mask="*" gives
// metadata AND full config in one sweep; search-all-iam-policies for IAM), and a
// born-correct export built on native Terraform import blocks (we author the
// `to` address; import IDs are derived per-type from CAI asset names).
package gcp

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

// Provider is the GCP implementation of provider.CloudProvider.
type Provider struct{}

func (p *Provider) Name() string { return "gcp" }

func (p *Provider) CheckDependencies(ctx context.Context, run *core.Run) (*provider.DependencyReport, error) {
	return checkDependencies(ctx, run)
}

func (p *Provider) Connect(ctx context.Context, run *core.Run) (*provider.AuthContext, error) {
	return connect(ctx, run)
}

func (p *Provider) Enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	// search-all-resources --read-mask="*" (floor+truth in one), search-all-iam-policies
	// (IAM join), + public-exposure enrichers -> cloud-neutral Inventory.
	return enumerate(ctx, run)
}

func (p *Provider) Export(ctx context.Context, run *core.Run, inv *model.Inventory) (*provider.ExportResult, error) {
	// per-type import-ID derivation -> born-correct `import{}` blocks ->
	// terraform plan -generate-config-out (draft) -> scrub secrets.
	return export(ctx, run, inv)
}

func (p *Provider) Templates() provider.ProviderTemplates {
	return provider.ProviderTemplates{
		MigrationAttrs: map[string]string{
			"project": "project", "region": "region", "zone": "zone", "location": "location",
		},
		ProviderTF: `terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
  }
}

provider "google" {}
`,
		// Keyless remote state: bucket/prefix supplied at init via -backend-config;
		// auth via ADC / Workload Identity Federation, never a service-account key
		// (inline creds would leak into .terraform and plan files).
		BackendTF: `terraform {
  backend "gcs" {
    # -backend-config="bucket=..." -backend-config="prefix=..."
    # auth: ADC / WIF (no keys). Never inline credentials.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply, Workload Identity Federation (no keys).
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
permissions: { contents: read, id-token: write }
jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: google-github-actions/auth@v3
        with:
          workload_identity_provider: '${{ vars.WIF_PROVIDER }}'  # Direct WIF, no SA key
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
