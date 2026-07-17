// Package aws implements the CloudProvider contract for Amazon Web Services:
// AWS Resource Explorer enumeration (cross-region floor + IAM/exposure enrichers)
// and a born-correct export via native Terraform import blocks +
// `plan -generate-config-out` (the same path as the GCP provider — no external
// export tool). Scope is an account; the per-resource container is its region
// (global-service resources land in a "global" stack).
package aws

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

// Provider is the AWS implementation of provider.CloudProvider.
type Provider struct{}

func (p *Provider) Name() string { return "aws" }

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
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  # region supplied per-stack (var/AWS_REGION); auth flows from the environment
  # (AWS_* / OIDC role in CI, or the shared config/SSO locally).
}
`,
		// Remote state on S3 with NATIVE locking (use_lockfile, no DynamoDB) and
		// keyless auth (the workflow assumes an IAM role via OIDC — no keys on disk).
		BackendTF: `terraform {
  backend "s3" {
    use_lockfile = true
    encrypt      = true
    # bucket / key / region supplied at init via -backend-config.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply via OIDC (no long-lived keys).
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
permissions:
  id-token: write   # required for OIDC
  contents: read
jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ secrets.TF_PLAN_ROLE_ARN }}   # WIF, read-only
          aws-region: us-east-1
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
