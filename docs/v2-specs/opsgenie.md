# Opsgenie provider — build spec

Research artifact for the `opsgenie` provider (Phase A scaffold; TF provider source is
`opsgenie/opsgenie`, product "Opsgenie" — the Atlassian on-call/alerting platform).
Sources: Terraformer's `providers/opsgenie/` (built on the `opsgenie/opsgenie-go-sdk-v2`
Go SDK — only `user` / `team` / `service` generators), the `opsgenie/opsgenie` registry
docs (import formats + schema, **verified per-resource below** against the provider's
`website/docs/r/*.markdown` on GitHub), and the Opsgenie REST API
(`https://api.opsgenie.com`). Build mirrors the **PagerDuty** provider
(`internal/providers/pagerduty/`) — a flat, account-scoped, single-container REST provider
(a direct `net/http` client, no CLI, `terraform plan -generate-config-out` for export) —
plus the same **per-parent fan-out** pattern for its sub-resources. Opsgenie is the closest
sibling to PagerDuty: another alerting/on-call product, a non-`Bearer` auth scheme, a
US/EU region split, and several parent→child fan-outs. This is **REST, PagerDuty/Datadog-
style, NOT GraphQL.** Four facts set it apart from every prior provider, all load-bearing
and called out below:

1. **Auth is the literal `Authorization: GenieKey <api-key>` header** — *not* `Bearer`,
   *not* a bespoke `X-…-Key` header. The string `GenieKey ` (with the trailing space) is a
   real prefix, the Opsgenie analogue of PagerDuty's `Token token=`.
