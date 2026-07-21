package auth0

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Auth0 is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/auth0.md). auth0_connection is the HEAVIEST
// curation surface (the strategy-dependent options tree — Auth0's analogue of Okta's app family
// / Datadog's datadog_dashboard); the recurring hazards are the client/connection/resource-
// server/action/email/guardian/log-stream SECRETS and the settings-singleton over-emit. Phase-B
// work, by resource:
//   - auth0_connection — the big one. `strategy` (auth0/google-oauth2/samlp/waad/oidc/okta/…)
//     selects the options sub-schema AND the secrets. SCRUB options.client_secret,
//     options.credentials (SAML/SCIM), and the auth0-db options.configuration / custom_scripts
//     secrets. enabled_clients refs client ids. Prune computed provisioning_ticket_url.
//   - auth0_client — SECRET. client_secret (Sensitive, returned on read) → SCRUB; prune read-only
//     signing_keys/encryption_key. CAUTION: the M2M app TerraLift authenticates with appears in
//     /clients — adopt but don't alter its grant/secret out from under the run.
//   - auth0_resource_server — SECRET. signing_secret (HS256, computed/Sensitive) → SCRUB. Skip the
//     system Management API resource server (done at enumeration).
//   - auth0_action — SECRET + code. `secrets` VALUES are write-only (API returns only names) →
//     EXCLUDE the values. `code` (JS) may contain ${…}-looking text → keep LITERAL.
//   - auth0_role / auth0_organization / auth0_client_grant — light, no secret; the role→permission,
//     org→connection/member, and (deferred) joins are separate :: resources.
//   - auth0_log_stream — SECRET in sink. sink datadog_api_key/splunk_token/HTTP authorization →
//     SCRUB those fields, keep the stream.
//   - auth0_email_provider — SECRET. credentials block (SMTP password / API key / AWS secret) is
//     write-only → EXCLUDE the credentials block.
//   - auth0_guardian — SECRET. SMS/push provider blocks carry Twilio auth_token / Duo secret_key /
//     custom keys → SCRUB.
//   - auth0_email_template — Liquid {{ }}/{% %} body must stay LITERAL. auth0_tenant/_branding/
//     _prompt/_attack_protection — settings singletons; defaults over-emit heavily; prune computed.
//     attack_protection combines 3 sub-objects into one resource (Phase-B).
//
// Until Phase B these are no-ops, so an Auth0 export is a breadth scaffold, not yet plan-clean
// (the pipeline's repo-wide secret scan is the backstop for the client_secret/signing_secret/
// connection-options/action/email/guardian/log-stream secrets that generate-config-out emits
// before the scrub rules land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
