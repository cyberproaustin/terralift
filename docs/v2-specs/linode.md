# Linode provider — build spec

Research artifact for the `linode` provider (Phase A scaffold). Sources:
Terraformer's `providers/linode/` (linodego-based), the `linode/linode` registry docs
(import formats, verified per-resource below), and the Linode REST API v4. Build
mirrors the DigitalOcean provider (`internal/providers/digitalocean/`) — a flat,
token-scoped, single-container provider with a `net/http` client, an envelope-aware
list helper, and `terraform plan -generate-config-out` for config drafting.

## Version pin (load-bearing)

Pin `linode/linode ~> 2.x` (current line). Two rename/format hazards to pin against:
- **Managed databases have a `_v2` successor.** `linode_database_mysql` /
  `linode_database_postgresql` are **deprecated** in the current provider in favor of
  `linode_database_mysql_v2` / `linode_database_postgresql_v2` (the v2 resources drop
  the legacy `allow_list`/`replication` shape and take `engine_id`). Both legacy and v2
  still import by the same bare numeric id, so enumeration is identical; the *only*
  choice is which TF type string to emit. This spec maps to the **legacy** names for
  now (they still plan-clean, just warn "deprecated") — flip the two `tfTypeMap`
  entries to `_v2` in one place when we adopt v2. Do not mix the two for one db.
- **Object Storage bucket key is `region`, not `cluster`.** Older provider versions
  keyed buckets (and their import id) by S3 `cluster` (`us-east-1`); the current
  provider keys by `region` (`us-east`). Both use the **colon** import format
  (`<region|cluster>:<label>`) — the separator is the stable fact; pin the left token
  to `region` for `~> 2.x`.

The REST API v4 endpoints are provider-version-independent.

## Shape

- Auth: `LINODE_TOKEN` env var, `Authorization: Bearer <token>`. No CLI — a direct
  `net/http` client to `https://api.linode.com/v4` (mirror `doapi.go`). The TF provider
  reads the same `LINODE_TOKEN`.
- Scope: the **whole account** (the token is account-scoped; there is no sub-account).
  One flat container = the account (`model.ScopeTenant`). Container id/name from
  `GET /v4/account` — use `euuid` (stable account uuid) as the id and `email` as the
  friendly name. If the token lacks the `account:read_only` scope, `/v4/account` 401s;
  fall back to `GET /v4/profile` (readable by any valid token) and use its
  `uid`/`username` for the container identity.
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Envelope — simpler than `doapi.go`.** DigitalOcean wraps each endpoint under its
  own named key (`droplets`, `domains`, …) so its list helper takes the key as a
  parameter. Linode wraps **every** list under a single fixed key, `data`, with numeric
  pagination:
  ```json
  { "data": [ ... ], "page": 1, "pages": 3, "results": 250 }
  ```
  So the list helper does **not** need a per-endpoint key parameter — always unmarshal
  `{"data": [...]}`. Singletons (`GET /v4/account`, `GET /v4/profile`) return the object
  directly (no `data` wrapper), so keep a separate `getOne` that unmarshals the body
  straight into the target struct (unlike `doGetOne`, which had to dig under a key).
- **Pagination — numeric `page`/`pages`.** Each page carries `page` (current), `pages`
  (total pages) and `results` (grand total). Request `?page=N&page_size=500` (500 is the
  max page size) and loop `N` from 1 to `pages` (read `pages` from the first response).
  This is strictly simpler and safer than DO's `links.pages.next`: **we build every URL
  ourselves from a page counter**, so there is no body-supplied next-URL to host-validate
  (the `isDigitalOceanURL` guard in `doapi.go` has no analogue here). Bound the loop
  defensively like `doMaxPages`.
- **`X-Filter` header (Linode-specific, load-bearing for two endpoints).** Linode
  collections are filtered server-side via a JSON `X-Filter` request header, e.g.
  `X-Filter: {"is_public": false}`. This matters because **`GET /v4/images` and
  `GET /v4/linode/stackscripts` return the entire PUBLIC catalog (thousands of rows)
  unfiltered** — paginating them raw would loop over the whole Linode marketplace. Send
  `X-Filter: {"is_public": false}` (images; account images also carry the id prefix
  `private/`) and `X-Filter: {"mine": true}` (stackscripts) to get only account-owned
  rows. Terraformer filtered stackscripts **client-side** via `!stackscript.IsPublic` —
  server-side `X-Filter` is the same effect without downloading the catalog; keep the
  `is_public == false` client-side check as a belt-and-suspenders backstop.
