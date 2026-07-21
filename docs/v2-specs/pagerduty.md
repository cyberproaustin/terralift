# PagerDuty provider — build spec

Research artifact for the `pagerduty` provider (Phase A scaffold; TF provider source is
`PagerDuty/pagerduty`, product "PagerDuty"). Sources: Terraformer's `providers/pagerduty/`
(built on the `heimweh/go-pagerduty` Go SDK), the `PagerDuty/pagerduty` registry docs
(import formats + schema, **verified per-resource below** against the provider's
`docs/resources/*.md`), and the PagerDuty REST API (`https://api.pagerduty.com`). Build
mirrors the **Datadog** provider (`internal/providers/datadog/`) — a flat, account-scoped,
single-container REST provider (a direct `net/http` client, no CLI, `terraform plan
-generate-config-out` for export) — plus the **Honeycomb** per-parent fan-out pattern
(`internal/providers/honeycomb/`) for its several sub-resource fan-outs. This is **REST,
Datadog-style, NOT GraphQL.** Two dominant divergences from every prior provider, both
called out below and load-bearing:

1. **Auth is the literal `Authorization: Token token=<token>` header** — *not* `Bearer`,
   *not* a bespoke `X-…-Key` header. The string `Token token=` is a real prefix, unique to
   PagerDuty among all providers built so far.
2. **Import IDs are a MIX of bare `P`-ids and composites with DOT-vs-COLON separators** —
   the sharp contrast to Datadog (where *everything* is a bare token). The dot-vs-colon
   choice and the parent-id ordering are the #1 hazard.

