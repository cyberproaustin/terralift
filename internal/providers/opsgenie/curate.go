package opsgenie

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Opsgenie is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/opsgenie.md). Opsgenie has NO single monster
// resource; the weight is spread across the routing/rule trees, and the recurring hazards are
// the api_integration api_key secret, the {{…}} alert-field placeholders in rule/policy
// message templates, and the schedule/rotation timestamp drift. Phase-B work, by resource:
//   - opsgenie_api_integration — SECRET. api_key is a COMPUTED Events-API credential returned
//     on read (the value that fires alerts into Opsgenie) → SCRUB the value, keep the block.
//     This is the provider's defining secret (the PagerDuty integration_key analogue).
//   - opsgenie_team — inline `member` blocks (id + role); CAUTION: the key-owner user likely
//     appears in a roster — adopt but don't lock yourself out. Prune computed.
//   - opsgenie_team_routing_rule / opsgenie_escalation — criteria/notify/rule trees; ORDER is
//     significant (routing/escalation precedence) — preserve it.
//   - opsgenie_schedule / _rotation — timezone required; rotation start_date/end_date (RFC3339)
//     can DRIFT (server-normalized) — a known perpetual-diff hazard, may need ignore_changes.
//     Prune computed rotation id.
//   - opsgenie_service_incident_rule / opsgenie_alert_policy / opsgenie_notification_policy —
//     filter/condition match trees; message/description carry {{…}} Opsgenie alert-field
//     placeholders → keep LITERAL (same class as PagerDuty PCL / Datadog widget-query escaping).
//     Alert policy emits team_id when team-scoped (drives the import id).
//   - opsgenie_user / _contact / opsgenie_notification_rule — PII (username/phone) but NOT
//     secret; adopt. CAUTION: the key-owner user appears.
//   - opsgenie_maintenance — only non-expired windows are enumerated (past = dead data).
//   - opsgenie_heartbeat — trivial, NOT secret (no api_key/ping-URL attribute); import by name.
//
// Until Phase B these are no-ops, so an Opsgenie export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the api_integration
// api_key that generate-config-out emits before the scrub rule lands).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
