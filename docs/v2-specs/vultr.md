# Vultr provider — build spec

Research artifact for the `vultr` provider (Phase A scaffold). Sources: Terraformer's
`providers/vultr/` (old **govultr v1 / API v1** — it emits the *renamed* `vultr_server`
and `vultr_network`; do not copy those), the current `vultr/vultr` registry docs (import
formats, verified verbatim per-resource below), the `vultr/govultr` v3 client (envelope
keys + pagination struct, read directly), and the Vultr REST API v2. Build mirrors the
DigitalOcean provider (`internal/providers/digitalocean/`) and the Linode provider — a
flat, token-scoped, single-container provider with a `net/http` client, an
envelope-aware list helper, and `terraform plan -generate-config-out` for config
drafting. It is closest to Linode: a **per-endpoint envelope key like DO, but a
self-built next-page URL like Linode** (no SSRF host-validation surface).

## Version pin (load-bearing)

Pin `vultr/vultr ~> 2.x` (current line). Terraformer targets the **v1** provider/SDK, so
three of its type strings are wrong for us and must not be copied:
- **`vultr_server` → `vultr_instance`.** Terraformer's `server.go` emits `vultr_server`
  (v1). The current provider is `vultr_instance`. Same object, new type string.
- **`vultr_network` → `vultr_vpc` (+ a distinct `vultr_vpc2`).** Terraformer's
  `network.go` emits the retired `vultr_network`. The current provider splits this into
  `vultr_vpc` (`/v2/vpcs`) and the newer `vultr_vpc2` (`/v2/vpc2`) — **two separate
  resources and endpoints**, both real, both adopted; enumerate both.
- **The VKE node-pool resource is PLURAL: `vultr_kubernetes_node_pools`** (not
  `_node_pool`). Easy to get wrong; the registry doc filename and resource name are both
  plural.

Two import-format facts are pinned to the registry docs (verified verbatim, below):
- `vultr_firewall_rule`'s second token is the **integer rule id** (`RuleNumber`), not a
  UUID — the composite is `<group_uuid>,<int>`.
- `vultr_kubernetes_node_pools` imports as a **space-delimited** `"<cluster_id>
  <pool_id>"` — the only space composite in the provider (everything else is comma).

The REST API v2 endpoints are provider-version-independent.

## Shape

- Auth: `VULTR_API_KEY` env var, `Authorization: Bearer <key>`. No CLI — a direct
  `net/http` client to `https://api.vultr.com/v2` (mirror `doapi.go`). The TF provider
  reads the same `VULTR_API_KEY`.
