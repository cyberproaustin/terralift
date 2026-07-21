# Datadog provider — build spec

Research artifact for the `datadog` provider (Phase A scaffold). Sources: Terraformer's
`providers/datadog/` (datadog-api-client-go v2 based), the `DataDog/datadog` registry
docs (import formats + nested schema, verified per-resource below against the provider
repo's `docs/resources/*.md`), and the Datadog REST API (v1 **and** v2 —
`https://api.datadoghq.com`). Build mirrors the Fastly provider
(`internal/providers/fastly/`) and DigitalOcean (`internal/providers/digitalocean/`) — a
flat, org-scoped, single-container provider — with **two** wrinkles beyond Fastly: (1) a
**two-header** auth scheme (not one custom header), and (2) Datadog straddles **API v1
and v2**, so there are *more than two* response shapes and *several* pagination
mechanisms. The two-list-helper pattern from `fastlyapi.go` generalises: a v1 family
(bare-array / keyed-object) and a v2 JSON:API family.

## Version pin (load-bearing)

Pin `DataDog/datadog ~> 3.x` (current major; note the org is `DataDog`, capital D-D).
Naming facts that matter:
- Terraformer emits `datadog_downtime` (the **legacy** monitor-downtime resource, now
  **deprecated** in the provider). The current resource is **`datadog_downtime_schedule`**
  (v2 API). **Emit `datadog_downtime_schedule`** — do not copy Terraformer's
  `datadog_downtime`.
- Terraformer has no generator for **`datadog_notebook`** or **`datadog_webhook`** — both
  are covered here from the registry + API directly.
- Terraformer's `dashboard_json` / `monitor_json` map to the raw-JSON escape-hatch
  resources (`datadog_dashboard_json`, `datadog_monitor_json`). Use the **typed**
  `datadog_dashboard` / `datadog_monitor` (generate-config-out emits typed HCL, not the
  JSON blob); the `_json` variants are out of scope.
- The REST API endpoints below are provider-version-independent.

## Shape

- **Auth — TWO headers on every request (the hard divergence from `fastlyapi.go`).**
  Datadog needs *both*:
  - `DD-API-KEY: <DD_API_KEY>`
  - `DD-APPLICATION-KEY: <DD_APP_KEY>`  ← **header is `DD-APPLICATION-KEY`, the env var is
    `DD_APP_KEY`** (name mismatch — do not send a `DD-APP-KEY` header). Plus
    `Accept: application/json`.
  A missing app key is *not* caught by `/validate` (see preflight) — it only fails on the
  first app-key-scoped list. Both keys are only ever on their headers, never in errors/
  logs. A direct `net/http` client (mirror `fastlyapi.go`); no Datadog CLI. The TF
  provider reads the same `DD_API_KEY` + `DD_APP_KEY` (aliases `DATADOG_API_KEY` /
  `DATADOG_APP_KEY`).
- **Site / base URL — configurable, must be read from env.** Default
  `https://api.datadoghq.com` (US1). Read `DD_HOST` (fallback `DATADOG_HOST`) as the full
  API base URL; the TF provider reads the same via `api_url` / `DD_HOST`. Known sites:
  | site | base URL |
  |---|---|
  | US1 (default) | `https://api.datadoghq.com` |
  | US3 | `https://api.us3.datadoghq.com` |
  | US5 | `https://api.us5.datadoghq.com` |
  | EU1 | `https://api.datadoghq.eu` |
  | AP1 | `https://api.ap1.datadoghq.com` |
  | US1-FED (Gov) | `https://api.ddog-gov.com` |
  Store the resolved base once; validate the host on any pagination follow-URL before
  re-sending the keys (mirror `isFastlyURL` → `isDatadogURL(base)`), since v2 next-links
  can be full URLs.
