# Logz.io provider — build spec

Research artifact for the `logzio` provider (Phase A scaffold). Sources: Terraformer's
`providers/logzio/` (built on the legacy `jonboydell/logzio_client v1.2.0` SDK — it covers
**only** alerts + endpoints, and emits the *deprecated* `logzio_alert`, see Version pin),
the `logzio/logzio` registry docs (import formats + resource set, verified per-resource
below against the provider repo's `docs/resources/*.md`), and the Logz.io public API
(`https://api.logz.io` and its regional siblings). Build mirrors the **Fastly** provider
(`internal/providers/fastly/`) — a flat, token-scoped, single-container REST provider driven
by a direct `net/http` client, NOT a CLI or vendor SDK — with **two** wrinkles beyond
Fastly:

1. **A custom auth header that is NOT `Fastly-Key`/`Authorization`** — Logz.io reads
   `X-API-TOKEN: <token>` on every request (§ Shape).
2. **A region-specific base URL that must be resolved from env** — like Datadog's multi-site
   `DD_HOST`, but Logz.io's is chosen from a small `region` code (`us`/`eu`/`au`/…) that maps
   to `api[-<region>].logz.io`, not a full host string (§ Shape). And, unlike Fastly's single
   bare-array plane, Logz.io's **enumeration shape varies per resource** (GET bare-list vs
   `POST …/search` vs singleton), which is the load-bearing per-resource fact (§ CRITICAL).

## Version pin (load-bearing)

Pin `logzio/logzio ~> 1.x` (current major; source `logzio/logzio`). Naming facts that matter:
- Terraformer emits **`logzio_alert`** — the **legacy v1** log-alert resource, now
  **deprecated** in the provider in favour of **`logzio_alert_v2`** (the v2 alerts API).
  **Emit `logzio_alert_v2`** — do not copy Terraformer's `logzio_alert`. (`logzio_alert`
  hits `/v1/alerts`; `logzio_alert_v2` hits `/v2/alerts`.)
- Terraformer's logzio provider is **thin**: only `alerts` + `alert_notification_endpoints`
  generators exist (legacy `jonboydell/logzio_client v1.2.0`). Everything else in the catalog
  below (drop filters, sub-accounts, users, log-shipping tokens, s3-fetchers, archive,
  metrics accounts, auth groups, kibana data views, the grafana_* plane) is covered here
  from the registry + API directly — Terraformer has no generator for them.
