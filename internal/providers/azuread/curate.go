package azuread

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Entra ID is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/azuread.md). The recurring hazard is FIELD-LEVEL
// write-only secrets on the identity objects — but Graph masks most on read (it returns credential
// METADATA, e.g. key ids/hints/expiry, not the secret value), so the primary Phase-B work is
// pruning computed metadata and confirming the v3 path-prefix imports round-trip. By resource:
//   - azuread_application_registration / azuread_service_principal — the app/SP SHELL is adoptable;
//     `passwordCredentials` (client secrets) and `keyCredentials` (certs) are never returned as
//     values on read (Graph gives metadata only) and are managed by the DEDICATED resources
//     (azuread_application_password / _certificate, azuread_service_principal_password) which are
//     NOT enumerated. Prune the computed credential-metadata blocks; keep the app manifest LITERAL.
//   - azuread_conditional_access_policy — no secret; large nested condition/grant blocks → keep
//     LITERAL, prune computed ids/createdDateTime.
//   - azuread_named_location — ip vs country discriminator (VERIFY); no secret.
//   - azuread_group / azuread_group_member / azuread_administrative_unit /
//     azuread_directory_role_assignment / azuread_app_role_assignment — no secret; adopt as-is.
//     group_member may show a post-import diff if the group also declares members inline
//     (ignore_changes candidate — VERIFY at Phase B).
//
// Until Phase B these are no-ops. A azuread export is a breadth scaffold, not yet plan-clean, but
// the secret-exposure surface is small (Graph does not return credential values on read; the
// credential resources are not enumerated). The client secret is never inlined — providers.tf is
// env-auth only (the Azure SDK credential chain).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