- Scope: the **whole org** — the `DD_API_KEY` + `DD_APP_KEY` pair is org-scoped; there is
  no sub-account and no multi-org resolution. One flat container = the org
  (`model.ScopeTenant`). Datadog has **no reliable "current org" endpoint** on all plans,
  and the org id/name is purely cosmetic here (exactly one container), so derive the
  container id/name best-effort — try `GET /api/v2/current_user` (org relationship) or
  `GET /api/v1/org`, else fall back to the DD site host string. This is *flatter* than
  Fastly's `/current_customer`: the key pair simply *is* the org, no lookup required.
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response families — MORE than two (the thing that differs from `fastlyapi.go`).**
  Fastly had a bare-array plane and a JSON:API plane. Datadog has **three** shapes
  spanning two API versions; classify per resource (§ next section):
  1. **v1 bare array** — body is a raw `[...]` (no envelope). `GET /api/v1/monitor`,
     `GET /api/v1/logs/config/pipelines`, `GET /api/v1/downtime` (legacy). Unmarshal
     straight into `[]T`.
  2. **v1 keyed object** — array under a *named* key, DigitalOcean-style:
     `GET /api/v1/dashboard` → `{"dashboards":[...]}`,
     `GET /api/v1/dashboard/lists/manual` → `{"dashboard_lists":[...]}`,
     `GET /api/v1/synthetics/tests` → `{"tests":[...]}`,
     `GET /api/v1/logs/config/indexes` → `{"indexes":[...]}`,
     `GET /api/v1/slo` → `{"data":[...]}` (keyed `data`, but **flat objects**, *not*
     JSON:API — the id is `data[].id`, there is no `type`/`attributes` wrapper). The list
     helper takes the nesting key as a parameter (mirror `doapi.go`).
  3. **v2 JSON:API** — `{"data":[{"id","type","attributes":{...}}],"meta":{"page":{...}}}`.
     `GET /api/v2/security_monitoring/rules`, `/api/v2/logs/config/metrics`,
     `/api/v2/roles`, `/api/v2/users`, `/api/v2/downtimes`. Also **v1 notebooks**
     (`GET /api/v1/notebooks`) is JSON:API-shaped (`data[].id`,`type`,`attributes`,`meta`).
     The id is `data[].id` (top-level, **not** under `attributes`) — same rule as Fastly's
     JSON:API plane. So the client needs, as Fastly did, a **v1 helper** (bare + keyed)
     and a **v2 JSON:API helper** (unmarshal `data`, read `meta.page`).
- **Pagination — several mechanisms; per-resource (§ catalog).**
  - Many v1 list endpoints return **everything in one call, no pagination**: dashboards,
    dashboard lists, synthetics tests, logs indexes, logs pipelines, logs metrics (v2,
    but unpaged), legacy downtime.
  - **v1 monitors**: `?page=<n>&page_size=<=1000>`, `page` is **0-based**; stop when a
    page returns fewer than `page_size` (mirror `fastlyListPaged`, 0-based).
  - **v1 SLO**: `?limit=<n>&offset=<n>`; loop offset until a short page.
  - **v1 notebooks**: `?count=<n>&start=<n>`; `meta.page.total_filtered_count` bounds it.
  - **v2 (roles/users/security rules)**: `?page[number]=<n>&page[size]=<=1000>`; loop
    `page[number]` until `page[size]*(n+1) >= meta.page.total_count` (Terraformer's
    `remaining` loop). **v2 downtimes** uses **`?page[offset]=<n>&page[limit]=<n>`**
    (offset-style, *not* `page[number]`) — a per-resource quirk. Bound every loop
    defensively (`ddMaxPages`).
- Status handling (mirror `fastlyAPIError`): 401/403 with an auth body → key invalid/
  insufficient scope (401 fatal & surfaced in preflight; a blanket 403 on the first list
  likely = bad/absent **app** key → treat like 401-fatal, since `/validate` passes on the
  API key alone); resource-level 403/404 → feature/permission absent → best-effort skip at
  Verbose; 429 (Datadog rate-limits aggressively, honour `X-RateLimit-Reset`) / 5xx /
  network → enumeration may be silently incomplete → Warn. Keys never in errors/logs.
- **Preflight**: `terraform` present + `DD_API_KEY` **and** `DD_APP_KEY` set + `GET
  /api/v1/validate` returns `{"valid":true}`. **Caveat:** `/api/v1/validate` only
  exercises the **API key** (it needs only `DD-API-KEY`); it returns `valid:true` even
  with a bogus app key. So *also* confirm the app key with one lightweight app-key-scoped
  call (e.g. `GET /api/v2/permissions` or `GET /api/v1/dashboard`) — otherwise the first
  real enumeration list is where a bad app key surfaces.
