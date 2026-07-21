package logzio

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Logz.io is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/logzio.md). Logz.io has NO single monster
// resource; the recurring hazard is that its secrets are spread as FIELD-LEVEL write-only
// values across the config resources (the API returns them null/masked on read) rather than as
// standalone credential resources. Phase-B work, by resource:
//   - logzio_endpoint — the #1 secret surface. The type-specific block (slack/custom-webhook/
//     pagerduty/datadog/victorops/opsgenie/bigpanda/servicenow/msteams) carries a Required
//     write-only credential the API does not return on read: slack/webhook url (embedded token),
//     custom headers, pagerduty service_key, datadog api_key, victorops routing_key/api_key,
//     opsgenie/bigpanda api keys → SCRUB + flag re-supply out-of-band. Adopt the endpoint shell.
//   - logzio_alert_v2 — sub_components (query + threshold tiers) + notification_emails +
//     alert_notification_endpoints (refs endpoint ids). Query strings carry ${…}/Lucene → keep
//     LITERAL. Prune computed alert_id/created_at/last_updated; tolerate tier-block ordering.
//   - logzio_log_shipping_token — the `token` VALUE is write-only (minted on create) → SCRUB;
//     the shell (name/enabled) is adoptable but the value can't be reproduced.
//   - logzio_s3_fetcher — aws_secret_key write-only → SCRUB (prefer the aws_arn IAM-role variant
//     with no inline secret). logzio_archive_logs — storage creds (S3 secret / Blob key / SAS)
//     write-only → SCRUB.
//   - logzio_subaccount / logzio_metrics_account — the sharing token (account_token /
//     sharing_objects tokens) is write-only → SCRUB. CAUTION: your own account/user may appear.
//   - logzio_user — no password attribute (no secret); CAUTION: the token-owner user may appear.
//   - logzio_drop_filter / logzio_authentication_groups — light, no secret (the auth-groups
//     singleton adopts the whole SAML group set — expect over-emit).
//
// Until Phase B these are no-ops, so a Logz.io export is a breadth scaffold, not yet plan-clean
// (the pipeline's repo-wide secret scan is the backstop for the endpoint credentials / token
// values / AWS+storage keys that generate-config-out nulls-or-leaks before the scrub rules land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
