package ns1

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for NS1 is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/ns1.md). Known gotchas when live:
//   - ns1_record: answers/filters/regions blocks reorder — tolerate; prune computed id;
//     short_answers vs answers form. The heaviest curation surface.
//   - ns1_zone: prune computed id/dns_servers/network_pools/hostmaster; linked/secondary
//     zones (skipped for records) still adopt as the zone shell.
//   - ns1_datasource / ns1_notifylist: config/notify blocks may carry provider tokens
//     (e.g. PagerDuty/Slack webhook secrets) → Phase-B scrub if generate-config-out
//     emits them (repo-wide secret scan is the backstop).
//   - ns1_monitoringjob: prune computed id/status; ns1_datafeed: prune computed id.
//   - ns1_team/ns1_user: prune computed id; user is secret-free.
//
// Until Phase B these are no-ops, so an NS1 export is a breadth scaffold, not yet
// plan-clean.

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