- **Connect**: no real resolution — the key pair is the org. Validate `/api/v1/validate`
  succeeds and set the single flat container (id/name best-effort per Scope above).

## API-version + response-shape determination — the CRITICAL classification

This is Datadog's analogue of Fastly's "service-centric" call: the load-bearing
per-resource fact is **which API version, which response shape, and which pagination**. Get
it wrong and either the decode fails (wrong envelope) or the inventory is truncated (wrong
pager). The rules:
- **v1 vs v2 is per-resource, not global.** Monitors/dashboards/SLOs/synthetics/downtime-
  legacy/logs-pipelines/logs-indexes are **v1**; security rules/logs-metrics/roles/users/
  downtime-schedule are **v2**; notebooks are **v1 but JSON:API-shaped**. There is no
  single "list" convention — each endpoint is pinned in the catalog.
- **`data`-keyed does NOT imply JSON:API.** v1 SLO returns `{"data":[...]}` with **flat**
  objects (`data[].id`, no `type`/`attributes`); v2 returns `{"data":[{"id","type",
  "attributes"}]}`. Both read the id from `data[].id`, but the *attribute* access differs —
  only relevant if you ever read past the id (enumeration only needs id + a display name).
- **id field type varies**: numeric (monitor, dashboard_list, legacy downtime, notebook —
  stringify), opaque string (dashboard, SLO, logs pipeline, logs metric, security rule),
  UUID string (role, user, downtime_schedule), the **public_id** (synthetics test), or the
  **name itself is the id** (logs index, metric metadata, webhook).
- Unlike Fastly/DO, **almost nothing is a composite** — every import id below is a *bare*
  id/name (see the quirks section). That is the good news; the classification above is the
  hard part.

## Enumeration spine

Flat org scope; **no parent fan-out at all** (contrast Fastly's per-service version→
dictionary/acl/snippet loop and DO's per-domain/per-cluster loops). Every resource is a
single best-effort org-level list (Verbose + continue on 403/404), each tagged with its
API version + shape + pager per the catalog:

- v1 bare array: `GET /api/v1/monitor` (paged 0-based; **skip `type ==
  synthetics alert`** monitors — those are owned by the synthetics test, not standalone,
  per Terraformer), `GET /api/v1/logs/config/pipelines` (**skip `is_read_only`
  integration pipelines**), `GET /api/v1/downtime` (legacy — see build order).
- v1 keyed object: `GET /api/v1/dashboard` (`dashboards`),
  `GET /api/v1/dashboard/lists/manual` (`dashboard_lists`),
  `GET /api/v1/synthetics/tests` (`tests`, id = `public_id`),
  `GET /api/v1/logs/config/indexes` (`indexes`, id = `name`),
  `GET /api/v1/slo` (`data`, flat).
- v1 JSON:API: `GET /api/v1/notebooks` (`data[].id`, paged `start`/`count`).
- v2 JSON:API: `GET /api/v2/security_monitoring/rules` (paged `page[number]`; **skip
  `attributes.isDefault`** rules), `GET /api/v2/logs/config/metrics` (unpaged),
  `GET /api/v2/downtimes` (paged **`page[offset]`/`page[limit]`**),
  `GET /api/v2/roles` (paged `page[number]`), `GET /api/v2/users` (paged `page[number]`;
  optionally `filter[status]=Active,Pending` to skip disabled).
- **Not cleanly enumerable → later/blocked** (surfaced, not in the spine): `datadog_webhook`
  (no list-all endpoint) and `datadog_metric_metadata` (needs an explicit metric-name
  filter). See build order.

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as `enumerate.go`).

## Resource catalog

Import IDs verified against the current `DataDog/datadog` registry docs
(`docs/resources/*.md`). All scope = org. "api" = v1/v2; "shape" = which response family
the enumeration endpoint uses.

