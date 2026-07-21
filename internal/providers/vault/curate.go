package vault

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Vault is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/vault.md). Vault is a secrets store, so the design's
// FIRST line of defense is that secret DATA is never enumerated — the enumeration only reads the
// sys/* config backbone and backend role LISTs (by name), never a KV/data/creds path. What remains
// for Phase B is scrubbing the FIELD-LEVEL write-only secrets that generate-config-out may emit on
// the adopted CONFIG resources (the API masks most on read, but not all), by resource:
//   - vault_ldap_auth_backend / vault_ldap_secret_backend — `bindpass` (the bind DN password) →
//     SCRUB + flag re-supply out-of-band.
//   - vault_database_secret_backend_connection — the `connection_url` embeds a password, and
//     `password`/`private_key` fields → SCRUB (prefer the templated {{username}}/{{password}} form).
//   - vault_aws_secret_backend — root `secret_key` (+ `access_key`) → SCRUB.
//   - vault_jwt_auth_backend / vault_*_auth_backend — `oidc_client_secret` / `client_secret` /
//     `bound_*` shared secrets → SCRUB.
//   - vault_pki_secret_backend_role / *_secret_backend_role / *_auth_backend_role — the ROLES carry
//     no secret material themselves (policy refs, TTLs, allowed domains) → adopt as-is; keep any
//     policy/template strings LITERAL (they carry ${…}-style Vault templating).
//   - vault_mount / vault_auth_backend / vault_audit / vault_policy / vault_namespace — config only,
//     no secret; adopt as-is. The audit device `options` may name a file path, not a secret.
//
// HARD-EXCLUDE (never adopted, never enumerated — permanent, NOT a Phase-B toggle): vault_generic_
// secret, vault_kv_secret*, any KV data path, dynamic credential reads (<pki>/issue, <db>/creds,
// <aws>/creds, <transit>/export), cubbyhole, and root/unseal/recovery keys.
//
// Until Phase B these are no-ops, so a Vault export is a breadth scaffold, not yet plan-clean (the
// pipeline's repo-wide secret scan is the backstop for the ldap bindpass / db connection password /
// aws secret_key / oidc client_secret that generate-config-out may leave in generated.tf before the
// scrub rules land). The Vault token is never inlined — providers.tf is env-auth only.

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
