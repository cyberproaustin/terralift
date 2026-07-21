# Fastly provider — build spec

Research artifact for the `fastly` provider (Phase A scaffold). Sources: Terraformer's
`providers/fastly/` (go-fastly v7 based), the `fastly/fastly` registry docs (import
formats + nested schema, verified per-resource below against the provider repo's
`docs/resources/*.md` and `examples/resources/*import*.txt`), and the Fastly API
(`https://api.fastly.com`). Build mirrors the DigitalOcean provider
(`internal/providers/digitalocean/`) — a flat, token-scoped, single-container provider —
with one **new** wrinkle: a second response family (JSON:API) for the TLS platform
endpoints, and the fact that Fastly is **service-centric** (see the shape section).

## Version pin (load-bearing)

Pin `fastly/fastly ~> 7.x` (current major). Two naming facts matter:
- Terraformer emits the **deprecated** names `fastly_service_v1` (VCL) and
  `fastly_user_v1`. The **current** names are `fastly_service_vcl` and `fastly_user`.
  Likewise `fastly_service_dictionary_items_v1` / `_acl_entries_v1` /
  `_dynamic_snippet_content_v1` are the deprecated aliases of
  `fastly_service_dictionary_items` / `fastly_service_acl_entries` /
  `fastly_service_dynamic_snippet_content`. **Use the current (un-`_v1`) names** for
  the TF types — do not copy Terraformer's.
- The Fastly API endpoints below are provider-version-independent.

## Shape

- Auth: `FASTLY_API_KEY` env var. **Fastly uses a custom header, NOT `Authorization:
  Bearer`** — every request carries `Fastly-Key: <token>` (plus `Accept:
  application/json`). This is the one hard divergence from `doapi.go`'s bearer header.
  A direct `net/http` client to `https://api.fastly.com` (mirror `doapi.go` →
  `fastlyapi.go`). The TF provider reads the same `FASTLY_API_KEY`.
- Scope: the **whole customer/account** (the token is scoped to one customer). One flat
  container = the customer (`model.ScopeTenant`). Container id/name from `GET
  /current_customer` — use `.id` (the customer id) and `.name`. Terraformer forced the
  user to set `FASTLY_CUSTOMER_ID` by hand; **do not** — auto-derive it from
  `/current_customer` (or `/tokens/self` → `.customer_id`). The customer id is needed
  for the users list endpoint and is the container id.
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Two response families — the thing that differs from `doapi.go`.** DigitalOcean
  wraps each array under a per-endpoint key; Fastly has *two* shapes depending on which
  API plane you hit:
  1. **Core config API** (`/service`, `/service/{id}/version`, `.../dictionary`,
     `.../acl`, `.../snippet`, `/customer/{id}/users`): the body is a **bare JSON
     array** — no envelope, no wrapping key. Unmarshal straight into `[]T`. (Singletons
     like `/current_customer`, `/tokens/self` are a bare JSON object.) Confirm on build:
     these return arrays, not `{ "data": … }`.
  2. **TLS platform API** (`/tls/subscriptions`, `/tls/activations`,
     `/tls/certificates`, `/tls/private_keys`, …) **and** `/service-authorizations`:
     **JSON:API** format —
     ```json
     { "data": [ { "id": "...", "type": "tls_subscription", "attributes": {...} } ],
       "links": { "next": "https://api.fastly.com/tls/subscriptions?page[number]=2" },
       "meta":  { "total_pages": 3, "record_count": 512 } }
     ```
     The id is `data[].id` (top-level, **not** under `attributes`). So the client needs
     **two** list helpers: a bare-array one and a JSON:API one (unmarshal `data`, read
     `links.next` / `meta.total_pages`).
- **Pagination — two mechanisms.**
  - Core: `?page=<n>&per_page=100` on `/service`; increment `page` until a page returns
    fewer than `per_page` items (or empty). The nested sub-lists (version/dictionary,
    version/acl, version/snippet, customer/users) are **not** paginated — one call
    returns all. `GET /service` also embeds each service's `versions` array inline, so
    the active version can be read from the list response with no extra call.
  - JSON:API: `?page[number]=<n>&page[size]=100`; follow `links.next` (a full URL) until
    absent, or loop `page[number]` to `meta.total_pages`. As in `doapi.go`, `links.next`
    is followed **with the `Fastly-Key` header**, so validate the host is
    `api.fastly.com` before sending the token (mirror `isDigitalOceanURL`). Bound both
    loops defensively (`fastlyMaxPages`).