Plus a third structural fact: pagination is the classic **keyed offset/limit + `more`
boolean** envelope (a distinct pager from Datadog's several), with a handful of **newer
endpoints on cursor pagination** flagged per-resource.

## Version pin (load-bearing)

Pin `PagerDuty/pagerduty ~> 3.x` (current major; org is `PagerDuty`). Naming facts that
matter (the Terraformer-vs-current divergences — the PagerDuty analogue of Datadog's
`datadog_downtime` → `_downtime_schedule`):

- **Terraformer emits the legacy `pagerduty_service_event_rule`** (per-service event rules,
  the old Event Rules Engine). PagerDuty has **superseded** per-service event rules — and
  the standalone **Rulesets/Ruleset-Rules plane** — with **Event Orchestration**. The
  current resources are `pagerduty_event_orchestration` (+ `_router` / `_service` /
  `_global` / `_integration`). **Emit the Event-Orchestration resources for new work; keep
  `pagerduty_ruleset` / `pagerduty_ruleset_rule` only for accounts that still run the
  legacy engine, and flag them as legacy.** Do not copy Terraformer's
  `pagerduty_service_event_rule` — it is deprecated in the provider.
- **Terraformer's coverage is a strict subset.** Its generator covers `business_service`,
  `escalation_policy`, `ruleset` (+ `ruleset_rule`), `schedule`, `service` (+ the deprecated
  `service_event_rule`), `team` (+ `team_membership`), and `user`. It does **not** cover
  `user_contact_method`, `user_notification_rule`, `maintenance_window`,
  `event_orchestration*`, `extension`/`extension_servicenow`, `webhook_subscription`,
  `slack_connection`, `tag`, `response_play`, or `automation_actions_*`. Those are covered
  here from the registry + REST API directly (as we did for the Datadog/Honeycomb resources
  Terraformer lacked).
- Terraformer reads only `PAGERDUTY_TOKEN`. The **TF provider** reads the *same*
  `PAGERDUTY_TOKEN` (config `token`) plus `PAGERDUTY_API_URL_OVERRIDE` /
  `PAGERDUTY_SERVICE_REGION` for the base URL. The REST endpoints below are
  provider-version-independent.

## Shape

- **Auth — the distinctive `Authorization: Token token=<token>` header (the hard
  divergence).** Every request carries **three** headers:
  - `Authorization: Token token=<PAGERDUTY_TOKEN>` — note the literal `Token token=`
    prefix. This is **not** `Bearer <token>` and **not** a custom `X-…-Key` header; the
    exact string is `Token token=` followed by the raw token. Getting this prefix wrong is a
    silent 401.
  - `Accept: application/vnd.pagerduty+json;version=2` — the API-version negotiation header;
    PagerDuty keys response shape on it. Send it on **every** request.
  - `Content-Type: application/json` (on write; harmless on GET).
  Read the token from `PAGERDUTY_TOKEN`. A direct `net/http` client (mirror
  `datadogapi.go`); **no PagerDuty CLI**, and do **not** pull the `heimweh/go-pagerduty` SDK
  the way Terraformer did — a raw client is smaller and matches the Datadog/Honeycomb
  providers. The token rides **only** on the `Authorization` header, **never** in errors/
  logs (same discipline as `DD-API-KEY`). The redirect-refusing client + host-validation
  still apply (mirror `datadogHTTPClient`/`isDatadogURL`): the token must not leave the
  configured host on a 3xx (Go strips `Authorization` on a *cross-host* redirect, but we
  refuse redirects outright rather than rely on that).
- **Base URL — US vs EU service region, must be read from env.** Default
  `https://api.pagerduty.com` (US). The **EU service region** is
  `https://api.eu.pagerduty.com` (accounts provisioned in the EU cannot be reached on the US
  host and vice-versa). Read `PAGERDUTY_API_URL` (fallback: a region hint like
  `PAGERDUTY_SERVICE_REGION=eu` → the EU host) as the full API base URL, default US. Store
  the resolved base once and host-validate it before sending the token; force `https` (the
  token is a bearer-equivalent secret). The TF provider takes `service_region` / an
  `api_url_override` for the same purpose.
  | region | base URL |
  |---|---|
  | US (default) | `https://api.pagerduty.com` |
  | EU | `https://api.eu.pagerduty.com` |
  - **Slack connections live on a DIFFERENT host** — the Slack integration API is served
    from `https://app.pagerduty.com/integration-slack/…`, not `api.pagerduty.com`. Treat
    `pagerduty_slack_connection` as a separate base + a per-workspace fan-out; see the
    catalog + gotchas. Add the app host to the host-validation allow-list.
- **Scope — one PagerDuty account = one flat container.** The token is **account-scoped**;
  there is no sub-account and no multi-account resolution — the token simply **is** the
  account. `model.ScopeTenant`. There is **no `GET /users/me`** on a general-access API
  token (that is an OAuth-user-token concept), and REST does not hand back the account
  subdomain cheaply, so resolve the container id/name **best-effort**: validate with a
  lightweight `GET /abilities` (returns the account's `{"abilities":[…]}` feature list) and,
  if a display name is wanted, fall back to the API host string. This is *flatter* than
  Datadog's org lookup: the token pair simply is the account, no id lookup required.
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response family — ONE shape: the keyed offset/limit/`more` envelope (the key structural
  fact).** Almost every list endpoint returns a **resource-named keyed envelope**:
  ```
  { "<resource>": [ … ], "limit": 25, "offset": 0, "more": true, "total": 123 }
  ```
  — the array lives under a resource-named key (`services`, `escalation_policies`,
  `schedules`, `teams`, `users`, `business_services`, `maintenance_windows`, `rulesets`,
  `extensions`, `webhook_subscriptions`, `tags`, `response_plays`, …), and the pager is
  **offset/limit + a `more` boolean**. This is Datadog's `datadogGetKeyed` **plus** a pager
  → implement one generic **keyed-offset-`more`** helper (the whole client surface for the
  common case) that takes the envelope key as a parameter and loops
  `?limit=100&offset=<n>`, incrementing `offset += limit` **while `more == true`** (do not
  rely on `total`; `more` is authoritative). Bound every loop defensively (`pdMaxPages`).
- **Pagination exceptions — cursor pagination on newer endpoints (flag these).** A few
  newer surfaces do **not** use offset/`more`; they use **cursor** pagination
  (`?limit=<n>&cursor=<c>` → `{"…":[…],"next_cursor":"…"}`, loop until `next_cursor` is
  empty/null). Confirmed/suspected cursor endpoints: **`GET /event_orchestrations`** (and
  its sub-lists) and **`GET /automation_actions/actions`** / `…/runners`. Provide a second
  generic **keyed-cursor** helper and tag each endpoint in the catalog with which pager it
  uses. **VERIFY** each newer endpoint's pager at build (PagerDuty has migrated some
  offset→cursor over time).
- **Status handling (mirror `list`/`datadogAPIError`).** PagerDuty errors are
  `{"error":{"code":…,"message":…,"errors":[…]}}`. 401 → token invalid/expired (fatal,
  surfaced in preflight; if it appears mid-run every remaining list fails too → treat as
  fatal, not a partial inventory). 403 → the token's scope lacks the read (e.g. a
  read-only-vs-full key, or an ability the account lacks) → best-effort skip at Verbose. 404
  → feature/endpoint absent on the plan → Verbose skip. **429** — PagerDuty rate-limits
  hard; honour `Retry-After` and back off. 5xx / network → enumeration may be silently
  incomplete → Warn + count. Token never in errors/logs; strip any query string before an
  error/URL is logged (belt-and-suspenders, mirror `redactURL`).
- **The `From` header quirk (do not miss it).** A handful of endpoints require a
  **`From: <valid-account-user-email>`** header, notably **`GET /response_plays`** (and
  response-play writes). Resolve one account user email up front (`GET /users?limit=1` →
  `users[0].email`) and set it as `From` on those calls; without it they 400. Flag prominently.
