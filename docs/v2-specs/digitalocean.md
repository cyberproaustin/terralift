# DigitalOcean provider — build spec

Research artifact for the `digitalocean` provider (Phase A scaffold). Sources:
Terraformer's `providers/digitalocean/` (godo-based), the `digitalocean/digitalocean`
registry docs (import formats, verified per-resource below), and the DigitalOcean
REST API v2. Build mirrors the Cloudflare provider (`internal/providers/cloudflare/`)
— a flat, token-scoped, single-container provider — and the GitHub provider.

## Version pin (load-bearing)

Pin `digitalocean/digitalocean ~> 2.x` (current line). Unlike Cloudflare there is no
v-major rename hazard, but two import formats are **not** what Terraformer's older
code implies and are pinned to current registry docs:
- `digitalocean_certificate` imports by **name**, not id (the provider re-keys certs
  by name because a Let's Encrypt cert's UUID rotates on renewal). Terraformer used
  the id — do not copy that.
- `digitalocean_kubernetes_node_pool` imports by the **bare pool id**, not a
  `cluster/pool` composite.

The REST API v2 endpoints are provider-version-independent.

## Shape

- Auth: `DIGITALOCEAN_TOKEN` env var, `Authorization: Bearer <token>`. No CLI — a
  direct `net/http` client to `https://api.digitalocean.com/v2` (mirror `cfapi.go`).
  The TF provider reads the same `DIGITALOCEAN_TOKEN`.
- Scope: the **whole account** (token-scoped; there is no sub-account). One flat
  container = the account (`model.ScopeTenant`). Container id/name from
  `GET /v2/account` — use `account.uuid` (email is also available for a friendly name).
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Envelope — the one thing that differs from `cfapi.go`.** Cloudflare wraps every
  payload under a generic `result`; DigitalOcean wraps each endpoint under **its own
  named key**:
  ```json
  { "droplets": [ ... ], "links": { "pages": { "next": "...", "last": "..." } }, "meta": { "total": 12 } }
  ```
  The results array lives under a per-endpoint key (`droplets`, `domains`,
  `domain_records`, `ssh_keys`, `endpoints`, `kubernetes_clusters`, …) — **not** a
  fixed field. So the client cannot hardcode `result`; the list helper must take the
  nesting key as a parameter (unmarshal into `map[string]json.RawMessage` and pick the
  key, or a per-type root struct). Singletons (`GET /v2/registry`, `GET /v2/account`)
  wrap a single object under the same key convention, not an array.
- **Pagination — `links.pages.next`.** Each page carries
  `links.pages.next` = the full URL of the next page (absent/empty on the last page)
  and `meta.total` = the grand total. Loop by following `links.pages.next` until it is
  missing (equivalently, until the accumulated count reaches `meta.total`). Request
  `?per_page=200` (the max). Bound the loop defensively like `cfMaxPages`.
- Status handling (mirror `cfAPIError`): 401 → token invalid (fatal, surfaced in
  preflight); 403/404 → feature/permission absent → best-effort skip at Verbose;
  429/5xx/network → enumeration may be silently incomplete → Warn. The token is only
  ever on the `Authorization` header, never in errors/logs.
- Preflight: `terraform` present + `DIGITALOCEAN_TOKEN` set + `GET /v2/account`
  succeeds (200; `account.status == "active"`).
- Connect: `GET /v2/account` → the account uuid is the flat container. The token *is*
  the account, so there is no multi-account resolution (unlike Cloudflare's
  `GET /accounts`); just validate the call succeeds.

## Enumeration spine