- **Terraformer inlines the token into the provider block** (`GetConfig` writes
  `api_token` + `base_url` into HCL) — a secret leak. TerraLift MUST NOT inline the token;
  the emitted `providers.tf` authenticates via the `LOGZIO_API_TOKEN` env var only (mirror
  Fastly's `providers.tf` note).
- The REST API endpoints below are provider-version-independent.

## Shape

- **Auth — the `X-API-TOKEN` header (the hard divergence from `fastlyapi.go`).** Logz.io
  authenticates with a custom header `X-API-TOKEN: <api-token>` (plus `Content-Type:
  application/json` / `Accept: application/json`). This is a **management / API-account
  token** — read it from `LOGZIO_API_TOKEN`. NOT `Authorization: Bearer`, NOT `Fastly-Key`.
  The token rides **only** on the `X-API-TOKEN` header — never in the URL, query, errors, or
  logs (redact any URL that ever appears in a message, belt-and-suspenders, mirror Datadog's
  `redactURL`). A direct `net/http` client (mirror `fastlyapi.go`); no Logz.io CLI. The TF
  provider reads the same token from `api_token` / `LOGZIO_API_TOKEN`.
- **Base URL — REGION-specific, must be resolved from env (the Datadog-style wrinkle).**
  Logz.io has regional API endpoints. Resolve the base URL from **`LOGZIO_REGION`** (a short
  region code, default `us`) via the map below, OR from an explicit **`LOGZIO_BASE_URL`**
  override (full URL wins over region). The TF provider takes the same as an in-config
  `region` (default `us`) or `base_url`; TerraLift reads them from env. Known regions:
  | region code | base URL |
  |---|---|
  | `us` (default) | `https://api.logz.io` |
  | `eu` | `https://api-eu.logz.io` |
  | `au` | `https://api-au.logz.io` |
  | `ca` | `https://api-ca.logz.io` |
  | `uk` | `https://api-uk.logz.io` |
  | `wa` | `https://api-wa.logz.io` |
  The general rule is **`https://api-<region>.logz.io`, except `us` → `https://api.logz.io`**
  (no region infix). Resolve the base once; force **https** (upgrade a bare host / explicit
  `http://`, like Datadog's `datadogBase`) so the token is never sent in cleartext; use a
  **redirect-refusing** client (mirror `datadogHTTPClient` — Go strips `Authorization`/`Cookie`
  on a cross-host 3xx but NOT a custom `X-API-TOKEN` header, so an auto-followed redirect
  would leak the token). URLs are always built from `base+path`; we never follow a
  server-supplied next-link.
- **Scope: one Logz.io account = one flat container.** The `X-API-TOKEN` is **account-scoped**
  — it *is* the account. No sub-account resolution, no multi-org lookup, no parent fan-out
  (contrast the identity providers). One flat container = the account (`model.ScopeTenant`).
  There is no reliable "current account" name endpoint, so derive the container id/name
  best-effort (fall back to the region/base-URL host string — always non-empty and stable,
  like Datadog's `datadogOrg` host fallback). **Note:** `logzio_sub_account` resources
  *within* this account are enumerable config objects, but they do NOT create new
  enumeration scopes — the token only sees its own account's view; we do not fan the token
  out into each sub-account (that needs per-sub-account tokens; out of scope).
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response / enumeration shapes vary per resource — the thing that differs from
  `fastlyapi.go`.** Fastly was uniformly bare-array. Logz.io has **three** list mechanisms;
  classify per resource (§ CRITICAL section):
  1. **GET bare-list** — `GET <path>` returns a raw `[...]` (or an object with a named
     array). Most `/v1/*` config endpoints: endpoints, drop-filters, sub-accounts, users,
     s3-fetchers, archive settings, metrics accounts. Unmarshal into `[]T`.
  2. **POST …/search** — the list is a `POST <path>/search` (or `…/retrieve`) taking a JSON
     **body** (filter + pagination), returning a paged envelope. Applies to at least
     **alerts** (`POST /v2/alerts/search`, though a bare `GET /v2/alerts` list-all also
     exists — VERIFY on build) and **log-shipping tokens** (`POST /v1/log-shipping/tokens/
     retrieve`). These need a request body and a pagination loop.
  3. **Singleton / whole-set** — one object per account: `logzio_authentication_groups`
     (the entire SAML auth-group set is ONE resource) and `logzio_grafana_notification_policy`
     (the single account-wide notification-policy tree). No list; a single GET.
  The grafana_* plane is a **fourth** shape: Grafana's own provisioning HTTP API proxied
  under `/v1/grafana/api/...` (search + provisioning endpoints, UID ids) — see below.
- **Pagination — per-resource.** The GET bare-lists generally return everything in one call
  (no pagination) — endpoints, drop-filters, sub-accounts, s3-fetchers, archive, metrics
  accounts. The `POST …/search` lists page via a body (`{"pagination":{"pageNumber","pageSize"}}`
  or `{"from","size"}` — VERIFY the exact envelope per endpoint); loop until a short/empty
  page. Bound every loop defensively (`logzioMaxPages`).
- Status handling (mirror `fastlyAPIError` / `datadogAPIError`): 401 (bad/absent/expired
  token) → fatal, surfaced in preflight (and if it appears mid-enumeration, treat as fatal —
  every remaining list will fail); 403/404 (feature/permission absent — e.g. metrics/archive
  not enabled on the plan) → best-effort skip at Verbose; 429 (Logz.io rate-limits) / 5xx /
  network → enumeration may be silently incomplete → Warn. Token never in errors/logs.
- **Preflight**: `terraform` present + `LOGZIO_API_TOKEN` set + one **lightweight
  account-scoped list** succeeds (e.g. `GET /v1/endpoints` — cheap and always present, or
  `GET /v1/drop-filters`). There is no dedicated `/validate` endpoint, so a real list is the
  auth check. A 200 confirms the token + region; a 401 is a bad token; a 403 on a
  feature-gated endpoint is NOT an auth failure (pick an always-available endpoint for the
  probe).
- **Connect**: no real resolution — the token *is* the account. Validate the lightweight list
  succeeds and set the single flat container (id/name best-effort per Scope above; fall back
  to the base-URL host).

## Enumeration-shape + import-id determination — the CRITICAL classification

This is Logz.io's analogue of Datadog's "which API version / response shape / pager" call.
Two per-resource facts are load-bearing; get either wrong and you either fail to list or fail
to import:

1. **List mechanism: GET-bare-list vs POST-search vs singleton.** There is **no single "list"
   convention.** Hitting a `POST …/search` endpoint with a `GET` returns 405/404; hitting a
   GET-list with a body is wasteful but usually harmless. The catalog pins each. The
   defaults: `/v1/*` config objects are **GET bare-lists** (one call, no page body); **alerts**
   and **log-shipping tokens** are **POST-search** (need a body + pager); **auth groups** and
   **grafana notification policy** are **singletons** (one GET, no list). VERIFY the alerts
   list on build — both `GET /v2/alerts` (list-all) and `POST /v2/alerts/search` (paged) are
   documented; prefer the paged search if the account can have many alerts.
2. **Import ID form: BARE NUMERIC id is the norm; grafana_* are UID exceptions.** Almost
   every native Logz.io resource imports by a **bare numeric id** off the list response
   (alert id, endpoint id, sub-account id, user id, log-shipping-token id, s3-fetcher id,
   archive id, metrics-account id — all `int`, **stringify** before use). The **exceptions**:
   - **`logzio_drop_filter`** — the id may be a **string** (a filter hash), not a bare int —
     VERIFY per registry; import by that id verbatim.
   - **`logzio_grafana_folder` / `_dashboard` / `_contact_point` / `_alert_rule`** — import by
     the Grafana **UID string** (`{{uid}}`), not a numeric id (same model as our `grafana`
     provider). These are the UID exceptions.
   - **`logzio_authentication_groups`** and **`logzio_grafana_notification_policy`** are
     **singletons** — import by a fixed/whole-set token, not a per-object id (VERIFY the exact
     import string the provider expects; typically a constant).
   - **`logzio_kibana_data_view`** imports by the data-view **id** (a string id — VERIFY).

Unlike Fastly (`<service_id>/<sub_id>` slashes) and the identity providers (`{{orgID}}:{{uid}}`
composites), **nothing here is a composite** — every import is a *single bare token* (numeric
id, string id, or UID). That is the good news; the list-mechanism classification (#1) is the
harder part.

## Enumeration spine

Flat account scope; **no parent fan-out** (contrast Fastly's per-service version→dictionary/
acl/snippet loop). Every resource is a single best-effort account-level list (Verbose +
continue on 403/404), each tagged with its list mechanism + shape per the catalog. Grouped
by mechanism:

- **GET bare-list** (one call, no body):
  - `GET /v1/endpoints` → `logzio_endpoint` (id `int`)
  - `GET /v1/drop-filters` → `logzio_drop_filter` (id — VERIFY numeric vs string)
  - `GET /v1/account-management/time-based-accounts` → `logzio_sub_account` (id `int`) — VERIFY
    the exact sub-accounts path (time-based vs flexible)
  - `GET /v1/user-management/users` → `logzio_user` (id `int`) — VERIFY the list sub-path
    (`/v1/user-management` vs `…/users`)
  - `GET /v1/s3-fetcher` → `logzio_s3_fetcher` (id `int`) — enumerate for visibility; AWS keys
    are write-only → scrub at export (below)
  - `GET /v1/archive/settings` → `logzio_archive_logs` (id `int`) — storage creds write-only → scrub
  - `GET /v1/account-management/metrics-accounts` → `logzio_metrics_account` (id `int`) — VERIFY
    path; sharing token write-only → scrub
- **POST …/search** (need a JSON body + pager):
  - `POST /v2/alerts/search` → `logzio_alert_v2` (id `int`) — or `GET /v2/alerts` list-all (VERIFY)
  - `POST /v1/log-shipping/tokens/retrieve` → `logzio_log_shipping_token` (id `int`) —
    enumerate; the token *value* is write-only → scrub
- **Singleton** (one GET, whole-set):
  - `GET /v1/authentication-groups` → `logzio_authentication_groups` (one resource = the set)
  - `GET /v1/grafana/api/v1/provisioning/policies` → `logzio_grafana_notification_policy` (one tree)
- **grafana_* provisioning plane** (Grafana HTTP API proxied under `/v1/grafana/api/...`;
  **DEFER — see recommendation below**):
  - `GET /v1/grafana/api/folders` → `logzio_grafana_folder` (id `uid`)
  - `GET /v1/grafana/api/search?type=dash-db` → `logzio_grafana_dashboard` (id `uid`)
  - `GET /v1/grafana/api/v1/provisioning/contact-points` → `logzio_grafana_contact_point`
    (id `uid`) — settings carry secrets → scrub
  - `GET /v1/grafana/api/v1/provisioning/alert-rules` → `logzio_grafana_alert_rule` (id `uid`)
- **Kibana data views** (VERIFY endpoint): `logzio_kibana_data_view` (id string).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as `enumerate.go`).

## Resource catalog

Import IDs to be verified against the current `logzio/logzio` registry docs
(`docs/resources/*.md`). All scope = account. "list" = the enumeration mechanism; "body?" =
whether the list call needs a request body.

| native key | TF type | list endpoint | list | body? | id field | import ID |
|---|---|---|---|---|---|---|
| logzio:alert_v2 | logzio_alert_v2 | `POST /v2/alerts/search` (or `GET /v2/alerts`) | POST-search | yes | `alertId` (int) | `<alert_id>` |
| logzio:endpoint | logzio_endpoint | `GET /v1/endpoints` | GET-list | no | `id` (int) | `<endpoint_id>` |
| logzio:drop_filter | logzio_drop_filter | `GET /v1/drop-filters` | GET-list | no | `id` (str/int — VERIFY) | `<drop_filter_id>` |
| logzio:sub_account | logzio_sub_account | `GET /v1/account-management/time-based-accounts` | GET-list | no | `id`/`accountId` (int) | `<sub_account_id>` |
| logzio:user | logzio_user | `GET /v1/user-management/users` (VERIFY sub-path) | GET-list | no | `id` (int) | `<user_id>` |
| logzio:log_shipping_token | logzio_log_shipping_token | `POST /v1/log-shipping/tokens/retrieve` | POST-search | yes | `id` (int) | `<token_id>` |
| logzio:s3_fetcher | logzio_s3_fetcher | `GET /v1/s3-fetcher` | GET-list | no | `id` (int) | `<s3_fetcher_id>` |
| logzio:archive_logs | logzio_archive_logs | `GET /v1/archive/settings` | GET-list | no | `id` (int) | `<archive_id>` |
| logzio:metrics_account | logzio_metrics_account | `GET /v1/account-management/metrics-accounts` (VERIFY) | GET-list | no | `id` (int) | `<metrics_account_id>` |
| logzio:authentication_groups | logzio_authentication_groups | `GET /v1/authentication-groups` | singleton | no | — (whole set) | `<fixed token>` **(singleton — VERIFY)** |
| logzio:kibana_data_view | logzio_kibana_data_view | Kibana data-views API (VERIFY) | GET-list | no | `id` (string) | `<data_view_id>` |
| logzio:grafana_folder | logzio_grafana_folder | `GET /v1/grafana/api/folders` | GET-list | no | `uid` (string) | `<folder_uid>` **(UID)** |
| logzio:grafana_dashboard | logzio_grafana_dashboard | `GET /v1/grafana/api/search?type=dash-db` | GET-list | no | `uid` (string) | `<dashboard_uid>` **(UID)** |
| logzio:grafana_contact_point | logzio_grafana_contact_point | `GET /v1/grafana/api/v1/provisioning/contact-points` | GET-list | no | `uid` (string) | `<contact_point_uid>` **(UID)** |
| logzio:grafana_alert_rule | logzio_grafana_alert_rule | `GET /v1/grafana/api/v1/provisioning/alert-rules` | GET-list | no | `uid` (string) | `<alert_rule_uid>` **(UID)** |
| logzio:grafana_notification_policy | logzio_grafana_notification_policy | `GET /v1/grafana/api/v1/provisioning/policies` | singleton | no | — (one tree) | `<fixed token>` **(singleton — VERIFY)** |

### Import-format quirks (§ do not get wrong)
1. **Everything imports by a BARE token — no composites.** Unlike Fastly's
   `<service_id>/<sub_id>` slashes or the identity providers' `{{orgID}}:{{uid}}`, every
   Logz.io resource imports by a single bare token. The variety is in *what* the token is
   (numeric id / string id / grafana UID / singleton constant), not in joining.
2. **Bare numeric id is the NORM.** Alert, endpoint, sub-account, user, log-shipping-token,
   s3-fetcher, archive, metrics-account all import by a bare numeric id that comes off the
   wire as a JSON number — **stringify** before use.
3. **`logzio_grafana_*` import by the Grafana UID** (`{{uid}}` string), not a numeric id —
   the four UID exceptions (folder, dashboard, contact_point, alert_rule). Same model as the
   standalone `grafana` provider (§ its spec). The **notification policy** is a UID-less
   **singleton**.
4. **`logzio_authentication_groups` and `logzio_grafana_notification_policy` are singletons**
   — one object per account managed as a whole; the import id is a fixed provider constant,
   not a per-object id. VERIFY the exact import string against the registry.
5. **`logzio_drop_filter` id may be a STRING** (a filter hash), not a bare int — VERIFY and
   emit it verbatim.
6. **`logzio_alert` (Terraformer's) vs `logzio_alert_v2`** — emit the **v2** type; both import
   by the bare alert id, but the schemas differ (v2 is the current API).

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a
live account — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the
Fastly/Datadog providers.

- **`logzio_alert_v2` — the beachhead surface.** `title`/`search_timeframe_minutes`/
  `sub_components` (the query + threshold tiers) + `notification_emails` +
  `alert_notification_endpoints` (refs `logzio_endpoint` ids). Prune computed `alert_id`/
  `created_at`/`created_by`/`last_updated`. `severity_threshold_tiers`/`sub_components` are
  nested blocks — tolerate ordering churn. Query strings may contain `${...}`/Lucene syntax →
  keep literal (HCL-template-escape on import, `util.EscapeHCLTemplate`, like the other
  providers). Medium weight.
- **`logzio_endpoint` — the credential-bearing config resource.** One resource per notification
  target; the *type* selects a nested block (`slack`, `custom` [webhook], `pagerduty`,
  `bigpanda`, `datadog`, `victorops`, `opsgenie`, `servicenow`, `microsoft_teams`). **Most of
  these carry a secret in a Required field the API does NOT return on read** — Slack/webhook
  `url` (embeds the secret token), custom `headers` (auth), PagerDuty `service_key`, Datadog
  `api_key`, VictorOps `routing_key`/`api_key`, OpsGenie/BigPanda api keys. generate-config-out
  will null these → **not plan-clean unless re-supplied out-of-band**. Adopt the endpoint
  *shell* (title/type/non-secret fields) and flag the credential as a manual re-supply (same
  class as Fastly `logging_*` secrets / Datadog synthetics secure vars). Scrub any secret that
  *does* leak into generated HCL (repo-wide secret scan is the backstop).
- **`logzio_drop_filter`.** `log_type` + `field_conditions` (field/value pairs) + `active`.
  Light; no secrets. Prune computed `id`. Note: drop filters are cheap to recreate but the id
  form (string vs int) affects import — VERIFY.
- **`logzio_sub_account`.** `email`/`account_name`/`max_daily_gb`/`retention_days`/
  `sharing_objects_accounts`/`utilization_settings`. **The sub-account "sharing token"
  (`sharing_objects_accounts` account tokens) is write-only** → scrub. Prune computed
  `account_id`/`account_token` (the account token returned at creation is sensitive). Adopt
  the shell; re-supply tokens out-of-band. Caution: your own account may appear.
- **`logzio_user`.** `username`/`fullname`/`role`/`account_id`. **No password attribute**
  (invites/resets are out-of-band) — no secret. Prune computed `id`/`active`. Caution: the
  user behind `LOGZIO_API_TOKEN` may appear — adopting it is allowed but flag it (don't lock
  yourself out; Fastly account-owner note).
- **`logzio_log_shipping_token`.** `name` + `enabled` — but the **`token` value is write-only**
  (the actual shipping secret) → scrub/exclude the value. Prune computed `id`/`token`/
  `created_at`/`updated_by`. Adopt the shell; the token value can't be reproduced (a fresh
  token is minted on create) — flag re-supply.
- **`logzio_s3_fetcher`.** `aws_access_key`/`aws_secret_key` (or an `aws_arn` role) +
  `bucket_name`/`region`/`logs_type`/`active`. **`aws_secret_key` is write-only** → scrub.
  Adopt the shell; re-supply the AWS secret (or prefer the IAM-role/`aws_arn` variant, which
  has no inline secret). Prune computed `id`.
- **`logzio_archive_logs`.** `storage_type` (S3/Blob) + the storage-target settings; the
  **credentials block carries write-only secrets** (S3 secret key / Blob account key / SAS) →
  scrub. Prune computed `id`. Adopt the shell; re-supply storage creds.
- **`logzio_metrics_account`.** `email`/`account_name`/`plan_uts`/`authorized_accounts`. The
  **metrics account sharing token** is write-only → scrub. Prune computed `id`/`account_token`.
- **`logzio_authentication_groups` (singleton).** Manages the whole SAML auth-group set as one
  resource (`authentication_group` blocks: `group` + `user_role`). No secret. Whole-set
  semantics: importing adopts the entire set — expect over-emit; author as one block list.
- **`logzio_kibana_data_view`.** `name`/`time_field_name`/`title` (index pattern). Light; no
  secret. VERIFY the API/import form.

## Write-only / secret resources (EXCLUDE / scrub)

Logz.io's secret material is spread across the config resources as write-only *fields* (the
API returns them null/masked on read) rather than as standalone credential resources (no
Fastly-`tls_private_key`-style pure-secret resource here). Handle them as **scrub-and-flag
re-supply**, not whole-resource exclusion — the surrounding config is worth adopting:

- **`logzio_endpoint` credentials** — Slack/webhook `url` (embedded token), custom `headers`,
  PagerDuty `service_key`, Datadog `api_key`, VictorOps `routing_key`/`api_key`,
  OpsGenie/BigPanda api keys. Required + write-only → scrub; flag re-supply. This is the #1
  secret surface (every alert points at one of these).
- **`logzio_log_shipping_token.token`** — the shipping token *value* (write-only, minted on
  create) → scrub.
- **`logzio_sub_account` / `logzio_metrics_account` sharing tokens** (`account_token` /
  `sharing_objects_accounts` tokens) — write-only → scrub.
- **`logzio_s3_fetcher.aws_secret_key`** and **`logzio_archive_logs`** storage credentials
  (S3 secret key / Azure Blob account key / SAS token) — write-only → scrub; prefer the
  IAM-role variant where available.
- **`logzio_grafana_contact_point` settings** — Slack/PagerDuty/etc. tokens inside the
  contact-point `settings` are secret (same as the standalone grafana provider's contact
  points) → scrub if the grafana_* plane is ever included.

There is **no purely-write-only resource to fully exclude** (unlike Fastly's
`tls_private_key`); the whole account's secrets are field-level. The repo-wide secret scan is
the backstop for anything that leaks past the field-level scrub.

## Deliberately out of scope
- **grafana_* embedded-Grafana plane — DEFER (recommendation).** Logz.io wraps an **embedded
  Grafana** and re-exposes Grafana's provisioning HTTP API under `/v1/grafana/api/...`. The
  five `logzio_grafana_*` resources (`_folder`, `_dashboard`, `_contact_point`, `_alert_rule`,
  `_notification_policy`) **mirror our standalone `grafana` provider** (§ `docs/v2-specs/
  grafana.md`) almost exactly: UID imports, the same provisioning endpoints, the same
  contact-point secret scrubbing, the same recursive dashboard/notification-policy trees.
  **Recommend deferring them to a later increment** (`INC-3`) and porting the grafana
  provider's curation logic (UID-import handling, dashboard `${}`/computed pruning, contact-
  point secret scrub) rather than re-deriving it. They are genuine Logz.io resources — not
  out of scope forever — but they add a whole second API shape (Grafana provisioning) + the
  UID-import exception, so they do not belong in the native-config beachhead. Include them in
  the catalog (above) flagged deferred.
- **Sub-account fan-out / per-sub-account tokens** — the account token sees only its own
  account. Enumerating *inside* each `logzio_sub_account` would need a separate token per
  sub-account (`Capabilities.Hierarchy` is false). Out of scope; we adopt the sub-account
  *objects*, not their contents.
- **Data / observability planes** — log events, metrics data points, search/query results,
  Kibana saved objects beyond data views, Explore/Insights, service-level dashboards' *data*.
  The DATA behind the config, per scope.
- **Restore jobs** (`logzio_restore_logs` — a transient archive-restore operation, not durable
  config) — later increment at best; it depends on `logzio_archive_logs`.
- **Cloud-IAM depth** (`Capabilities.IAM=false`): `logzio_user` + `logzio_authentication_groups`
  are modeled at breadth (no secrets / SAML group set), but deeper SSO/SCIM/role management is
  not.
- **Legacy `logzio_alert`** — deprecated in favour of `logzio_alert_v2` (covered). Do not adopt
  both for the same alert.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `logzio_alert_v2` + `logzio_endpoint` (what essentially every Logz.io customer
manages as IaC — log alerts and the notification targets they fire at; these are also the two
Terraformer already covered, so the shapes are well-understood. `logzio_endpoint` carries the
credential-scrub/re-supply pattern the rest of the provider reuses; `logzio_alert_v2` exercises
the `POST /v2/alerts/search` pager and the query-string template-escaping) → INC-1
`logzio_drop_filter` + `logzio_sub_account` + `logzio_user` (the rest of the GET-bare-list
native-config core — light, account-management objects; sub-account/user add the sharing-token
scrub + self-adoption caution) → INC-2 `logzio_log_shipping_token` + `logzio_s3_fetcher` +
`logzio_archive_logs` + `logzio_metrics_account` (the ingestion / retention plane — exercises
the second `POST …/retrieve` pager and the AWS/storage/token write-only scrubs) → INC-3
(grafana_* plane, ported from the `grafana` provider) `logzio_grafana_folder` +
`logzio_grafana_dashboard` + `logzio_grafana_contact_point` + `logzio_grafana_alert_rule` +
`logzio_grafana_notification_policy` (adds the Grafana provisioning-API shape + UID imports +
the notification-policy singleton) → INC-4 `logzio_authentication_groups` +
`logzio_kibana_data_view` (the singleton auth-group set + Kibana data views) → LATER/OUT
`logzio_restore_logs`, per-sub-account fan-out, deeper SSO/SCIM, and the data planes.