- **Preflight**: `terraform` present + `PAGERDUTY_TOKEN` set + `GET /abilities` returns 200.
  `/abilities` doubles as a capability probe — log which abilities are absent so later 403/
  404 skips (e.g. event-orchestration or automation-actions endpoints on a plan that lacks
  them) are explained rather than surprising. A `GET /users?limit=1` is an equally cheap
  fallback validation.
- **Connect**: no real resolution — the token is the account. Validate `/abilities`
  succeeds, set the single flat container (id/name = host string best-effort per Scope), and
  cache the `From` user email for the response-play calls.

## Import-ID separator + fan-out scope — the CRITICAL determination

This is PagerDuty's analogue of Datadog's "which API version/shape" call and Honeycomb's
"dataset-scoped-vs-bare" call. The load-bearing per-resource facts are **(a) is the import
id BARE, a DOT-composite, or a COLON-composite, and in what parent-id order; and (b) is the
resource enumerated flat or via a per-parent fan-out.** Get (a) wrong and every import block
for that type is un-importable; get (b) wrong and you never reach the sub-resources. The
rules:

- **Most resources import by a BARE `P`-prefixed id** (e.g. `PXXXXXX`): service,
  escalation_policy, schedule, team, user, business_service, maintenance_window,
  event_orchestration, ruleset, response_play, extension, extension_servicenow,
  webhook_subscription, tag, automation_actions_*. The id is the object's own id off the
  list.
- **Several import by a DOT-composite `<parent_id>.<child_id>`** — VERIFY each against the
  registry:
  - `pagerduty_service_integration` = **`<service_id>.<integration_id>`** (dot). The
    canonical registry example is `PGADR38.PROIN01`-style.
  - `pagerduty_ruleset_rule` = **`<ruleset_id>.<rule_id>`** (dot).
  - `pagerduty_slack_connection` = **`<workspace_id>.<connection_id>`** (dot) — and note
    the different host (above); `workspace_id` is the Slack team id, not a PagerDuty id.
    **VERIFY** (dot vs slash) at build.
- **Several import by a COLON-composite `<parent_id>:<child_id>`** — VERIFY each:
  - `pagerduty_team_membership` = **`<user_id>:<team_id>`** (colon). Order is
    **user-first** (Terraformer emits exactly `fmt.Sprintf("%s:%s", member.User.ID,
    team.ID)`).
  - `pagerduty_user_contact_method` = **`<user_id>:<contact_method_id>`** (colon).
  - `pagerduty_user_notification_rule` = **`<user_id>:<notification_rule_id>`** (colon).
  - `pagerduty_event_orchestration_integration` = **`<event_orchestration_id>:<integration_id>`**
    (colon). **VERIFY**.
- **Singleton-per-parent Event-Orchestration children import by the PARENT's bare id** (no
  separator, no own id): `pagerduty_event_orchestration_router` and
  `pagerduty_event_orchestration_global` import by **`<event_orchestration_id>`**;
  `pagerduty_event_orchestration_service` imports by **`<service_id>`** (the service it is
  attached to). These are one-object-per-parent, so the parent id *is* the import id.
- **Fan-outs (the Honeycomb per-parent pattern).** Five parent→child fan-outs; each parent
  is a flat list, then a per-parent sub-list (or a sub-array already on the parent object):
  - **service → integrations** (`pagerduty_service_integration`): the service object carries
    an `integrations[]` array when fetched with `?include[]=integrations` — enumerate
    services with that include and read `service.integrations[].id` (avoids a per-service
    call). Import `<service_id>.<integration_id>` (dot).
  - **user → contact_methods** and **user → notification_rules**: per user,
    `GET /users/<id>/contact_methods` and `GET /users/<id>/notification_rules`. Import
    `<user_id>:<child_id>` (colon).
  - **team → memberships** (`pagerduty_team_membership`): per team,
    `GET /teams/<id>/members` → each `members[].user.id`. Import `<user_id>:<team_id>`
    (colon).
  - **ruleset → rules** (`pagerduty_ruleset_rule`): per ruleset,
    `GET /rulesets/<id>/rules`. Import `<ruleset_id>.<rule_id>` (dot). Legacy plane.
  - **event_orchestration → integrations** (`pagerduty_event_orchestration_integration`):
    per orchestration, `GET /event_orchestrations/<id>/integrations`. Import
    `<event_orchestration_id>:<integration_id>` (colon). **VERIFY**.

The dot-vs-colon separator and the parent-id ordering are the things we cannot get wrong —
they are enumerated per-resource in the catalog and re-verified against the registry docs at
build.

## Enumeration spine

