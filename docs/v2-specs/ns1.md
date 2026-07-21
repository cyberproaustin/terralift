# NS1 provider — build spec

Research artifact for the `ns1` provider (Phase A scaffold). Sources: Terraformer's
`providers/ns1/` (ns1-go `gopkg.in/ns1/ns1-go.v2` based — only `zone`, `monitoringjob`,
`team` implemented there), the `ns1-terraform/ns1` registry docs (import formats +
secret/write-only attributes, verified per-resource below against the provider repo's
`website/docs/r/*.html.markdown`), and the NS1 REST API v1 (`https://api.nsone.net/v1`).
Build mirrors the DigitalOcean provider (`internal/providers/digitalocean/`) — a flat,
token-scoped, single-container provider — with **one hard divergence borrowed from the
Fastly provider**: a custom auth header (`X-NSONE-Key`, like Fastly's `Fastly-Key`), NOT
`Authorization: Bearer`.

## Version pin (load-bearing)

Pin `ns1-terraform/ns1 ~> 2.0` (current major line; registry source `ns1-terraform/ns1`).
The provider's default API endpoint is `https://api.nsone.net/v1/`, matching the REST base
below, and it reads the same `NS1_APIKEY` env var TerraLift uses — so no endpoint or auth
config need be inlined. The REST v1 endpoints are provider-version-independent.

Naming note: Terraformer only implemented three services (`zone`, `monitoringjob`,
`team`) and used `NewSimpleResource` for jobs/teams (bare-id import). It never covered
`record` composite import correctly for our purposes, `datasource`/`datafeed`/`notifylist`,
or `user`/`apikey`. Do **not** treat Terraformer's coverage as complete — the catalog
below is the target.

## Shape

- Auth: `NS1_APIKEY` env var. **NS1 uses a custom header, NOT `Authorization: Bearer`** —
  every request carries `X-NSONE-Key: <apikey>` (plus `Accept: application/json`). This is
  the one hard divergence from `doapi.go`'s bearer header; it is exactly the Fastly
  `Fastly-Key` pattern (mirror `fastlyapi.go` → `ns1api.go`). The TF provider reads the
  same `NS1_APIKEY`. The token is only ever on the `X-NSONE-Key` header, never in
  errors/logs.
- Base URL: `https://api.nsone.net/v1`. A direct `net/http` client (no CLI).
- Scope: the **whole account** (the API key is scoped to one account, with granular
  per-key permissions). One flat container = the account (`model.ScopeTenant`).
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response family — bare JSON arrays, NO pagination (the thing that differs from BOTH
  `doapi.go` and `fastlyapi.go`).** NS1 v1 list endpoints return a **bare JSON array** with
  no envelope and no wrapping key — unmarshal straight into `[]T` (like Fastly's *core*
  bare-array endpoints, but simpler: **no `?page=`/`per_page` pagination** on the primary
  lists; one call returns everything). Singletons (`GET /v1/zones/{zone}`,
  `GET /v1/account/settings`) are a bare JSON object. There is **no** `result`/`data`
  wrapper and **no** `links.next` cursor to follow. So the client needs only a
  bare-array list helper and a bare-object get helper — no envelope-key parameter, no
  `links.pages.next` loop. (Confirm on build: if NS1 ever adds pagination to a huge list,
  it is not the documented default; bound any loop defensively anyway, `ns1MaxItems`.)
- **Records are NOT a separate list — they are embedded in the per-zone GET (the central
  enumeration fact).** `GET /v1/zones` returns only zone *summaries* (no records).
  `GET /v1/zones/{zone}` returns the full zone object with an embedded **`records`** array;
  each element carries `domain` (FQDN), `type` (e.g. `A`, `CNAME`, `MX`), `ttl`, and
  `short_answers`. That is the entire record fan-out — one GET per zone, then read the
  embedded array. (This mirrors DigitalOcean's `kubernetes_clusters.node_pools` embedded
  model, and DO domains→records — except NS1 records ride inside the parent GET rather than
  a child list endpoint.)
- Status handling (mirror `fastlyAPIError`/`doAPIError`): 401 → key invalid/revoked
  (fatal; surfaced in preflight, and if it happens mid-run record it as fatal like
  `fastly` `enumerate.go` rather than shipping a partial inventory); 403 → this key lacks
  the permission for that resource type (NS1 keys are granularly scoped) → best-effort skip
  at Verbose; 404 → absent → Verbose skip; 429/5xx/network → enumeration may be silently
  incomplete → Warn + count.
- Preflight: `terraform` present + `NS1_APIKEY` set + a validation GET succeeds. **Prefer
  `GET /v1/zones`** (200 + a JSON array, even empty, proves the key authenticates and has
  zone-read). Do **not** use `GET /v1/account/settings` as the *primary* probe: NS1 keys
  are permission-scoped and a DNS-only key legitimately 403s on account-management
  endpoints — that would false-negative a perfectly good key. `/account/settings` is a fine
  *secondary*, best-effort call for a friendly account name only.
- Connect — **no clean `whoami`/account-id endpoint (divergence from DO/Fastly).** Unlike
  DigitalOcean (`GET /v2/account` → `account.uuid`) and Fastly (`GET /current_customer` →
  `.id`), NS1 exposes no simple token→account-id call that works for every key.
  `GET /v1/account/settings` returns account details (and may include a `customerid`) but
  requires the account-manage permission, so it is not universally available. Resolution:
  best-effort `GET /v1/account/settings` for a friendly name / `customerid`; if it is
  unreadable, fall back to a **stable synthetic scope id** (e.g. `"ns1-account"`). The
  token *is* the account, so the exact id is cosmetic — it is only the single container key
  and the export dir name. There is no multi-account resolution.

## Enumeration spine

Flat account scope, one container. Two fan-outs; everything else is a flat account-level
bare-array list (best-effort, Verbose+continue on 403/404):

1. **zones → embedded records (the big fan-out).** `GET /v1/zones` (bare array of zone
   summaries) → for each zone emit `ns1_zone`, then `GET /v1/zones/{zone}` and iterate the
   embedded `records` array → emit one `ns1_record` per element, building the import id
   directly from `domain`+`type` (see below). This is the heaviest fan-out in the provider
   — a single zone can hold thousands of records, and there is one zone GET per zone.
   - **Optimization vs Terraformer:** Terraformer does an *extra* `GET /v1/zones/{zone}/{domain}/{type}` per record purely to fetch the record's internal `id` for its resource
     name. TerraLift does **not** need that — the import id is `<zone>/<domain>/<type>`,
     all three of which are already present on the embedded record summary. Skip the
     per-record GET; build the id from the zone GET alone.
2. **datasources → datafeeds (fan-out).** `GET /v1/data/sources` (bare array) → emit
   `ns1_datasource`, then `GET /v1/data/feeds/{source_id}` (bare array) → emit
   `ns1_datafeed` (composite import `<datasource_id>/<datafeed_id>`).

Flat account-level lists:
- `GET /v1/monitoring/jobs` → `ns1_monitoringjob`
- `GET /v1/lists` → `ns1_notifylist` (NS1 "notify lists" live at `/lists`)
- `GET /v1/account/teams` → `ns1_team` (breadth; see IAM plane decision)
- `GET /v1/account/users` → `ns1_user` (breadth; see IAM plane decision)
- `GET /v1/account/apikeys` → `ns1_apikey` (**enumerate for visibility, EXCLUDE at export**)
- `GET /v1/tsig` → `ns1_tsigkey` (**EXCLUDE — secret material; niche secondary-DNS, later**)

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as the DO/Fastly
`enumerate.go`).

