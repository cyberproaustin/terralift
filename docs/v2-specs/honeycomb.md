# Honeycomb provider — build spec

Research artifact for the `honeycomb` provider (Phase A scaffold; TF provider source is
`honeycombio/honeycombio`, product "Honeycomb"). Sources: Terraformer's
`providers/honeycombio/` (a real generator exists — v0.0.2, built on the
`honeycombio/terraform-provider-honeycombio/client` Go SDK), the `honeycombio/honeycombio`
registry docs (import formats + schema, **verified per-resource below** against the
provider repo's `docs/resources/*.md` on `main`), and the Honeycomb REST API
(`https://api.honeycomb.io`, the `/1/…` config plane). Build mirrors the **Fastly**
provider (`internal/providers/fastly/`) and **Datadog** (`internal/providers/datadog/`) —
a flat, single-header, single-container REST provider — with one dominant structural
wrinkle borrowed straight from Fastly: Honeycomb is **dataset-centric**, so most resources
are enumerated via a **per-dataset fan-out** (parent `GET /1/datasets` → per-dataset
sub-lists), exactly like Fastly's per-service fan-out. This is **REST, Datadog/Fastly-style,
NOT GraphQL.** The `fastlyapi.go` bare-array list helper generalises almost unchanged
(Honeycomb's v1 plane is bare JSON arrays with **no pagination**); the composite import id
(`<dataset>/<id>`) is the Fastly `<service_id>/<sub_id>` pattern.

## Version pin (load-bearing)

Pin `honeycombio/honeycombio ~> 0.x` — this provider is **pre-1.0** (current is the 0.3x
line); there is no stable major, so expect schema churn and pin a concrete minor at build.
Naming facts that matter (the Terraformer-vs-current divergences — the Honeycomb analogue
of Datadog's `datadog_downtime` → `_downtime_schedule` and Fastly's `_v1` aliases):

- **Boards: Terraformer emits the legacy `honeycombio_board` (classic boards). The current
  provider has removed the classic-board docs from `main`** — Honeycomb deprecated classic
  boards in favour of **flexible boards**. The current board resources are
  **`honeycombio_flexible_board`** (the board itself; import a bare id) and
  **`honeycombio_board_view`** (a panel/view *within* a flexible board). **Emit
  `honeycombio_flexible_board`** for boards — do **not** copy Terraformer's
  `honeycombio_board`. VERIFY at build whether classic `honeycombio_board` still exists as a
  deprecated resource and whether `GET /1/boards` tags board type (classic vs flexible) so
  the enumerator maps correctly.
- **Recipients: there is no generic `honeycombio_recipient` resource anymore.** It was split
  into **typed** resources: `honeycombio_email_recipient`, `honeycombio_pagerduty_recipient`,
  `honeycombio_slack_recipient`, `honeycombio_webhook_recipient`,
  `honeycombio_msteams_recipient`, `honeycombio_msteams_workflow_recipient`. `GET /1/recipients`
  returns all of them with a `type` discriminator → map `type` to the typed TF resource.
  Terraformer covers **no** recipients at all.
- **Terraformer's coverage is a strict subset.** Its v0.0.2 generator covers `dataset`,
  `column`, `derived_column`, `query`, `query_annotation`, `board` (classic), `trigger`,
  `slo`, `burn_alert`. It does **not** cover `dataset_definition`, `marker`,
  `marker_setting`, any recipient, `flexible_board`/`board_view`, or the v2 management plane
  (`environment`, `api_key`). Those are covered here from the registry + API directly (as we
  did for the Datadog resources Terraformer lacked).
- Terraformer reads `HONEYCOMB_API_KEY` + `HONEYCOMB_API_URL`; the **TF provider** reads
  `HONEYCOMB_API_KEY` (config `api_key`) + `HONEYCOMB_API_ENDPOINT` (config `api_url`), plus
  the v2 management pair `HONEYCOMB_KEY_ID`/`HONEYCOMB_KEY_SECRET`. The REST endpoints below
  are provider-version-independent.

## Shape

- **Auth — the `X-Honeycomb-Team` header (the one hard divergence from `fastlyapi.go`'s
  `Fastly-Key`).** Every request carries **`X-Honeycomb-Team: <api key>`** (plus
  `Accept: application/json`). Read the key from `HONEYCOMB_API_KEY`. A direct `net/http`
  client to `https://api.honeycomb.io` (mirror `fastlyapi.go`); **no Honeycomb CLI**, and do
  **not** pull the `honeycombio` Go SDK the way Terraformer did — a raw client is smaller and
  matches the Fastly/Datadog providers. The key rides **only** on the `X-Honeycomb-Team`
  header, **never** in errors/logs (same discipline as `Fastly-Key`/`DD-API-KEY`).
  - **Two key generations (the config layer is v1).** Honeycomb has **v1 configuration keys**
    (env `HONEYCOMB_API_KEY`; the main key for dataset/column/board/trigger/SLO/burn-alert/
    marker/recipient config — the entire `/1/…` plane below) and **v2 management keys** (env
    `HONEYCOMB_KEY_ID` + `HONEYCOMB_KEY_SECRET`; a Key-ID/Key-Secret pair for the `/2/…`
    environment/api-key/team **management** plane, with a *different* auth mechanism — not
    `X-Honeycomb-Team`). **Base the scaffold's client on the v1 configuration key on
    `X-Honeycomb-Team`.** The v2 management plane (environment/api_key) is a later increment
    with its own client; see build order.
- **Base URL — US vs EU, must be read from env.** Default `https://api.honeycomb.io` (US);
  EU is `https://api.eu1.honeycomb.io`. Read `HONEYCOMB_API_ENDPOINT` (fallback
  `HONEYCOMB_API_HOST`; Terraformer used `HONEYCOMB_API_URL` — accept it too) as the full API
  base URL, default US. Store the resolved base once and host-validate it (mirror
  `isFastlyURL` → `isHoneycombURL`) before sending the key; the redirect-refusing client
  still applies even though the v1 plane is unpaginated (defence in depth).
- **Scope — one flat container = the Honeycomb environment/team.** A configuration key is
  scoped to exactly **one** environment (a "team" in Classic). There is no sub-account and no
  multi-org resolution: the key **is** the environment. `model.ScopeTenant`. Resolve the
  container best-effort from **`GET /1/auth`**, which returns the key's `team` (`name`/`slug`)
  and `environment` (`name`/`slug`) plus its `api_key_access` scope map — use
  `environment.slug` as the container id (fall back to `team.slug`, then the host string).
  This is the Honeycomb analogue of Fastly's `/current_customer` — the key simply *is* the
  environment, so Connect just validates the call succeeds.
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response family — ONE shape (simpler than Fastly/Datadog): bare JSON arrays, no
  pagination.** The `/1/…` config plane returns raw `[...]` arrays (no envelope, no wrapping
  key, no `data:` — contrast Fastly's JSON:API plane and Datadog's three shapes). Singletons
  (`/1/auth`, and `/1/dataset_definitions/<dataset>`) return a bare object. **v1 list
  endpoints are unpaginated** — each returns the full set in one call — so the
  `fastlyListPaged`/JSON:API pagers are **not** needed; the `fastlyGet[T]` bare-array helper
  and `fastlyGetOne[T]` singleton helper are the whole client surface. (The v2 management
  plane may paginate — handle that in the v2 increment.)
- **Status handling (mirror `fastlyAPIError`/`list`).** Honeycomb errors are
  `{"error":"…"}`. 401 → key invalid/expired (fatal, surfaced in preflight; if it appears
  mid-run every remaining list will fail too → treat as fatal, not a partial inventory). 403
  → the key lacks the relevant scope bit (e.g. `boards`/`triggers`/`columns` in
  `api_key_access`) → best-effort skip at Verbose. 404 → dataset/resource absent → Verbose
  skip. 429 (Honeycomb rate-limits; honour `Retry-After`) / 5xx / network → enumeration may be
  silently incomplete → Warn. Key never in errors/logs.
- **Preflight**: `terraform` present + `HONEYCOMB_API_KEY` set + `GET /1/auth` returns 200
  with a `team`/`environment` body. `/1/auth` also hands back the `api_key_access` scope map
  for free — log which scopes are missing so the 403 skips below are explained rather than
  surprising.
- **Connect**: `GET /1/auth` → `environment.slug` (fallback `team.slug`) is the single flat
  container. No multi-account resolution — validate the call succeeds and set the container.

## Dataset scope + fan-out + the composite-vs-bare import — the CRITICAL determination

This is Honeycomb's analogue of Fastly's "service-centric" call and Datadog's "which API
version/shape" call: the load-bearing per-resource facts are **(a) is this resource
dataset-scoped or team/environment-wide**, and **(b) does its import id carry the `<dataset>/`
prefix or is it bare**. Get (a) wrong and you fan out to the wrong endpoint (or miss the
`__all__` env-wide objects); get (b) wrong and every import block for that type is
un-importable. The rules:

- **Dataset-scoped (the majority) → enumerated PER dataset via fan-out, import id
  `<dataset>/<id-or-name>`.** columns, derived columns (dataset variant), triggers, SLOs
  (single-dataset variant), burn alerts (single-dataset variant), markers, marker settings,
  query annotations, queries, dataset definitions. First `GET /1/datasets` (the parent list),
  then for each dataset the per-dataset sub-list. This is exactly Fastly's per-service
  fan-out.
- **Team/environment-wide → a single flat list, import id is BARE (no dataset prefix).**
  boards (`GET /1/boards`), recipients (`GET /1/recipients`), and the v2 management plane
  (environments, api keys). Datasets themselves (`GET /1/datasets`) are the parent anchor,
  imported by their bare slug.
- **The `__all__` environment-wide variant — the subtle trap.** In a modern (non-Classic)
  environment, some normally-dataset-scoped resources can be **environment-wide**: derived
  columns, and multi-dataset (MD) triggers/SLOs/burn alerts. In the API these live under the
  pseudo-dataset slug **`__all__`** (Terraformer models it as an extra dataset named
  `__all__`). **But their Terraform import id is BARE — the `__all__/` prefix is dropped.**
  So the composite is conditional: `<dataset>/<id>` for a real dataset, **`<id>` alone** when
  the resource is environment-wide. This is verified per-resource below (derived_column,
  trigger, slo, burn_alert all show the two forms) and is the #1 hazard alongside the prefix
  itself.
- **Classic vs modern environments.** Classic environments have **no** `__all__` env-wide
  scope (and no v2 environment concept). Terraformer detects Classic by **API key length ==
  32 chars**; reproduce that heuristic (or read it off `/1/auth`) and, for Classic, skip the
  `__all__` fan-out entirely.
- **id-vs-name choice.** Most composites take the opaque **id** (`<dataset>/<id>`), but
  **columns import by `<dataset>/<key_name>`** (the human column name, e.g.
  `my-dataset/duration_ms`) and **derived columns import by `<dataset>/<alias>`**. The id form
  is also accepted for both, but the enumerator should emit the name/alias form to match the
  docs' canonical example. This is the id-vs-name thing we cannot get wrong.

## Enumeration spine

Flat environment scope. The fan-out is: **`GET /1/datasets` (parent) → per-dataset sub-lists**
(the Fastly per-service pattern), plus a handful of team/environment-wide flat lists.
Best-effort per list (403 scope-absent / 404 → Verbose skip; other errors → Warn + count,
so a systemic failure is told apart from an empty environment). Add `__all__` to the dataset
loop for non-Classic keys so env-wide derived columns / MD triggers / MD SLOs are captured.

- **Parent:** `GET /1/datasets` → bare array of `{name, slug, …}`. Each `slug` becomes a
  `honeycombio_dataset`, and the slug is the fan-out key for every sub-list below.
- **Per dataset `<ds>` (fan-out):**
  - `GET /1/columns/<ds>` → `honeycombio_column` (id `key_name`).
  - `GET /1/derived_columns/<ds>` → `honeycombio_derived_column` (id `alias`). Also
    `GET /1/derived_columns/__all__` for env-wide ones (non-Classic).
  - `GET /1/dataset_definitions/<ds>` → the definition **singleton** object.
    **Enumerate for visibility but it CANNOT be imported (see below) → surface, don't adopt.**
  - `GET /1/query_annotations/<ds>` → `honeycombio_query_annotation` (carries `query_id`).
  - `GET /1/triggers/<ds>` → `honeycombio_trigger` (id `id`; carries `query_id`, `recipients`).
  - `GET /1/slos/<ds>` → `honeycombio_slo` (id `id`); then per SLO
    `GET /1/burn_alerts/<ds>?slo_id=<slo_id>` → `honeycombio_burn_alert` (a second-level
    fan-out, exactly like Fastly's per-service-version sub-lists).
  - `GET /1/markers/<ds>` → `honeycombio_marker` (**enumerate for visibility; no import — see
    below**), `GET /1/marker_settings/<ds>` → `honeycombio_marker_setting` (**no import**).
- **Team/environment-wide flat lists:**
  - `GET /1/boards` → `honeycombio_flexible_board` (bare id). Map board type at build.
  - `GET /1/recipients` → typed recipient resources by `type` (email/pagerduty/slack/webhook/
    msteams). Scrub secrets (see EXCLUDE).
- **Queries — special (no list endpoint).** There is **no** `list` for queries: the API only
  exposes `GET /1/queries/<ds>/<id>` (by id) and create. Queries are discovered **only** by
  walking the `query_id` fields on the enumerated **boards** and **triggers** (exactly what
  Terraformer does) → emit `honeycombio_query` with `dataset` + the referenced `query_id`.
  Queries are **immutable and cannot be deleted** (create/read only). See gotchas.
- **v2 management plane (later increment, separate client + management key):**
  `GET /2/teams/<team_slug>/environments` → `honeycombio_environment`;
  `GET /2/teams/<team_slug>/api-keys` → `honeycombio_api_key` (**EXCLUDE — write-only secret**).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as `enumerate.go`).

## Resource catalog

Import IDs verified against the current `honeycombio/honeycombio` provider repo
(`docs/resources/*.md` on `main`, fetched per-resource). "scope" = dataset-scoped (fan-out)
vs env/team-wide vs v2-management. All response bodies are **bare JSON arrays** (singletons
noted). "cfg" endpoints are v1 config plane (`X-Honeycomb-Team`); "mgmt" are v2.

| native key | TF type | endpoint | scope | id field | import ID |
|---|---|---|---|---|---|
| honeycomb:dataset | honeycombio_dataset | `GET /1/datasets` | env-wide (parent) | `slug` | `<slug>` **(bare)** |
| honeycomb:dataset_definition | honeycombio_dataset_definition | `GET /1/dataset_definitions/<ds>` (singleton obj) | dataset | — | **CANNOT be imported — surface only** |
| honeycomb:column | honeycombio_column | `GET /1/columns/<ds>` | dataset | `key_name` (or `id`) | `<ds>/<key_name>` **(composite; NAME)** |
| honeycomb:derived_column | honeycombio_derived_column | `GET /1/derived_columns/<ds>` (+ `/__all__`) | dataset **or** env-wide | `alias` (or `id`) | `<ds>/<alias>` **/ `<alias>` bare if env-wide** |
| honeycomb:query | honeycombio_query | *no list* — via boards/triggers `query_id` | dataset | `id` | `<ds>/<query_id>` **(composite)** |
| honeycomb:query_annotation | honeycombio_query_annotation | `GET /1/query_annotations/<ds>` | dataset | `id` | `<ds>/<annotation_id>` **(composite)** |
| honeycomb:board | honeycombio_flexible_board | `GET /1/boards` | env-wide | `id` | `<board_id>` **(bare)** |
| honeycomb:board_view | honeycombio_board_view | (panel within a flexible board) | env-wide | `id` | **no documented import — defer** |
| honeycomb:trigger | honeycombio_trigger | `GET /1/triggers/<ds>` | dataset **or** env-wide | `id` | `<ds>/<trigger_id>` **/ `<trigger_id>` bare if env-wide** |
| honeycomb:slo | honeycombio_slo | `GET /1/slos/<ds>` | dataset **or** env-wide (MD) | `id` | `<ds>/<slo_id>` **/ `<slo_id>` bare if MD** |
| honeycomb:burn_alert | honeycombio_burn_alert | `GET /1/burn_alerts/<ds>?slo_id=<id>` | dataset **or** env-wide (MD) | `id` | `<ds>/<id>` **/ `<id>` bare if MD** |
| honeycomb:marker | honeycombio_marker | `GET /1/markers/<ds>` | dataset | `id` | **no documented import — surface only** |
| honeycomb:marker_setting | honeycombio_marker_setting | `GET /1/marker_settings/<ds>` | dataset | `id` | **no documented import — surface only** |
| honeycomb:email_recipient | honeycombio_email_recipient | `GET /1/recipients` (`type==email`) | env-wide | `id` | `<recipient_id>` **(bare)** |
| honeycomb:pagerduty_recipient | honeycombio_pagerduty_recipient | `GET /1/recipients` (`type==pagerduty`) | env-wide | `id` | `<recipient_id>` **(bare; SECRET — scrub)** |
| honeycomb:slack_recipient | honeycombio_slack_recipient | `GET /1/recipients` (`type==slack`) | env-wide | `id` | `<recipient_id>` **(bare)** |
| honeycomb:webhook_recipient | honeycombio_webhook_recipient | `GET /1/recipients` (`type==webhook`) | env-wide | `id` | `<recipient_id>` **(bare; SECRET — scrub)** |
| honeycomb:msteams_recipient | honeycombio_msteams_recipient | `GET /1/recipients` (`type==msteams`) | env-wide | `id` | `<recipient_id>` **(bare; URL secret — scrub)** |
| honeycomb:environment | honeycombio_environment | `GET /2/teams/<team>/environments` | v2-mgmt | `id` (`hcaen_…`) | `<environment_id>` **(bare; mgmt key)** |
| honeycomb:api_key | honeycombio_api_key | `GET /2/teams/<team>/api-keys` | v2-mgmt | `id` | **CANNOT be imported; write-only secret — EXCLUDE** |

### Import-format quirks (§ do not get wrong)
1. **Dataset-scoped resources use a `/`-slash composite `<dataset>/<id>`** — the Fastly
   `<service_id>/<sub_id>` pattern, `<dataset>` first. Applies to column, derived_column
   (dataset variant), query, query_annotation, trigger (dataset), slo (single-dataset),
   burn_alert (single-dataset). Verified examples: `my-dataset/duration_ms` (column),
   `my-dataset/any_error` (derived_column), `my-dataset/bj9BwOb1uKz` (trigger/slo/burn_alert),
   `my-dataset/bj8BwOa1uRz` (query), `my-dataset/JL0Xp8SH0Dg` (query_annotation).
2. **The `__all__`/environment-wide variants import BARE — the `<dataset>/` prefix is
   DROPPED.** derived_column env-wide → `<alias>`; MD trigger → `<trigger_id>`; MD slo →
   `<slo_id>`; MD burn_alert → `<id>`. Decide per resource whether it is dataset-scoped or
   env-wide (the API dataset is `__all__`) and emit the composite **only** for real datasets.
   This conditional is the single most error-prone thing in the provider.
3. **columns import by `<dataset>/<key_name>` and derived_columns by `<dataset>/<alias>`** —
   the human name/alias, not the opaque id (the id form is also accepted). Emit the name/alias.
4. **Team/environment-wide resources import BARE**: `honeycombio_dataset` (`<slug>`),
   `honeycombio_flexible_board` (`<board_id>`, e.g. `AobW9oAZX71`), all typed recipients
   (`<recipient_id>`, e.g. `nx2zsegA0dZ`), `honeycombio_environment` (`<env_id>`,
   e.g. `hcaen_01j1…`). No parent prefix.
5. **Not importable at all → cannot be adopted by an import-based tool** (surface as honest
   gaps, adopt/author out-of-band): `honeycombio_dataset_definition` ("Dataset Definitions
   cannot be imported"), `honeycombio_api_key` ("API Keys cannot be imported"),
   `honeycombio_marker` and `honeycombio_marker_setting` (no import section in the docs),
   `honeycombio_board_view` (no documented import). Enumerate markers/definitions for
   visibility; do not emit import blocks for them.
6. **Terraformer's names are stale** — it emits classic `honeycombio_board` (use
   `honeycombio_flexible_board`) and has no recipient types. Emit the current names.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a
live environment — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like
the Fastly/Datadog providers. Honeycomb has **no single monster resource** (contrast Fastly's
`fastly_service_vcl`); the weight is spread and the recurring hazard is the **`query_json`
heredoc** and the **recipient/notification secrets**.

- **`honeycombio_query` — the awkward one.** A query is a `query_json` blob (a JSON
  QuerySpec). Terraformer emits it as a raw string and had a (commented-out) hook to reformat
  it as a `<<EOH` heredoc; generate-config-out will emit an escaped one-line JSON string —
  verify it stays valid and template-safe. Because queries are **immutable and never
  deleted**, and are usually *referenced by* boards/triggers rather than managed standalone,
  treat standalone `honeycombio_query` adoption as **niche/later** — the common path is to let
  the board/trigger carry its query inline. There is no list endpoint, so coverage is only
  what boards/triggers reference (dedupe repeated `query_id`s across boards + triggers).
- **`honeycombio_flexible_board` — medium curation surface.** Emits a nested `panel` tree
  (query panels + SLO panels) with inline `query_json`. Prune computed `id`/`links`/`url`;
  tolerate panel ordering. VERIFY how `GET /1/boards` distinguishes classic vs flexible and
  whether classic boards still round-trip (Terraformer's `honeycombio_board` is legacy). The
  `honeycombio_board_view` panel resource has no documented import → do not split panels out.
- **`honeycombio_trigger`.** `query_id` (or inline `query_json`), `threshold`, `frequency`,
  `alert_type`, `disabled`, and a **`recipient` block** that may inline PagerDuty/webhook
  secrets — prefer referencing an existing `honeycombio_*_recipient` by `id`; scrub any inline
  secret (see EXCLUDE). Prune computed `id`. Threshold/`frequency` defaults over-emit.
- **`honeycombio_slo` / `honeycombio_burn_alert`.** SLO: `sli` (a derived-column alias ref),
  `target_per_million`, `time_period_days`, `dataset` (or MD/env-wide). Burn alert: `slo_id`
  ref, `alert_type` (`exhaustion_time`/`budget_rate`), `exhaustion_minutes` **or**
  `budget_rate_window_minutes`+`budget_rate_decrease_percent`, and a `recipient` block
  (Terraformer *ignored* `recipient` on burn_alert — scrub/reference, don't inline secrets).
  Prune computed `id`. Note the dataset-vs-`__all__` import fork from §quirks.
- **`honeycombio_column` / `honeycombio_derived_column`.** Column: `key_name`, `type`,
  `hidden`, `description` — Terraformer set `IgnoreKeys{hidden,type}` (they over-emit /
  default) — verify and prune. Derived column: `alias`, `expression`, `description`; the
  `expression` is a Honeycomb-language string with `$…`/`?…` syntax → keep literal (same
  template-escaping hazard as Datadog widget queries; `hcl.ImportBlock`/writer must not
  interpolate). Light otherwise.
- **`honeycombio_query_annotation`.** `name`, `description`, `query_id` ref, `dataset`.
  Light; prune computed `id`.
- **`honeycombio_marker` / `honeycombio_marker_setting`.** Enumerate for visibility only — no
  import (above). Markers are also frequently deploy-event data (created by CI at deploy
  time), not durable IaC — low adoption value even if import lands later.
- **Recipients (typed).** `honeycombio_email_recipient` (`address`) and
  `honeycombio_slack_recipient` (`channel`) are **secret-free** and safe to adopt.
  `honeycombio_pagerduty_recipient` (`integration_key`, Sensitive),
  `honeycombio_webhook_recipient` (`secret` Sensitive + custom `header` values that can hold
  bearer tokens), and `honeycombio_msteams_*` (webhook `url`) carry write-only material →
  scrub (see EXCLUDE). Prune computed `id` on all.
- **`honeycombio_dataset`.** `name`/`slug`/`description`/`expand_json_depth`/
  `delete_protected`. Note datasets are usually auto-created by ingest, not authored — adoption
  is a thin anchor. Prune computed timestamps/`last_written_at`. **`delete_protected` defaults
  true** — keep it explicit so a later destroy doesn't surprise.

## Write-only / secret resources (EXCLUDE)

The credential/notification plane is where Honeycomb's secrets live — exclude or scrub
(surface, adopt out-of-band), exactly like Fastly's `tls_private_key` / Datadog's
`datadog_api_key`:

- **`honeycombio_api_key` (v2) — EXCLUDE.** The key `secret` is write-only (returned only at
  creation, never on read) **and the resource cannot be imported** — a double reason it can't
  be adopted plan-clean. Surface; the actual secret material.
- **`honeycombio_pagerduty_recipient.integration_key`** (Sensitive) — scrub if emitted;
  re-supply out-of-band.
- **`honeycombio_webhook_recipient.secret`** (Sensitive) **and custom `header` values** (can
  carry `Authorization: Bearer …` tokens; not formally marked Sensitive but are secret in
  practice) — scrub.
- **`honeycombio_msteams_recipient` / `honeycombio_msteams_workflow_recipient`** — the MS
  Teams webhook `url` is a secret capability URL → scrub.
- **Inline `recipient` blocks on `honeycombio_trigger` / `honeycombio_burn_alert`** — when a
  trigger/burn-alert defines a PagerDuty/webhook recipient inline (rather than referencing a
  recipient id), the same secrets appear → prefer referencing an `honeycombio_*_recipient.id`;
  scrub any inline secret (Terraformer sidestepped this by ignoring the `recipient` key on
  burn_alert). Email/Slack recipient references are safe.

## Deliberately out of scope
- **`honeycombio_dataset_definition`** — cannot be imported (no import support) → an
  import-based tool can't adopt it; surface as a gap. If ever needed it must be authored by
  hand (it maps column roles: trace_id/span_id/duration/name/…). Blocked, not deferred.
- **`honeycombio_marker` / `honeycombio_marker_setting`** — no import support, and markers are
  largely deploy-event data authored by CI, not durable config. Enumerate for visibility;
  out of scope for adoption until/unless import lands.
- **`honeycombio_board_view`** — a panel/view within a flexible board with no documented
  import; the flexible board carries its panels inline, so splitting views out is
  counterproductive. Out of scope (covered via `honeycombio_flexible_board`).
- **Classic `honeycombio_board`** — deprecated; docs removed from `main`. Emit
  `honeycombio_flexible_board` instead. Only relevant if a live environment still has classic
  boards that the current provider still imports — verify at build.
- **v2 management depth beyond environment** (`Capabilities.IAM=false`): teams, team
  membership, SSO, and the api-key plane (`honeycombio_api_key`, excluded above) are the
  management/IAM surface — modeled at breadth only via `honeycombio_environment`; deeper
  team/IAM management is not covered.
- **Data planes**: events/spans (ingest), query *results*, trigger *evaluation* history, SLO
  *compliance* history, and Query Data API results — the DATA behind the config, per
  environment. Out of scope (config only).
- **Honeycomb SDK dependency** — Terraformer pulls the `honeycombio` Go client; TerraLift
  uses a raw `net/http` client (smaller, matches Fastly/Datadog). Not a resource, but a
  deliberate non-adoption.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `honeycombio_dataset` + `honeycombio_trigger` + `honeycombio_slo` (the parent
anchor plus the reliability/alerting core nearly every Honeycomb team manages as IaC;
triggers/SLOs establish the `GET /1/datasets` → per-dataset fan-out spine, the
`<dataset>/<id>` composite import, **and** the env-wide/MD bare-import fork — the two
structural facts of the provider) → INC-1 `honeycombio_burn_alert` (the second-level per-SLO
fan-out `?slo_id=`) + `honeycombio_column` + `honeycombio_derived_column` (the schema plane;
exercises the `<dataset>/<key_name>`-vs-`<dataset>/<alias>` name-import and the env-wide
`__all__` derived-column variant) → INC-2 `honeycombio_flexible_board` +
`honeycombio_query` + `honeycombio_query_annotation` (boards + the no-list-endpoint query
discovery via board/trigger `query_id`; the `query_json` heredoc curation) → INC-3
`honeycombio_email_recipient` + `honeycombio_slack_recipient` (secret-free) then the
secret-scrubbing `honeycombio_pagerduty_recipient` + `honeycombio_webhook_recipient` +
`honeycombio_msteams_recipient` (the typed recipient plane feeding off one `GET /1/recipients`)
→ INC-4 (v2 management, separate client + management key) `honeycombio_environment` →
LATER/BLOCKED `honeycombio_api_key` (EXCLUDE — write-only secret, no import),
`honeycombio_dataset_definition` / `honeycombio_marker` / `honeycombio_marker_setting` /
`honeycombio_board_view` (no import support), classic `honeycombio_board` (deprecated), and
the ingest/query/SLO-history data planes.
