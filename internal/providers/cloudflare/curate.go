package cloudflare

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Cloudflare is a Phase-B task: the rules are confirmed against real
// `terraform plan -generate-config-out` output on a live account, not guessed. The
// plan is documented in docs/v2-specs/cloudflare.md; the known gotchas to implement
// when live are:
//   - cloudflare_record: drop whichever of value / empty data{} block is unused
//     (ConflictsWith), and prune computed metadata/proxiable/created_on/modified_on/
//     hostname (per-block, via hcl.WalkResourceBlocks).
//   - cloudflare_zone: prune computed meta/status/name_servers/vanity_name_servers/
//     verification_key/cname_suffix/plan; author account_id if dropped.
//   - cloudflare_zone_settings_override: keep a curated allow-list of writable
//     settings (plan-gated Enterprise settings otherwise perpetually drift).
//   - cloudflare_firewall_rule: drop priority = 0 (default).
//   - load_balancer_pool/_monitor: author account_id if dropped; prune created_on/
//     modified_on.
//
// Until then these are no-ops, so a Cloudflare export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the redaction backstop).

// pruneGeneratedHCL removes attributes generate-config-out over-emits. Phase B.
func pruneGeneratedHCL(path string) int { return 0 }

// scrubGeneratedHCL redacts secret-looking values. custom_ssl (write-only private
// key) is EXCLUDED so its key is never fetched, but two adopted resources still
// carry secrets that generate-config-out can emit and that Phase-B scrubbing MUST
// handle: cloudflare_access_application (client_secret, for SaaS/OIDC apps) and
// cloudflare_load_balancer_monitor (auth values in its header block). Until Phase B,
// the pipeline's repo-wide secret scan is the only backstop for those.
func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