## Resource catalog

Import IDs **verified verbatim** against the `ns1-terraform/terraform-provider-ns1` repo
(`website/docs/r/*.html.markdown`, quoted in the quirks section). All scope = account.
Response shape is bare array (list) or bare object (per-zone GET) — never an envelope. NS1
ids are hex strings (Mongo-style ObjectIDs, e.g. `52a27d4397d5f07003fdbcf4`); zones import
by FQDN, not id.

| native key | TF type | endpoint | shape | id field | fans out | import ID |
|---|---|---|---|---|---|---|
| ns1:zone | ns1_zone | `GET /v1/zones` | bare array | `zone` (FQDN) | — (parent of records) | `<zone>` **(FQDN, not id)** |
| ns1:record | ns1_record | embedded in `GET /v1/zones/{zone}` `.records[]` | bare object (parent) | `domain`+`type` | **fanned out per zone** | `<zone>/<domain>/<type>` **(slash; zone/domain/type order)** |
| ns1:monitoringjob | ns1_monitoringjob | `GET /v1/monitoring/jobs` | bare array | `id` | — | `<id>` |
| ns1:datasource | ns1_datasource | `GET /v1/data/sources` | bare array | `id` | — (parent of feeds) | `<id>` |
| ns1:datafeed | ns1_datafeed | `GET /v1/data/feeds/{source_id}` | bare array | `id` | **fanned out per datasource** | `<datasource_id>/<datafeed_id>` **(slash; datasource first)** |
| ns1:notifylist | ns1_notifylist | `GET /v1/lists` | bare array | `id` | — | `<id>` |
| ns1:team | ns1_team | `GET /v1/account/teams` | bare array | `id` | — | `<id>` |
| ns1:user | ns1_user | `GET /v1/account/users` | bare array | `username` | — | `<username>` **(username, not id)** |
| ns1:apikey | ns1_apikey | `GET /v1/account/apikeys` | bare array | `id` | — | `<id>` **(EXCLUDE — credential plane)** |
| ns1:tsigkey | ns1_tsigkey | `GET /v1/tsig` | bare array | `name` | — | `<name>` **(EXCLUDE — write-only `secret`)** |