- Status handling (mirror `doAPIError`): 401 → token invalid (fatal, surfaced in
  preflight); 403/404 → feature/permission absent (e.g. Managed Databases not enabled,
  a token scope missing) → best-effort skip at Verbose; 429/5xx/network → enumeration
  may be silently incomplete → Warn and count as a hard-fail. Linode error bodies are
  `{"errors":[{"reason":"...","field":"..."}]}` (an array, unlike DO's single
  `{"message":...}`) — pull `errors[0].reason` for the message. The token is only ever
  on the `Authorization` header, never in errors/logs.
- Preflight: `terraform` present + `LINODE_TOKEN` set + `GET /v4/account` (or
  `/v4/profile`) returns 200.
- Connect: `GET /v4/account` → `euuid` is the flat container. The token *is* the
  account, so there is no multi-account resolution; just validate the call succeeds.

## Enumeration spine

The account is flat — most resources are top-level account collections. Four resources
fan out on a parent loop (like DO's domains→records), and one (LKE) embeds its children:
- `domains` → per-domain `records` (`/v4/domains/{id}/records`)
- `nodebalancers` → per-nb `configs` (`/v4/nodebalancers/{id}/configs`) → per-config
  `nodes` (`/v4/nodebalancers/{id}/configs/{cfg}/nodes`) — a **two-level** fan-out
- `vpcs` → per-vpc `subnets` (`/v4/vpcs/{id}/subnets`)
- `lke/clusters` → node pools are **embedded** in the `linode_lke_cluster` resource as
  inline `pool {}` blocks; there is **no** standalone `linode_lke_node_pool` resource in
  the current provider, so (exactly like DO's `kubernetes_cluster` default pool) there
  is nothing separate to import. `/v4/lke/clusters/{id}/pools` exists but is only needed
  for curation/inspection, not as its own inventory entry.
- databases: hit the **per-engine** lists `GET /v4/databases/mysql/instances` and
  `GET /v4/databases/postgresql/instances` directly — each maps cleanly to its TF type
  with no `engine`-field branch. (`GET /v4/databases/instances` is the combined list if
  we ever want a single call, but the per-engine split is cleaner and each is
  best-effort 403/404 when that engine isn't provisioned.)

Everything else is a single best-effort account-level list (Verbose + continue on
403/404, Warn + hard-fail count otherwise), identical to DO's `list(run, &hardFails, …)`
pattern.

## Resource catalog

Import IDs verified against the current `linode/linode` registry docs. Linode's simple
ids are the **bare numeric resource id** (rendered as a string); its composites are
**comma-joined** (like `digitalocean_record`), with the sole exception of Object
Storage, which is **colon-joined**. `id field` is the API field to stash for the import
id; all scope = the account container.

| native key | TF type | endpoint | fans out | id field | import ID |
|---|---|---|---|---|---|
| linode:instance | linode_instance | `GET /v4/linode/instances` | no | `id` (int) | `<instance_id>` |
| linode:domain | linode_domain | `GET /v4/domains` | → records | `id` (int) | `<domain_id>` |
| linode:domain_record | linode_domain_record | `GET /v4/domains/{domain_id}/records` | — | `id` (int) | `<domain_id>,<record_id>` **(comma, 2)** |
| linode:firewall | linode_firewall | `GET /v4/networking/firewalls` | no | `id` (int) | `<firewall_id>` |
| linode:nodebalancer | linode_nodebalancer | `GET /v4/nodebalancers` | → configs | `id` (int) | `<nodebalancer_id>` |
| linode:nodebalancer_config | linode_nodebalancer_config | `GET /v4/nodebalancers/{nb_id}/configs` | → nodes | `id` (int) | `<nodebalancer_id>,<config_id>` **(comma, 2)** |
| linode:nodebalancer_node | linode_nodebalancer_node | `GET /v4/nodebalancers/{nb_id}/configs/{cfg_id}/nodes` | — | `id` (int) | `<nodebalancer_id>,<config_id>,<node_id>` **(comma, 3)** |
| linode:volume | linode_volume | `GET /v4/volumes` | no | `id` (int) | `<volume_id>` |
| linode:stackscript | linode_stackscript | `GET /v4/linode/stackscripts` **(X-Filter `mine:true`)** | no | `id` (int) | `<stackscript_id>` |
| linode:lke_cluster | linode_lke_cluster | `GET /v4/lke/clusters` | pools embedded | `id` (int) | `<cluster_id>` |
| linode:vpc | linode_vpc | `GET /v4/vpcs` | → subnets | `id` (int) | `<vpc_id>` |
| linode:vpc_subnet | linode_vpc_subnet | `GET /v4/vpcs/{vpc_id}/subnets` | — | `id` (int) | `<vpc_id>,<subnet_id>` **(comma, 2)** |
| linode:image | linode_image | `GET /v4/images` **(X-Filter `is_public:false`)** | no | `id` (string `private/<n>`) | `<image_id>` (e.g. `private/12345`) |
| linode:rdns | linode_rdns | `GET /v4/networking/ips` | no | `address` (IP) | `<ip_address>` |
| linode:sshkey | linode_sshkey | `GET /v4/profile/sshkeys` | no | `id` (int) | `<sshkey_id>` |
| linode:object_storage_bucket | linode_object_storage_bucket | `GET /v4/object-storage/buckets` | no | `region`+`label` | `<region>,<label>`? → **NO: `<region>:<label>` (COLON)** |
| linode:database_mysql | linode_database_mysql | `GET /v4/databases/mysql/instances` | no | `id` (int) | `<database_id>` |
| linode:database_postgresql | linode_database_postgresql | `GET /v4/databases/postgresql/instances` | no | `id` (int) | `<database_id>` |

**EXCLUDED (secret / write-only — never enumerate for adoption):**
| native key | TF type | why excluded |
|---|---|---|
| linode:token | linode_token | the token value is returned **only once at creation** (write-only) — Terraformer imports these; TerraLift must **not**. Adopting one produces a resource whose secret can't be reproduced → never plan-clean, and a repo-wide secret. |
| linode:object_storage_key | linode_object_storage_key | `secret_key` is write-only (returned once at creation); `access_key`/`secret_key` are S3 credentials — exclude entirely, same class as `digitalocean_spaces_key`. |

## Import-format quirks (§ do not get wrong)

1. **Comma composites — order and arity are load-bearing** (all comma-joined, like DO's
   `digitalocean_record`, **not** slash):
   - `linode_domain_record` = `<domain_id>,<record_id>` (2)
   - `linode_nodebalancer_config` = `<nodebalancer_id>,<config_id>` (2)
   - `linode_nodebalancer_node` = `<nodebalancer_id>,<config_id>,<node_id>` (**3** — the
     node needs BOTH ancestor ids, carried down through the two-level fan-out)
   - `linode_vpc_subnet` = `<vpc_id>,<subnet_id>` (2)
   Stash the parent id(s) on the child's `Properties` during enumeration (mirror how DO
   stashes `domain`/`cluster_id`) so `rawImportID` can reassemble them.
2. **Object Storage is the one COLON composite:** `linode_object_storage_bucket` =
   `<region>:<label>` (e.g. `us-east:my-bucket`). Do not comma-join it. The API object
   exposes both `region` and (legacy) `cluster` plus `label`; build from `region` for
   `~> 2.x`. Note there is **no** account-wide list-auth wrinkle like DO Spaces — the
   bearer-token endpoint `/v4/object-storage/buckets` lists buckets directly (unlike
   `digitalocean_spaces_bucket`, which had no bearer list endpoint). Only the *object*
   data plane needs S3 keys, and that's out of scope.
3. **`linode_image` id includes the `private/` prefix.** The import id is the full image
   id string (`private/12345`), not a bare number. Enumerate with
   `X-Filter {"is_public": false}` so only account-created images (which all carry the
   `private/` prefix) are adopted — public/distribution images (`linode/debian12`) are
   read-only catalog entries and must be skipped.
4. **`linode_rdns` imports by the IP address**, not an id. Enumerate `/v4/networking/ips`
   and adopt only IPs whose `rdns` is **customized** — skip the default
   `<dashed-ip>.ip.linodeusercontent.com` PTR (adopting a default rdns just re-sets the
   default; noise at best, and clutters the account with meaningless resources).
5. Everything else is the **bare numeric id** as a string (`instance`, `domain`,
   `firewall`, `nodebalancer`, `volume`, `stackscript`, `lke_cluster`, `vpc`, `sshkey`,
   both databases). No slashes, no prefixes.

## Curation gotchas (Phase B, when live)

Confirm against real `terraform plan -generate-config-out` output on a live account —
not guessed. Prune computed via `hcl.WalkResourceBlocks`; scrub secrets like the DO
provider's Phase-B backstop. (Like DO, Phase-A ships these as no-op `pruneGeneratedHCL`
/ `scrubGeneratedHCL` stubs; the pipeline's repo-wide secret scan is the redaction
backstop until the rules below are implemented.)

- **`linode_instance` — heavy computed over-emit (the droplet analogue).** `root_pass`,
  `authorized_keys`, `authorized_users` are **write-only** (never returned) → config
  emits null, which is fine. Prune computed `id`/`status`/`ip_address`/`ipv4`/`ipv6`/
  `private_ip_address`/`specs`/`backups`/`has_user_data`/`host_uuid`/`watchdog_enabled`.
  Keep `region`/`type`/`image`/`label`. `interface`/`config`/`disk` blocks over-emit for
  instances that use explicit `linode_instance_config`/`_disk` sub-resources (out of
  scope) — flag at live QA; a bare image-built instance should draft clean.
- **Managed databases — SCRUB credentials, keep the resource.** `linode_database_mysql`/
  `_postgresql` expose sensitive computed `root_password` + `root_username` (and a
  `host_primary`/`host_secondary`/`port` set that's computed). generate-config-out should
  drop the computed sensitive creds; if it emits any, Phase-B scrubbing MUST redact them.
  The cluster resource itself is adoptable and plan-clean (label/engine/type/region/
  allow_list). `root_password` is **not** a reason to exclude the whole resource (unlike
  `linode_token`) — it's computed, not a required write-only input.
- **`linode_lke_cluster` — prune the sensitive kubeconfig + embedded pool computed.**
  Prune sensitive computed `kubeconfig` (base64 kube credentials) and `dashboard_url`,
  plus computed `status`/`api_endpoints`/`pool[*].nodes` (the per-node instance ids/
  status). The `pool {}` blocks themselves stay (that's how node pools are managed).
- **`linode_domain_record` / `linode_domain`**: prune computed `fqdn`; default
  `ttl_sec`/`priority`/`weight`/`port`/`protocol`/`service`/`tag` may over-emit on
  records. `linode_domain` — prune computed `status`; `soa_email` required for master.
- **`linode_firewall`**: rules are inline `inbound`/`outbound` blocks (keep). Prune
  computed `status`/`created`/`updated`. `linodes`/`nodebalancers` device attachments may
  reorder → tolerate; firewall→entity attachment is really a `linode_firewall_device`
  association (out of scope — managed inline here).
- **`linode_nodebalancer` / `_config` / `_node`**: NB — prune computed `ipv4`/`ipv6`/
  `hostname`/`created`/`updated`/`transfer`. Config — `ssl_cert`/`ssl_key` are
  **write-only** (config emits null; fine), prune computed `nodes_status`/`ssl_commonname`
  /`ssl_fingerprint`. Node — prune computed `status`/`config_id`/`nodebalancer_id` (those
  live in the import id, not the body).
- **`linode_volume`**: prune computed `status`/`filesystem_path`; `linode_id` is the
  optional attachment (may be null if detached).
- **`linode_stackscript`**: keep `script`/`images`/`rev_note`; prune computed
  `deployments_active`/`deployments_total`/`user_gravatar_id`/`created`/`updated`. Only
  account-owned (`is_public == false`) are enumerated.
- **`linode_vpc` / `_vpc_subnet`**: prune computed `created`/`updated`; subnet — prune
  computed `linodes` (the attached-instance list) and `ipv4` if auto-assigned.
- **`linode_object_storage_bucket`**: prune computed `hostname`/`created`; `cert`
  (TLS upload) is write-only. `acl`/`cors_enabled`/`versioning` are real config → keep.
- **`linode_image`**: keep `label`/`description`; `source_file`/`disk_id`/`linode_id`
  are create-time inputs the API won't round-trip for an already-created image — prune
  computed `status`/`created`/`size`/`is_public`/`type`/`vendor`. (Adopting an image
  mainly captures its metadata; the disk contents aren't reproducible from config — flag
  at live QA whether these draft clean at all, like DO snapshots.)
- **`linode_sshkey`**: `ssh_key` (public key) only, no secret; prune computed `created`.
  Safe.
- **`linode_rdns`**: `address` + `rdns` only; skip default PTRs (see quirk 4).

## Deliberately out of scope
- **Secrets / IAM plane** (`Capabilities.IAM=false`): `linode_token`,
  `linode_object_storage_key` (both write-only, EXCLUDED above), `linode_user`,
  `linode_user_grants`.
- **Data planes**: Object Storage objects/policies/ACL/website config, image *disk
  contents*, database rows/tables — the DATA inside buckets/images/databases.
- **Associations / sub-resources better managed inline**: `linode_firewall_device`,
  `linode_instance_config`, `linode_instance_disk`, `linode_instance_ip`,
  `linode_volume` attachments, `linode_nodebalancer_vpc_config`.
- **Point-in-time data**: instance backups/snapshots.
- **Networking primitives** low-value as IaC: `linode_ipv6_range`, reserved-IP
  assignments (rdns covers the useful reverse-DNS config).

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD instance + domain + domain_record + firewall + volume + sshkey (the core
account infra everyone has; all bare numeric ids bar record's comma composite) → INC-1
nodebalancer + nodebalancer_config + nodebalancer_node (the two-level fan-out + 2-part
and 3-part comma composites) → INC-2 vpc + vpc_subnet + stackscript (X-Filter `mine`) →
INC-3 lke_cluster (kubeconfig scrub, embedded pools) → INC-4 image (X-Filter
`is_public:false`) + rdns (skip default PTR) → INC-5 object_storage_bucket (colon import
composite) → INC-6 database_mysql + database_postgresql (credential scrub; decide legacy
vs `_v2` type strings) → EXCLUDED/never: token, object_storage_key (write-only secrets).