Flat account scope. The pattern is: a set of top-level keyed-offset-`more` lists, plus the
five per-parent fan-outs above. Best-effort per list (403 scope-absent / 404 feature-absent
→ Verbose skip; 401 → fatal; other errors → Warn + count, so a systemic failure is told
apart from an empty account). Each list is tagged with its envelope key + pager (offset/`more`
vs cursor) per the catalog.

- **Top-level flat lists (offset/`more`):**
  - `GET /services?include[]=integrations` → `services` → `pagerduty_service` (+ read
    `integrations[]` for the service_integration fan-out).
  - `GET /escalation_policies` → `escalation_policies` → `pagerduty_escalation_policy`.
  - `GET /schedules` → `schedules` → `pagerduty_schedule`.
  - `GET /teams` → `teams` → `pagerduty_team` (+ members fan-out below).
  - `GET /users` → `users` → `pagerduty_user` (+ contact_methods / notification_rules
    fan-outs). Cache one `users[0].email` for the `From` header.
  - `GET /business_services` → `business_services` → `pagerduty_business_service`.
    (Terraformer called this **unpaged** — it does support offset/`more`; loop it.)
  - `GET /maintenance_windows?filter=open&filter=future` → `maintenance_windows` →
    `pagerduty_maintenance_window`. **Filter to open+future** — past windows are expired
    historical data, not durable config (see gotchas).
  - `GET /rulesets` → `rulesets` → `pagerduty_ruleset` (+ rules fan-out). **Legacy** —
    enumerate for accounts still on the Event Rules Engine; prefer event orchestration.
  - `GET /extensions` → `extensions` → `pagerduty_extension` **or**
    `pagerduty_extension_servicenow` (discriminate on `extension_schema` — see catalog).
  - `GET /webhook_subscriptions` → `webhook_subscriptions` → `pagerduty_webhook_subscription`.
  - `GET /tags` → `tags` → `pagerduty_tag`.
  - `GET /response_plays` → `response_plays` → `pagerduty_response_play`. **Requires the
    `From: <user email>` header** (see Shape) — 400 without it.
- **Top-level flat lists (CURSOR — verify pager):**
  - `GET /event_orchestrations` → `orchestrations` → `pagerduty_event_orchestration`
    (+ per-orchestration `_router` / `_global` / `_integration` fan-outs, and per-service
    `_service`). **Cursor pagination — VERIFY.**
  - `GET /automation_actions/actions` → `actions` → `pagerduty_automation_actions_action`;
    `GET /automation_actions/runners` → `runners` → `pagerduty_automation_actions_runner`.
    **Cursor pagination; feature-gated (Automation Actions add-on) — VERIFY current schema
    + that the account has the ability; 404/403 → skip.**
- **Per-parent fan-outs:**
  - per **service**: read `integrations[]` off the included service object →
    `pagerduty_service_integration` (`<service_id>.<integration_id>`).
  - per **user**: `GET /users/<id>/contact_methods` → `pagerduty_user_contact_method`
    (`<user_id>:<cm_id>`); `GET /users/<id>/notification_rules` →
    `pagerduty_user_notification_rule` (`<user_id>:<rule_id>`).
  - per **team**: `GET /teams/<id>/members` → `pagerduty_team_membership`
    (`<user_id>:<team_id>`).
  - per **ruleset**: `GET /rulesets/<id>/rules` → `pagerduty_ruleset_rule`
    (`<ruleset_id>.<rule_id>`). Legacy.
  - per **event_orchestration**: singleton `…/router` (`pagerduty_event_orchestration_router`,
    import `<eo_id>`), singleton `…/global` (`pagerduty_event_orchestration_global`, import
    `<eo_id>`), list `…/integrations` (`pagerduty_event_orchestration_integration`, import
    `<eo_id>:<integration_id>`).
  - per **service** (again): `GET /event_orchestrations/services/<service_id>` →
    `pagerduty_event_orchestration_service` (singleton per service, import `<service_id>`).
    Only present when service-level orchestration is configured — 404 → skip.
- **Slack connections (separate host + fan-out):** `pagerduty_slack_connection` is served
  from `https://app.pagerduty.com/integration-slack/workspaces/<workspace_id>/connections`
  — you must know the Slack `workspace_id`(s) first. There is no clean account-wide list on
  the API host; treat as a **later increment / surface-only** unless a workspace id is
  supplied. See out-of-scope.

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as `enumerate.go`).

## Resource catalog

Import IDs verified against the current `PagerDuty/pagerduty` registry docs
(`docs/resources/*.md`). All scope = account. "endpoint → key" is the list path and its
envelope key; "pager" is offset/`more` unless noted cursor; "fan-out" marks a per-parent
sub-list. Separator column is the #1 hazard — **bare / dot / colon**.

