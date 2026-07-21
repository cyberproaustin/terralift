package linode

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Linode is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/linode.md). Known gotchas when live:
//   - linode_instance: heavy computed over-emit (id/status/ipv4/ipv6/specs/backups/...);
//     keep region/type/image/label. root_pass/authorized_keys are write-only (emit null).
//   - linode_database_mysql/_postgresql: SCRUB computed-sensitive root_password/
//     root_username/host_* (the resource itself is adoptable — not a whole-resource
//     exclude).
//   - linode_lke_cluster: prune sensitive kubeconfig + dashboard_url + computed status/
//     api_endpoints/pool[*].nodes; the pool {} blocks stay.
//   - nodebalancer_config ssl_cert/ssl_key are write-only (emit null); node/config prune
//     computed nodebalancer_id/config_id (those live in the import id, not the body).
//   - firewall/volume/domain/record/vpc/subnet/image/bucket: prune the documented
//     computed attrs (status/created/updated/ipv4/hostname/fqdn/...).
//
// Until Phase B these are no-ops, so a Linode export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for DB/kubeconfig
// secrets above).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