| native key | TF type | endpoint | api | shape | id field | import ID |
|---|---|---|---|---|---|---|
| datadog:monitor | datadog_monitor | `GET /api/v1/monitor` (skip synthetics-alert) | v1 | bare array | `id` (int) | `<monitor_id>` |
| datadog:dashboard | datadog_dashboard | `GET /api/v1/dashboard` | v1 | keyed `dashboards` | `id` (string) | `<dashboard_id>` |
| datadog:dashboard_list | datadog_dashboard_list | `GET /api/v1/dashboard/lists/manual` | v1 | keyed `dashboard_lists` | `id` (int) | `<dashboard_list_id>` |
| datadog:service_level_objective | datadog_service_level_objective | `GET /api/v1/slo` | v1 | keyed `data` (flat) | `id` (string) | `<slo_id>` |
| datadog:synthetics_test | datadog_synthetics_test | `GET /api/v1/synthetics/tests` | v1 | keyed `tests` | `public_id` | `<public_id>` |
| datadog:logs_index | datadog_logs_index | `GET /api/v1/logs/config/indexes` | v1 | keyed `indexes` | `name` | `<index_name>` **(name IS the id)** |
| datadog:logs_custom_pipeline | datadog_logs_custom_pipeline | `GET /api/v1/logs/config/pipelines` (skip read-only) | v1 | bare array | `id` (string) | `<pipeline_id>` |
| datadog:logs_metric | datadog_logs_metric | `GET /api/v2/logs/config/metrics` | v2 | JSON:API | `data[].id` (= metric name) | `<metric_name>` |
| datadog:notebook | datadog_notebook | `GET /api/v1/notebooks` | v1 | JSON:API | `data[].id` (int) | `<notebook_id>` |
| datadog:security_monitoring_rule | datadog_security_monitoring_rule | `GET /api/v2/security_monitoring/rules` (skip default) | v2 | JSON:API | `data[].id` (string) | `<rule_id>` |
| datadog:downtime_schedule | datadog_downtime_schedule | `GET /api/v2/downtimes` | v2 | JSON:API | `data[].id` (uuid) | `<downtime_id>` |
| datadog:role | datadog_role | `GET /api/v2/roles` | v2 | JSON:API | `data[].id` (uuid) | `<role_id>` |
| datadog:user | datadog_user | `GET /api/v2/users` | v2 | JSON:API | `data[].id` (uuid) | `<user_id>` |
| datadog:webhook | datadog_webhook | **no list endpoint** (name-filter only) | v1 | — | `name` | `<webhook_name>` **(BLOCKED — see below)** |
| datadog:metric_metadata | datadog_metric_metadata | **no list endpoint** (metric-name filter only) | v1 | — | `metric` name | `<metric_name>` **(filter-required)** |

### Import-format quirks (§ do not get wrong)
1. **Everything imports by a BARE id/name — no composites.** Unlike Fastly's
   `<service_id>/<sub_id>` slashes and DO's `<cluster>,<name>` commas, every Datadog
   resource above imports by a single bare token. The variety is in *what* the token is
   (numeric id / opaque string id / UUID / `public_id` / the name itself), not in
   joining.
2. **`datadog_logs_index` and `datadog_metric_metadata` import by NAME** — the index name
   and the metric name respectively *are* the id; there is no separate numeric id.
3. **`datadog_synthetics_test` imports by `public_id`** (e.g. `abc-def-ghi`), the
   `public_id` field from the list — not the internal `monitor_id`.
4. **`datadog_webhook` imports by the webhook `name`.** But there is **no list-all
   endpoint** in the public Webhooks integration API (only get/create/update/delete by
   name), so it cannot be discovered without a caller-supplied name list — blocked from
   the enumeration spine (see gotchas + build order).
5. **`datadog_user`** current registry imports by the **user id (UUID)**. Terraformer had
   a historical fork (users lacking role relationships — created via the v1 API — were
   imported by **handle/email**); the current v2-first provider keys on the id. Emit the
   id.
6. Numeric ids (monitor, dashboard_list, legacy downtime, notebook) come off the wire as
   JSON numbers — **stringify** before use.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on
a live org — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the
Fastly/DigitalOcean providers. **`datadog_dashboard` is the heaviest curation surface**
(deeply-recursive widget tree), the Datadog analogue of `fastly_service_vcl`.