| native key | TF type | endpoint → envelope key | fan-out | id field | import ID | sep |
|---|---|---|---|---|---|---|
| pagerduty:service | pagerduty_service | `GET /services?include[]=integrations` → `services` | parent | `id` | `<service_id>` | bare |
| pagerduty:service_integration | pagerduty_service_integration | `service.integrations[]` (via include) | ← service | `id` | `<service_id>.<integration_id>` | **dot** |
| pagerduty:escalation_policy | pagerduty_escalation_policy | `GET /escalation_policies` → `escalation_policies` | — | `id` | `<escalation_policy_id>` | bare |
| pagerduty:schedule | pagerduty_schedule | `GET /schedules` → `schedules` | — | `id` | `<schedule_id>` | bare |
| pagerduty:team | pagerduty_team | `GET /teams` → `teams` | parent | `id` | `<team_id>` | bare |
| pagerduty:team_membership | pagerduty_team_membership | `GET /teams/<id>/members` → `members[].user.id` | ← team | user+team | `<user_id>:<team_id>` | **colon** |
| pagerduty:user | pagerduty_user | `GET /users` → `users` | parent | `id` | `<user_id>` | bare |
| pagerduty:user_contact_method | pagerduty_user_contact_method | `GET /users/<id>/contact_methods` → `contact_methods` | ← user | `id` | `<user_id>:<contact_method_id>` | **colon** |
| pagerduty:user_notification_rule | pagerduty_user_notification_rule | `GET /users/<id>/notification_rules` → `notification_rules` | ← user | `id` | `<user_id>:<notification_rule_id>` | **colon** |
| pagerduty:business_service | pagerduty_business_service | `GET /business_services` → `business_services` | — | `id` | `<business_service_id>` | bare |
| pagerduty:maintenance_window | pagerduty_maintenance_window | `GET /maintenance_windows?filter=open&filter=future` → `maintenance_windows` | — | `id` | `<maintenance_window_id>` | bare |
| pagerduty:event_orchestration | pagerduty_event_orchestration | `GET /event_orchestrations` → `orchestrations` **(cursor?)** | parent | `id` | `<event_orchestration_id>` | bare |
| pagerduty:event_orchestration_router | pagerduty_event_orchestration_router | `GET /event_orchestrations/<id>/router` (singleton) | ← EO | — | `<event_orchestration_id>` | bare (parent id) |
| pagerduty:event_orchestration_global | pagerduty_event_orchestration_global | `GET /event_orchestrations/<id>/global` (singleton) | ← EO | — | `<event_orchestration_id>` | bare (parent id) |
| pagerduty:event_orchestration_service | pagerduty_event_orchestration_service | `GET /event_orchestrations/services/<service_id>` (singleton) | ← service | — | `<service_id>` | bare (parent id) |
| pagerduty:event_orchestration_integration | pagerduty_event_orchestration_integration | `GET /event_orchestrations/<id>/integrations` → `integrations` | ← EO | `id` | `<event_orchestration_id>:<integration_id>` | **colon** (VERIFY) |
| pagerduty:ruleset | pagerduty_ruleset | `GET /rulesets` → `rulesets` **(legacy)** | parent | `id` | `<ruleset_id>` | bare |
| pagerduty:ruleset_rule | pagerduty_ruleset_rule | `GET /rulesets/<id>/rules` → `rules` **(legacy)** | ← ruleset | `id` | `<ruleset_id>.<rule_id>` | **dot** |
| pagerduty:response_play | pagerduty_response_play | `GET /response_plays` → `response_plays` **(needs `From`)** | — | `id` | `<response_play_id>` | bare |
| pagerduty:extension | pagerduty_extension | `GET /extensions` → `extensions` | — | `id` | `<extension_id>` | bare |
| pagerduty:extension_servicenow | pagerduty_extension_servicenow | `GET /extensions` → `extensions` (schema = ServiceNow) | — | `id` | `<extension_id>` | bare |
| pagerduty:webhook_subscription | pagerduty_webhook_subscription | `GET /webhook_subscriptions` → `webhook_subscriptions` | — | `id` | `<webhook_subscription_id>` | bare |
| pagerduty:slack_connection | pagerduty_slack_connection | `GET /integration-slack/workspaces/<ws>/connections` **(app host)** | ← workspace | `id` | `<workspace_id>.<connection_id>` | **dot** (VERIFY) |
| pagerduty:tag | pagerduty_tag | `GET /tags` → `tags` | — | `id` | `<tag_id>` | bare |
| pagerduty:automation_actions_action | pagerduty_automation_actions_action | `GET /automation_actions/actions` → `actions` **(cursor; add-on)** | — | `id` | `<action_id>` | bare |
| pagerduty:automation_actions_runner | pagerduty_automation_actions_runner | `GET /automation_actions/runners` → `runners` **(cursor; add-on)** | — | `id` | `<runner_id>` | bare |

