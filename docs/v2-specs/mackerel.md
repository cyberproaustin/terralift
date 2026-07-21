# Mackerel provider — build spec

Research artifact for the `mackerel` provider (Phase A scaffold; TF provider source is
**`mackerelio-labs/mackerel`** — note the **`-labs`**, see Version pin — the Terraform provider for
**Mackerel**, Hatena's SaaS server/service monitoring platform). Sources: Terraformer's
`providers/mackerel/` (built on the official `github.com/mackerelio/mackerel-client-go` Go SDK — a
per-resource generator set: service, role, monitor, channel, notification_group, aws_integration,
downtime, alert_group_setting), the `mackerelio-labs/mackerel` registry docs (import formats +
schema, **verified per-resource below** against the provider repo's `docs/resources/*.md`), and the
Mackerel REST API (`https://api.mackerelio.com/api/v0/…`). Build mirrors the **Logz.io** provider
(`internal/providers/logzio/`) — a **flat, single-container, custom-header** REST provider driven by
a direct `net/http` client (NOT a CLI or the vendor SDK) — with **one structural wrinkle beyond
Logz.io, and it is the load-bearing one:**

1. **Auth is a custom header exactly like Logz.io's** — Mackerel reads **`X-Api-Key: <key>`** on
   every request (§ Shape). One flat org container, one vendor base URL (no region table).
2. **Unlike Logz.io — which was ALL bare-token imports and NO fan-out — Mackerel has a
   service→role(→metadata) FAN-OUT and genuine COMPOSITE import ids.** Roles are enumerated
   per-service (the LaunchDarkly/Keycloak project→child pattern), and the role import id is the
   **colon composite `<service>:<role>`**; role-metadata is a **two-level fan-out** with a
   **`<service>:<role>/<namespace>` colon+slash** import. This is the single hazard that separates
   Mackerel from Logz.io — get the separators (and which resources are composite) wrong and every
   role/metadata import block is un-importable (§ CRITICAL).

## Version pin (load-bearing)

Pin `mackerelio-labs/mackerel ~> 0.x` (current is **0.9.1**, published 2026-04; **still pre-1.0 —
VERIFY the current major/minor at build**; source `mackerelio-labs/mackerel`). Naming facts that
matter (the Terraformer-vs-current divergences):

- **The registry source is `mackerelio-labs/mackerel`, NOT `mackerelio/mackerel`.** The task brief
  and older references say `mackerelio/mackerel`; the live registry namespace is **`mackerelio-labs`**.
  Emit `source = "mackerelio-labs/mackerel"` in `providers.tf`. (Both the provider and Terraformer
  build on the same underlying `mackerelio/mackerel-client-go` **Go SDK** — that's the `mackerelio`
  org — but the *Terraform provider* is `mackerelio-labs`.) VERIFY the namespace at build.
- **Terraformer INLINES the API key into the provider block** — `GetConfig()` returns
  `"api_key": cty.StringVal(p.apiKey)`, writing the key straight into HCL. **This is a secret leak.**
  TerraLift MUST NOT inline the key; the emitted `providers.tf` authenticates via the
  `MACKEREL_APIKEY` env var only (mirror Logz.io's `providers.tf` note). This is the #1 "do not copy
  Terraformer" item.
- **Terraformer's resource set is 8 generators** (service, role, monitor, channel,
  notification_group, aws_integration, downtime, alert_group_setting) — it does **not** cover
  `mackerel_dashboard`, `mackerel_service_metadata`, `mackerel_role_metadata`, or
  `mackerel_default_notification_group`, which exist in the current provider and are covered here
  from the registry + API directly.
- **There is NO `mackerel_host` and NO `mackerel_user` resource** in the provider (the task brief
  lists them; they don't exist). Hosts are **agent-registered** (the mackerel-agent POSTs them; not
  a durable IaC object) and users are managed by **invitation**, not a create/delete resource — both
  correctly out of scope (§ Out of scope). Do not try to emit them.
- **Terraformer reads `MACKEREL_API_KEY`** only; the **provider** reads **`MACKEREL_APIKEY`** (primary)
  **or `MACKEREL_API_KEY`** (alias) for `api_key`, and **`API_BASE`** for the `api_base` endpoint
  override. TerraLift should accept `MACKEREL_APIKEY` (primary) with `MACKEREL_API_KEY` as an alias,
  and an optional `MACKEREL_API_BASE` override (§ Shape). The REST endpoints below are
  provider-version-independent.

## Shape

- **Auth — the `X-Api-Key` header (the same custom-header shape as `logzioapi.go`'s `X-API-TOKEN`).**
  Mackerel authenticates with `X-Api-Key: <api-key>` on **every** request (confirmed exact casing
  from `mackerel-client-go/mackerel.go`: `req.Header.Set("X-Api-Key", c.APIKey)`), plus
  `Content-Type: application/json` / `Accept: application/json`. This is an **organization API key**
  — read it from **`MACKEREL_APIKEY`** (accept `MACKEREL_API_KEY` as an alias). NOT
  `Authorization: Bearer`, NOT a query param. The key rides **only** on the `X-Api-Key` header —
  never in the URL, query, request body, errors, logs, config, or state (redact any URL that ever
  appears in a message, belt-and-suspenders, mirror `logzioapi.go`'s `redactURL`). A direct
  `net/http` client (mirror `logzioapi.go`); **no `mkr` CLI, no `mackerel-client-go` SDK** (a raw
  client is smaller and matches Logz.io). The TF provider reads the same key from `api_key` /
  `MACKEREL_APIKEY` / `MACKEREL_API_KEY`.
- **Base URL — ONE vendor host, no region table (simpler than Logz.io's regional map).** Default
  **`https://api.mackerelio.com`** (confirmed `defaultBaseURL = "https://api.mackerelio.com/"`).
  There is **no `.co.jp` variant** — the docs offer a Japanese-*language* page
  (`mackerel.io/ja/api-docs/`) but the **same** API host. There **is** an enterprise/managed
  variant, **KCPS Mackerel** (`https://kcps-mackerel.io`), reachable via an override. Resolve the
  base once: default `https://api.mackerelio.com`, overridable via **`MACKEREL_API_BASE`** (or the
  provider's `API_BASE`) for KCPS / a proxy. Force **https** (upgrade a bare host / explicit
  `http://`, mirror Logz.io's `forceHTTPS`) so the key is never sent in cleartext; guard the host
  charset (reject `@`/path/port-splice, like Logz.io's `validRegion` guard on the interpolated
  segment). Use a **redirect-refusing** client (mirror `lzHTTPClient` — Go does NOT strip a custom
  `X-Api-Key` header on a cross-host 3xx, so an auto-followed redirect would leak the key). URLs are
  always built from `base+path`; we never follow a server-supplied next-link.
- **Scope: one Mackerel ORGANIZATION = one flat container.** The `X-Api-Key` is **org-scoped** — it
  *is* the organization. No sub-org resolution, no multi-tenant lookup. One flat container = the org
  (`model.ScopeTenant`). Services are a **fan-out key, not a container tree** (like LaunchDarkly's
  projects / Keycloak's realms) — roles and metadata live *under* services but that is an enumeration
  detail, so `Capabilities.Hierarchy` stays **false**. There is a `GET /api/v0/org` endpoint that
  returns the org name (`{"name":"…"}`) — use it best-effort for the container id/name; fall back to
  the base-URL host if it 403s (a restricted key may lack org read). `Capabilities{IAM:false,
  Exposure:false, Hierarchy:false}`.
- **Response family — an OBJECT wrapping ONE named array per endpoint (the key structural fact;
  unlike Logz.io's three-way GET/POST-search/singleton mix).** Every Mackerel list endpoint is a
  plain `GET` returning `{"<key>":[ … ]}` — a single named array in an object envelope. **The
  envelope key is NOT uniform** across endpoints (mostly the plural lowercase resource name, but two
  camelCase and one snake_case — see the catalog); pin it per endpoint. No POST-search, no bare-array
  plane. Unmarshal into a per-endpoint envelope struct keyed on the exact JSON key. Confirmed keys
  (from `mackerel-client-go`):
  | endpoint | envelope key |
  |---|---|
  | `GET /api/v0/services` | `services` |
  | `GET /api/v0/services/<svc>/roles` | `roles` |
  | `GET /api/v0/monitors` | `monitors` |
  | `GET /api/v0/channels` | `channels` |
  | `GET /api/v0/notification-groups` | `notificationGroups` **(camelCase)** |
  | `GET /api/v0/dashboards` | `dashboards` |
  | `GET /api/v0/aws-integrations` | `aws_integrations` **(snake_case — the odd one)** |
  | `GET /api/v0/downtimes` | `downtimes` |
  | `GET /api/v0/alert-group-settings` | `alertGroupSettings` **(camelCase)** |
  | `GET /api/v0/services/<svc>/metadata` | `metadata` (elements are `{"namespace":"…"}`) |
  | `GET /api/v0/services/<svc>/roles/<role>/metadata` | `metadata` |
  **Do NOT assume a uniform envelope name** — `aws_integrations` is snake_case while
  `notificationGroups`/`alertGroupSettings` are camelCase and the rest are plain plural. This is the
  Mackerel analogue of Logz.io's per-endpoint shape VERIFY, but narrower (always a named-array
  object).
- **Pagination — NONE for the config core (a simplification over Logz.io's per-resource pagers).**
  Every config-plane list above returns the **full set in one call** (no `limit`/cursor). The only
  Mackerel endpoints that page are **`/api/v0/hosts`** (host list) and **`/api/v0/alerts`** (a
  `nextId` cursor) — **both are in the deferred DATA plane**, so Phase A needs **no pager**. Still,
  bound any accidental loop defensively if one is ever added (`mkMaxItems`), and re-VERIFY at build
  that none of the config lists silently cap.
- **Monitors are POLYMORPHIC — one list, one TF type, seven `type` variants.** `GET /api/v0/monitors`
  returns `{"monitors":[…]}` where each element carries a **`type` discriminator**:
  `host` / `connectivity` / `service` / `external` / `expression` / `anomalyDetection` / `query`
  (confirmed constants in `monitors.go`). **All seven map to the single `mackerel_monitor` TF type**
  (its schema has a nested block per variant) and **all import by the bare monitor `id`** — so unlike
  Keycloak/Okta discriminators (which pick different TF types), the monitor discriminator does **not**
  change the TF type or the import shape. Decode each element tolerantly by `id` (+ `name`/`type` for
  the label); we never need the variant-specific fields for enumeration/import. (Mirror Logz.io's
  tolerant `lzObj` — read only id + a display field.)
- **Status handling (mirror `logzioapi.go` / `enumerate.go`'s `list`).** Mackerel errors are
  `{"error":{"message":"…"}}` (HTTP status carries the meaning). **401** (bad/absent/revoked key) →
  fatal, surfaced in preflight (and if it appears mid-enumeration, fatal — every remaining list will
  fail). **403** (the key lacks the capability — Mackerel keys have per-key **Read/Write/Monitor**
  scopes, so a read-only key or one without a scope 403s on the relevant list) → best-effort **Verbose
  skip**. **404** (feature/endpoint absent) → Verbose skip. **429 / 5xx / network** → enumeration may
  be silently incomplete → **Warn + count** (tell a systemic failure apart from an empty org). The key
  never appears in errors/logs.
- **Preflight**: `terraform` present + `MACKEREL_APIKEY` (or `MACKEREL_API_KEY`) set + one
  **lightweight org-scoped list succeeds**. Use **`GET /api/v0/services`** as the probe (cheap, always
  present, needs only the base Read scope); a 200 confirms the key + base, a 401 is a bad key. There
  is no dedicated `/validate` endpoint; a real list is the auth check. A 403 on a *scoped* endpoint is
  NOT an auth failure — probe an always-available endpoint.
- **Connect**: no real resolution — the key *is* the org. Validate the `GET /api/v0/services` probe
  succeeds, best-effort `GET /api/v0/org` for the container name (fall back to the base-URL host), and
  set the single flat container.

## Service FAN-OUT + role/metadata COMPOSITE import — the CRITICAL determination

This is Mackerel's analogue of Logz.io's "which list mechanism / which bare-token form" call, but the
hazard is **inverted**: Logz.io had zero composites and zero fan-out; Mackerel's whole risk surface is
**(a) which resources need a service (or service→role) FAN-OUT to enumerate, and (b) which import ids
are COMPOSITES and with WHICH separator.** Get (a) wrong and you never reach roles/metadata; get (b)
wrong and every role/metadata import block is un-importable. Both are pinned per-resource in the
catalog and re-verified against the registry `docs/resources/*.md`.

1. **Fan-out depth — org-level vs service-scoped vs service×role-scoped.**
   - **Org-level (one flat `GET`):** services, monitors, channels, notification-groups, dashboards,
     aws-integrations, downtimes, alert-group-settings. No fan-out.
   - **Service-scoped (one-level fan-out):** roles (`GET /api/v0/services/<svc>/roles`) and
     service-metadata (`GET /api/v0/services/<svc>/metadata`). First list services, then per service
     the sub-list — exactly LaunchDarkly's project fan-out. (NB: the `services` list objects *also*
     carry a `roles: []string` field, so role **names** are available without the fan-out; but the
     per-service `FindRoles` call returns the role **objects** with `memo` — prefer the fan-out for a
     clean role object, matching Terraformer's `FindServices`→`FindRoles`.)
   - **Service×role-scoped (TWO-level fan-out):** role-metadata
     (`GET /api/v0/services/<svc>/roles/<role>/metadata`) — services → per service its roles → per
     role its metadata. The Keycloak realm×client precedent.
2. **Import id form — bare token vs colon composite vs colon+slash composite (the #1 hazard).**
   - **Bare token** (a single id/name, `util.EscapeHCLTemplate` then emit): `mackerel_service`
     (the service **name**), and every org-level object by its **id** (`mackerel_monitor`,
     `mackerel_channel`, `mackerel_notification_group`, `mackerel_dashboard`,
     `mackerel_aws_integration`, `mackerel_downtime`, `mackerel_alert_group_setting`). Mackerel ids
     are **opaque strings** off the wire (e.g. `ABCDEFG`, `2qtozU21abc`) — copy verbatim, **no numeric
     stringify** (unlike Logz.io/Datadog).
   - **Colon composite `<service>:<role>`** — `mackerel_role`. Confirmed:
     `terraform import mackerel_role.foo foo:bar`. Terraformer builds the same
     (`fmt.Sprintf("%s:%s", serviceName, roleName)`). **The separator is a colon `:`.**
   - **Colon+slash composite `<service>:<role>/<namespace>`** — `mackerel_role_metadata`. Confirmed
     doc: "imported using their `<service_name>:<role_name>/<metadata>`", example `foo:bar/bar`.
     (The example command text says `mackerel_role.foo` — a doc copy-paste bug; the **ID form**
     `foo:bar/bar` is what matters.)
   - **`mackerel_service_metadata` — CONFLICTING docs, must VERIFY.** The prose says
     "imported using their **`<service_name>/<namespace>`**" (slash) but the example shows
     **`foo:bar`** (colon). By analogy with role-metadata's `service:role/namespace`, the colon form
     `<service>:<namespace>` is the more likely truth — **but this is a hard VERIFY on live import;
     encode whichever the provider actually accepts.** Flag prominently.
   - **`mackerel_default_notification_group` — a SINGLETON**, adopt-in-place (see below); its import
     token is **VERIFY** (the API type is the fixed `group-default`; the provider "does not create or
     delete" it). Likely a fixed constant import — pin against the registry.
3. **NO discriminator changes the TF type.** The monitor `type` field selects a nested block within
   the one `mackerel_monitor` type, not a different type (contrast Keycloak/Okta). Enumeration reads
   only `id`+`name`.

Encode the import id as an explicit per-TF-type switch in `importid.go` (mirror Okta's `rawImportID`) —
**never infer the separator or the part-count.** Bare / colon / colon+slash are three distinct forms;
the whole composite is `EscapeHCLTemplate`-wrapped before emit (parity with the other providers).

## Enumeration spine

Flat org scope. The spine is a **service fan-out**: org-level lists in one pass, then per service its
roles (and service-metadata), then per (service, role) its role-metadata. Best-effort per list (403
scope-absent / 404 feature-absent → Verbose skip; 401 → fatal; other → Warn + count, so a systemic
failure is told apart from an empty org). The key never appears in errors/logs. (Mirror
`logzio/enumerate.go`'s `list` helper for the top-level lists; a `subList`-style helper for the fan-out
that does NOT bump the systemic-fail count, since sub-lists multiply by service/role count — the
LaunchDarkly pattern.)

- **Org-level (one `GET` each, envelope key in the catalog):**
  - `GET /api/v0/services` → `{"services":[{name, roles:[…], memo}]}` → `mackerel_service` (bare import
    `<name>`). **Capture each service `name`** — it is the fan-out key for roles + service-metadata.
  - `GET /api/v0/monitors` → `{"monitors":[{id, name, type}]}` → `mackerel_monitor` (bare import `<id>`;
    `type` selects the block but not the TF type).
  - `GET /api/v0/channels` → `{"channels":[{id, name}]}` → `mackerel_channel` (bare `<id>`).
  - `GET /api/v0/notification-groups` → `{"notificationGroups":[{id, name}]}` →
    `mackerel_notification_group` (bare `<id>`).
  - `GET /api/v0/dashboards` → `{"dashboards":[{id, title}]}` → `mackerel_dashboard` (bare `<id>` —
    **import undocumented, VERIFY**).
  - `GET /api/v0/aws-integrations` → `{"aws_integrations":[{id, name}]}` → `mackerel_aws_integration`
    (bare `<id>`).
  - `GET /api/v0/downtimes` → `{"downtimes":[{id, name}]}` → `mackerel_downtime` (bare `<id>`).
  - `GET /api/v0/alert-group-settings` → `{"alertGroupSettings":[{id, name}]}` →
    `mackerel_alert_group_setting` (bare `<id>`).
- **Per service `<svc>` (one-level fan-out):**
  - `GET /api/v0/services/<svc>/roles` → `{"roles":[{name, memo}]}` → `mackerel_role`
    (**colon composite `<svc>:<role>`**).
  - `GET /api/v0/services/<svc>/metadata` → `{"metadata":[{namespace}]}` → `mackerel_service_metadata`
    (composite `<svc>:<namespace>` **/ `<svc>/<namespace>` — VERIFY separator**). **DEFER to INC-2.**
- **Per (service `<svc>`, role `<role>`) (two-level fan-out):**
  - `GET /api/v0/services/<svc>/roles/<role>/metadata` → `{"metadata":[{namespace}]}` →
    `mackerel_role_metadata` (**colon+slash composite `<svc>:<role>/<namespace>`**). **DEFER to INC-2.**
- **Singleton:** `mackerel_default_notification_group` (the fixed `group-default`) — adopt-in-place,
  **DEFER** (import token VERIFY).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic failure
rather than shipping an empty inventory (same guard as `logzio/enumerate.go`).

## Resource catalog

Import IDs verified against the current `mackerelio-labs/mackerel` registry docs
(`docs/resources/*.md`). All scope = org. "list" = the enumeration mechanism; "fan-out" names the
parent. The **sep** column is the #1 hazard — **bare / colon / colon+slash**.

| native key | TF type | list endpoint → envelope | fan-out | id field | import ID | sep |
|---|---|---|---|---|---|---|
| mackerel:service | mackerel_service | `GET /api/v0/services` → `services` | — (parent) | `name` | `<name>` | **bare** |
| mackerel:role | mackerel_role | `GET …/services/<svc>/roles` → `roles` | ← service | `name` | `<service>:<role>` | **colon** |
| mackerel:monitor | mackerel_monitor | `GET /api/v0/monitors` → `monitors` | — | `id` | `<id>` | **bare** (7 `type` variants → 1 TF type; external `headers` SECRET) |
| mackerel:channel | mackerel_channel | `GET /api/v0/channels` → `channels` | — | `id` | `<id>` | **bare** (**slack/webhook `url` SECRET**) |
| mackerel:notification_group | mackerel_notification_group | `GET /api/v0/notification-groups` → `notificationGroups` | — | `id` | `<id>` | **bare** |
| mackerel:dashboard | mackerel_dashboard | `GET /api/v0/dashboards` → `dashboards` | — | `id` | `<id>` | **bare** (**import undocumented — VERIFY**) |
| mackerel:aws_integration | mackerel_aws_integration | `GET /api/v0/aws-integrations` → `aws_integrations` | — | `id` | `<id>` | **bare** (**`secret_key` write-only SECRET**) |
| mackerel:downtime | mackerel_downtime | `GET /api/v0/downtimes` → `downtimes` | — | `id` | `<id>` | **bare** |
| mackerel:alert_group_setting | mackerel_alert_group_setting | `GET /api/v0/alert-group-settings` → `alertGroupSettings` | — | `id` | `<id>` | **bare** |
| mackerel:service_metadata | mackerel_service_metadata | `GET …/services/<svc>/metadata` → `metadata` | ← service | `namespace` | `<service>:<namespace>` **(or `/` — VERIFY)** | **colon? — VERIFY** (DEFER INC-2) |
| mackerel:role_metadata | mackerel_role_metadata | `GET …/services/<svc>/roles/<role>/metadata` → `metadata` | ← service → role | `namespace` | `<service>:<role>/<namespace>` | **colon+slash** (DEFER INC-2) |
| mackerel:default_notification_group | mackerel_default_notification_group | (singleton `group-default`) | — | — | `<fixed token>` **(VERIFY)** | **singleton** (DEFER) |

### Import-format quirks (§ do not get wrong)

1. **Three distinct import forms — bare / colon / colon+slash — encode per TF type, never infer.**
   `mackerel_service` is a **bare name**; the eight org-level id-keyed resources are a **bare id**;
   `mackerel_role` is **`<service>:<role>`** (colon); `mackerel_role_metadata` is
   **`<service>:<role>/<namespace>`** (colon then slash). This is the provider's defining hazard.
2. **`mackerel_role` is a COLON composite `<service>:<role>`** — confirmed registry
   (`terraform import mackerel_role.foo foo:bar`) AND Terraformer
   (`fmt.Sprintf("%s:%s", serviceName, roleName)`). The `:` is also Mackerel's universal "role
   fullname" convention — it recurs inside `dashboard.role_fullname`, `aws_integration.*.role`,
   `downtime.role_scopes`, `alert_group_setting.role_scopes` (all `service:role`). Not a coincidence;
   the same join everywhere.
3. **`mackerel_service_metadata` import separator is CONTRADICTED in the docs** — prose says
   `<service>/<namespace>` (slash), example says `foo:bar` (colon). **VERIFY on live import**; encode
   whichever the provider accepts (colon is the likelier truth by analogy with role-metadata).
4. **`mackerel_dashboard` has NO documented Import section** — enumerate it (the API returns dashboard
   `id`s) but **VERIFY** that `terraform import mackerel_dashboard.x <id>` works before trusting the
   bare-id assumption; if it does not import, treat dashboards as a Phase-B/HCL-only gap.
5. **Ids are opaque strings — copy verbatim, no numeric stringify.** Mackerel ids
   (`2qtozU21abc`, `ABCDEFG`) and service/role names are strings off the wire; unlike Logz.io/Datadog
   there is no JSON-number id to stringify. Template-escape the whole (composite) id on emit
   (`util.EscapeHCLTemplate`) for parity.
6. **`mackerel_default_notification_group` is a singleton** (the fixed `group-default`; the provider
   does not create/delete it) — its import token is a fixed value, VERIFY against the registry.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a live org —
prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the Logz.io/Keycloak
providers. Mackerel has **no single monster resource** (`mackerel_dashboard` is the widest — a widget
tree); the recurring hazard is the **channel/aws-integration/external-monitor secrets** and the
**`service:role` fullname** references that recur across resources.

- **`mackerel_service` — trivial.** `name`, `memo`. No secret. Prune computed nothing much. The
  fan-out parent.
- **`mackerel_role` — trivial; composite import.** `service`, `name`, `memo`. No secret. The colon
  import is the only subtlety.
- **`mackerel_monitor` — medium; polymorphic + one SECRET.** Core per `type`: host_metric
  (`metric`/`operator`/`warning`/`critical`/`duration`/`scopes`), connectivity, service_metric,
  external, expression, anomaly_detection, query. **Secret:** the **external** monitor's `headers`
  map can carry an `Authorization`/token header → **scrub the value** (keep the block). `scopes` /
  `exclude_scopes` are `service:role` fullnames → keep literal, tolerate ordering churn (sort on
  emit). Monitor `expression`/`query` strings may contain `${…}`/PromQL → keep LITERAL
  (HCL-template-escape). Prune computed `id`.
- **`mackerel_channel` — the #1 secret surface.** One of `email` / `slack` / `webhook` nested blocks.
  **Secrets:** the **slack `url`** (an incoming-webhook URL that embeds the secret token) and the
  **webhook `url`** (may embed a token) → **scrub the value**, flag re-supply out-of-band. `mentions`
  / `emails` / `user_ids` / `events` are not secret. The Mackerel API does **not** return the slack
  URL on read (masked) → generate-config-out will null it → not plan-clean unless re-supplied (same
  class as Logz.io `logzio_endpoint` credentials). Prune computed `id`.
- **`mackerel_notification_group` — light.** `name`, `notification_level`, `child_channel_ids`,
  `child_notification_group_ids`, `monitor` blocks (`id`+`skip_default`), `service` blocks (`name`).
  References channel/group/monitor ids → keep. No secret. Prune computed `id`.
- **`mackerel_dashboard` — widest; no secret.** A widget tree: `graph` / `value` / `markdown` /
  `alert_status` blocks, each with a `layout` (x/y/width/height) and type-specific metric refs
  (`role_fullname` = `service:role`, `host_id`, `expression`, `query`). `markdown` bodies and
  `expression`/`query` strings can contain `$`/`{` → **`$`→`$$` / template-escape hazard** (the
  Keycloak `$`→`$$` precedent — verify generate-config-out escapes these). Layout ordering may churn.
  No secret. Prune computed `id`. **Also confirm the import works (quirk #4).**
- **`mackerel_aws_integration` — medium; one write-only SECRET.** `name`, `memo`, `key`
  (AWS access key id), `role_arn`, `external_id`, `region`, `included_tags`, `excluded_tags`, and
  per-service blocks (`ec2`/`alb`/`rds`/`nlb`/… each `enable`/`role`/`excluded_metrics`). **Secret:**
  **`secret_key`** (the AWS IAM user secret key) — the API does NOT return it on read (registry:
  "In addition to the above arguments **except for the secret key**, the following attributes are
  exported"; the client struct marks it `json:"secretKey,omitempty"`) → **scrub / do not round-trip**,
  flag re-supply (or prefer the `role_arn`+`external_id` IAM-role variant, which has no inline secret).
  `external_id` and `key` ARE returned — keep (external_id is low-sensitivity but not a credential).
  `role` fields are `service:role` fullnames. Prune computed `id`.
- **`mackerel_downtime` — light; no secret.** `name`, `start`, `duration`, `memo`,
  monitor/service/role `*_scopes` + `*_exclude_scopes`, `recurrence` block (`interval`/`type`/`until`/
  `weekdays`). Scope lists are `service:role` fullnames / ids → tolerate ordering. Prune computed `id`.
- **`mackerel_alert_group_setting` — light; no secret.** `name`, `memo`, `monitor_scopes`,
  `role_scopes` (fullnames), `service_scopes`, `notification_interval`. Prune computed `id`.
- **`mackerel_service_metadata` / `mackerel_role_metadata` — light; composite import.** `service`
  (+`role`), `namespace`, `metadata_json` (arbitrary JSON blob — the actual content comes from a
  second GET `…/metadata/<namespace>`). `metadata_json` may contain `$`/`{` → template-escape. No
  secret. The import-separator VERIFY (quirk #3) is the risk. DEFER to INC-2.
- **`mackerel_default_notification_group` — singleton, adopt-in-place.** `notification_level`,
  `child_channel_ids`, `child_notification_group_ids`. No secret. DEFER.

Until Phase B these are no-ops, so a Mackerel export is a breadth scaffold, not yet plan-clean (the
pipeline's repo-wide secret scan is the backstop for the channel slack/webhook `url` / external-monitor
`headers` / aws-integration `secret_key` that generate-config-out nulls-or-leaks before the scrub rules
land).

## Write-only / secret resources (EXCLUDE / scrub)

Mackerel's secret material is **field-level write-only** across the config resources (the API returns
it null/masked on read), like Logz.io — there is **no purely-write-only resource to fully exclude**.
Handle as **scrub-and-flag-re-supply**, keeping the surrounding config:

- **`mackerel_channel` slack `url` / webhook `url`** — an incoming-webhook URL embeds the secret token;
  Mackerel masks it on read → **scrub the value**, flag re-supply. The #1 secret surface (channels are
  where every notification token lives).
- **`mackerel_aws_integration.secret_key`** — the AWS IAM user secret key; **not returned on read**
  (write-only) → **scrub / do not round-trip**; prefer the `role_arn`+`external_id` IAM-role variant.
- **`mackerel_monitor` external `headers`** — the external-HTTP monitor's request headers can carry an
  `Authorization`/API-token header → **scrub** the header value(s), keep the block.
- **The org API key itself** — `MACKEREL_APIKEY` / `MACKEREL_API_KEY` — lives **only** on the
  `X-Api-Key` header, never in generated config, state, errors, or logs. **Do NOT inline it into
  `providers.tf` (Terraformer does — the leak we refuse to copy).** There is no round-trippable
  "api key" resource to adopt.
- **Not secret, do not over-scrub:** service/role names + memos, `aws_integration` `key`/`external_id`/
  `role_arn`/`region`/tags (returned, low-sensitivity), dashboard widget content, monitor thresholds/
  scopes, notification-group/downtime/alert-group scope lists, channel `emails`/`mentions`/`events`.

## Deliberately out of scope

- **Hosts** (`GET /api/v0/hosts`) — **agent-registered** (the mackerel-agent POSTs them), not a durable
  IaC object, and there is **no `mackerel_host` TF resource**. The paginated host list is DATA, not
  config. Out of scope (the "defer host-agent" call — here it is doubly out, since no resource exists).
- **Users** (`GET /api/v0/users`) — managed by **invitation**, not a create/delete resource; there is
  **no `mackerel_user` TF resource**, and the list is PII. Out of scope.
- **Alerts** (`GET /api/v0/alerts`, `nextId`-paged) — transient runtime state (open/closed alert
  events), not config. Out of scope (and no TF resource). The reason the deferred pager would exist.
- **Metric / monitoring DATA** — host & service metric points, graph values, check reports, graph
  annotations, APM/traces, `service_metric_names` — the DATA behind the config. Out of scope.
- **`mackerel_service_metadata` / `mackerel_role_metadata`** — genuine config, but they add the
  one-/two-level fan-out and the composite-import-separator VERIFY; **DEFERRED to INC-2** (not out
  forever). Included in the catalog flagged deferred.
- **`mackerel_default_notification_group`** — a singleton adopt-in-place (`group-default`); low value
  as a first-pass import and its import token needs VERIFY → **DEFERRED**.
- **The `mackerelio/mackerel-client-go` SDK + the `mkr` CLI** — Terraformer pulls the SDK; TerraLift
  uses a raw `net/http` client (smaller, matches Logz.io), and there is no `mkr` dependency. A
  deliberate non-adoption. (Also non-adopted: Terraformer's api-key inlining.)
- **Cloud-IAM depth** (`Capabilities.IAM=false`) — Mackerel per-key Read/Write/Monitor scopes and org
  invitations are not modeled; the config plane only.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)

The **recommended Phase-A CONFIG CORE** (9 TF types): `mackerel_service`, `mackerel_role`,
`mackerel_monitor`, `mackerel_channel`, `mackerel_notification_group`, `mackerel_dashboard`,
`mackerel_aws_integration`, `mackerel_downtime`, `mackerel_alert_group_setting`. (Metadata + the
default-notification-group singleton are INC-2.)

BEACHHEAD `mackerel_service` + `mackerel_role` + `mackerel_monitor` (the service/role tree and the
monitors that watch it — what essentially every Mackerel org manages as IaC; these are also three of
Terraformer's generators so the shapes are well-understood). `mackerel_service` establishes the flat
org list + the **fan-out parent**; `mackerel_role` establishes the **service→role fan-out** and the
**`<service>:<role>` colon composite** import — the provider's defining hazard; `mackerel_monitor`
exercises the **polymorphic `type` decode → single TF type**, the **bare-id import**, and the
**external-monitor `headers` scrub**. → INC-1 `mackerel_channel` + `mackerel_notification_group` +
`mackerel_aws_integration` + `mackerel_downtime` + `mackerel_alert_group_setting` + `mackerel_dashboard`
(the rest of the org-level bare-id plane — `mackerel_channel` adds the **slack/webhook `url` scrub**,
`mackerel_aws_integration` the **`secret_key` write-only scrub**, `mackerel_dashboard` the widget-tree
`$`→`$$` escape + the **undocumented-import VERIFY**). → INC-2 `mackerel_service_metadata` +
`mackerel_role_metadata` (the metadata plane — the one-/two-level fan-out + the **colon vs slash
separator VERIFY** + the `<service>:<role>/<namespace>` colon+slash composite) +
`mackerel_default_notification_group` (the singleton). → LATER/OUT hosts, users, alerts, and the
metric/trace DATA planes (no TF resources; DATA behind the config).