- **`datadog_dashboard` — the big one.** generate-config-out emits the entire nested
  `widget` tree; **group widgets recursively contain widgets**, so the block depth is
  unbounded. Prune computed `url`/`author_handle`/`author_name`/`created_at`/`modified_at`
  and per-widget computed ids. Widget-block ordering may churn (tolerate). `restricted_roles`
  references role ids. **Template-string hazard:** widget queries and titles contain
  `${...}` template variables and Datadog `%{...}`/`{{...}}` syntax → the generated HCL must
  keep these literal. `terraform` escapes its own generate-config-out strings, but this is
  the #1 thing to verify on real output (Terraformer needed a manual `%{`→`%%{` hook —
  confirm terraform's writer does the equivalent). Phase-B-heavy; treat Phase-A export as a
  breadth scaffold.
- **`datadog_monitor`.** `query` + `message` are core; `message` carries `{{#is_alert}}`
  Datadog template syntax and `@notification` handles → same literal-string hazard as
  dashboards. Prune computed `id`; `restricted_roles` refs roles. Defaults over-emit
  (`notify_no_data`/`renotify_interval`/`notify_audit`/`include_tags`/`new_group_delay`);
  `silenced` is deprecated → drop. Skip `synthetics alert` type monitors on enumeration
  (owned by the synthetics test).
- **`datadog_synthetics_test` — nested secrets.** Browser/multistep tests emit a large
  `steps`/`api_step` tree. Write-only material to scrub/re-supply: `config_variable` with
  `type=text` + `secure=true` (value never returned), `request_client_certificate`
  (cert + key), and any auth headers/basic-auth in `request_definition`/`request_headers`.
  Prune computed `monitor_id`. Adopt the shell; flag secret vars as out-of-band.
- **`datadog_logs_custom_pipeline` — `%{` escaping.** Grok-parser processors use `%{...}`
  patterns; Terraformer needed a `%{`→`%%{` `PostConvertHook`. Verify terraform's writer
  escapes these (else plan breaks). Skip `is_read_only` integration pipelines on
  enumeration. `filter.query` is required.
- **`datadog_logs_index`.** Import/adopt is fine, but note **indexes cannot be freely
  created/deleted** (Datadog plan-limited) — adoption is the realistic path. Prune nothing
  secret; `filter.query`, `daily_limit`, `exclusion_filter` are the surface. The index
  *ordering* is a separate `datadog_logs_index_order` singleton (out of scope).
- **`datadog_logs_metric`.** `compute`/`filter`/`group_by` blocks; light. id = the metric
  name.
- **`datadog_service_level_objective`.** `monitor_ids` refs monitors; `thresholds`/`type`
  (metric vs monitor). Prune computed `id`. Light.
- **`datadog_notebook`.** `cells` nested tree (markdown/timeseries/etc.); prune computed
  `author`/`created`/`modified`. Medium weight.
- **`datadog_security_monitoring_rule`.** `query`/`case`/`options` blocks; `enabled` is
  carried explicitly (Terraformer sets it from `isEnabled`). Skip `isDefault` rules
  (managed by Datadog — use `datadog_security_monitoring_default_rule` if ever needed, out
  of scope here).
- **`datadog_downtime_schedule`.** `scope` + `monitor_identifier` (monitor_id **or** tags)
  + `schedule` (one-time/recurring). Prune computed `id`. Note this **supersedes the
  deprecated `datadog_downtime`** — do not adopt both for the same downtime.
- **`datadog_role` (IAM-ish, breadth).** Terraformer quirk to reproduce: the `permission`
  blocks over-emit a computed `name` — only `permission.id` is authoritative
  (Terraformer `IgnoreKeys "permission.[n].name"`). Prune the computed `name` per
  permission (and `user_count`). No secrets.
- **`datadog_user` (IAM-ish, breadth).** `email`/`name`/`roles` (role-id refs). **No
  secret** — there is no password attribute (invites/password resets are out-of-band).
  Prune computed `verified`/`disabled`/`handle`. Caution: your own user (the one behind
  `DD_APP_KEY`) may appear — adopting it is allowed but flag it (like Fastly's account-owner
  note; don't disable yourself).
- **`datadog_webhook`.** `custom_headers` can carry auth tokens → scrub if emitted. `url`
  is not itself secret. (Also blocked on enumeration — below.)

## Write-only / secret resources (EXCLUDE)

The credential/integration plane is where Datadog's secrets live — exclude these (surface,
adopt out-of-band), exactly like Fastly's `tls_private_key` / DO's custom certificate:
- **`datadog_api_key`, `datadog_application_key`** — the key *value* is write-only
  (returned once at creation, never on read) → exclude. This is the actual secret material.