### Import-format quirks (§ do not get wrong)
1. **DOT vs COLON is per-resource — the single most error-prone thing.** Dot-composites:
   `pagerduty_service_integration` (`<service_id>.<integration_id>`), `pagerduty_ruleset_rule`
   (`<ruleset_id>.<rule_id>`), `pagerduty_slack_connection` (`<workspace_id>.<connection_id>`).
   Colon-composites: `pagerduty_team_membership` (`<user_id>:<team_id>`),
   `pagerduty_user_contact_method` (`<user_id>:<contact_method_id>`),
   `pagerduty_user_notification_rule` (`<user_id>:<notification_rule_id>`),
   `pagerduty_event_orchestration_integration` (`<event_orchestration_id>:<integration_id>`).
   Everything else is bare. Encode this as an explicit per-TF-type switch in `importid.go`
   (mirror Datadog's `rawImportID` switch) — never infer the separator.
2. **Parent-id ORDER matters and is not uniform.** `team_membership` is **user-first**
   (`<user_id>:<team_id>`), matching Terraformer's `member.User.ID` + `team.ID`. The
   `user_*` colon composites are also **user-first** (`<user_id>:<child_id>`).
   `service_integration` / `ruleset_rule` are **parent-first**
   (`<service_id>.<integration_id>` / `<ruleset_id>.<rule_id>`). Verify each order against
   the registry example — a reversed order is a silent import failure.
3. **Singleton-per-parent EO children import by the PARENT's bare id, with no own id.**
   `event_orchestration_router` and `event_orchestration_global` → `<event_orchestration_id>`;
   `event_orchestration_service` → `<service_id>`. There is exactly one of each per parent,
   so the object's own id never appears in the import id.
4. **`pagerduty_extension` vs `pagerduty_extension_servicenow` share the `/extensions`
   list** — discriminate on the extension's `extension_schema` (the ServiceNow extension
   schema id) or the emitted `config` shape, and map to the right TF type. Both import by
   the bare `<extension_id>`.
5. **`pagerduty_response_play` enumeration needs the `From` header.** Not an import-id
   quirk, but the play cannot be *listed* (400) without `From: <user email>`; the import id
   itself is the bare `<response_play_id>`. Response plays also require a `from` on create —
   surface that for authoring.
6. **`pagerduty_slack_connection` is a different beast** — different host
   (`app.pagerduty.com`), a `workspace_id` you must know, and a dot-composite import. Treat
   as later/surface-only (out of scope for the beachhead).
