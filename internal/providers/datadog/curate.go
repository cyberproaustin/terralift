package datadog

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Datadog is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/datadog.md). datadog_dashboard is the
// HEAVIEST curation surface (the Datadog analogue of fastly_service_vcl): one resource
// emits the entire recursive widget tree — group widgets contain widgets, so block depth
// is unbounded. Phase-B work, by resource:
//   - datadog_dashboard: prune computed url/author_handle/author_name/created_at/
//     modified_at and per-widget computed ids; tolerate widget-block reordering. TEMPLATE
//     HAZARD: widget queries/titles carry ${...} template vars and Datadog %{...}/{{...}}
//     syntax — the generated HCL must keep these literal (Terraformer needed a manual
//     %{→%%{ hook; verify terraform's writer does the equivalent, else plan breaks).
//   - datadog_monitor: message carries {{#is_alert}} template syntax + @notification
//     handles (same literal-string hazard); prune computed id; drop deprecated `silenced`;
//     defaults over-emit (notify_no_data/renotify_interval/notify_audit/include_tags).
//   - datadog_synthetics_test: browser/multistep tests emit a large steps/api_step tree;
//     SCRUB write-only material — config_variable (type=text, secure=true), request_client_
//     certificate (cert+key), and any auth headers/basic-auth in request_definition. Prune
//     computed monitor_id.
//   - datadog_logs_custom_pipeline: grok processors use %{...} patterns (Terraformer needed
//     a %{→%%{ PostConvertHook) — verify terraform escapes these. filter.query is required.
//   - datadog_service_level_objective: monitor_ids ref monitors; prune computed id.
//   - datadog_logs_index / datadog_logs_metric / datadog_notebook / datadog_downtime_
//     schedule / datadog_security_monitoring_rule: prune the documented computed attrs;
//     security rule carries `enabled` explicitly.
//   - datadog_role: the permission blocks over-emit a computed `name` — only permission.id
//     is authoritative (prune permission.[n].name and user_count).
//   - datadog_user: no secret (no password attribute); prune computed verified/disabled/
//     handle; CAUTION — your own user (behind DD_APP_KEY) may appear; adopt but do not
//     disable it.
//
// Until Phase B these are no-ops, so a Datadog export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for any nested secret
// that generate-config-out might emit before the scrub rules above land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
