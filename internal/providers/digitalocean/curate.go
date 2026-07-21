package digitalocean

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for DigitalOcean is a Phase-B task: rules are confirmed against real
// `terraform plan -generate-config-out` output on a live account, not guessed. The
// plan is in docs/v2-specs/digitalocean.md; the known gotchas to implement when live:
//   - digitalocean_droplet: prune heavy computed over-emit (id/urn/status/locked/
//     created_at/disk/memory/vcpus/price_*/ipv4_address*/ipv6_address/backup_ids/
//     snapshot_ids/volume_ids); keep image/region/size.
//   - digitalocean_kubernetes_cluster: prune sensitive kube_config + computed
//     endpoint/ipv4_address/status/urn/created_at/updated_at; the default node pool
//     stays embedded. Import needs the default pool pre-tagged terraform:default-node-
//     pool (a pre-step — flag at live QA).
//   - database cluster/user/connection_pool: SCRUB sensitive password/uri/private_uri
//     (computed; generate-config-out should drop them, but redact if emitted).
//   - digitalocean_project: drop the computed resources (URN) list + owner_*/*_at.
//   - loadbalancer/firewall/record/cdn/volume/reserved_ip: prune computed
//     ip/status/urn/fqdn/endpoint/created_at as documented in the spec.
//
// Until then these are no-ops, so a DigitalOcean export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the redaction backstop for the
// database/kube_config secrets above).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
