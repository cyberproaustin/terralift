package honeycomb

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Honeycomb is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/honeycomb.md). Honeycomb has NO single monster
// resource (contrast fastly_service_vcl); the weight is spread, and the recurring hazards are
// the query_json blob and the recipient/notification secrets. Phase-B work, by resource:
//   - honeycombio_trigger: query_id (or inline query_json), threshold/frequency/alert_type,
//     and a `recipient` block that may INLINE PagerDuty/webhook secrets → prefer referencing an
//     honeycombio_*_recipient by id; SCRUB any inline secret. Prune computed id; defaults
//     over-emit (frequency/threshold).
//   - honeycombio_slo / honeycombio_burn_alert: SLO sli (a derived-column alias ref),
//     target_per_million, time_period_days; burn_alert slo_id ref + alert_type + a `recipient`
//     block (Terraformer ignored it — scrub/reference, don't inline secrets). Prune computed
//     id. Note the dataset-vs-__all__ import fork (importid.go).
//   - honeycombio_column / honeycombio_derived_column: column key_name/type/hidden/description
//     (Terraformer IgnoreKeys{hidden,type} — over-emit, prune). Derived-column `expression` is a
//     Honeycomb-language string with $…/?… syntax → keep LITERAL (same template hazard as the
//     Datadog widget queries).
//   - honeycombio_flexible_board: nested `panel` tree (query + SLO panels) with inline
//     query_json; prune computed id/links/url; tolerate panel ordering. board_view panels are
//     carried inline (not split out — no import).
//   - honeycombio_query_annotation: name/description/query_id ref/dataset; light; prune id.
//   - Recipients: email_recipient (address) and slack_recipient (channel) are SECRET-FREE.
//     pagerduty_recipient (integration_key), webhook_recipient (secret + custom header values),
//     msteams_recipient (webhook url) carry WRITE-ONLY material → scrub + re-supply. Prune
//     computed id on all.
//   - honeycombio_dataset: name/slug/description; delete_protected defaults true — keep explicit
//     so a later destroy doesn't surprise. Prune computed timestamps/last_written_at.
//
// Until Phase B these are no-ops, so a Honeycomb export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the recipient/trigger
// secrets that generate-config-out might emit before the scrub rules above land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