- Status handling (mirror `doAPIError`): 401 → token invalid/expired (fatal, surfaced
  in preflight); 403/404 → feature/permission absent → best-effort skip at Verbose;
  429/5xx/network → enumeration may be silently incomplete → Warn. Token only ever on
  the `Fastly-Key` header, never in errors/logs.
- Preflight: `terraform` present + `FASTLY_API_KEY` set + `GET /tokens/self` succeeds
  (200) — this also yields the token's `customer_id` and `scope` for free. Fall back to
  `GET /current_customer` for the customer name.
- Connect: `GET /current_customer` → `.id` is the flat container. The token *is* the
  customer, so there is no multi-account resolution — just validate the call succeeds.

## Service-centric shape — the CRITICAL determination

Fastly's Terraform provider is **service-centric**: the vast majority of what looks like
"a resource" (domains, backends, headers, conditions, gzip, healthchecks, cache
settings, request settings, response objects, directors, VCL snippets, VCL files,
dictionaries, ACLs, and **~35 logging_* endpoint types**) are **nested blocks inside a
single `fastly_service_vcl` (or `fastly_service_compute`) resource — NOT separate
top-level resources.** There is no `fastly_backend` or `fastly_domain` (config) resource;
there is only a `backend {}` / `domain {}` block on the service. `terraform plan
-generate-config-out` emits this entire block tree under one resource.

