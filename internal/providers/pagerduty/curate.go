package pagerduty

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for PagerDuty is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/pagerduty.md). PagerDuty has NO single monster
// resource; the weight is spread, and the recurring hazards are the integration/webhook/
// extension SECRETS, the schedule timestamp drift, and (in the deferred event-orchestration
// plane) the PCL/template escaping. Phase-B work, by resource:
//   - pagerduty_service_integration — SECRET. integration_key (the Events-API routing key) is
//     Computed+Sensitive and IS returned on read → SCRUB the value, keep the block shape.
//   - pagerduty_webhook_subscription — SECRET. delivery_method.custom_header[] values (bearer/
//     signing tokens) → scrub; the signing secret is create-only (never on read) → note as
//     un-round-trippable, re-supply out-of-band.
//   - pagerduty_extension / _servicenow — SECRET. generic config auth tokens; the ServiceNow
//     snow_password / api_key (write-only) and secret endpoint_url → scrub.
//   - pagerduty_service — prune computed html_url/created_at/status/last_incident_timestamp;
//     null auto_resolve/acknowledgement timeouts mean "account default" (tolerate). escalation_
//     policy is a ref.
//   - pagerduty_schedule — layer[] rotations; time_zone required; prune computed layer[].id/
//     html_url. The start/rotation_virtual_start timestamps DRIFT (server-normalized) — a known
//     perpetual-diff hazard; may need ignore_changes.
//   - pagerduty_escalation_policy — rule[] ordering is significant (preserve); prune html_url.
//   - pagerduty_team_membership — role (manager/responder/observer) is carried; user-first colon
//     import. pagerduty_user — CAUTION: your own user (behind the token) appears; adopt, don't
//     lock yourself out. contact_method/notification_rule are PII but NOT secret (adopt).
//   - pagerduty_maintenance_window — only future windows are enumerated (past = dead data);
//     start_time/end_time + services refs.
//   - pagerduty_ruleset_rule (legacy) — conditions/actions carry PagerDuty PCL/{{…}} template
//     strings → keep LITERAL (same class as the Datadog widget-query escaping).
//
// Until Phase B these are no-ops, so a PagerDuty export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the integration_key /
// webhook / extension secrets that generate-config-out emits before the scrub rules land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
