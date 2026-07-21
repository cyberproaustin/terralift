package okta

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Okta is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/okta.md). Okta has NO single monster resource;
// the weight is spread across the app/policy/rule config, and the recurring hazards are the
// app/idp/hook SECRETS, the ${…} Okta Expression Language templates (Terraformer needed an
// escapeDollar ${→$${ hook), and the default/built-in singletons. Phase-B work, by resource:
//   - App family (okta_app_oauth/_saml/_swa/_auto_login/_basic_auth/_three_field/
//     _secure_password_store/_bookmark) — SECRET + EL. okta_app_oauth.client_secret (Sensitive,
//     returned on read) → SCRUB; SWA-family shared_password → scrub. SAML attribute_statements
//     + OIDC/SWA field mappings carry Okta-EL ${user.…}/${app.…} strings → keep LITERAL
//     (escapeDollar ${→$${). Prune computed sign_on_mode/name/logo_url/id. App assignments NOT
//     adopted (deferred).
//   - okta_user — EXCLUDE the credentials block (password / recovery_question.answer are
//     write-only, never returned). PII (email/name) is not secret. CAUTION: the token-owner
//     admin user appears — adopt, don't lock yourself out.
//   - okta_group / okta_group_rule — group_rule.expression_value is Okta EL (${…} literal
//     hazard); group_assignments are group-id refs.
//   - okta_auth_server (+ scope/claim/policy/rule) — the deepest fan-out. Signing credentials
//     rotate + are Okta-managed (prune computed kid/credentials). Claim value is Okta EL
//     (${…} literal). Rule priority ordering is significant. default server + sub claim are
//     adopt-in-place singletons.
//   - okta_policy_* / okta_policy_rule_* — priority ordering churns; the Default Policy /
//     default rule are built-in (adopt-in-place, not creatable/deletable). Prune computed id/system.
//   - okta_inline_hook / okta_event_hook — SECRET. channel.config.auth_scheme.value (+ custom
//     headers) is the write-only bearer token Okta presents to your callback → SCRUB the value;
//     the uri is not secret.
//   - okta_idp_oidc — SECRET. client_secret (write-only on read) → SCRUB. okta_idp_saml — the
//     signing key is Okta-managed / the IdP cert is public (nothing to scrub).
//   - okta_network_zone / okta_trusted_origin / okta_user_type — light, no secret; the default
//     user type is built-in.
//
// Until Phase B these are no-ops, so an Okta export is a breadth scaffold, not yet plan-clean
// (the pipeline's repo-wide secret scan is the backstop for the app/idp client_secret and the
// hook auth-header value that generate-config-out emits before the scrub rules land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