- **`datadog_integration_gcp`** (`private_key` / `private_key_id` — GCP service-account
  secret), **`datadog_integration_azure`** (`client_secret`),
  **`datadog_integration_pagerduty`** + **`_service_object`** (`api_token` / `service_key`),
  **`datadog_integration_opsgenie_service`** (`opsgenie_api_key`) — all carry write-only
  credentials → exclude the whole integration plane.
- **`datadog_synthetics_global_variable`** — a variable with `secure = true` has a
  write-only `value` → exclude the secure ones (or scrub the value). Terraformer enumerates
  these; TerraLift should not adopt secret ones.
- **`datadog_synthetics_private_location`** — the `config` output (the private-location
  install key/config) is returned only at creation and is sensitive → exclude / re-supply.
- **`datadog_webhook`** — not fully excluded, but `custom_headers` must be scrubbed (above).

## Deliberately out of scope
- **Integration plane** (`datadog_integration_aws`/`_aws_lambda_arn`/`_aws_log_collection`/
  `_azure`/`_gcp`/`_pagerduty`/`_slack_channel`/`_opsgenie_service`/`_cloudflare`/…) — a
  large separate credential-bearing plane (most carry write-only secrets, several need a
  filter/account-name and have no clean list). Excluded above; a much-later increment at
  best, not core observability config.
- **Ordering singletons** (`datadog_logs_index_order`, `datadog_logs_pipeline_order`,
  `datadog_logs_archive_order`) — single org-wide ordering objects, better authored by
  hand; adopting them fights the individual resources. Out of scope.
- **`datadog_logs_archive`** — depends on the excluded integration plane (S3/GCS/Azure
  archive targets). Later increment, gated on the integrations.
- **Legacy `datadog_downtime`** — deprecated in favour of `datadog_downtime_schedule`
  (covered). Enumerated by the v1 `GET /api/v1/downtime` only if we ever need the legacy
  objects; default to the schedule resource.
- **JSON escape-hatch resources** (`datadog_dashboard_json`, `datadog_monitor_json`) — raw
  JSON blobs; we emit the typed resources instead.
- **APM / RUM / CI / Cloud-Cost / Software-Catalog / Sensitive-Data-Scanner planes**
  (`datadog_rum_application`, `datadog_apm_retention_filter`, `datadog_service_definition_yaml`,
  `datadog_restriction_policy`, `datadog_spans_metric`, …) — separate product planes,
  dedicated later increments.
- **Cloud-IAM depth** (`Capabilities.IAM=false`): `datadog_role`/`datadog_user` are modeled
  at breadth (no secrets), but SSO/SAML (`datadog_authn_mapping`), service accounts, and
  team management are not.
- **Data planes**: metric points, log events, traces, SLO history — the DATA behind the
  config, per scope.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `datadog_monitor` + `datadog_dashboard` (what essentially every Datadog customer
manages as IaC; monitor = simple v1 bare-array numeric id, dashboard = v1 keyed object and
the heaviest curation surface — the recursive widget tree, where the template-escaping work
lives) → INC-1 `datadog_service_level_objective` + `datadog_synthetics_test` +
`datadog_dashboard_list` (the rest of the v1 keyed-object observability core; SLO/synthetics
are ubiquitous) → INC-2 `datadog_logs_index` + `datadog_logs_custom_pipeline` +
`datadog_logs_metric` (logs config; exercises v1-bare + v1-keyed + v2 in one increment and
the `%{` grok-escaping) → INC-3 `datadog_notebook` + `datadog_security_monitoring_rule` +
`datadog_downtime_schedule` (adds the v1-JSON:API notebooks pager and the v2 `page[number]`
/ `page[offset]` pagers) → INC-4 (IAM-ish breadth) `datadog_role` + `datadog_user` (v2
JSON:API; permission-block `name` pruning; no secrets; self-adoption caution) →
LATER/BLOCKED `datadog_webhook` (no list endpoint — needs a name filter),
`datadog_metric_metadata` (metric-name filter required, not enumerable), the integration
plane (write-only secrets), `datadog_logs_archive`, ordering singletons, legacy
`datadog_downtime`, and the APM/RUM/CI/SSO planes.
