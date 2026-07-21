package vultr

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Vultr is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/vultr.md). Known gotchas when live:
//   - vultr_instance / vultr_bare_metal_server: heavy computed over-emit (id/main_ip/
//     v6_*/ram/disk/status/date_created/os_id/...); SCRUB the computed sensitive
//     default_password; keep plan/region/os_id|image_id|snapshot_id/label/hostname.
//   - vultr_object_storage: SCRUB computed s3_access_key/s3_secret_key; keep cluster_id/
//     tier_id/label. vultr_database: SCRUB computed password (+ Kafka access_key/cert);
//     keep engine/plan/region/label. (Neither is a whole-resource exclude — the secret is
//     computed, not a required input.)
//   - vultr_kubernetes: prune sensitive kube_config + computed ip/endpoint/status; inline
//     pool blocks stay. (node_pools standalone is deferred — see enumerate.go.)
//   - vultr_load_balancer: forwarding_rules/firewall_rules are inline blocks (keep); ssl{}
//     is write-only (emits null). firewall_group/rule, dns, vpc, block_storage,
//     reserved_ip, startup_script: prune the documented computed date_created/status/etc.
//     (startup_script.script is config, not a secret — keep.)
//
// Until Phase B these are no-ops, so a Vultr export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the computed
// passwords / S3 keys / kube_config above).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
