package mackerel

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Mackerel is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/mackerel.md). Mackerel has NO single monster
// resource; the recurring hazard is that a few resources carry FIELD-LEVEL write-only secrets the
// API returns null/masked on read. Phase-B work, by resource:
//   - mackerel_channel — the #1 secret surface. The slack `url` and webhook `url` embed a token /
//     signing secret the API masks on read → SCRUB + flag re-supply out-of-band. Adopt the shell
//     (name/type/events/mentions). email channels carry no secret.
//   - mackerel_aws_integration — `secret_key` (an AWS IAM secret) is not returned on read →
//     SCRUB; prefer the IAM-role (role_arn + external_id) variant which carries no inline secret.
//     external_id is a low-sensitivity correlation value but pairs with the role.
//   - mackerel_monitor — the `external` monitor's `headers` map can carry an Authorization/API-key
//     header → SCRUB those header VALUES. The 7 monitor kinds otherwise adopt cleanly; prune the
//     computed id/created/registered fields and keep Lucene/expression query strings LITERAL.
//   - mackerel_service / mackerel_role — no secret (name + memo only); adopt as-is. CAUTION: the
//     role composite <service>:<role> and the dashboard import id are VERIFY items (dashboard has
//     no documented import section) — pin the exact form against generate-config-out first.
//   - mackerel_notification_group / mackerel_downtime / mackerel_alert_group_setting — reference
//     ids (monitor/channel/service) but no secret; adopt as-is once the refs resolve.
//
// Until Phase B these are no-ops, so a Mackerel export is a breadth scaffold, not yet plan-clean
// (the pipeline's repo-wide secret scan is the backstop for the channel webhook tokens, AWS
// secret_key, and external-monitor auth headers that generate-config-out nulls-or-leaks before the
// scrub rules land). The org API key is never inlined — providers.tf is env-auth only.

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