The account is flat — most resources are top-level account collections. Only three
resources nest and need a parent loop (like Cloudflare's per-zone fan-out):
- `domains` → per-domain `domain_records`
- `databases` (clusters) → per-cluster `dbs` / `users` / `pools` / `replicas`
- `kubernetes/clusters` → per-cluster node pools (**embedded in the cluster object's
  `node_pools`**, no separate list call)

Everything else is a single best-effort account-level list (Verbose + continue on
403/404).

## Resource catalog

Import IDs verified against the current `digitalocean/digitalocean` registry docs —
the DO composite ids are **comma-separated** (like `digitalocean_record`), not
slash-separated. The JSON nesting key and id field are the two API-side things not to
get wrong (all scope = account).

| native key | TF type | endpoint | JSON key | id field | paged | import ID |
|---|---|---|---|---|---|---|
| digitalocean:droplet | digitalocean_droplet | `GET /v2/droplets` | `droplets` | `id` (int) | yes | `<droplet_id>` |
| digitalocean:domain | digitalocean_domain | `GET /v2/domains` | `domains` | `name` | yes | `<domain_name>` |
| digitalocean:record | digitalocean_record | `GET /v2/domains/{domain}/records` | `domain_records` | `id` (int) | yes | `<domain>,<record_id>` **(comma)** |
| digitalocean:firewall | digitalocean_firewall | `GET /v2/firewalls` | `firewalls` | `id` (uuid) | yes | `<firewall_id>` |
| digitalocean:vpc | digitalocean_vpc | `GET /v2/vpcs` | `vpcs` | `id` (uuid) | yes | `<vpc_id>` |
| digitalocean:ssh_key | digitalocean_ssh_key | `GET /v2/account/keys` | `ssh_keys` | `id` (int) | yes | `<ssh_key_id>` |
| digitalocean:project | digitalocean_project | `GET /v2/projects` | `projects` | `id` (uuid) | yes | `<project_id>` |
| digitalocean:loadbalancer | digitalocean_loadbalancer | `GET /v2/load_balancers` | `load_balancers` | `id` (uuid) | yes | `<lb_id>` |
| digitalocean:reserved_ip | digitalocean_reserved_ip | `GET /v2/reserved_ips` | `reserved_ips` | `ip` | yes | `<ip>` |
| digitalocean:floating_ip | digitalocean_floating_ip | `GET /v2/floating_ips` | `floating_ips` | `ip` | yes | `<ip>` **(dup of reserved_ip — pick one)** |
| digitalocean:reserved_ipv6 | digitalocean_reserved_ipv6 | `GET /v2/reserved_ipv6` | `reserved_ipv6s` | `ip` | yes | `<ip>` |
| digitalocean:certificate | digitalocean_certificate | `GET /v2/certificates` | `certificates` | `name` | yes | `<certificate_name>` **(NAME, not id)** |
| digitalocean:cdn | digitalocean_cdn | `GET /v2/cdn/endpoints` | `endpoints` | `id` (uuid) | yes | `<cdn_id>` |
| digitalocean:container_registry | digitalocean_container_registry | `GET /v2/registry` | `registry` | `name` | no (singleton) | `<registry_name>` |
| digitalocean:kubernetes_cluster | digitalocean_kubernetes_cluster | `GET /v2/kubernetes/clusters` | `kubernetes_clusters` | `id` (uuid) | yes | `<cluster_id>` |
| digitalocean:kubernetes_node_pool | digitalocean_kubernetes_node_pool | (embedded: cluster `.node_pools`) | — | `id` (uuid) | — | `<node_pool_id>` |
| digitalocean:database_cluster | digitalocean_database_cluster | `GET /v2/databases` | `databases` | `id` (uuid) | yes | `<cluster_id>` |
| digitalocean:database_db | digitalocean_database_db | `GET /v2/databases/{id}/dbs` | `dbs` | `name` | yes | `<cluster_id>,<db_name>` **(comma)** |
| digitalocean:database_user | digitalocean_database_user | `GET /v2/databases/{id}/users` | `users` | `name` | yes | `<cluster_id>,<user_name>` **(comma)** |
| digitalocean:database_connection_pool | digitalocean_database_connection_pool | `GET /v2/databases/{id}/pools` | `pools` | `name` | yes | `<cluster_id>,<pool_name>` **(comma)** |
| digitalocean:database_replica | digitalocean_database_replica | `GET /v2/databases/{id}/replicas` | `replicas` | `name` | yes | `<cluster_id>,<replica_name>` **(comma)** |
| digitalocean:volume | digitalocean_volume | `GET /v2/volumes` | `volumes` | `id` (uuid) | yes | `<volume_id>` |
| digitalocean:tag | digitalocean_tag | `GET /v2/tags` | `tags` | `name` | yes | `<tag_name>` |
| digitalocean:spaces_bucket | digitalocean_spaces_bucket | **no bearer-token list endpoint** (see below) | — | — | — | `<region>,<name>` **(BLOCKED)** |

### Import-format quirks (§ do not get wrong)
1. DO composite ids are **comma-joined**, not slash-joined: `digitalocean_record`
   (`<domain>,<record_id>`) and all four database sub-resources
   (`<cluster_id>,<name>`). Contrast Cloudflare's `/`-joined ids.
2. `digitalocean_certificate` imports by **name**; `digitalocean_container_registry`,
   `digitalocean_domain`, `digitalocean_tag`, and the DB sub-resource *names* are also
   name-keyed. Everything else is an id (numeric for droplet/ssh_key/record, uuid for
   the rest) or an IP (reserved/floating).
3. `digitalocean_kubernetes_node_pool` is the **bare pool id** — no `cluster/` prefix.
   Node pools are read from the cluster object's embedded `node_pools`, not a list
   call, and the pool tagged `terraform:default-node-pool` is **skipped** (it belongs
   to `digitalocean_kubernetes_cluster` and the provider refuses to import it).
4. `reserved_ip` and `floating_ip` are the **same underlying object** under two names
   (`/v2/reserved_ips` and `/v2/floating_ips` return the same IPs). Adopt **one** to
   avoid managing an IP twice — prefer `digitalocean_reserved_ip` (current);
   `digitalocean_floating_ip` is the deprecated alias.

## Curation gotchas (Phase B, when live)

Confirmed against real `terraform plan -generate-config-out` output on a live account
— not guessed. Prune computed via `hcl.WalkResourceBlocks`; scrub secrets like the
GitHub/Cloudflare providers.

- **`digitalocean_certificate` — EXCLUDE custom-type certs (write-only).** A
  `type = "custom"` cert requires `private_key` + `leaf_certificate` +
  `certificate_chain`, all **write-only** (the API never returns them);
  generate-config-out nulls them → no plan-clean config. `excludedReason` it (surface,
  adopt out-of-band), exactly like Cloudflare `custom_ssl` / GitHub actions_secret.
  `type = "lets_encrypt"` certs are safe (only `type` + `domains`) — adopt those. Note
  Let's Encrypt certs rotate their UUID on renewal, which is why the import key is the
  name.
- **`digitalocean_kubernetes_cluster` — import pre-tag blocker.** The provider refuses
  to import unless the cluster's default node pool carries the
  `terraform:default-node-pool` tag. Import blocks run no pre-hook, so either (a)
  TerraLift PATCHes the tag onto the default pool via the API before export, or (b) it
  surfaces a manual pre-step. Flag at live QA. Also prune the computed **sensitive**
  `kube_config` block (tokens/certs/keys) plus `endpoint`/`ipv4_address`/`status`/
  `urn`/`created_at`/`updated_at`; the default node pool must stay embedded as the
  `node_pool` block.
- **Database secrets (scrub).** `digitalocean_database_cluster` exposes computed
  sensitive `password`/`uri`/`private_uri`/`host`/`user`/`port`;
  `digitalocean_database_user` returns a `password` (MySQL/PostgreSQL; MongoDB only at
  creation, so imported Mongo users have none); `digitalocean_database_connection_pool`
  returns a sensitive `uri`/`password`. These are computed so generate-config-out
  should drop them — but if it emits any, Phase-B scrubbing MUST redact them (repo-wide
  secret scan is the backstop). Skip the DO-managed defaults on enumeration: database
  `defaultdb` and user `doadmin`.
- **`digitalocean_droplet` — heavy computed over-emit.** Prune `id`/`urn`/`status`/
  `locked`/`created_at`/`disk`/`memory`/`vcpus`/`price_monthly`/`price_hourly`/
  `ipv4_address`(+`_private`)/`ipv6_address`/`backup_ids`/`snapshot_ids`/`volume_ids`.
  Keep `image`/`region`/`size`. `user_data` is stored hashed (write-only-ish) → config
  emits nothing, which is fine. `tags`/`ssh_keys`/`volume_ids` may reorder → tolerate.
- **`digitalocean_record`**: prune computed `fqdn`; default `ttl`/`priority`/`port`/
  `weight` may over-emit. Minor.
- **`digitalocean_project`**: prune computed `owner_uuid`/`owner_id`/`created_at`/
  `updated_at`/`num_resources`. generate-config-out may emit the `resources` (URN)
  list, which drifts — drop it (project↔resource membership belongs to a separate
  `digitalocean_project_resources`, out of scope). Adopting the `is_default` project is
  allowed but note it.
- **`digitalocean_loadbalancer` / `digitalocean_firewall`**: prune computed `ip`/
  `status`/`urn` (LB) and `status`/`created_at`/`pending_changes` (firewall);
  `droplet_ids`/`tags` may reorder.
- **`digitalocean_reserved_ip` / `_floating_ip`**: prune computed `urn`; `droplet_id`
  is the optional assignment.
- **`digitalocean_cdn`**: `certificate_id` is deprecated in favor of
  `certificate_name`; prune computed `endpoint`/`created_at`.
- **`digitalocean_container_registry`**: singleton; `subscription_tier_slug` required;
  prune computed `endpoint`/`server_url`/`storage_usage_bytes`/`created_at`. No secrets
  (docker creds are a separate data source, not this resource).
- **`digitalocean_ssh_key`**: `public_key` only (no secret); prune computed
  `fingerprint`. Safe.
- **`digitalocean_volume`**: prune computed `urn`; `droplet_ids` is the computed
  attachment.

## Spaces buckets — blocked, separate auth plane

`digitalocean_spaces_bucket` has **no bearer-token list endpoint**. Spaces is
S3-compatible: the TF provider (and any enumerator) authenticates with
`SPACES_ACCESS_KEY_ID` / `SPACES_SECRET_ACCESS_KEY` (a distinct key pair from
`DIGITALOCEAN_TOKEN`) and lists buckets via S3 `ListBuckets` against a regional
endpoint (`<region>.digitaloceanspaces.com`). Import id is `<region>,<name>` (comma).
Out of the beachhead; a later increment can add an S3 path gated on the Spaces keys
being present. `digitalocean_spaces_key` itself is write-only (secret key) → exclude.

## Deliberately out of scope
- **Data / backups**: droplet & volume snapshots (`GET /v2/snapshots`), custom images —
  low-value point-in-time data.
- **Data planes**: Spaces objects/policies/CORS/ACL, container-registry
  repositories/tags/blobs, database rows/tables (the DATA *inside* spaces/registries/
  databases, per scope).
- **Cloud-IAM plane** (`Capabilities.IAM=false`): DO team/member management is not
  modeled by this provider.
- **Associations** better managed inline: `digitalocean_project_resources`,
  `digitalocean_floating_ip_assignment` / `_reserved_ip_assignment`, volume
  attachments.
- **Observability**: `monitor_alert`, `uptime_check`, `uptime_alert` — optional later.
- **App Platform** (`digitalocean_app`) — a large PaaS spec object; candidate for a
  dedicated later increment, not core infra.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD droplet + domain + record + firewall + vpc + ssh_key + project (the core
account infra everyone has; all simple ids bar record's comma composite) → INC-1
loadbalancer + reserved_ip + certificate(lets_encrypt-only) + cdn → INC-2
kubernetes_cluster + kubernetes_node_pool + container_registry → INC-3 database_cluster
+ database_db + database_user + database_connection_pool + database_replica (with
password scrubbing) → INC-4 volume + tag + reserved_ipv6 → BLOCKED/LATER spaces_bucket
(needs SPACES keys + S3 API), digitalocean_app.
</content>
</invoke>