### Import-format quirks (§ do not get wrong)

Verbatim from the provider docs:
1. **`ns1_record` is a THREE-part slash composite `<zone>/<domain>/<type>`** — order is
   zone, then the **FQDN** domain, then the uppercase record type; single slash separators.
   Provider doc: `terraform import ns1_record.<name> <zone>/<domain>/<type>`, example
   `terraform import ns1_record.www terraform.example.io/www.terraform.example.io/CNAME`.
   Note the domain segment is the **fully-qualified** name (includes the zone); at the zone
   apex `domain == zone` (e.g. `example.com/example.com/A`). NS1 permits only one record per
   `(zone, domain, type)`, so the composite is unique. Contrast DigitalOcean's *comma*
   composites and Fastly's *two*-part slash composites — NS1 records are **slash and
   three-part**.
2. **`ns1_zone` imports by the zone FQDN, not its internal id**:
   `terraform import ns1_zone.example terraform.example.io`.
3. **`ns1_datafeed` is a `/`-slash composite `<datasource_id>/<datafeed_id>`**, datasource
   id first: `terraform import ns1_datafeed.<name> <datasource_id>/<datafeed_id>`. This is
   why datafeeds must be fanned out per datasource (you need the parent id for the import).
4. **`ns1_user` imports by `<username>`**, not a hex id:
   `terraform import ns1_user.<name> <username>`. (`ns1_team`, `ns1_monitoringjob`,
   `ns1_notifylist`, `ns1_datasource`, `ns1_apikey` all import by the bare hex `<id>`.)
5. **`ns1_tsigkey` imports by `<name>`**: `terraform import ns1_tsigkey.importTest <name>`.

## Curation gotchas (Phase B, when live)

Confirmed shapes; gotchas to verify against real `terraform plan -generate-config-out` on
a live account — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like
the DO/Fastly providers. Until then curation is a no-op and an NS1 export is a breadth
scaffold, not yet plan-clean (the pipeline's repo-wide secret scan is the redaction
backstop).

- **`ns1_record` — the heavy surface.** Keep `zone`/`domain`/`type` (the identity) and the
  `answers {}` blocks (each `answer` + optional `region`/`meta`), `ttl`, `filters {}`
  (the filter chain — ordered, do not reorder), `regions {}`, `use_client_subnet`. Prune
  computed `id`. `answers`/`filters`/`regions` are sets/lists prone to ordering churn —
  tolerate reorder. **Linked records:** a record with a `link` field points at another
  domain and has no `answers` — keep the `link`, expect no answer blocks.
