package keycloak

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Keycloak is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/keycloak.md). keycloak_realm is the HEAVIEST
// curation surface (a sprawling settings object — the Keycloak analogue of auth0_connection /
// datadog_dashboard); the recurring hazards are the client/idp/ldap/smtp SECRETS, the `$`→`$$`
// literal-escape hazard (Terraformer's PostConvertHook), and the list-ordering churn. Phase-B
// work, by resource:
//   - keycloak_realm — the big one. SECRET: smtp_server.password → SCRUB. Sort
//     internationalization.supported_locales for reproducibility. Defaults over-emit heavily.
//   - keycloak_openid_client — SECRET: client_secret (CONFIDENTIAL clients, returned on read) →
//     SCRUB (PUBLIC/BEARER-ONLY have none). Sort valid_redirect_uris + web_origins. `$`→`$$` on
//     root_url/name. Prune computed service_account_user_id.
//   - keycloak_saml_client / keycloak_saml_identity_provider — signing_private_key /
//     encryption_private_key is key material → SCRUB/do-not-round-trip (the public cert is fine).
//   - keycloak_role — client_id (UUID attr) is the realm-vs-client discriminator; sort
//     composite_roles. No secret.
//   - keycloak_group — parent_id refs the parent group UUID (map through the flattened tree);
//     prune computed path. Group memberships/roles are separate deferred resources.
//   - keycloak_openid_client_scope — consent_screen_text carries the `$`→`$$` hazard; the scope's
//     protocol-mapper children are a separate deferred plane.
//   - keycloak_authentication_flow — the flow SHELL only; sub-flows/executions/execution-configs
//     are deferred. builtIn flows skipped at enumeration.
//   - keycloak_oidc_identity_provider — SECRET: client_secret (external IdP secret) → SCRUB.
//   - keycloak_ldap_user_federation — SECRET: config.bindCredential (LDAP bind password) → SCRUB.
//     LDAP mappers are a separate deferred plane.
//   - keycloak_required_action — trivial; priority ordering may churn (tolerate).
//
// Until Phase B these are no-ops, so a Keycloak export is a breadth scaffold, not yet plan-clean
// (the pipeline's repo-wide secret scan is the backstop for the client_secret / bind_credential /
// idp client_secret / smtp password / SAML private-key material that generate-config-out emits
// before the scrub rules land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
