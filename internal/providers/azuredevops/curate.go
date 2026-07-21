package azuredevops

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Azure DevOps is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/azuredevops.md). The recurring hazard is FIELD-LEVEL
// write-only secrets on a few resources. Phase-B work, by resource:
//   - azuredevops_variable_group — the #1 secret surface. Secret variables carry a `value` the API
//     masks on read (returns null) but the SHELL adopts; Key Vault-backed groups reference a
//     connected vault. SCRUB any emitted secret value; keep the non-secret variables + the KV link.
//     enumerate never decodes the variables map, so the leak risk is only in generate-config-out.
//   - azuredevops_build_definition — inline `is_secret` pipeline variables → SCRUB those values.
//   - azuredevops_git_repository / azuredevops_project / azuredevops_team / azuredevops_environment /
//     azuredevops_agent_queue / azuredevops_agent_pool / azuredevops_group — config only, no secret;
//     adopt as-is.
//
// HARD-DEFER (never adopted in Phase A — the whole family holds credentials): the service-endpoint /
// service-connection resources (azuredevops_serviceendpoint_*), whose `authorization`/credential
// blobs are live secrets (a PAT, client secret, SSH key, cloud credential). They are never
// enumerated. Service hooks (consumer auth) and the personal-access-token/entitlement admin planes
// are likewise deferred.
//
// Until Phase B these are no-ops, so an Azure DevOps export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the variable-group secret
// values / is_secret pipeline vars that generate-config-out may leave in generated.tf before the
// scrub rules land). The PAT is never inlined — providers.tf is env-auth only.

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