Verified nested blocks on `fastly_service_vcl` (from the provider's `docs/resources/
service_vcl.md` "Nested Schema for …" headings): `domain`, `backend`, `condition`,
`cache_setting`, `request_setting`, `response_object`, `header`, `gzip`, `healthcheck`,
`director`, `dictionary`, `acl`, `snippet`, `dynamicsnippet`, `vcl`, `rate_limiter`,
`product_enablement`, `image_optimizer_default_settings`, and `logging_*`
(bigquery, blobstorage, cloudfiles, datadog, digitalocean, elasticsearch, ftp, gcs,
googlepubsub, grafanacloudlogs, heroku, honeycomb, https, kafka, kinesis, logentries,
loggly, logshuttle, newrelic, newrelicotlp, openstack, papertrail, s3, scalyr, sftp,
splunk, sumologic, syslog). `fastly_service_compute` is a leaner subset (`domain`,
`backend`, `healthcheck`, `dictionary`, `acl`, `logging_*`, `product_enablement`,
`resource_link`, and the **`package`** block) — no VCL-only blocks.

**Consequence for TerraLift:** the standalone-resource set is small. We enumerate the
*services* (and a handful of genuinely-standalone resources), and let generate-config-out
emit the nested blocks. We do **not** enumerate domains/backends/headers/etc. as
separate inventory items — they have no standalone TF type and no standalone import id.

### Standalone vs nested — the exact split
- **Standalone (own TF type + own import id):** `fastly_service_vcl`,
  `fastly_service_compute`, and the "content of a container" companions
  `fastly_service_dictionary_items`, `fastly_service_acl_entries`,
  `fastly_service_dynamic_snippet_content` (these manage the *entries/items/content*
  living on Fastly's mutable/unversioned plane, separate from the versioned block that
  *declares* the dictionary/acl/dynamicsnippet). Plus account-plane standalones:
  `fastly_tls_subscription`, `fastly_tls_activation`, `fastly_tls_certificate`,
  `fastly_tls_private_key` (EXCLUDE), `fastly_user`, `fastly_service_authorization`.
- **Nested block only (never a standalone resource):** everything else listed above —
  domain, backend, header, gzip, condition, cache_setting, request_setting,
  response_object, healthcheck, director, vcl, snippet (static), rate_limiter,
  product_enablement, all `logging_*`. The **dictionary/acl/dynamicsnippet _containers_
  themselves** are also blocks on the service; only their _contents_ are the companion
  standalone resources above.

### Version / active-version model
Services carry **versioned** configs. A service has many versions; one may be `active`.
- Import id for a service is the **bare `<service_id>`**; the provider imports the
  **active** version, **or the latest version if none is active** (verified in the
  service_vcl docs). There is also an optional `<service_id>@<version>` form to pin a
  specific version — **TerraLift should import the bare id** (active/latest), not pin.
- To enumerate a service's dictionaries/acls/snippets **consistently with what the
  service resource will manage**, resolve the version the same way the provider does:
  pick `active==true`, else the highest `number`, from the service's `versions` array.
  Terraformer used `LatestVersion` (highest number) unconditionally — that is **wrong**
  when the active version ≠ the latest draft; use active-then-latest.

## Enumeration spine

Flat customer scope. The fan-out is: **services → (resolve active version) → per-service
dictionaries / ACLs / dynamic snippets**; everything else is a flat account-level list.

- `GET /service` (bare array; paginate `page`/`per_page`; `versions` embedded) → for
  each service, `type=="vcl"` → `fastly_service_vcl`, `type=="wasm"` →
  `fastly_service_compute`.
- Per service, resolve active version `v`, then three sub-lists (bare arrays, no
  pagination):
  - `GET /service/{id}/version/{v}/dictionary` → per **non-write_only** dictionary emit
    `fastly_service_dictionary_items` (skip `write_only==true` dictionaries — their items
    can't be managed by this resource; see gotchas).
  - `GET /service/{id}/version/{v}/acl` → per acl emit `fastly_service_acl_entries`.
  - `GET /service/{id}/version/{v}/snippet` → per snippet with `dynamic==1` emit
    `fastly_service_dynamic_snippet_content` (static snippets, `dynamic==0`, are the
    service `snippet {}` block — skip).
- Flat account-level lists (best-effort, Verbose+continue on 403/404):
  - `GET /tls/subscriptions` (JSON:API) → `fastly_tls_subscription`
  - `GET /tls/activations` (JSON:API) → `fastly_tls_activation`
  - `GET /tls/certificates` (JSON:API) → `fastly_tls_certificate`
  - `GET /service-authorizations` (JSON:API) → `fastly_service_authorization`
  - `GET /customer/{customer_id}/users` (bare array) → `fastly_user`
  - `GET /tls/private_keys` (JSON:API) → enumerate for visibility but **EXCLUDE** at
    export (write-only key material; see below).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as `enumerate.go`).

## Resource catalog

Import IDs verified against the `fastly/fastly` provider repo
(`examples/resources/*import*.txt` + `docs/resources/*.md`). All scope = customer.
"Shape" = which response family the enumeration endpoint uses.

| native key | TF type | endpoint | shape | id field | import ID |
|---|---|---|---|---|---|
| fastly:service_vcl | fastly_service_vcl | `GET /service` (filter `type==vcl`) | bare array | `id` | `<service_id>` *(active/latest version; `@<v>` optional — don't pin)* |
| fastly:service_compute | fastly_service_compute | `GET /service` (filter `type==wasm`) | bare array | `id` | `<service_id>` *(same; but see package caveat)* |
| fastly:dictionary_items | fastly_service_dictionary_items | `GET /service/{id}/version/{v}/dictionary` (skip `write_only`) | bare array | `id` | `<service_id>/<dictionary_id>` **(slash)** |
| fastly:acl_entries | fastly_service_acl_entries | `GET /service/{id}/version/{v}/acl` | bare array | `id` | `<service_id>/<acl_id>` **(slash)** |
| fastly:dynamic_snippet_content | fastly_service_dynamic_snippet_content | `GET /service/{id}/version/{v}/snippet` (filter `dynamic==1`) | bare array | `id` | `<service_id>/<snippet_id>` **(slash)** |
| fastly:service_authorization | fastly_service_authorization | `GET /service-authorizations` | JSON:API | `data[].id` | `<id>` |
| fastly:tls_subscription | fastly_tls_subscription | `GET /tls/subscriptions` | JSON:API | `data[].id` | `<id>` |
| fastly:tls_activation | fastly_tls_activation | `GET /tls/activations` | JSON:API | `data[].id` | `<id>` |
| fastly:tls_certificate | fastly_tls_certificate | `GET /tls/certificates` | JSON:API | `data[].id` | `<id>` |
| fastly:tls_private_key | fastly_tls_private_key | `GET /tls/private_keys` | JSON:API | `data[].id` | `<id>` **(EXCLUDE — write-only key_pem)** |
| fastly:user | fastly_user | `GET /customer/{customer_id}/users` | bare array | `id` | `<id>` |

### Import-format quirks (§ do not get wrong)
1. **Services import by the bare `<service_id>`** — no version suffix in normal use. The
   `@<version>` form exists but pins a frozen version; TerraLift wants the live
   active/latest, so emit the bare id.
2. **The three service-content companions use a `/`-slash composite**
   `<service_id>/<dictionary_id|acl_id|snippet_id>` — contrast DigitalOcean's
   comma-joined composites. Single slash, `<service_id>` first.
3. **TLS + service_authorization + user import by a bare opaque `<id>`** (the JSON:API
   `data[].id`, or the bare-array `id`). No parent prefix.
4. `fastly_service_v1` / `fastly_user_v1` / `..._items_v1` / `..._entries_v1` /
   `..._content_v1` (Terraformer's names) are **deprecated aliases** — emit the current
   names in the catalog above.

## Curation gotchas (Phase B, when live)

Confirmed shapes, gotchas to verify against real `terraform plan -generate-config-out`
on a live account — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets
like the Cloudflare/DigitalOcean providers. **The service resource is by far the heaviest
curation surface in any provider so far** — one `fastly_service_vcl` emits the entire
nested block tree.

- **`fastly_service_vcl` — the big one.** generate-config-out emits *every* nested block
  (dozens of domains/backends/headers/conditions/logging endpoints). Expect: heavy
  computed over-emit; block ordering churn (`backend`/`header`/`condition` are sets —
  tolerate reordering); computed read-only attrs `active_version`, `cloned_version`,
  `force_refresh` (drop); and `activate`/`reuse`/`force_destroy`/`default_ttl`/
  `default_host`/`stale_if_error` control attrs (keep, but `activate` defaults `true`).
  This is a Phase-B-heavy resource; treat the Phase-A export as a breadth scaffold.
- **Nested secrets inside the service (write-only → scrub/author out-of-band).** Many
  `logging_*` blocks and backends carry secrets the API never returns:
  `logging_s3.secret_key`, `logging_gcs`/`logging_bigquery` service-account keys,
  `logging_splunk.token`, `logging_https`/`logging_datadog`/`logging_newrelic` tokens,
  `logging_kafka`/`logging_kinesis` credentials, `backend.ssl_client_cert` /
  `ssl_client_key`, etc. generate-config-out nulls these → they break plan-clean unless
  authored out-of-band. Phase-B scrubbing MUST redact any that leak (repo-wide secret
  scan is the backstop), and the export note should flag "logging/backend credentials
  must be re-supplied."
- **`fastly_service_compute` — package is not recoverable (fundamental limit).** The
  `package {}` block requires `filename` + `source_code_hash` of the compiled Wasm
  `.tar.gz` build artifact. The API returns only package *metadata* (hashsum/size), not
  the artifact — so no plan-clean config can be produced without the user supplying the
  package out-of-band. Same class of limit as the GCP gen2-function clone blocker: adopt
  the service shell + config, but flag the package as a manual re-supply. Later
  increment, not beachhead.
- **`fastly_tls_private_key` — EXCLUDE (write-only).** `key_pem` is Required + Sensitive
  and never returned by the API; generate-config-out nulls it → no plan-clean config.
  `excludedReason` it (surface, adopt out-of-band), exactly like DigitalOcean custom
  certificate / Cloudflare `custom_ssl`.
- **`fastly_tls_certificate` — `certificate_body` re-supply caveat.** `certificate_body`
  (the PEM) is Required; it is public (not a secret) but pairs with the excluded private
  key, and the custom-upload workflow is niche. The common, adoptable TLS path is
  `fastly_tls_subscription` (Fastly-managed Let's Encrypt/GlobalSign/Certainly). Treat
  `fastly_tls_certificate` as a later increment with a "may need cert body re-supply"
  note; prune computed `created_at`/`updated_at`/`issued_to`/`issuer`/`serial_number`/
  `signature_algorithm`/`domains`/`replace`.
- **Private dictionaries — skip for the items resource.** A service `dictionary` block
  with `write_only == true` is a *private* dictionary; its items **cannot** be managed by
  `fastly_service_dictionary_items` (provider limitation, stated in the docs). Skip
  `write_only` dictionaries during the items fan-out (the dictionary *block* still lives
  on the service resource). Non-private dictionary items are non-secret key-values —
  adopt.
- **`fastly_service_dictionary_items` / `_acl_entries` / `_dynamic_snippet_content` —
  `manage_*` flag.** Each has a `manage_entries`/`manage_items` boolean; on import the
  provider pulls existing entries into state. Emit `manage_* = true` so TF owns them, and
  expect the full entry set to over-emit — tolerate ordering.
- **`fastly_tls_subscription`**: `certificate_authority` + `domains` + `configuration_id`
  required; prune computed `created_at`/`updated_at`/`state`/`certificate_id`; the
  `managed_dns_challenge` map is Deprecated — drop it (use `managed_http_challenges`).
- **`fastly_tls_activation`**: just `certificate_id`/`configuration_id`/`domain` refs +
  computed `created_at` — light.
- **`fastly_user`**: `login`/`name`/`role` — no secret (no password attr; password reset
  is out-of-band). Prune computed `id`/`created_at`/`updated_at`/`locked`/
  `email_hash`/`two_factor_auth_enabled`. Safe. NOTE the account owner/superuser is your
  own token identity — adopting it is allowed but flag it (don't lock yourself out).
- **`fastly_service_authorization`**: `service_id` + `user_id` + `permission` — light;
  prune computed `id`.

## Write-only / secret resources (EXCLUDE)
- `fastly_tls_private_key` (`key_pem`) — excluded, above.
- `fastly_object_storage_access_keys` — the secret access key is write-only → exclude
  (out of scope anyway).
- `fastly_tsig_key` — DNS TSIG secret material, write-only → exclude (out of scope).
- Secret store *secrets* and KV/Config store *values* are the data plane (out of scope);
  the store *objects* are fine but deferred (below).

## Deliberately out of scope
- **Next-Gen WAF (`ngwaf_*`)** — a large separate security product plane (workspaces,
  rules, signals, thresholds, alerts, redactions, virtual patches). Like DO's
  observability plane; a dedicated much-later increment, not core infra.
- **New Domains/DNS product** (`fastly_domain`, `fastly_domain_v1`,
  `fastly_domain_service_link`, `fastly_dns_zone`, `fastly_tsig_key`) — the standalone
  Domains-API product, distinct from the service `domain {}` block (which IS covered via
  the service). Later increment.
- **Compute ACLs** (`fastly_compute_acl`, `fastly_compute_acl_entries`) — newer, separate
  from the VCL service `acl` block. Later increment. (import: `<compute_acl_id>` /
  `<compute_acl_id>/entries`.)
- **Data-plane stores** (`fastly_kvstore`, `fastly_configstore`,
  `fastly_configstore_entries`, `fastly_secretstore`) — the store *objects* are
  adoptable (import `<store_id>`; entries `<store_id>/entries`) but hold data-plane
  contents; defer to a later increment. Secret-store secret *values* are write-only.
- **API Security** (`fastly_api_security_operation`, `_tag`) — separate product.
- **Observability / misc**: `fastly_alert`, `fastly_custom_dashboard`,
  `fastly_integration` — optional later.
- **TLS platform (bulk) certs & mutual auth**: `fastly_tls_platform_certificate`
  (`GET /tls/bulk/certificates`, import `<id>`), `fastly_tls_mutual_authentication`
  (import `<id>`), `fastly_tls_subscription_validation` (a wait-helper, no real import) —
  optional later increment alongside `fastly_tls_certificate`.
- **Cloud-IAM plane** (`Capabilities.IAM=false`): user role management is modeled via
  `fastly_user`/`fastly_service_authorization` at breadth, but deeper IAM is not.
- **Data planes**: purge, real-time/historical stats, KV/secret/config store contents.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `fastly_service_vcl` alone (the whole product for most customers — one service
carries all domains/backends/headers/logging as nested blocks; this is where the
curation weight lives) → INC-1 `fastly_service_dictionary_items` +
`fastly_service_acl_entries` + `fastly_service_dynamic_snippet_content` (the versioned
service-content companions; needs the active-version resolver) → INC-2
`fastly_tls_subscription` + `fastly_tls_activation` + `fastly_service_authorization` +
`fastly_user` (account-plane JSON:API + users) → INC-3 `fastly_service_compute` (with the
package re-supply caveat) + `fastly_tls_certificate` → LATER/BLOCKED
`fastly_tls_private_key` (EXCLUDE), Domains-API product, Compute ACLs, KV/Config/Secret
stores, NGWAF.
