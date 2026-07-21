package newrelic

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for New Relic is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/newrelic.md). newrelic_one_dashboard is the
// HEAVIEST curation surface (the New Relic analogue of datadog_dashboard / fastly_service_vcl):
// one resource emits the full page → typed-widget → nrql_query tree. Phase-B work, by resource:
//   - newrelic_one_dashboard: prune computed guid/permalink/per-page guid/per-widget id;
//     tolerate widget-block reordering. TEMPLATE HAZARD: NRQL widget queries + titles carry
//     ${...}-style dashboard template variables — the generated HCL must keep these literal
//     (verify terraform's writer escapes them, as flagged for the Datadog widget tree).
//   - newrelic_nrql_alert_condition: nrql{query} + critical/warning term blocks are core; the
//     `type` (static/baseline) drives BOTH the schema and the import id. Prune computed
//     entity_guid; defaults over-emit (aggregation_window/aggregation_method/fill_option/
//     violation_time_limit_seconds). Same NRQL literal-string hazard.
//   - newrelic_alert_policy: light (name + incident_preference); prune computed. Import
//     composite order is REVERSED (<policy_id>:<account_id>) — see importid.go.
//   - newrelic_synthetics_* : locations/period/status/uri/script per type. The script_monitor
//     `script` may reference $secure.CRED secure credentials — KEEP the references literal, do
//     not inline secrets. Prune computed guid/period_in_minutes. The monitorType split picks
//     the resource.
//   - newrelic_notification_destination — NESTED SECRET (scrub, not exclude). The shell
//     (name/type/property) is adoptable, but auth_token/auth_basic/auth_custom_header/
//     secure_url are WRITE-ONLY (the API does not return them on read) → scrub + flag for
//     out-of-band re-supply. Slack destinations import/destroy only. Analogue of Datadog's
//     datadog_webhook.custom_headers.
//   - newrelic_notification_channel / _workflow: type + property/enrichments/issues_filter;
//     channel refs destination_id, workflow refs channels; no secret in these themselves.
//   - newrelic_workload / newrelic_service_level: entity_guids/entity_search_query blocks;
//     prune computed guid/workload_status/sli_guid; the workload_id / sli_id assembly is the
//     hardest part of the import id (see importid.go / the spec).
//   - newrelic_obfuscation_rule / _expression: NOT secret — they name what to mask. Adopt
//     freely; rule refs an expression_id, expression carries a regex.
//   - newrelic_key_transaction: apdex_target/application_guid; light; GUID import.
//
// Until Phase B these are no-ops, so a New Relic export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the destination-auth
// secrets that generate-config-out might emit before the scrub rules above land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
