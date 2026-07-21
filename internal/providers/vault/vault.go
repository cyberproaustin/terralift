// Package vault implements the CloudProvider contract for HashiCorp Vault — a flat, server-scoped
// REST provider for Vault's CONFIGURATION plane. Vault is a secrets store, so the defining design
// rule is that TerraLift adopts config (secret-engine mounts, auth-method mounts, ACL policies,
// audit devices, namespaces, and backend roles) but NEVER enumerates or reads secret DATA — the
// enumeration only touches the sys/* backbone and backend role LISTs by name, never a KV/data/creds
// path, because reading a secret value would write it into config/state. Auth is the X-Vault-Token
// header (+ optional X-Vault-Namespace); the base is VAULT_ADDR. The sys/mounts, sys/auth, and
// sys/audit responses are map-keyed by mount path (a shape unique to this provider); LIST endpoints
// return {"data":{"keys":[...]}}. Roles fan out per mount, discriminated on the mount's type.
// Export reuses `terraform plan -generate-config-out`.
//
// NOTE: Phase-A scaffold. Curation (the field-level secret scrubs — ldap bindpass, db connection
// password, aws secret_key, oidc client_secret) is added when live-validated. Secret DATA
// (vault_generic_secret / vault_kv_secret* / dynamic credentials / root+unseal keys) is
// HARD-EXCLUDED and never enumerated. The type-specific auth-backend CONFIG resources
// (vault_ldap_auth_backend, vault_github_*, identity/*, transit keys) are deferred to later
// increments; TLS is verified against the system trust store (the scaffold does not disable it).
package vault

import (
	"context"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/provider"
)

func init() { provider.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) Name() string { return "vault" }

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
    # Any supported backend works; Vault config is versioned like any other module.
  }
}
`,
		Pipeline: `# GitHub Actions: plan-on-PR + gated apply. Auth via VAULT_ADDR/VAULT_TOKEN secrets,
# never a token committed to config.
name: terraform
on:
  pull_request: { paths: [ 'live/**' ] }
  push: { branches: [ main ], paths: [ 'live/**' ] }
jobs:
  plan:
    runs-on: ubuntu-latest
    env:
      VAULT_ADDR: '${{ vars.TF_VAULT_ADDR }}'
      VAULT_TOKEN: '${{ secrets.TF_VAULT_TOKEN }}'
      # VAULT_NAMESPACE: '${{ vars.TF_VAULT_NAMESPACE }}'  # Enterprise only
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init && terraform validate && terraform plan
`,
	}
}