2. **Pagination is a `{data:[…],paging:{next:<full-url>},totalCount}` envelope with a
   SERVER-SUPPLIED next-URL cursor** — which must be **host-validated before the GenieKey
   is re-sent** (mirror `isFastlyURL` / Fastly's `links.next` concern), because it is not an
   HTTP redirect and Go will not strip the header.
3. **Import IDs are a MIX of bare UUIDs and SLASH composites** — and the composites do not
   even agree on the parent identifier (some use the parent *id*, one uses the *username*).
   The separator, the parent-id type, and the parent-id order are the #1 hazard.
4. **Two policy resources are TEAM-scoped OR GLOBAL** — `opsgenie_alert_policy` and
   `opsgenie_notification_policy` change both their enumeration (global list vs per-team
   fan-out) and their import id (bare vs `team_id/…`) depending on team attachment.

## Version pin (load-bearing)

Pin `opsgenie/opsgenie ~> 0.6` (org is lowercase `opsgenie`). **The provider is still
pre-1.0** — current is **0.6.40** (Oct 2025). This matters: a `~> 0.6` constraint pins the
minor (a 0.x provider can break on a minor bump), unlike the `~> 3.x` / `~> 3.0` pins used
for PagerDuty/Datadog. Naming facts that matter (the Terraformer-vs-current divergences):

- **Terraformer's coverage is a tiny subset.** Its generator covers only `user`, `team`,
  and `service` — three flat bare-id lists via the `opsgenie-go-sdk-v2` SDK. It does **not**
  cover `team_routing_rule`, `schedule` (+ `schedule_rotation`), `escalation`,
  `service_incident_rule`, `api_integration` (+ `integration_action`), `email_integration`,
  `alert_policy`, `notification_policy`, `notification_rule`, `maintenance`, `heartbeat`,
  `custom_role`, or `user_contact`. Those are covered here from the registry + REST API
  directly (as we did for the Datadog/PagerDuty resources Terraformer lacked).
- **Do NOT pull the `opsgenie/opsgenie-go-sdk-v2` SDK** the way Terraformer did — a raw
  `net/http` client is smaller and matches the PagerDuty/Datadog providers (a deliberate
  non-adoption, same call as dropping `heimweh/go-pagerduty`).
- Terraformer reads only `OPSGENIE_API_KEY`. The **TF provider** reads the *same*
  `OPSGENIE_API_KEY` (config `api_key`) plus `api_url` (`api.opsgenie.com` |
  `api.eu.opsgenie.com`) for the region. The REST endpoints below are
  provider-version-independent.
- **Team membership has no standalone resource.** The roster is an inline `member` block on
  `opsgenie_team` (`member { id = <user_uuid>, role = "admin"|"user" }`) — there is **no**
  `opsgenie_team_member` resource (contrast PagerDuty's separate `pagerduty_team_membership`).
  Emit the roster as part of the team; do not fan out a membership resource.

## Shape

- **Auth — the distinctive `Authorization: GenieKey <api-key>` header (the hard
  divergence).** Every request carries:
  - `Authorization: GenieKey <OPSGENIE_API_KEY>` — note the literal `GenieKey ` prefix
    (with the trailing space before the raw key). This is **not** `Bearer <token>` and
    **not** a custom `X-…-Key` header; getting the prefix wrong is a silent 401/403. It is
    the Opsgenie analogue of PagerDuty's `Token token=`.
  - `Content-Type: application/json` (on write; harmless on GET).
  Read the key from `OPSGENIE_API_KEY`. A direct `net/http` client (mirror `pagerdutyapi.go`
  / `datadogapi.go`); **no Opsgenie CLI**, and **no** `opsgenie-go-sdk-v2`. The key rides
  **only** on the `Authorization` header, **never** in the URL, errors, or logs (same
  discipline as `DD-API-KEY` / the PagerDuty token). Force `https`; refuse redirects (mirror
  `pdHTTPClient` / `datadogHTTPClient`) so the key cannot be replayed to another host on a
  3xx.
- **Base URL — US vs EU region, must be read from env.** Default `https://api.opsgenie.com`
  (US). The **EU region** is `https://api.eu.opsgenie.com` (an account provisioned in the EU
  cannot be reached on the US host and vice-versa — same as PagerDuty's US/EU split). The TF
  provider reads `api_url` = `"api.opsgenie.com"` | `"api.eu.opsgenie.com"`; mirror that with
  an env/config override (e.g. `OPSGENIE_API_URL`, fallback a region hint), default US. Store
  the resolved base once and **host-validate** it before sending the key; force `https` (the
  key is a bearer-equivalent secret).
  | region | base URL |
  |---|---|
  | US (default) | `https://api.opsgenie.com` |
  | EU | `https://api.eu.opsgenie.com` |
- **Scope — one Opsgenie account = one flat container.** The key is **account-scoped**;
  there is no sub-account and no multi-account resolution — the key simply **is** the
  account. `model.ScopeTenant`, `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
  Resolve the container id/name **best-effort** via a lightweight **`GET /v2/account`**
  (returns `{"data":{"name":…,"plan":{…}}}` — the account name/plan); if that endpoint is
  gated on some plans, fall back to **`GET /v2/users?limit=1`** for validation and use the
  API host string as the display id. This is as flat as PagerDuty: the key is the account,
  no id lookup required.
- **Response family — ONE dominant shape: the `data`/`paging.next`/`totalCount` envelope
  (the key structural fact).** Almost every list endpoint returns:
  ```
  { "data": [ … ], "paging": { "first":…, "last":…, "next":"<full-url>" }, "totalCount": n }
  ```
  — the array lives under a fixed **`data`** key (not a resource-named key like PagerDuty),
  and the pager is a **`paging.next` FULL-URL cursor**: follow `paging.next` until it is
  **absent** (single-page endpoints simply omit it). Implement one generic **data/paging.next**
  helper (the whole client surface for the common case) that fetches the first page with
  `?limit=100&offset=0` and then loops on `paging.next`. **Two envelope quirks to special-case:**
  - **`GET /v2/heartbeats` is NESTED** — it returns `{"data":{"heartbeats":[…]}}` (the
    array is under `data.heartbeats`, *not* `data`). Decode it separately.
  - **`GET /v2/schedules?expand=rotation`** embeds each schedule's rotations inline under
    `data[].rotations[]` — use the `expand` to avoid a per-schedule rotations call (see
    fan-outs).
- **`paging.next` is a SERVER-SUPPLIED URL — host-validate it before re-sending the key
  (do not miss this).** Unlike PagerDuty's offset/`more` (where *we* build every URL from
  `pdBase()+path`), Opsgenie hands back a **fully-qualified next URL** in `paging.next`.
  Because we then issue a GET to that URL with the `Authorization: GenieKey` header, a
  malicious/misconfigured next-link pointing at another host would leak the account key —
  and this is **not** an HTTP redirect, so `CheckRedirect` never fires and Go does not strip
  the header. **Validate `paging.next`'s scheme+host against the configured base before
  each follow** (mirror Fastly's `isFastlyURL` and the Datadog next-link caveat): require
  `https` and host ∈ {`api.opsgenie.com`, `api.eu.opsgenie.com`} (or exactly the resolved
  base host); refuse and error otherwise. This is the single most important safety rule of
  the client.
- **Pagination — per-endpoint (§ catalog).** The `paging.next` helper handles both paged and
  single-page endpoints uniformly (single-page just omits `paging.next`). Empirically:
  - **Paged (follow `paging.next`):** `GET /v2/users`, `GET /v2/services`,
    `GET /v2/integrations` (and, if ever needed, the alerts data plane — out of scope).
    These also accept `?limit=&offset=` for the first page; `totalCount` bounds the loop
    defensively.
  - **Single-page (`data` only, no `paging.next`):** `GET /v2/teams`, `GET /v2/schedules`,
    `GET /v2/escalations`, `GET /v2/maintenance`, `GET /v2/custom-user-roles`, the policy
    lists, and the per-parent sub-lists (routing-rules, rotations, incident-rules, actions,
    contacts, notification-rules). **VERIFY** at build — treat every list as *potentially*
    paged (always honour `paging.next` if present) so a large account is never truncated.
  Bound every loop defensively (`ogMaxPages`).
- **Status handling (mirror `list` / `pagerdutyAPIError`).** Opsgenie errors are
  `{"message":…,"took":…,"requestId":…}` (HTTP status carries the meaning). **401/403** →
  key invalid/insufficient scope → fatal, surfaced in preflight; if a 401/403 appears
  mid-run every remaining list fails too → treat as fatal, not a partial inventory. **404**
  → feature/endpoint absent on the plan → best-effort Verbose skip. **429** — Opsgenie
  rate-limits per key; honour `Retry-After` and back off. **5xx / network** → enumeration
  may be silently incomplete → Warn + count (tell a systemic failure apart from an empty
  account). The key never appears in errors/logs; strip any query string before a URL is
  logged (belt-and-suspenders, mirror `redactURL`/`redactPath`).
- **Preflight**: `terraform` present + `OPSGENIE_API_KEY` set + `GET /v2/account` returns
  200 (fallback `GET /v2/users?limit=1`). `/v2/account` doubles as the account-name/plan
  probe so later 404 skips (e.g. a resource absent on the plan) are explained rather than
  surprising.
- **Connect**: no real resolution — the key is the account. Validate `/v2/account` succeeds,
  resolve the account name from its `data.name` (best-effort; else host string), and set the
  single flat container (`model.ScopeTenant`).

## Composite import IDs + fan-out scope + team-scoping — the CRITICAL determination

This is Opsgenie's analogue of PagerDuty's "dot-vs-colon separator + fan-out" call and
Datadog's "which API version/shape" call. The load-bearing per-resource facts are **(a) is
the import id a BARE uuid/name or a SLASH composite, and — for composites — what is the
parent identifier (parent id vs username) and its order; (b) is the resource enumerated flat
or via a per-parent fan-out; and (c) for the two policy resources, is it team-scoped or
global** (which flips both (a) and (b)). Get (a) wrong and every import block for that type
is un-importable; get (b) wrong and you never reach the sub-resources; get (c) wrong and you
miss the team policies entirely (or emit a bad import id). All three are **verified against
the registry `website/docs/r/*.markdown`** and pinned per-resource in the catalog. The
rules:

- **Most resources import by a BARE id** — a UUID for most, a **name** for two:
  - Bare **UUID**: `opsgenie_team` (`team_id`), `opsgenie_user` (`user_id` — the id, **not**
    the username), `opsgenie_service` (`service_id`), `opsgenie_schedule` (`schedule_id`),
    `opsgenie_escalation` (`escalation_id`), `opsgenie_api_integration` (`integration_id`),
    `opsgenie_maintenance` (`policy_id`), `opsgenie_email_integration` (`id`).
  - Bare **NAME**: `opsgenie_heartbeat` imports by **`name`** (heartbeats are keyed by name
    in the Opsgenie API — there is no separate id). The Datadog `logs_index`-by-name analogue.
- **Several import by a SLASH composite `<parent>/<child>`** — all verified SLASH (never
  underscore), but the **parent identifier is NOT uniform**:
  - `opsgenie_schedule_rotation` = **`<schedule_id>/<rotation_id>`** (schedule-first).
  - `opsgenie_team_routing_rule` = **`<team_id>/<routing_rule_id>`** (team-first). *(The
    separator was in doubt — slash vs underscore — the registry confirms **slash**.)*
  - `opsgenie_service_incident_rule` = **`<service_id>/<service_incident_rule_id>`**
    (service-first).
  - `opsgenie_notification_rule` = **`<user_id>/<notification_rule_id>`** — parent is the
    **user _id_** (a UUID).
  - `opsgenie_user_contact` = **`<username>/<contact_id>`** — parent is the **username**
    (e.g. `jane@corp.com`), **NOT** the user id. ⚠️ This is the trap: the two per-user
    fan-outs use **different** parent identifiers — `notification_rule` keys on `user_id`,
    `user_contact` keys on `username`. Store both off the user object and pick the right one
    per child.
- **The two policy resources are TEAM-scoped OR GLOBAL — the (c) axis:**
  - `opsgenie_alert_policy`: `team_id` is **optional**. **Global** policy → import by the
    **bare `policy_id`**; **team** policy → import by **`<team_id>/<policy_id>`** (slash).
    Enumeration must cover *both* the global list and a per-team fan-out (below).
  - `opsgenie_notification_policy`: `team_id` is **required** → **always**
    **`<team_id>/<notification_policy_id>`** (slash). Enumerated **only** via the per-team
    fan-out (there is no global notification policy).
- **Two resources have NO documented Terraform import** — flag them, do not assume:
  - `opsgenie_integration_action` — the registry doc has **no Import section**. It is a
    **singleton per integration** (one resource with `integration_id` (required) holding all
    `create`/`close`/`acknowledge`/`add_note`/`ignore` action blocks). If importable at all
    it would key on the bare `integration_id`; **VERIFY importability at build** and, if
    unsupported, surface it as an honest gap (adopt the parent `api_integration`, author the
    actions by hand) rather than emitting an un-appliable import block.
  - `opsgenie_custom_role` — the registry doc has **no Import section** either. Args are
    `role_name` (required) + `extended_role` (`user`/`observer`/`stakeholder`). If importable
    it would key on the bare role id (`GET /v2/custom-user-roles` returns `data[].id`);
    **VERIFY** and treat as a gap if not.
- **Fan-outs (the PagerDuty per-parent pattern).** Each parent is a flat list, then a
  per-parent sub-list (or an embedded sub-array via `expand`):
  - **team → routing_rules**: `GET /v2/teams/<team_id>/routing-rules` →
    `opsgenie_team_routing_rule` (`<team_id>/<routing_rule_id>`).
  - **team → members**: **inline** on `opsgenie_team` (the `member` block) — **not** a
    fan-out resource. Members arrive on the team object; emit the block, do not create a
    membership resource.
  - **schedule → rotations**: `GET /v2/schedules?expand=rotation` embeds `data[].rotations[]`
    (preferred — one call), else `GET /v2/schedules/<schedule_id>/rotations` →
    `opsgenie_schedule_rotation` (`<schedule_id>/<rotation_id>`).
  - **service → incident_rules**: `GET /v1/services/<service_id>/incident-rules` →
    `opsgenie_service_incident_rule` (`<service_id>/<rule_id>`). **Note the `/v1/` path**
    (incident rules live on the v1 service API — **VERIFY**).
  - **api_integration → actions**: `GET /v2/integrations/<integration_id>/actions` →
    `opsgenie_integration_action` (singleton per integration; import undocumented — above).
  - **user → contacts**: `GET /v2/users/<user_id>/contacts` → `opsgenie_user_contact`
    (`<username>/<contact_id>` — parent is the **username**).
  - **user → notification_rules**: `GET /v2/users/<user_id>/notification-rules` →
    `opsgenie_notification_rule` (`<user_id>/<rule_id>` — parent is the **user id**).
  - **team (or global) → alert/notification policies**: `GET /v2/policies/alert` (global) +
    `GET /v2/policies/alert?teamId=<team_id>` (per team) → `opsgenie_alert_policy`;
    `GET /v2/policies/notification?teamId=<team_id>` (per team) → `opsgenie_notification_policy`.
    **VERIFY the exact policy path** (`/v2/policies/{type}` + `teamId` query) at build.

The slash separator, the parent-identifier type (id vs username), the parent-id order, and
the team-scoped-vs-global flip are the things we cannot get wrong — they are enumerated
per-resource in the catalog and re-verified against the registry docs at build. Encode the
import id as an explicit per-TF-type switch in `importid.go` (mirror PagerDuty's
`rawImportID` switch) — never infer the separator or the parent key.

## Enumeration spine

Flat account scope. The pattern is: a set of top-level `data`/`paging.next` lists, plus the
per-parent fan-outs above. Best-effort per list (404 feature-absent → Verbose skip; 401/403
→ fatal; other errors → Warn + count, so a systemic failure is told apart from an empty
account). Each list is tagged with its envelope + pager per the catalog. The GenieKey never
appears in errors/logs; every `paging.next` follow is host-validated first.

- **Top-level flat lists:**
  - `GET /v2/teams` → `data` → `opsgenie_team` (+ inline `member` roster, + routing-rules
    fan-out below, + per-team policy fan-out).
  - `GET /v2/users` → `data` (paged) → `opsgenie_user` (+ contacts / notification-rules
    fan-outs). Capture **both** `id` and `username` per user (the two fan-outs need
    different parents).
  - `GET /v2/schedules?expand=rotation` → `data` (rotations embedded) → `opsgenie_schedule`
    (+ `opsgenie_schedule_rotation` from `data[].rotations[]`).
  - `GET /v2/escalations` → `data` → `opsgenie_escalation`.
  - `GET /v2/services` → `data` (paged) → `opsgenie_service` (+ incident-rules fan-out).
  - `GET /v2/integrations` → `data` (paged) → `opsgenie_api_integration` (type `API`) **or**
    `opsgenie_email_integration` (type `Email`) — **discriminate on `type` and skip every
    other integration type** (Datadog/Prometheus/etc. vendor integrations carry vendor
    secrets and are out of scope; see below). Fan out to actions per API integration.
  - `GET /v2/policies/alert` → `data` → `opsgenie_alert_policy` (**global**); then per team
    `GET /v2/policies/alert?teamId=<team_id>` for **team** policies.
  - per team `GET /v2/policies/notification?teamId=<team_id>` → `opsgenie_notification_policy`
    (team-scoped only — no global list).
  - `GET /v2/maintenance?type=non-expired` → `data` → `opsgenie_maintenance`. **Filter to
    non-expired** — past maintenance windows are expired historical data, not durable config
    (the Opsgenie analogue of PagerDuty's open+future filter; the `type` query takes
    `all`/`past`/`upcoming`/`non-expired`).
  - `GET /v2/heartbeats` → **`data.heartbeats`** (nested envelope) → `opsgenie_heartbeat`
    (import by `name`).
  - `GET /v2/custom-user-roles` → `data` → `opsgenie_custom_role` (import undocumented —
    surface as gap if not importable).
- **Per-parent fan-outs:**
  - per **team**: `GET /v2/teams/<team_id>/routing-rules` → `opsgenie_team_routing_rule`
    (`<team_id>/<routing_rule_id>`); plus the per-team policy lists above.
  - per **schedule**: from the embedded `rotations[]` (or `…/rotations`) →
    `opsgenie_schedule_rotation` (`<schedule_id>/<rotation_id>`).
  - per **service**: `GET /v1/services/<service_id>/incident-rules` →
    `opsgenie_service_incident_rule` (`<service_id>/<rule_id>`).
  - per **api_integration**: `GET /v2/integrations/<integration_id>/actions` →
    `opsgenie_integration_action` (singleton per integration).
  - per **user**: `GET /v2/users/<user_id>/contacts` → `opsgenie_user_contact`
    (`<username>/<contact_id>`); `GET /v2/users/<user_id>/notification-rules` →
    `opsgenie_notification_rule` (`<user_id>/<rule_id>`).

If nothing was found AND lists failed with real (non-404) errors, surface a systemic failure
rather than shipping an empty inventory (same guard as PagerDuty/Datadog `enumerate.go`).

## Resource catalog

Import IDs verified against the current `opsgenie/opsgenie` registry docs
(`website/docs/r/*.markdown`). All scope = account. "endpoint → data" is the list path and
its envelope (`data` unless noted); "fan-out" marks a per-parent sub-list. The **sep** column
is the #1 hazard — **bare / slash**; the **parent** column names the composite's left token.

| native key | TF type | endpoint → envelope | fan-out | id field | import ID | sep / parent |
|---|---|---|---|---|---|---|
| opsgenie:team | opsgenie_team | `GET /v2/teams` → `data` | parent | `id` | `<team_id>` | bare uuid |
| opsgenie:team_routing_rule | opsgenie_team_routing_rule | `GET /v2/teams/<id>/routing-rules` → `data` | ← team | `id` | `<team_id>/<routing_rule_id>` | **slash** (team-first) |
| opsgenie:user | opsgenie_user | `GET /v2/users` → `data` (paged) | parent | `id` | `<user_id>` | bare uuid (**id, not username**) |
| opsgenie:user_contact | opsgenie_user_contact | `GET /v2/users/<id>/contacts` → `data` | ← user | `id` | `<username>/<contact_id>` | **slash** (**username**-first) |
| opsgenie:notification_rule | opsgenie_notification_rule | `GET /v2/users/<id>/notification-rules` → `data` | ← user | `id` | `<user_id>/<rule_id>` | **slash** (**user_id**-first) |
| opsgenie:schedule | opsgenie_schedule | `GET /v2/schedules?expand=rotation` → `data` | parent | `id` | `<schedule_id>` | bare uuid |
| opsgenie:schedule_rotation | opsgenie_schedule_rotation | `data[].rotations[]` (via expand) | ← schedule | `id` | `<schedule_id>/<rotation_id>` | **slash** (schedule-first) |
| opsgenie:escalation | opsgenie_escalation | `GET /v2/escalations` → `data` | — | `id` | `<escalation_id>` | bare uuid |
| opsgenie:service | opsgenie_service | `GET /v2/services` → `data` (paged) | parent | `id` | `<service_id>` | bare uuid |
| opsgenie:service_incident_rule | opsgenie_service_incident_rule | `GET /v1/services/<id>/incident-rules` → `data` | ← service | `id` | `<service_id>/<rule_id>` | **slash** (service-first, **v1 path**) |
| opsgenie:api_integration | opsgenie_api_integration | `GET /v2/integrations` → `data` (type=API) | parent | `id` | `<integration_id>` | bare uuid (**api_key SECRET**) |
| opsgenie:email_integration | opsgenie_email_integration | `GET /v2/integrations` → `data` (type=Email) | — | `id` | `<id>` | bare uuid |
| opsgenie:integration_action | opsgenie_integration_action | `GET /v2/integrations/<id>/actions` | ← api_integration | — | *(no documented import — VERIFY; likely `<integration_id>`)* | singleton per integration |
| opsgenie:alert_policy | opsgenie_alert_policy | `GET /v2/policies/alert` (+ `?teamId=`) | ← team (opt.) | `id` | **global** `<policy_id>` / **team** `<team_id>/<policy_id>` | bare **OR** slash |
| opsgenie:notification_policy | opsgenie_notification_policy | `GET /v2/policies/notification?teamId=<id>` | ← team (req.) | `id` | `<team_id>/<notification_policy_id>` | **slash** (team-first, team **required**) |
| opsgenie:maintenance | opsgenie_maintenance | `GET /v2/maintenance?type=non-expired` → `data` | — | `id` | `<policy_id>` | bare uuid |
| opsgenie:heartbeat | opsgenie_heartbeat | `GET /v2/heartbeats` → **`data.heartbeats`** | — | `name` | `<name>` | bare **name** (nested envelope) |
| opsgenie:custom_role | opsgenie_custom_role | `GET /v2/custom-user-roles` → `data` | — | `id` | *(no documented import — VERIFY; likely `<role_id>`)* | bare uuid? |

### Import-format quirks (§ do not get wrong)
1. **SLASH is the only composite separator — but the parent identifier is NOT uniform.**
   Every composite verified above uses a **forward slash** (`/`), never an underscore (the
   `team_routing_rule` slash-vs-underscore doubt is resolved: **slash**). The trap is the
   *left* token: `notification_rule` = `<user_id>/…` (a UUID), while `user_contact` =
   `<username>/…` (an email-shaped username) — **two per-user children, two different
   parents.** Encode the parent key per TF type; never reuse the user id for a contact.
2. **`opsgenie_user` imports by the user _id_ (UUID), not the username** — even though
   `user_contact` and the human-readable object key on the username. Copy the `id` field.
3. **`opsgenie_heartbeat` imports by NAME** — the heartbeat name *is* the id (no separate
   uuid), and its list endpoint is the odd **nested** `{"data":{"heartbeats":[…]}}` envelope.
   Both facts are heartbeat-specific.
4. **The two policy resources flip on team-scope.** `opsgenie_alert_policy` is **bare
   `<policy_id>` when global** and **`<team_id>/<policy_id>` when team-attached** — decide
   per policy from whether it came off the global list or a `?teamId=` fan-out (and whether
   the object carries a `teamId`). `opsgenie_notification_policy` is **always**
   `<team_id>/<notification_policy_id>` (team is required). Getting the global-vs-team branch
   wrong yields an un-importable id.
5. **`opsgenie_integration_action` and `opsgenie_custom_role` have no documented import.**
   Do not emit a speculative import block that `terraform apply` will reject — VERIFY
   importability at build; if unsupported, surface as an honest gap (adopt the parent /
   author by hand). `integration_action` is additionally a **singleton per integration** (one
   resource, many action blocks) — do not split it per action.
6. **All uuids are opaque strings off the wire** (no numeric stringify step, unlike Datadog's
   numeric monitor ids) — copy `data[].id` verbatim.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a
live account — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the
PagerDuty/Datadog providers. Opsgenie has **no single monster resource** (contrast Datadog's
`datadog_dashboard`); the weight is spread across the routing/rule trees, and the recurring
hazards are the **integration api_key secret**, the **policy/rule filter trees**, and the
**schedule/rotation time fields**.

- **`opsgenie_api_integration` — SECRET (scrub).** `api_key` is a **computed** attribute
  returned on read — it *is* the Events-API credential used to fire alerts into Opsgenie →
  **scrub the value**, keep the block. This is the most common secret in an Opsgenie
  adoption (the PagerDuty `integration_key` analogue). `type` (`API`) and `responders`
  (team/user/schedule/escalation refs) are the config surface. Non-`API`/`Email` integration
  types carry vendor credentials → not enumerated (below).
- **`opsgenie_team` — light, but roster + refs.** `name`, `description`, inline `member`
  blocks (`id` = member UUID, `role` = `admin`/`user`). Prune computed. **CAUTION:** your own
  user (behind the key) likely appears in a team roster — adopt but do not lock yourself out.
- **`opsgenie_team_routing_rule` — medium.** `criteria` (match-all / match-any-condition
  trees), `notify` (target ref), `timeRestriction`. Order is significant (routing precedence)
  — preserve it. Refs to escalations/schedules.
- **`opsgenie_user` / `opsgenie_user_contact` / `opsgenie_notification_rule` — light, PII not
  secret.** User: `username` (email), `full_name`, `role`, `locale`, `timezone`. Contact:
  `method` (`email`/`sms`/`voice`/`mobile`), `to` (phone/email) — **PII, not a credential**
  (adopt, do not over-scrub). Notification rule: `action_type`, `notification_time`,
  `steps`/`criteria`. **CAUTION:** the key-owner user appears — adopt but do not disable it.
- **`opsgenie_schedule` / `opsgenie_schedule_rotation` — medium; time-field drift.** Schedule:
  `timezone` (required), `enabled`, `owner_team_id`. Rotation: `type`
  (`daily`/`weekly`/`hourly`), `start_date`/`end_date` (RFC3339), `participants` (ordered
  user/team/escalation refs), `time_restriction`. **The `start_date`/`end_date` timestamps
  can drift** (server normalizes the timezone/format) — a known perpetual-diff hazard; may
  need `ignore_changes` or tolerance (same class as PagerDuty's schedule `start` drift).
  Prune computed rotation `id`.
- **`opsgenie_escalation` — light/medium.** `rules[]` (each with `condition`,
  `notify_type`, `delay`, `recipient` refs to users/schedules/teams), `owner_team_id`,
  `repeat`. Rule ordering is significant. Prune computed.
- **`opsgenie_service` / `opsgenie_service_incident_rule` — medium.** Service: `name`,
  `team_id` (ref, required), `description`. Incident rule: `condition_match_type`,
  `conditions[]` (field/operator/expected-value match trees), `incident_properties`
  (message/priority/tags/details) — **template hazard:** incident message/description can
  carry `{{…}}` Opsgenie alert-field placeholders → the generated HCL must keep these
  **literal** (same class as PagerDuty PCL / Datadog widget-query escaping — verify
  terraform's writer does not interpolate `${…}`/`%{…}`).
- **`opsgenie_alert_policy` / `opsgenie_notification_policy` — medium; team-scope + filters.**
  `filter` (match-all / match-any-condition trees), `message`/`tags`/`priority`
  transformations (alert policy), `auto_close_action`/`auto_restart_action`/`de_duplication`
  (notification policy), `time_restrictions`. Emit `team_id` when team-scoped (drives the
  import id). Same `{{…}}` placeholder hazard in message templates. `enabled` carried
  explicitly. Order/`policy_order` may churn.
- **`opsgenie_integration_action` — medium; no secret.** One resource per integration holding
  `create`/`close`/`acknowledge`/`add_note`/`ignore` blocks, each with `filter` + field
  mappings and `{{…}}` placeholders (literal-string hazard). Bound to `integration_id`
  (ref). No import (above) — adopt the parent integration, author actions by hand if the
  gap stands.
- **`opsgenie_maintenance` — light; staleness.** `description`, `time` (`type`
  `for-5-minutes`/`indefinitely`/`schedule` + `start_date`/`end_date`), `rules[]` (entity
  refs). Filter enumeration to `non-expired` — a **past** window is immutable expired data
  with no adoption value.
- **`opsgenie_heartbeat` — trivial; NO secret.** `name`, `description`, `interval` +
  `interval_unit`, `enabled`, `owner_team_id`, `alert_message`/`alert_priority`/`alert_tags`.
  The registry confirms **no api_key / token / ping-URL / credential attribute** is exposed —
  adopt as plain config. Import by `name`. (Grouped with the alert-ingestion plane for
  caution, but it is not itself secret — see the secret section for the honest classification.)
- **`opsgenie_custom_role` — light; IAM-ish breadth.** `role_name`, `extended_role`
  (`user`/`observer`/`stakeholder`), `granted_rights`/`disallowed_rights`. No secret. No
  documented import (above).

Until Phase B these are no-ops, so an Opsgenie export is a breadth scaffold, not yet
plan-clean (the pipeline's repo-wide secret scan is the backstop for the `api_integration`
`api_key` before the scrub rule below lands).

## Write-only / secret resources (EXCLUDE / scrub)

The credential/integration plane is where Opsgenie's secrets live — scrub the value (keep the
block, re-supply out-of-band) or exclude the resource, exactly like PagerDuty's
`service_integration.integration_key` / Datadog's `datadog_api_key`:

- **`opsgenie_api_integration.api_key`** — the computed **Events-API key**, live credential
  material returned on read, used to send alerts into Opsgenie → **scrub the value**, keep
  the integration block. This is the single most important scrub of the provider.
- **Non-`API`/`Email` integration types (the vendor-integration credential plane) — EXCLUDE
  from enumeration.** `GET /v2/integrations` also returns Datadog/Prometheus/Zabbix/Nagios/
  Amazon-SNS/… integration objects that carry vendor-side credentials, webhook secrets, or
  callback tokens, and map to type-specific or unsupported TF resources. **Discriminate on
  `type` and adopt only `API` and `Email`**; surface the rest as out-of-band (not adopted).
- **`opsgenie_integration_action`** — no secret of its own, but bound to an
  `api_integration`; its value is only meaningful with the scrubbed parent key. Adopt the
  shell (subject to the no-import caveat).
- **`opsgenie_heartbeat` — NOT actually secret (honest correction).** It was grouped with the
  alert-ingestion/liveness plane out of caution, but the registry confirms the resource
  exposes **no api_key, token, or ping-URL attribute** — only name/interval/enabled/owner/
  alert-metadata. **Adopt it as plain config** (import by `name`); there is nothing to
  scrub. If a future provider version ever surfaces a computed ping-URL containing the
  account key, scrub only that field.
- **Not secret, do not over-scrub:** `opsgenie_user_contact` (phone/email are PII, not
  credentials — adopt), `opsgenie_email_integration` (the ingest email address is not a
  secret — the PagerDuty `integration_email` analogue), `opsgenie_custom_role` (rights lists,
  no secret).

## Deliberately out of scope
- **The vendor-integration plane** (every `GET /v2/integrations` object whose `type` is not
  `API`/`Email` — Datadog, Prometheus, Zabbix, Amazon SNS, etc.) — a large, credential-bearing
  plane that maps to type-specific/unsupported resources. Excluded above; a much-later
  increment at best, not core on-call config.
- **`opsgenie_incident_template`** — an incident-template resource exists in the provider
  (adoptable config, no secret) but is a separate incident-management surface from the
  alerting/on-call core; a later increment.
- **Alerts / incidents / notifications DATA plane** — live alerts, incidents, alert logs,
  who's-on-call rendering, and notification history are runtime DATA behind the config, per
  scope. Out of scope (config only).
- **`opsgenie_notification_rule_step` / on-call schedule overrides / schedule final
  rendering** — sub-step and runtime-override objects; the schedule/notification-rule shells
  are adopted, but overrides and rendered on-call are runtime data.
- **Account / plan / billing settings, SSO/SAML, and team-hierarchy management**
  (`Capabilities.IAM=false`) — users/teams/custom-roles are modeled at breadth, but identity
  federation, license assignment, and account-level settings are not.
- **Opsgenie SDK dependency** — Terraformer pulls `opsgenie/opsgenie-go-sdk-v2`; TerraLift
  uses a raw `net/http` client (smaller, matches PagerDuty/Datadog). A deliberate
  non-adoption.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `opsgenie_team` + `opsgenie_user` + `opsgenie_schedule` + `opsgenie_escalation`
(the on-call routing core nearly every Opsgenie account manages as IaC — all four are simple
flat bare-uuid lists; team establishes the inline `member` roster and the first per-parent
fan-out surface, and the four anchor the refs every other resource points at) → INC-1
`opsgenie_schedule_rotation` + `opsgenie_team_routing_rule` (the first SLASH composites and
the schedule `expand=rotation` embed; establishes the per-parent fan-out + the
`<parent_id>/<child_id>` import machinery, both team-first/schedule-first) → INC-2
`opsgenie_service` + `opsgenie_service_incident_rule` + `opsgenie_api_integration` +
`opsgenie_integration_action` (services + the integration plane; api_integration establishes
the **`api_key` secret-scrub** — the provider's defining secret — the `type`-discriminated
integration list, and the singleton-per-integration action fan-out; incident_rule adds the
v1 service path) → INC-3 `opsgenie_user_contact` + `opsgenie_notification_rule` (the two
per-user fan-outs — the trap where `user_contact` keys on **username** and
`notification_rule` keys on **user_id**) → INC-4 `opsgenie_alert_policy` +
`opsgenie_notification_policy` (the **team-scoped-vs-global** flip — the global list + the
per-team `?teamId=` fan-out, and the bare-vs-`team_id/…` import branch) → INC-5
`opsgenie_maintenance` + `opsgenie_heartbeat` + `opsgenie_custom_role` +
`opsgenie_email_integration` (the tail — the `non-expired` maintenance filter, the
heartbeat import-by-name + nested `data.heartbeats` envelope, and the two no-documented-import
resources `custom_role`/`integration_action` to VERIFY/gap) → LATER `opsgenie_incident_template`,
the vendor-integration plane (type ≠ API/Email, secret-bearing), and the alerts/incidents
data planes.
