package grafana

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Grafana is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/grafana.md). grafana_dashboard is the HEAVIEST
// curation surface (the giant model JSON), the Grafana analogue of datadog_dashboard /
// fastly_service_vcl. Phase-B work, by resource:
//   - grafana_dashboard: config_json is the entire dashboard model. TEMPLATE HAZARD: the
//     model is full of ${var}/[[var]] dashboard-variable syntax and $__rate_interval/
//     ${datasource} macros → the generated HCL must keep these LITERAL (EscapeHCLTemplate the
//     blob; verify terraform's writer does the equivalent). Prune churny computed model fields
//     (id/version/iteration/dashboard_id/url/slug); folder refs the folder uid.
//   - grafana_message_template: `template` is a raw Go/Alertmanager body full of {{ define }}/
//     {{ .Labels }}/{{ range }} — the WORST literal-{{…}} hazard in the provider; escaping is
//     mandatory or the HCL breaks.
//   - grafana_rule_group: per-rule `data` query models carry $__interval/${…} macros (same
//     hazard); prune per-rule computed uid.
//   - grafana_data_source — NESTED SECRET (scrub, not exclude). type/name/url/json_data_encoded
//     are the shell; secure_json_data_encoded (DB passwords, API keys, TLS client key),
//     http_headers, basic_auth_password, password are WRITE-ONLY (Grafana redacts to a
//     secureJsonFields bool map) → scrub + re-supply out-of-band.
//   - grafana_contact_point — REDACTED SECRET. The provisioning GET redacts secret settings to
//     literal "[REDACTED]" (Slack url/token, PagerDuty integrationKey, webhook password) → scrub
//     the [REDACTED] values, do NOT write them into applied config.
//   - grafana_notification_policy: the whole routing tree (nested policy blocks, contact_point
//     refs by name); no secret. Adopting it makes the org's entire alert routing TF-owned — flag.
//   - grafana_folder / grafana_playlist / grafana_mute_timing: light; prune computed id.
//   - grafana_team: prune computed id/team_id. grafana_service_account: NO token (excluded);
//     prune computed id; if GRAFANA_AUTH is an SA token, that SA self-appears — adopt but do
//     not disable it.
//   - grafana_library_panel: model_json (same template hazard); prune computed id/version.
//   - grafana_role (Enterprise): skip fixed/global built-ins (done at enumeration); prune
//     computed version/id.
//
// Until Phase B these are no-ops, so a Grafana export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the data_source secure
// fields and contact_point [REDACTED] settings that generate-config-out might emit before the
// scrub rules above land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