- Scope: the **whole account** (the key is account-scoped; there is no sub-account plane
  in the beachhead). One flat container = the account (`model.ScopeTenant`). Container
  identity from `GET /v2/account`. **Vultr's account object has NO uuid** — only
  `{ "balance", "pending_charges", "name", "email", "acls": [...] }`. Use **`email`** as
  the stable container id and `name` (falling back to `email`) as the friendly name. The
  `acls` array is the token's granted scopes (surface it in a preflight note; a
  restricted key can 403 individual lists).
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Envelope — per-endpoint key, like `doapi.go` (NOT Linode's fixed `data`).** Vultr
  wraps each list under **its own named key**, plus a `meta`:
  ```json
  { "instances": [ ... ], "meta": { "total": 12, "links": { "next": "<cursor>", "prev": "" } } }
  ```
  The array lives under a per-endpoint key (`instances`, `bare_metals`, `domains`,
  `records`, `firewall_groups`, `firewall_rules`, `blocks`, `load_balancers`, `vpcs`,
  `ssh_keys`, `reserved_ips`, `startup_scripts`, `vke_clusters`, `node_pools`,
  `databases`, `object_storages`, …) — **not** a fixed field. So the list helper must
  take the nesting key as a parameter (unmarshal into `map[string]json.RawMessage` and
  pick the key), exactly like `doList[T](ctx, path, key)`. Singletons (`GET /v2/account`)
  wrap the single object under a key too (`account`), so keep a `getOne` that digs under
  a key like `doGetOne` (unlike Linode, whose singletons are bare).
- **Pagination — CURSOR, `meta.links.next` (a cursor STRING, not a URL).** Confirmed from
  `govultr/meta.go`: `Meta{ Total int; Links *Links }`, `Links{ Next string; Prev string }`.
  `meta.links.next` is an **opaque cursor token** (empty `""` on the last page), *not* a
  full next-page URL. Request each page as `?per_page=500&cursor=<next>` (per_page max is
  500; `cursor` omitted/empty on the first page) and loop **while `meta.links.next != ""`**.
  Because **we build every URL ourselves** from the cursor param, there is **no
  body-supplied next-URL to host-validate** — DO's `isDigitalOceanURL` guard has **no
  analogue here** (same safety posture as Linode's page counter). Bound the loop
  defensively like `doMaxPages`. (Terraformer used per-ip-type v1 pagination; ignore it —
  v2 `firewall_rules`, and every other list, is a single cursor-paged collection.)
- Status handling (mirror `doAPIError`): 401 → key invalid (fatal, surfaced in
  preflight); 403/404 → feature/permission absent (a resource type the key's ACL doesn't
  cover, or a product not enabled) → best-effort skip at Verbose; 429/5xx/network →
  enumeration may be silently incomplete → Warn and count as a hard-fail. Vultr v2 error
  bodies are `{"error":"<message>","status":<code>}` — pull `error` for the message
  (contrast DO's `{"message":...}` and Linode's `{"errors":[{"reason":...}]}`). The key is
  only ever on the `Authorization` header, never in errors/logs.
- Preflight: `terraform` present + `VULTR_API_KEY` set + `GET /v2/account` returns 200
  (a valid key always returns an account body; there is no `status == "active"` field to
  check, unlike DO).
- Connect: `GET /v2/account` → `email` is the flat container. The key *is* the account,
  so there is no multi-account resolution; just validate the call succeeds.

## Enumeration spine

The account is flat — most resources are top-level account collections. Three fan out on
a parent loop (like DO's domains→records), and one (VKE) also embeds its children:
- `domains` → per-domain `records` (`GET /v2/domains/{domain}/records`, key `records`).
- `firewalls` (groups) → per-group `rules` (`GET /v2/firewalls/{group_id}/rules`, key
  `firewall_rules`) — **one list call per group returns ALL rules** (v4 and v6 together).
  Terraformer's v1 code fanned out per-ip-type (`ListByIPType` v4 then v6); the v2
  endpoint does not need that — a single call per group.
- `kubernetes/clusters` (VKE) → node pools are **embedded** in each cluster object's
  `node_pools` array (no separate call needed to see them) but are **also** a standalone
  `vultr_kubernetes_node_pools` resource. The **initial** pool created with the cluster is
  represented inside `vultr_kubernetes.node_pools` and must **not** be re-imported as a
  separate resource (double-management); additional pools are separate. See the import
  gotcha below for how to tell them apart.

`databases` is a **flat** list in the beachhead (the managed-DB *cluster* only). Its
sub-resources (`vultr_database_db` / `_user` / `_connection_pool` / `_replica`, under
`/v2/databases/{id}/dbs|users|...`) are a later increment with credential scrubbing —
unlike DO, they are not part of the core catalog.

Everything else is a single best-effort account-level list (Verbose + continue on
403/404, Warn + hard-fail count otherwise), identical to DO's `list(run, &hardFails, …)`.

## Resource catalog

Import IDs verified **verbatim** against the current `vultr/vultr` registry docs
(examples quoted). Vultr's simple ids are the resource **UUID**; two are composites
(`dns_record`, `firewall_rule`, **comma**), one is space-delimited (`node_pools`), and
`dns_domain` imports by the **domain name**. `id field` is the API field to stash for the
import id; all scope = the account container.

| native key | TF type | endpoint | JSON key | fans out | id field | import ID |
|---|---|---|---|---|---|---|
| vultr:instance | vultr_instance | `GET /v2/instances` | `instances` | no | `id` (uuid) | `<id>` |
| vultr:bare_metal_server | vultr_bare_metal_server | `GET /v2/bare-metals` | `bare_metals` | no | `id` (uuid) | `<id>` |
| vultr:dns_domain | vultr_dns_domain | `GET /v2/domains` | `domains` | → records | `domain` | `<domain>` **(NAME)** |
| vultr:dns_record | vultr_dns_record | `GET /v2/domains/{domain}/records` | `records` | — | `id` (uuid) | `<domain>,<record_id>` **(comma)** |
| vultr:firewall_group | vultr_firewall_group | `GET /v2/firewalls` | `firewall_groups` | → rules | `id` (uuid) | `<id>` |
| vultr:firewall_rule | vultr_firewall_rule | `GET /v2/firewalls/{group_id}/rules` | `firewall_rules` | — | `id` (**int**) | `<firewall_group_id>,<rule_id>` **(comma; rule_id is an INT)** |
| vultr:block_storage | vultr_block_storage | `GET /v2/blocks` | `blocks` | no | `id` (uuid) | `<id>` |
| vultr:load_balancer | vultr_load_balancer | `GET /v2/load-balancers` | `load_balancers` | no (rules inline) | `id` (uuid) | `<id>` |
| vultr:vpc | vultr_vpc | `GET /v2/vpcs` | `vpcs` | no | `id` (uuid) | `<id>` |
| vultr:vpc2 | vultr_vpc2 | `GET /v2/vpc2` | `vpcs` | no | `id` (uuid) | `<id>` |
| vultr:ssh_key | vultr_ssh_key | `GET /v2/ssh-keys` | `ssh_keys` | no | `id` (uuid) | `<id>` |
| vultr:reserved_ip | vultr_reserved_ip | `GET /v2/reserved-ips` | `reserved_ips` | no | `id` (uuid) | `<id>` |
| vultr:startup_script | vultr_startup_script | `GET /v2/startup-scripts` | `startup_scripts` | no | `id` (uuid) | `<id>` |
| vultr:kubernetes | vultr_kubernetes | `GET /v2/kubernetes/clusters` | `vke_clusters` | pools embedded + separate | `id` (uuid) | `<id>` |
| vultr:kubernetes_node_pool | vultr_kubernetes_node_pools | (cluster `.node_pools`, or `GET /v2/kubernetes/clusters/{id}/node-pools`, key `node_pools`) | `node_pools` | — | `id` (uuid) | `<cluster_id> <pool_id>` **(SPACE, one quoted string)** |
| vultr:database | vultr_database | `GET /v2/databases` | `databases` | no (subs = later inc) | `id` (uuid) | `<id>` |
| vultr:object_storage | vultr_object_storage | `GET /v2/object-storage` | `object_storages` | no | `id` (uuid) | `<id>` |

Verbatim import examples from the registry docs (the load-bearing ones):
- `terraform import vultr_dns_record.rec domain.com,1a0019bd-7645-4310-81bd-03bc5906940f`
- `terraform import vultr_firewall_rule.my_rule b6a859c5-b299-49dd-8888-b1abbc517d08,1`
- `terraform import vultr_kubernetes_node_pools.my-k8s-np "7365a98b-5a43-450f-bd27-d768827100e5 ec330340-4f50-4526-858f-a39199f568ac"`
- `terraform import vultr_dns_domain.name domain.com`

**No whole-resource EXCLUDES in this catalog.** Unlike DO (`digitalocean_certificate`
custom) and Linode (`linode_token`, `linode_object_storage_key`), none of the covered
Vultr resources is a *pure write-only secret*. The secrets that appear
(`object_storage.s3_secret_key`, `database.password`, `instance.default_password`) are all
**computed exports** on otherwise-adoptable resources → **scrub-not-exclude** (see
curation). Pure-secret / IAM resources like `vultr_user` are out of scope entirely
(`Capabilities.IAM=false`).

## Import-format quirks (§ do not get wrong)

1. **Comma composites — arity 2, order load-bearing** (both comma-joined, like DO's
   `digitalocean_record`, **not** slash):
   - `vultr_dns_record` = `<domain>,<record_id>` — `domain` is the *name* (e.g.
     `domain.com`), `record_id` is the record's UUID string. Stash `domain` on the
     record's `Properties` during the domains→records fan-out (mirror how DO stashes
     `domain`).
   - `vultr_firewall_rule` = `<firewall_group_id>,<rule_id>` — `firewall_group_id` is the
     parent group UUID, **`rule_id` is an INTEGER** (the API `id`/rule number, e.g. `1`),
     not a UUID. Stash `firewall_group_id` on the rule during the groups→rules fan-out.
2. **`vultr_kubernetes_node_pools` is the ONE space composite:** `"<cluster_id> <pool_id>"`
   — two UUIDs separated by a **space**, passed as a single quoted argument. Do **not**
   comma-join it. Stash `cluster_id` on the pool during enumeration.
3. **`vultr_dns_domain` imports by the domain NAME** (`domain.com`), not an id — the
   `Domain` object's identity *is* its `domain` field. Everything else with a UUID `id`
   imports by that bare UUID.
4. **`vpc` vs `vpc2` are distinct** (`/v2/vpcs` and `/v2/vpc2`) yet **both** return their
   arrays under the **same** envelope key `vpcs`. Do not assume the key encodes which one;
   the *endpoint* is what distinguishes them. Enumerate both; each imports by its own UUID.
5. Everything else — `instance`, `bare_metal_server`, `firewall_group`, `block_storage`,
   `load_balancer`, `ssh_key`, `reserved_ip`, `startup_script`, `kubernetes` (cluster),
   `database`, `object_storage` — is the **bare resource UUID**. No prefixes, no slashes.

## Curation gotchas (Phase B, when live)

Confirm against real `terraform plan -generate-config-out` output on a live account — not
guessed. Prune computed via `hcl.WalkResourceBlocks`; scrub secrets like the DO/Linode
Phase-B backstop. (Like DO/Linode, Phase-A ships these as no-op `pruneGeneratedHCL` /
`scrubGeneratedHCL` stubs; the pipeline's repo-wide secret scan is the redaction backstop
until the rules below are implemented.) Struct fields below are from `govultr` v3.

- **`vultr_instance` — heavy computed over-emit + a computed secret (the droplet
  analogue).** Prune computed `id`/`main_ip`/`ram`/`disk`/`vcpu_count`/`allowed_bandwidth`/
  `netmask_v4`/`gateway_v4`/`v6_network`/`v6_main_ip`/`v6_network_size`/`internal_ip`/
  `kvm`/`status`/`power_status`/`server_status`/`date_created`/`os`/`os_id`. **SCRUB the
  computed sensitive `default_password`** if emitted. Keep `plan`/`region`/`os_id`-or-
  `image_id`-or-`snapshot_id`-or-`app_id`/`label`/`hostname`/`tags`/`enable_ipv6`/
  `firewall_group_id`/`vpc_ids`/`ssh_key_ids`/`user_data`/`script_id`. `user_data` is
  stored (config may re-emit base64) → tolerate. `tags`/`ssh_key_ids`/`vpc_ids` may
  reorder → tolerate.
- **`vultr_object_storage` — SCRUB S3 creds, keep the resource.** Exported computed
  `s3_access_key` + `s3_secret_key` are the S3 credential pair, `s3_hostname` is computed.
  generate-config-out should drop the computed creds; if it emits any, Phase-B scrubbing
  MUST redact `s3_access_key`/`s3_secret_key` (repo-wide secret scan is the backstop). The
  resource itself is adoptable and plan-clean (`cluster_id`/`tier_id` + `label`) — the S3
  secret is **not** a reason to exclude the whole resource (contrast
  `linode_object_storage_key`, which *is* the key). Prune computed `s3_hostname`/`date_created`.
- **`vultr_database` — SCRUB credentials, keep the resource.** Exported computed sensitive
  `password` (+ `user`/`host`/`public_host`/`port`/`sasl_port`, and Kafka-only
  `access_key`/`access_cert`). generate-config-out should drop the computed sensitive
  creds; if any leak, Phase-B scrubbing MUST redact `password`/`access_key`/`access_cert`.
  The cluster is adoptable and plan-clean (`database_engine`/`database_engine_version`/
  `plan`/`region`/`label`). `password` is computed, **not** a required write-only input →
  not a reason to exclude (like `linode_database_*`). DB sub-resources (db/user/
  connection_pool/replica) are a later increment; a `vultr_database_user` carries its own
  `password` to scrub then.
- **`vultr_kubernetes` — prune the sensitive kubeconfig + embedded-pool computed, and the
  default-pool import hazard.** Prune sensitive computed `kube_config` (base64 cluster
  credentials) plus computed `ip`/`endpoint`/`status`/`date_created`/`cluster_subnet`/
  `service_subnet`/`version` drift. The initial `node_pools` block stays embedded (that is
  how the first pool is managed). **Import hazard:** the initial pool is both inside
  `vultr_kubernetes.node_pools` *and* returned by the node-pools list — adopting it *also*
  as a `vultr_kubernetes_node_pools` double-manages it. The govultr `NodePool` has a `tag`
  field; the provider ties the cluster's inline pool to a specific pool. Resolve at live
  QA: adopt only the *additional* pools as separate resources and leave the initial one to
  the cluster (mirrors DO skipping the `terraform:default-node-pool`-tagged pool). Per-pool
  prune computed `status`/`date_created`/`date_updated`/`nodes` (the per-node instance
  ids/status).
- **`vultr_firewall_group` / `vultr_firewall_rule`**: group — prune computed
  `date_created`/`date_modified`/`instance_count`/`max_rule_count`/`rule_count`; keep
  `description`. Rule — keep `ip_type`/`protocol`/`subnet`/`subnet_size`/`port`/`source`/
  `notes`; prune computed `id`/`action` if defaulted. The rule's `id` (int) lives in the
  import id, not the body.
- **`vultr_dns_domain` / `vultr_dns_record`**: domain — `domain` + `dns_sec`; prune
  computed `date_created`. Record — keep `type`/`name`/`data`/`priority`/`ttl`; the apex
  record has an empty `name` (tolerate). Vultr auto-creates NS/SOA records on a new domain
  — those are provider-managed defaults; flag at live QA whether generate-config-out drafts
  them cleanly or they should be skipped on enumeration (analogous to DO defaultdb/doadmin).
- **`vultr_load_balancer`**: forwarding rules and firewall rules are **inline blocks**
  (`forwarding_rules {}`, `firewall_rules {}`) on the resource — keep them (not separate
  resources). Prune computed `id`/`ipv4`/`ipv6`/`status`/`has_ssl`/`date_created`;
  `ssl {}` (private key/cert) is write-only → config emits null, which is fine.
  `instances`/`attached_nodes` are computed attachments → prune.
- **`vultr_block_storage`**: keep `size_gb`/`region`/`label`/`block_type`; prune computed
  `date_created`/`status`/`mount_id`. `attached_to_instance` is the optional attachment
  (may be empty if detached).
- **`vultr_vpc` / `vultr_vpc2`**: keep `region`/`description` and the subnet fields
  (`v4_subnet`/`v4_subnet_mask`, and vpc2 `ip_block`/`prefix_length`); prune computed
  `date_created`.
- **`vultr_reserved_ip`**: keep `region`/`ip_type`/`label`; `instance_id` is the optional
  assignment (may be empty). Prune computed `subnet`/`subnet_size`.
- **`vultr_ssh_key`**: `name` + `ssh_key` (public key) only, no secret; prune computed
  `date_created`. Safe.
- **`vultr_startup_script`**: keep `name`/`type`/`script` (base64 body — **config, not a
  secret**; the API returns it, so it round-trips → adopt as-is). Prune computed
  `date_created`/`date_modified`. Note a user-authored script *could* embed a secret, but
  that is the user's config, not a provider-issued credential — not a reason to exclude.
- **`vultr_bare_metal_server`**: same over-emit class as `vultr_instance` (prune
  `main_ip`/`v6_*`/`netmask_v4`/`gateway_v4`/`status`/`date_created`/`cpu_count`/`ram`/
  `disk`/`mac_address`); **SCRUB computed `default_password`**. Keep `plan`/`region`/
  `os_id`-or-`image_id`-or-`snapshot_id`-or-`app_id`/`label`/`hostname`/`tags`/`user_data`.

## Deliberately out of scope
- **Point-in-time data**: `vultr_snapshot` (`/v2/snapshots`), block-storage snapshots
  (`/v2/blocks/snapshots`), ISOs (`/v2/iso`) — low-value snapshot/image data.
- **Data planes**: object-storage buckets/objects/policies (`vultr_object_storage_bucket`
  and the S3 object data *inside* a subscription — needs the scrubbed S3 keys, a distinct
  auth plane), database rows/tables, container-registry repositories/artifacts
  (`/v2/registry`) — the DATA inside buckets/registries/databases.
- **Cloud-IAM / account plane** (`Capabilities.IAM=false`): `vultr_user` (Terraformer's
  `user.go` — has a password), sub-accounts (`/v2/subaccount`), API-key/ACL management,
  OIDC/organization objects. Not modeled by this provider's beachhead.
- **Associations / attachments better managed inline**: reserved-IP↔instance attach,
  block-storage attach, VPC↔instance membership (`vpc_ids` on the instance), VPC 2.0
  `/nodes`, LB↔instance attachment, NAT-gateway objects (`/v2/vpcs/{id}/nat-gateway`).
- **Managed-DB sub-resources** (later increment, not out of scope forever):
  `vultr_database_db` / `_user` / `_connection_pool` / `_replica` / `_connector` /
  `_topic` / `_quota` — added with credential scrubbing after the flat `vultr_database`
  cluster lands.
- **Marketplace / inference / CDN / VFS**: `/v2/marketplace`, `/v2/inference`, `/v2/cdn`,
  `/v2/virtual-file-system-storage` — niche products; optional later increments.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD instance + dns_domain + dns_record + firewall_group + firewall_rule + ssh_key +
startup_script + reserved_ip (the core account infra everyone has; all bare UUIDs bar
dns_domain's name and the two comma composites — `dns_record` and `firewall_rule`, the
latter's second token an INT) → INC-1 bare_metal_server + block_storage + load_balancer +
vpc + vpc2 (flat UUIDs; scrub `default_password` on bare metal) → INC-2 kubernetes +
kubernetes_node_pools (space composite `"<cluster_id> <pool_id>"`, kube_config scrub,
skip the cluster's embedded initial pool) → INC-3 database (credential scrub;
`database_engine`/`plan`/`region`) → INC-4 object_storage (S3 s3_secret_key/s3_access_key
scrub) → LATER database sub-resources, object-storage buckets (S3 auth plane), snapshots,
registry. EXCLUDED/never in-scope: `vultr_user` and sub-accounts (IAM plane).