- **`ns1_zone` — skip records for linked / secondary zones.** A zone with a `link` set is a
  *linked* zone (its records mirror the link target — do **not** fan out / adopt individual
  `ns1_record`s for it; adopt only the `ns1_zone` with its `link`). A *secondary* zone
  (has `secondary {}` / primary master IPs; records are AXFR'd in) likewise should **not**
  have its records adopted as `ns1_record` — they are not individually managed. Only fan out
  records for standard **primary** zones. Prune computed `id`/`dns_servers`/`hostmaster`/
  `network_pools`; keep `zone`/`ttl`/`refresh`/`retry`/`expiry`/`nx_ttl` and
  `primary`/`additional_primaries`/`secondaries`/`link` as present.
- **`ns1_monitoringjob`** — `config` is a job_type-specific map; keep `job_type`/`config`/
  `regions`/`frequency`/`rapid_recheck`/`policy`/`rules {}`/`notify_*`. `notify_list`
  references an `ns1_notifylist` id → **dependency ordering** (notify lists must exist
  first; the import-block set has no ordering, but `-generate-config-out` should emit a
  literal id — fine for adoption). Prune computed `id`.
- **`ns1_notifylist`** — `notifications {}` blocks have a `type` + `config` map. Some
  notifier configs carry secrets/URLs (e.g. `webhook` URL, PagerDuty service key, Slack
  webhook). generate-config-out will emit whatever the API returns; **Phase-B scrubbing
  must redact any secret-bearing `config` values** (repo-wide secret scan is the backstop).
- **`ns1_datasource`** — `config` map varies by `sourcetype`; a few third-party sourcetypes
  carry credentials in `config`. Flag for scrub if a live account has such a source. Prune
  computed `id`.
- **`ns1_datafeed`** — `name`/`source_id`/`config` map; light. Prune computed `id`.
- **`ns1_team`** — `name` + permission blocks (`dns`/`data`/`account`/`security`/
  `monitoring`/`dhcp`/`ipam`). No secrets. Prune computed `id`.
- **`ns1_user`** — `username`/`name`/`email`/`teams` + permissions. **No password/secret
  attribute** (NS1 users authenticate with their own credentials; the resource does not
  hold one) — safe to adopt, like `fastly_user`. Prune computed `id`. NOTE: the user tied to
  *your own* `NS1_APIKEY` may appear — adopting it is allowed but flag it (don't fight your
  own access).

## Write-only / secret resources (EXCLUDE)

- **`ns1_tsigkey` — EXCLUDE (write-only secret).** `secret` is **Required + sensitive** (the
  hashed TSIG key material). Like DO custom-certificate `private_key` / Fastly
  `tls_private_key.key_pem`: a required secret that cannot be reproduced into plan-clean
  config from a read. Also a niche secondary-DNS / AXFR feature. `excludedReason` it
  (surface, adopt out-of-band). Later increment at best.
- **`ns1_apikey` — EXCLUDE from adoption, but for a DIFFERENT reason than tsigkey (be
  precise here).** It is **not** a hard write-only config-blocker: `key` is **Computed**
  (so generate-config-out won't emit it), and the provider docs explicitly state
  *"Imported keys will not have their key stored in the state file"* — so importing an
  apikey does **not** leak the secret into state, and a plan-clean config is technically
  producible. The reason to exclude is **credential hygiene + IAM plane**: it is a live
  account credential (potentially the very key TerraLift is running under), it belongs to
  the `Capabilities.IAM=false` plane, and its `expiry_duration` changes force key
  recreation (destroy/recreate churn, per the provider's "Important Notes"). Recommendation:
  enumerate for visibility, `excludedReason` at export ("account API key — adopt via a
  dedicated IAM increment, out-of-band"). Do **not** describe it as write-only like tsigkey.

## Account / IAM plane decision (teams / users / apikeys)

`Capabilities.IAM=false` — NS1's account plane is **not** modeled as a full IAM capability.
Within that, mirror the Fastly precedent (`fastly_user` + `fastly_service_authorization`
breadth-included, `fastly_tls_private_key`/`fastly_tsig_key` excluded) and GitHub
membership breadth:
- **Breadth-INCLUDE `ns1_team` and `ns1_user`** — non-secret account-config objects,
  useful for a complete account picture, plan-clean-able. They are adopted at breadth, not
  as a managed IAM plane (`Capabilities.IAM` stays false).
- **EXCLUDE `ns1_apikey`** (credential; above) and **`ns1_tsigkey`** (secret; above).

## Deliberately out of scope
- **DNSSEC** — `ns1_dnssec` is a *data source*, not an adoptable resource; DNSSEC on a zone
  is a computed/flag attribute of `ns1_zone`, not separately imported.
- **DNS Views / networks** — `ns1_dnsview`, view/network partitioning (advanced multi-tenant
  DNS). Later increment.
- **Pulsar (RUM) / applications** — `ns1_application`, `ns1_pulsarjob` (real-user-monitoring
  traffic steering). A separate product plane; later increment.
- **URL redirects** — `ns1_redirect`, `ns1_redirect_certificate` (the redirect
  `certificate` carries key material → exclude that one). Later increment.
- **IPAM / DHCP product** — `ns1_network`, `ns1_subnet`, `ns1_scope_group`, `ns1_dhcp*`,
  `ns1_optiondef`, `ns1_reservation`, etc. A large separate address-management product
  plane (like DO's observability or Fastly's NGWAF); a dedicated much-later increment, not
  core DNS infra.
- **Billing / usage** — `ns1_billingusage` and similar are data sources, not resources.
- **Data planes**: the answers/traffic *data* is modeled inside `ns1_record`; there is no
  separate data plane to enumerate.

## Build order (Phase B increments; Phase A builds all at once)

BEACHHEAD `ns1_zone` + `ns1_record` (the whole product for most NS1 customers — authoritative
DNS; the record fan-out via the embedded per-zone GET is where all the weight and curation
live; three-part slash import id) → INC-1 `ns1_monitoringjob` + `ns1_notifylist` (health
checks + notification targets; notifylist first for the monitoringjob `notify_list` ref;
watch notifylist config secrets) → INC-2 `ns1_datasource` + `ns1_datafeed` (traffic-steering
data inputs; the datafeed `<datasource_id>/<datafeed_id>` composite needs the per-datasource
fan-out) → INC-3 `ns1_team` + `ns1_user` (account breadth, `Capabilities.IAM` stays false) →
EXCLUDED/LATER `ns1_apikey` (credential-plane, dedicated IAM increment), `ns1_tsigkey`
(write-only secret), Pulsar/applications, redirects, DNS views, IPAM/DHCP product.