7. **All ids are opaque `P`-prefixed strings off the wire** (no numeric stringify step,
   unlike Datadog's numeric monitor/notebook ids) — copy them verbatim.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a
live account — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the
Datadog/Honeycomb providers. PagerDuty has **no single monster resource** (contrast
Datadog's `datadog_dashboard`); the weight is spread, and the recurring hazards are the
**Event-Orchestration rule trees**, the **`.references`/relationship over-emit**, and the
**integration/webhook/extension secrets**.

- **`pagerduty_service` — medium.** `escalation_policy` (ref), `alert_creation`,
  `auto_resolve_timeout`/`acknowledgement_timeout` (over-emit defaults; `null` means "use
  account default" — tolerate), `incident_urgency_rule` + `scheduled_actions` +
  `support_hours` nested blocks, `alert_grouping_parameters`. Prune computed
  `html_url`/`created_at`/`status`/`last_incident_timestamp`. The legacy inline
  `incident_urgency_rule` churns.
- **`pagerduty_service_integration` — SECRET (scrub).** `integration_key` (a.k.a. the
  Events-API **routing key**) is `Computed`+`Sensitive` and *is* returned on read — it is
  live credential material for sending events → **scrub the value**, keep the block shape.
  `vendor` (ref) and `type` are the config surface; `integration_email` for email
  integrations is not secret but is the ingest address.
- **`pagerduty_escalation_policy` — light/medium.** `rule[]` with `escalation_delay_in_minutes`
  + `target[]` (users/schedules refs), `num_loops`, `teams` (refs). Prune computed
  `html_url`. Rule ordering is significant — preserve it.
- **`pagerduty_schedule` — medium.** `layer[]` (rotation layers) with `start`,
  `rotation_virtual_start`, `rotation_turn_length_seconds`, `users` (ordered refs),
  `restriction[]`. `time_zone` required. Prune computed `layer[].id`/`html_url`. **The
  `start`/`rotation_virtual_start` timestamps drift** (server normalizes) — a known
  perpetual-diff hazard; may need `ignore_changes` or tolerance. Overrides are separate data.
- **`pagerduty_team` / `pagerduty_team_membership` — light.** Team: `name`,
  `description`, `parent` (ref, for team hierarchy). Membership: `user_id`, `team_id`,
  `role` (`manager`/`responder`/`observer`) — Terraformer's members list carries the role;
  emit it. Prune computed `html_url`.
- **`pagerduty_user` / `_contact_method` / `_notification_rule` — light, no secret.** User:
  `email`, `name`, `role`, `job_title`, `time_zone`, `teams` (refs — note the provider
  deprecated inline `teams` on the user in favour of `team_membership`; prefer the
  membership resource). Contact method: `type`
  (`email_contact_method`/`phone_contact_method`/`sms_contact_method`/
  `push_notification_contact_method`), `address`, `country_code` — **no secret** (phone/
  email are PII but not credentials). Notification rule: `start_delay_in_minutes`,
  `urgency`, `contact_method` (ref). **CAUTION:** your own user (behind the token) appears —
  adopt but do not delete/lock yourself out.
- **`pagerduty_business_service` — light.** `name`, `description`, `point_of_contact`,
  `team` (ref). Prune computed `html_url`. Business-service **dependencies** are a separate
  resource (out of scope, below).
- **`pagerduty_maintenance_window` — light but note staleness.** `start_time`/`end_time`,
  `services` (refs), `description`. Filter enumeration to open+future — a **past** window is
  immutable expired data with no adoption value (adopting it just imports a dead object).
- **`pagerduty_event_orchestration` + children — the heaviest curation surface here.** The
  orchestration itself is light (`name`, `team`). The weight is in `_router` / `_global` /
  `_service`, whose `set[].rule[]` trees carry `condition[].expression` (PagerDuty
  PCL/event-condition strings, e.g. `event.severity matches 'critical'`) and
  `actions{…}` blocks. **Template hazard:** conditions and `variable`/`extraction`
  templates use `{{…}}`/`event.custom_details.*` syntax → the generated HCL must keep these
  literal (same class as Datadog widget-query escaping — verify terraform's writer does not
  interpolate `${…}`/`%{…}`). Rule/set ordering is significant. `_router` and `_global` are
  **singletons** — one block per parent; do not try to split them.
- **`pagerduty_ruleset` / `_ruleset_rule` — legacy, medium.** Rule: `conditions`,
  `actions`, `position`, `disabled`, `time_frame`. Same PCL/template hazard as event
  orchestration. **Legacy plane** — only adopt for accounts still on the Event Rules Engine;
  otherwise steer to event orchestration.
- **`pagerduty_response_play` — medium.** `subscribers`, `responders` (escalation-policy/
  user refs), `runnability`, `conference_number`/`conference_url`. Requires a `from` (user
  email) on create — surface it. Prune computed `id`/`html_url`.
- **`pagerduty_extension` / `_servicenow` — SECRET (scrub).** Generic extension `config` is
  a JSON blob that can carry endpoint auth tokens/headers → scrub. **ServiceNow extension**
  carries `snow_user` + **`snow_password`** (write-only) and `api_key` → scrub the password/
  key. `endpoint_url` may itself be a secret webhook URL for some schemas → treat as
  sensitive.
- **`pagerduty_webhook_subscription` — SECRET (scrub).** `delivery_method` carries the
  destination `url` and `custom_header[]` values (can hold `Authorization: Bearer …` /
  signing tokens) → scrub the header values. The webhook **signing secret** is returned only
  at creation (never on read) → cannot be adopted round-trip; note it as re-supply-out-of-band.
  `events[]` + `filter{type,id}` are the config surface.
- **`pagerduty_tag` — trivial.** `label` only. Tag **assignments** (tag↔entity) are a
  separate composite resource, out of scope below.
- **`pagerduty_slack_connection` — SECRET + different host.** OAuth-backed; `config`,
  `channel_id`, `notification_type`. Adoption needs the Slack `workspace_id`; the OAuth
  linkage is out-of-band. Later/surface-only.
- **`pagerduty_automation_actions_*` — feature-gated, may carry secrets.** Runner/action
  configs can hold connection tokens → scrub. Only present on accounts with the Automation
  Actions add-on; VERIFY schema currency before adopting.

## Write-only / secret resources (EXCLUDE / scrub)

The credential/integration plane is where PagerDuty's secrets live — scrub the value (keep
the block, re-supply out-of-band) or exclude the resource, exactly like Datadog's
`datadog_api_key` / Honeycomb's recipient secrets:

- **`pagerduty_service_integration.integration_key` / routing key** — live Events-API
  credential (Sensitive; *is* returned on read) → **scrub the value**, keep the integration
  block. This is the most common secret in a PagerDuty adoption.
- **`pagerduty_webhook_subscription`** — `delivery_method.custom_header[]` values (bearer/
  signing tokens) → scrub; the generated **signing secret** is create-only (never on read)
  → note as un-round-trippable, re-supply out-of-band.
- **`pagerduty_extension` / `pagerduty_extension_servicenow`** — extension `config` auth
  tokens, and specifically **`snow_password`** / `api_key` on the ServiceNow extension
  (write-only) → scrub. `endpoint_url` for webhook-style schemas is a secret capability URL
  → scrub.
- **`pagerduty_slack_connection`** — OAuth token linkage is not exposed via TF and cannot be
  round-tripped; the connection is a secret integration → excluded from the beachhead
  (surface only).
- **`pagerduty_automation_actions_action` / `_runner`** — runner/connection tokens in the
  action config → scrub; feature-gated, later increment.
- **Not secret, do not over-scrub:** `pagerduty_user_contact_method` (phone/email are PII,
  not credentials — adopt), `pagerduty_service_integration.integration_email` (ingest
  address, not a secret), `pagerduty_tag` (label only).

## Deliberately out of scope
- **Legacy Event Rules Engine as the *primary* path** — `pagerduty_service_event_rule`
  (Terraformer's, deprecated) is dropped entirely; `pagerduty_ruleset` / `_ruleset_rule` are
  kept only for accounts still on the legacy engine and flagged legacy. New adoptions use
  Event Orchestration.
- **`pagerduty_slack_connection`** — different host (`app.pagerduty.com`), needs a Slack
  `workspace_id` (no clean account-wide list on the API host), and an OAuth linkage that
  can't round-trip. Surface-only / a much-later increment; not in the beachhead.
- **Dependency / assignment composites** — `pagerduty_service_dependency`,
  `pagerduty_business_service_dependency`, `pagerduty_tag_assignment`,
  `pagerduty_slack_connection` mappings: relationship objects with their own composite
  import ids and cross-references. A later increment once the anchor resources are solid.
- **`pagerduty_automation_actions_*`** (action/runner) beyond a stub — feature-gated add-on,
  cursor-paginated, secret-bearing; VERIFY currency and gate on the ability before building.
- **IAM depth (`Capabilities.IAM=false`)** — users/teams/team_membership are modeled at
  breadth, but SSO/SAML, `pagerduty_user_handoff_notification_rule`, business-roles, and
  license assignment are not.
- **Schedule overrides / on-call data** — `pagerduty_schedule` is adopted as config, but
  schedule *overrides*, final-schedule rendering, and current on-call are runtime data, not
  IaC. Out of scope.
- **Incident / data planes** — incidents, alerts, log entries, notifications, analytics,
  status-dashboard data — the DATA behind the config. Out of scope (config only).
- **PagerDuty SDK dependency** — Terraformer pulls `heimweh/go-pagerduty`; TerraLift uses a
  raw `net/http` client (smaller, matches Datadog/Honeycomb). A deliberate non-adoption.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `pagerduty_service` + `pagerduty_service_integration` + `pagerduty_escalation_policy`
(the on-call config nearly every PagerDuty account manages as IaC; service establishes the
`?include[]=integrations` fan-out **and** the first DOT-composite import
`<service_id>.<integration_id>` — plus the `integration_key` secret-scrub — the two
structural facts of the provider; escalation_policy is the simple bare-id anchor services
reference) → INC-1 `pagerduty_schedule` + `pagerduty_team` + `pagerduty_team_membership`
(the rest of the routing core; team_membership establishes the **user-first COLON**
composite `<user_id>:<team_id>` and the per-team members fan-out) → INC-2 `pagerduty_user` +
`pagerduty_user_contact_method` + `pagerduty_user_notification_rule` (the identity plane and
the two per-user COLON fan-outs; self-adoption caution) → INC-3 `pagerduty_business_service`
+ `pagerduty_maintenance_window` + `pagerduty_response_play` (the `From`-header quirk and the
open+future window filter) → INC-4 `pagerduty_event_orchestration` (+ `_router` / `_global` /
`_service` / `_integration`) (the modern rules plane — the singleton-per-parent bare-parent-id
imports, the EO-integration COLON composite, cursor pagination, and the PCL/template-escaping
curation) → INC-5 `pagerduty_extension` + `pagerduty_extension_servicenow` +
`pagerduty_webhook_subscription` + `pagerduty_tag` (the outbound-integration plane — extension/
webhook secret-scrub, ServiceNow `snow_password`, the extension-schema discriminator) →
LATER/LEGACY `pagerduty_ruleset` + `pagerduty_ruleset_rule` (legacy Event Rules Engine — DOT
composite, only for accounts still on it), `pagerduty_automation_actions_*` (feature-gated,
cursor, secrets), `pagerduty_slack_connection` (different host + OAuth, surface-only), and the
dependency/assignment composites + incident/data planes.
