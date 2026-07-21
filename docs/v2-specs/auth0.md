# Auth0 provider — build spec

Research artifact for the `auth0` provider (Phase A scaffold; TF provider source is
`auth0/auth0`, product "Auth0" — the identity/CIAM platform, an Okta company). Sources:
Terraformer's `providers/auth0/` (built on the dated `gopkg.in/auth0.v5/management` Go SDK —
16 generators), the `auth0/auth0` registry docs (import formats + schema, **verified
per-resource below** against the provider's `docs/resources/*.md`), and the Auth0 Management
API v2 (`https://<AUTH0_DOMAIN>/api/v2/…`). Build mirrors the **Okta** provider
(`internal/providers/okta/`) and **Opsgenie** (`internal/providers/opsgenie/`) — a flat,
tenant-scoped, single-container REST provider (a direct `net/http` client, no CLI,
`terraform plan -generate-config-out` for export). This is **REST, Okta/Opsgenie-style, NOT
GraphQL.**

**Auth0 is a large provider (like Okta — 60+ resources). This spec scopes Phase A to a
tractable CONFIG CORE (~15 resource types across ~11 enumeration passes)** — the
application/API/identity/RBAC config that essentially every Auth0 tenant manages as IaC — and
explicitly DEFERS the long tail (the user plane, the `::`-composite relationship/membership
resources, the deprecated rules/hooks, custom domains, the many singleton sub-settings, and
the log/data planes) to later increments (see Build order). **Four facts set Auth0 apart from
every prior provider, all load-bearing and called out below:**

1. **Auth is an OAuth2 CLIENT-CREDENTIALS token EXCHANGE — a genuinely NEW auth pattern, the
   key divergence.** Unlike every prior provider's static header token (Okta's `SSWS`,
   Opsgenie's `GenieKey`, Datadog's `DD-API-KEY`), Auth0's Management API needs a **short-lived
   Bearer obtained at connect-time by a token exchange**: `POST https://<AUTH0_DOMAIN>/oauth/token`
   with a machine-to-machine (M2M) client_id + client_secret → `{access_token, expires_in}`,
   then `Authorization: Bearer <access_token>` on every `/api/v2/` call. A static
   `AUTH0_API_TOKEN` **bypasses** the exchange.
2. **The base URL is the TENANT DOMAIN (user-supplied, like Okta/Grafana).** `https://<AUTH0_DOMAIN>`
   where `AUTH0_DOMAIN` is e.g. `mytenant.us.auth0.com`; all Management paths sit under `/api/v2/`
   and the token endpoint at `/oauth/token`. Construct + validate (https-forced), redirect-refusing
   client.
3. **Pagination is `page`/`per_page` + `include_totals` (keyed envelope with a `total`), with
   CHECKPOINT exceptions.** Most list endpoints return `{"<key>":[...],"total","start","limit","length"}`
   and you loop pages until `start+length >= total`; a few newer/data endpoints use CHECKPOINT
   (`from`/`take` → `{...,"next":"<cursor>"}`); and `auth0_log_stream` returns a **bare array**.
4. **Composite import ids use `::` (DOUBLE-COLON) — the #1 hazard — and several config
   resources are SINGLETONS imported by an ARBITRARY placeholder string.** The relationship
   plane (deferred) joins ids with `::` (not `/`, not `:`), and the tenant-wide settings
   singletons (`auth0_tenant`/`_branding`/`_attack_protection`/`_prompt`/`_guardian`/
   `_email_provider`) import by a **don't-care placeholder** the provider discards. Phase A's
   own set is deliberately composite-free (bare ids, two name-keyed, and the singletons).

## Version pin (load-bearing)

Pin `auth0/auth0 ~> 1.x` (current major; org is lowercase `auth0`, product "Auth0"). Naming
facts that matter (the Terraformer-vs-current divergences):

- **Terraformer's coverage is broad but dated and SDK-bound.** Its 16 generators (built on
  `gopkg.in/auth0.v5/management`) cover client, resource_server, role, action, client_grant,
  log_stream, trigger_binding, user, tenant, branding, email, prompt, custom_domain, rule,
  rule_config, hook. **Do NOT pull the `auth0.v5` SDK** the way Terraformer did — a raw
  `net/http` client is smaller and matches the Okta/Opsgenie providers (a deliberate
  non-adoption).
- **Terraformer has NO generator for `auth0_connection`, `auth0_organization`,
  `auth0_attack_protection`, `auth0_guardian`, or `auth0_email_template`** — five of the
  Phase-A set. These are covered here from the **registry + Management API directly** (the
  Honeycomb/`datadog_webhook` precedent: no upstream generator ⇒ derive from the API). The
  connection and organization resources are among the most-managed Auth0 objects, so their
  absence in Terraformer is not a signal to skip them.
- **Renames + deprecations to honour (do NOT copy Terraformer's names):**
  - Terraformer emits `auth0_email` (the tenant email *provider*). The current resource is
    **`auth0_email_provider`** — emit that; `auth0_email` is gone. There is also a *separate*
    **`auth0_email_template`** (per-template content) with no Terraformer generator.
  - Terraformer emits `auth0_rule`, `auth0_rule_config`, and `auth0_hook`. All three are
    **DEPRECATED** in the provider (superseded by **`auth0_action`** + `auth0_trigger_actions`).
    **Emit `auth0_action`; drop rules/hooks** (out of scope, below).
  - Terraformer's `auth0_trigger_binding` is the current **`auth0_trigger_actions`** (the
    action-to-flow binding). Deferred to the increment right after actions (ordering-sensitive
    `::` composite; below), not Phase A.
- The Management API endpoints below are provider-version-independent.

## Shape

- **Auth — OAuth2 client-credentials token EXCHANGE (the hard divergence; a new pattern).**
  Auth0's Management API is Bearer-authenticated, but the Bearer is **not static** — it is a
  short-lived token minted at connect time:
  - **Connect-time exchange:** `POST https://<AUTH0_DOMAIN>/oauth/token`, `Content-Type:
    application/json`, body
    ```json
    { "grant_type": "client_credentials",
      "client_id": "<AUTH0_CLIENT_ID>",
      "client_secret": "<AUTH0_CLIENT_SECRET>",
      "audience": "https://<AUTH0_DOMAIN>/api/v2/" }
    ```
    → `{"access_token":"<jwt>","expires_in":86400,"token_type":"Bearer","scope":"read:clients …"}`.
    The **`audience` must be exactly the Management API identifier** `https://<AUTH0_DOMAIN>/api/v2/`
    (with the trailing slash) — a wrong/absent audience returns `access_denied` /
    `Service not enabled within domain`. This POST is itself **unauthenticated** (no Bearer);
    the `client_secret` rides in the JSON body only.
  - **Then, on every `/api/v2/` request:** `Authorization: Bearer <access_token>` +
    `Accept: application/json` (+ harmless `Content-Type: application/json` on GET). Cache the
    `access_token` for the run and refresh if `expires_in` (default 24h) would elapse mid-run
    (a very long enumeration only — one exchange normally suffices).
  - **Static-token BYPASS:** if `AUTH0_API_TOKEN` is set, **skip the exchange entirely** and use
    it directly as the Bearer (a hand-minted Management API token from the dashboard, handy for
    CI or a scoped run). Precedence: `AUTH0_API_TOKEN` > client-credentials.
  - **The M2M app must be authorized for the Management API** with the `read:*` scopes for the
    Phase-A objects (`read:clients`, `read:resource_servers`, `read:connections`, `read:roles`,
    `read:actions`, `read:organizations`, `read:client_grants`, `read:email_templates`,
    `read:log_streams`, `read:tenant_settings`, `read:branding`, `read:attack_protection`,
    `read:prompts`, `read:guardian_factors`, `read:email_provider`). A missing scope surfaces as
    a **403 `insufficient_scope`** on that one endpoint → per-endpoint Verbose skip (see status
    handling), not a fatal error.
  - **Secret discipline (mirror the SSWS/GenieKey rule):** the `client_secret` appears ONLY in
    the `/oauth/token` request body; the `access_token` ONLY on the `Authorization` header.
    Neither ever appears in the URL, errors, or logs. A direct `net/http` client (mirror
    `oktaapi.go` / `opsgenieapi.go`); **no Auth0 CLI**, and **no** `auth0.v5` SDK. Force `https`;
    **refuse redirects** (mirror `oktaHTTPClient`) so neither secret can be replayed to another
    host on a 3xx.
- **Base URL — the TENANT DOMAIN (must be read from env; the Grafana/Okta host-from-config
  pattern).** There is no fixed base and no region table baked in: `AUTH0_DOMAIN` is the full
  host (`mytenant.us.auth0.com`, `mytenant.eu.auth0.com`, `mytenant.au.auth0.com`, or a legacy
  `mytenant.auth0.com`). Construction rules:
  - Require `AUTH0_DOMAIN`; **force https**; strip any scheme/slashes a user pastes in;
    `base = "https://" + AUTH0_DOMAIN`. Management paths are `base + "/api/v2/…"`; the token
    endpoint is `base + "/oauth/token"`.
  - **Custom-domain gotcha:** a tenant may serve a vanity domain (`auth.example.com`), but the
    **Management API audience stays the canonical `https://<tenant>.<region>.auth0.com/api/v2/`**
    — it does *not* use the custom domain. Phase A assumes `AUTH0_DOMAIN` is the **canonical**
    tenant domain (what the M2M app is issued against); a custom-domain front-end is a later
    consideration.
  - Store the resolved base host once; https-force it. Unlike Okta/Opsgenie, Auth0 does **not**
    hand back a fully-qualified next-page URL to follow — page-based paging is a query we build
    ourselves (`page++`) and the checkpoint `next` is a bare cursor token we append — so there is
    **no server-supplied cross-host next-URL to validate**. The redirect-refusing https client is
    still the safety backstop for the Bearer/`client_secret`.
- **Scope — one Auth0 tenant = one flat container.** The M2M credentials (or the static token)
  are **tenant-scoped**; there is no sub-tenant and no multi-tenant resolution — the credentials
  simply **are** the tenant. `model.ScopeTenant`, `Capabilities{IAM:false, Exposure:false,
  Hierarchy:false}`. Resolve the container id/name **best-effort** via `GET /api/v2/tenants/settings`
  (returns `friendly_name`, `enabled_locales`, …) → use `friendly_name`; if that scope is absent
  (403 `read:tenant_settings`) fall back to `AUTH0_DOMAIN` (the host string). As flat as Okta:
  the credentials are the tenant, no id lookup required.
- **Response families — FOUR shapes; classify per resource (§ next section).** Auth0's
  Management API is not uniform; the load-bearing per-resource fact is which body shape the list
  endpoint returns:
  1. **keyed envelope + total (the common case)** — `{"<key>":[...],"total":N,"start":S,
     "limit":L,"length":len}` when `?include_totals=true` is passed. `<key>` is the plural
     resource name (`clients`, `resource_servers`, `connections`, `roles`, `client_grants`,
     `organizations`) or `actions` (under `/actions/actions`). The list helper takes the nesting
     key as a parameter (mirror `doapi.go` / the Opsgenie `decodeData` key) and reads
     `total`/`start`/`length` off the same envelope for the pager.
  2. **checkpoint envelope** — `{"<key>":[...],"next":"<cursor>"}` (NO `total`); loop `?from=<cursor>&take=<n>`
     until `next` is empty. Used by the log/data plane (deferred) and available (not required)
     on organizations; Phase A uses page/total for organizations.
  3. **bare array** — `GET /api/v2/log-streams` returns a raw `[...]` (no envelope, no
     pagination). Unmarshal straight into `[]T`. (Also what a keyed endpoint degrades to if you
     forget `include_totals`.)
  4. **singleton object** — `GET /api/v2/tenants/settings`, `/branding`, `/prompts`,
     `/emails/provider`, the three `/attack-protection/*` objects, `/guardian/factors` — a single
     JSON object (or, for guardian, a small array of factor objects), NOT a list. Enumerated as
     one object, never paged (see the singleton note).
  So the client needs, like Fastly/Datadog, **a keyed-list-with-total helper** (Phase-A pager), a
  **bare-array helper** (log streams), a **singleton-GET helper** (mirror `oktaGetObject` /
  `ogGetData`), and a **checkpoint helper stub** for the later data-plane increments.
- **Pagination — `page`/`per_page` + `include_totals`; a few exceptions.**
  - **Keyed + total (Phase-A default):** `?page=<n>&per_page=<=100&include_totals=true`; `page`
    is **0-based**. Accumulate and stop when `start + length >= total` (or `length == 0`).
    `per_page` max is **100** on the Phase-A endpoints (VERIFY per endpoint at build; some cap at
    50 by default, a few allow more). **`include_totals=true` is REQUIRED** to get the envelope —
    without it the body is a bare array and you cannot know `total` (you would loop until a short
    page; prefer the envelope).
  - **Deep-pagination cap (flag it):** page-based retrieval is **capped at ~1000 records** on
    several endpoints (Auth0 refuses `page` beyond the 1000th record). For a tenant with >1000 of
    a given object you MUST switch to **checkpoint** (`from`/`take`) where the endpoint supports
    it. Phase-A tenants rarely exceed 1000 apps/connections/roles, but bound every loop
    defensively (`auth0MaxPages`) and note this as the reason a very large tenant would need the
    checkpoint path.
  - **Checkpoint:** `?take=<n>&from=<cursor>` → `{...,"next":"<cursor>"}`; loop until `next`
    empty. (Data-plane/logs; deferred.)
  - **Unpaged singletons + `log-streams`:** one GET, no loop.
- **Rate limiting (flag it).** The Management API is rate-limited per-tenant and returns **429**
  with `X-RateLimit-Limit` / `X-RateLimit-Remaining` / `X-RateLimit-Reset` (epoch seconds) headers
  (and often `Retry-After`). Read these off the response and back off (mirror `oktaBackoff` —
  the client already needs to expose headers for the reset). A bulk enumeration on a large
  tenant WILL brush the limit; bound + honour the reset, do not hammer.
- **Status handling (mirror `list` / `oktaAPIError`).** Auth0 errors are
  `{"statusCode":N,"error":"…","message":"…","errorCode":"…"}` (HTTP status carries the meaning).
  **401** (invalid/expired token, or a mis-scoped `audience`) → fatal, surfaced in preflight; if
  it appears mid-run (token expiry) refresh once, else treat as fatal — every remaining list
  would fail too. **403** (`insufficient_scope` — the M2M app lacks the endpoint's `read:` scope,
  or the feature is not on the plan) → best-effort **Verbose skip**. **404** (feature/object
  absent — e.g. no email provider configured) → Verbose skip. **429** → honour
  `X-RateLimit-Reset`/`Retry-After` and back off. **5xx / network** → enumeration may be
  silently incomplete → **Warn + count** (tell a systemic failure apart from an empty tenant).
  The `client_secret`/`access_token` never appear in errors/logs; strip any query string before a
  URL is logged (belt-and-suspenders, mirror `redactURL`).
- **Preflight**: `terraform` present + `AUTH0_DOMAIN` set + **either** `AUTH0_API_TOKEN` **or**
  (`AUTH0_CLIENT_ID` **and** `AUTH0_CLIENT_SECRET`) set + the credentials authenticate. When
  using client-credentials, the **token exchange itself is the first check** (a 401/403 from
  `/oauth/token` means bad client_id/secret or the M2M app is not authorized for the Management
  API). Then a lightweight authenticated probe: `GET /api/v2/tenants/settings` (doubles as the
  tenant-name probe; `read:tenant_settings` is a common default scope). If that scope is absent,
  fall back to `GET /api/v2/clients?per_page=1&include_totals=true`. `/tenants/settings` doubling
  as the name probe means later 403/404 skips are explained rather than surprising.
- **Connect**: run the token exchange (or accept `AUTH0_API_TOKEN`), validate the probe succeeds,
  resolve the tenant name from `/api/v2/tenants/settings` (`friendly_name`; else `AUTH0_DOMAIN`),
  and set the single flat container (`model.ScopeTenant`).

## Response-shape + import-id determination — the CRITICAL classification

This is Auth0's analogue of Okta's "discriminator + fan-out + composite-depth" call and
Datadog's "which API version/shape" call. The load-bearing per-resource facts are **(a) which
RESPONSE SHAPE the list endpoint uses (keyed+total / bare array / singleton object / name
fan-out), which fixes the decode + pager; and (b) the IMPORT-ID FORM — bare id / name / `::`
composite / singleton placeholder.** Get (a) wrong and the decode fails (wrong envelope) or the
inventory is truncated (wrong pager); get (b) wrong and every import block for that type is
un-importable. Both are **verified against the registry `docs/resources/*.md`** and pinned
per-resource in the catalog. The rules:

- **Most Phase-A lists are keyed+total** — `clients` / `resource_servers` / `connections` /
  `roles` / `actions` / `organizations` / `client_grants`. One `?include_totals=true` loop each,
  keyed by the plural name. **`actions` lives at the doubled path `/api/v2/actions/actions`**
  (the outer `actions` is the API group) and keys on `actions`.
- **`auth0_log_stream` is a BARE ARRAY** — `GET /api/v2/log-streams` → `[...]`, no envelope, no
  pagination. Do not pass `include_totals`; do not look for a `total`.
- **The six settings singletons are single-object GETs, NOT lists** — `auth0_tenant`
  (`/tenants/settings`), `auth0_branding` (`/branding`), `auth0_prompt` (`/prompts`),
  `auth0_email_provider` (`/emails/provider`), `auth0_attack_protection` (three sub-objects under
  `/attack-protection/*`, combined into one TF resource), `auth0_guardian` (`/guardian/factors`
  + related). Each is **always present** (one per tenant) — enumerate as exactly one object;
  there is no list and no pager. `auth0_email_provider` may 404 if no provider is configured
  (→ skip, feature absent).
- **`auth0_email_template` is a NAME FAN-OUT** — there is **no list endpoint**; you GET each
  template by its fixed name (`GET /api/v2/email-templates/<name>`). Enumeration loops the known
  template names (mirror Okta's `?type=` policy loop) and adopts the ones that exist (404 →
  template not configured → skip). The template **name IS the import id**.
- **Import-id form is per-type (the #1 hazard, § quirks):**
  - **bare id** — clients (by **`client_id`**, not the internal `id`), resource_servers, connections,
    roles, actions, organizations, client_grants (all by their `id`).
  - **name** — email templates (the template name).
  - **singleton placeholder** — the six settings singletons import by an **arbitrary string the
    provider discards** (below); emit a stable sentinel.
  - **`::` double-colon composite** — the DEFERRED relationship plane only (connection_client,
    organization_connection/member, role_permission, trigger_actions, …). **No Phase-A resource
    uses `::`** — but the increment that adds them must encode `::` (not `/`, not `:`) in an
    explicit per-TF-type switch in `importid.go` (mirror Okta's `rawImportID`), never inferred.

## Enumeration spine

Flat tenant scope; **almost no fan-out** (contrast Okta's auth-server/policy fan-outs) — only
the email-template name loop, and `auth0_attack_protection` reading three sub-objects into one
resource. Every list is a single best-effort tenant-level pass (403 scope-absent / 404
feature-absent → Verbose skip; 401 → fatal; other → Warn + count), each tagged with its response
shape + pager per the catalog. The `client_secret`/`access_token` never appear in errors/logs.

- **Keyed + total (`?page=&per_page=100&include_totals=true`):**
  - `GET /api/v2/clients` (`clients`) → `auth0_client` (id = **`client_id`**). **Skip the
    Auth0-internal apps** (`global` client / the "All Applications" global; and note the M2M app
    behind the run appears — flag, don't break it — see curation).
  - `GET /api/v2/resource-servers` (`resource_servers`) → `auth0_resource_server`. **Skip the
    system "Auth0 Management API"** resource server (identifier `https://<domain>/api/v2/`, and
    the read-only `is_system` one) — it can't be managed.
  - `GET /api/v2/connections` (`connections`) → `auth0_connection` (curation discriminator =
    `strategy`; see gotchas).
  - `GET /api/v2/roles` (`roles`) → `auth0_role`.
  - `GET /api/v2/actions/actions` (`actions`, doubled path) → `auth0_action`. Optionally
    `?deployed=true`; **skip built-in/system actions** if any surface.
  - `GET /api/v2/organizations` (`organizations`) → `auth0_organization`.
  - `GET /api/v2/client-grants` (`client_grants`) → `auth0_client_grant`.
- **Bare array (unpaged):** `GET /api/v2/log-streams` → `auth0_log_stream`.
- **Name fan-out:** loop the known email-template names → `GET /api/v2/email-templates/<name>`
  → `auth0_email_template` (adopt those that exist; 404 → skip). Names to loop (VERIFY the set at
  build): `verify_email`, `verify_email_by_code`, `reset_email`, `reset_email_by_code`,
  `welcome_email`, `blocked_account`, `stolen_credentials`, `enrollment_email`, `mfa_oob_code`,
  `user_invitation`, `change_password` (legacy), `password_reset` (legacy).
- **Settings singletons (one object each; always present):**
  - `GET /api/v2/tenants/settings` → `auth0_tenant`.
  - `GET /api/v2/branding` → `auth0_branding`.
  - `GET /api/v2/attack-protection/breached-password-detection` +
    `…/brute-force-protection` + `…/suspicious-ip-throttling` → **one** `auth0_attack_protection`
    (three GETs populate the single resource; presence is unconditional).
  - `GET /api/v2/prompts` → `auth0_prompt`.
  - `GET /api/v2/guardian/factors` (+ related guardian objects) → `auth0_guardian`.
  - `GET /api/v2/emails/provider` → `auth0_email_provider` (404 → no provider configured → skip).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic failure
rather than shipping an empty inventory (same guard as the Okta/Opsgenie `enumerate.go`).

## Resource catalog

Import IDs verified against the current `auth0/auth0` registry docs (`docs/resources/*.md`). All
scope = tenant. "endpoint → shape" is the list path + response family. The **id/import** columns
are the #1 hazard — **bare id / name / singleton placeholder** (no Phase-A `::` composite).

| native key | TF type | endpoint → shape | id field | import ID | form |
|---|---|---|---|---|---|
| auth0:client | auth0_client | `GET /api/v2/clients` → keyed `clients`+total | `client_id` | `<client_id>` | bare (**client_secret SECRET**) |
| auth0:resource_server | auth0_resource_server | `GET /api/v2/resource-servers` → keyed `resource_servers`+total | `id` | `<resource_server_id>` | bare (**signing_secret SECRET**; skip system API) |
| auth0:connection | auth0_connection | `GET /api/v2/connections` → keyed `connections`+total | `id` (`con_…`) | `<connection_id>` | bare (**options secrets**; `strategy` disc.) |
| auth0:role | auth0_role | `GET /api/v2/roles` → keyed `roles`+total | `id` (`rol_…`) | `<role_id>` | bare |
| auth0:action | auth0_action | `GET /api/v2/actions/actions` → keyed `actions`+total | `id` (uuid) | `<action_id>` | bare (**secrets EXCLUDE**) |
| auth0:organization | auth0_organization | `GET /api/v2/organizations` → keyed `organizations`+total | `id` (`org_…`) | `<organization_id>` | bare |
| auth0:client_grant | auth0_client_grant | `GET /api/v2/client-grants` → keyed `client_grants`+total | `id` (`cgr_…`) | `<client_grant_id>` | bare |
| auth0:log_stream | auth0_log_stream | `GET /api/v2/log-streams` → **bare array** | `id` (`lst_…`) | `<log_stream_id>` | bare |
| auth0:email_template | auth0_email_template | `GET /api/v2/email-templates/<name>` → **name fan-out** | `template` name | `<template_name>` | **name IS the id** |
| auth0:tenant | auth0_tenant | `GET /api/v2/tenants/settings` → **singleton** | — (no API id) | `<placeholder>` | **singleton placeholder** |
| auth0:branding | auth0_branding | `GET /api/v2/branding` → **singleton** | — | `<placeholder>` | **singleton placeholder** |
| auth0:attack_protection | auth0_attack_protection | `GET /api/v2/attack-protection/*` (×3) → **singleton** | — | `<placeholder>` | **singleton placeholder** |
| auth0:prompt | auth0_prompt | `GET /api/v2/prompts` → **singleton** | — | `<placeholder>` | **singleton placeholder** |
| auth0:guardian | auth0_guardian | `GET /api/v2/guardian/factors` → **singleton** | — | `<placeholder>` | **singleton placeholder** (provider creds SECRET) |
| auth0:email_provider | auth0_email_provider | `GET /api/v2/emails/provider` → **singleton** (may 404) | — | `<placeholder>` | **singleton placeholder** (**credentials EXCLUDE**) |

### Import-format quirks (§ do not get wrong)

1. **The `::` DOUBLE-COLON composite is the #1 hazard — but it is DEFERRED-plane only; verify
   each and its DEPTH.** Auth0's relationship/membership resources join their parts with `::`
   (two colons), **not** Okta's `/`, Opsgenie's `/`, or a single `:`. **No Phase-A resource is a
   `::` composite** (Phase A is bare / name / singleton), but the increment that adds the
   relationship plane must encode `::` per-type, and the DEPTH varies (VERIFY at build):
   - `auth0_connection_client` = **`<connection_id>::<client_id>`** (2-part).
   - `auth0_organization_connection` = **`<organization_id>::<connection_id>`** (2-part).
   - `auth0_organization_member` = **`<organization_id>::<user_id>`** (2-part).
   - `auth0_role_permission` = **`<role_id>::<resource_server_identifier>::<permission_name>`**
     — **3-part** (correcting the "`<role_id>::<permission>`?" guess: it carries the resource
     server identifier in the middle).
   - `auth0_trigger_actions` (Terraformer's `trigger_binding`) = **`<trigger>::<action_id>`**
     (2-part; e.g. `post-login::<uuid>`), ordering-significant.
   - **`auth0_client_grant` is a BARE `id`** (`cgr_…`) — a *grant object*, not a `::` join,
     despite relating a client to an API. (In scope for Phase A; bare.)
   Encode the separator + part-count per TF type in `importid.go`; never infer.
2. **`auth0_client` imports by `client_id`, NOT the internal `id`.** The client's identifier is
   its `client_id` (a ~32-char opaque string); the list also carries other fields but the import
   token is `client_id`. Getting this wrong (using an `id`-style field) makes every client import
   fail. Copy `client_id` verbatim.
3. **The six settings SINGLETONS import by an ARBITRARY placeholder the provider DISCARDS.**
   `auth0_tenant`/`_branding`/`_attack_protection`/`_prompt`/`_guardian`/`_email_provider` have
   **no id in the Management API** (they are the one-per-tenant settings object); the provider's
   importer **ignores the supplied id and always reads the tenant-wide object**. The registry docs
   recommend a random UUIDv4 as the placeholder. **TerraLift should emit a STABLE deterministic
   sentinel** (e.g. the resource kind — `"tenant"`, `"branding"`, `"attack_protection"`,
   `"prompt"`, `"guardian"`, `"email_provider"`) so re-runs produce identical import blocks
   (idempotent), rather than a fresh random UUID each time. VERIFY per registry that the importer
   still discards the id at build (it has historically).
4. **`auth0_email_template` imports by the template NAME** (`welcome_email`, `verify_email`, …) —
   the name *is* the id; there is no separate numeric/opaque id.
5. **Bare ids are opaque strings off the wire, mostly prefixed** (`con_` connections, `rol_`
   roles, `org_` organizations, `cgr_` client grants, `lst_` log streams; actions are bare
   UUIDs; resource servers are 24-hex or the identifier URL). Copy verbatim — no numeric
   stringify (unlike Datadog).
6. **Skip the system objects on enumeration, not on import.** The global `all-applications`
   client and the `is_system` "Auth0 Management API" resource server exist but cannot be adopted —
   filter them out during enumeration (like Okta's `saasure`/own-app skip), so no un-appliable
   import block is emitted.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a live
tenant — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the Okta/Opsgenie
providers. **`auth0_connection` is the heaviest curation surface** (the `strategy`-dependent
`options` tree — Auth0's analogue of Okta's app family / Datadog's `datadog_dashboard`), and the
recurring hazards are the **client/connection/resource-server/action/email secrets** and the
**settings-singleton over-emit**.

- **`auth0_connection` — the big one; `strategy` discriminates the `options` sub-schema + the
  secrets.** One TF type, but the `strategy` field (`auth0` database, `google-oauth2`,
  `windowslive`, `samlp`, `waad`, `adfs`, `oidc`, `okta`, `email`, `sms`, …) selects a completely
  different `options` block (like `okta_network_zone`'s `type`, not like Okta apps' one-type-per-
  mode). **Secrets to scrub:** `options.client_secret` (social/enterprise/OIDC connections),
  `options.credentials` (SAML/SCIM signing + provisioning creds), and for `auth0` database
  connections the `options.configuration` (encrypted custom-DB script env vars) + any
  `options.custom_scripts` embedded secrets. `enabled_clients` references client ids (breadth
  ref). Prune computed `provisioning_ticket_url`. Phase-B-heavy; treat Phase-A export as a
  breadth scaffold.
- **`auth0_client` — medium; SECRET + refs.** Core: `name`, `app_type` (spa/native/
  regular_web/non_interactive), `callbacks`, `allowed_origins`, `web_origins`, `grant_types`,
  `jwt_configuration`, `oidc_conformant`. **Secret:** `client_secret` (returned on read but
  Sensitive; the confidential-app secret) → **scrub**; the computed `signing_keys` (cert/key
  material) and `encryption_key` are read-only → prune. **CAUTION:** the M2M app TerraLift
  authenticates with appears in `/clients` — adopting it is allowed but **flag it** (like
  Datadog's own-key / Okta's own-admin note; do not alter its grant/secret out from under the
  run). Prune computed `client_id` from nested refs where echoed.
- **`auth0_resource_server` — light; SECRET.** `identifier` (the API audience URL — the natural
  key), `scopes`, `signing_alg`, `token_lifetime`. **Secret:** `signing_secret` (the HS256
  signing secret, computed/Sensitive) → **scrub**. Skip the system Management API resource server
  (above).
- **`auth0_action` — medium; SECRET (values) + code.** `code` (the JS action body — keep literal;
  it may contain `${…}`-looking template text → verify terraform's writer keeps it literal),
  `runtime`, `dependencies`, `supported_triggers`. **`secrets` are write-only** — the API returns
  only the secret *names*, never the values → **exclude the values** (keep the names / re-supply
  out-of-band). `deployed` is a lifecycle flag.
- **`auth0_role` — light.** `name`, `description`. Role→permission assignments are a **separate**
  resource (`auth0_role_permission(s)`, deferred `::` composite) — the role shell only. No secret.
- **`auth0_organization` — light.** `name`, `display_name`, `branding`, `metadata`. Org→connection
  and org→member are **separate** resources (deferred `::` composites) — the org shell only. No
  secret.
- **`auth0_client_grant` — light.** `client_id` (ref), `audience` (resource-server identifier
  ref), `scopes`. No secret. Prune computed `id` where echoed.
- **`auth0_log_stream` — light; possible SECRET in sink.** `type` (http/eventbridge/datadog/
  splunk/…), `sink` (the destination config). Some sinks carry a **token/api-key** in `sink`
  (e.g. `datadog_api_key`, `splunk_token`, HTTP `authorization`) → **scrub** those sink fields.
  Prune computed `status`.
- **`auth0_email_provider` — SECRET (whole credentials block).** `name` (smtp/mandrill/ses/
  sendgrid/…), `credentials` (SMTP password / API key / AWS secret) is **write-only** →
  **EXCLUDE the credentials block** (adopt the provider shell only; re-supply out-of-band). May
  404 if unconfigured.
- **`auth0_email_template` — light; content.** `template` (name), `body` (HTML — keep literal),
  `from`, `subject`, `syntax` (liquid), `enabled`. The Liquid `{{ }}` / `{% %}` template markup
  must stay literal in generated HCL — verify terraform's writer does not interpolate it. No
  secret.
- **`auth0_tenant` — light; over-emit.** `friendly_name`, `support_email`, `session_lifetime`,
  `flags`, `sandbox_version`, `default_directory`. Defaults over-emit heavily (many `flags`);
  prune computed. No secret.
- **`auth0_branding` — light.** `logo_url`, `colors`, `font`, `universal_login` (page template —
  keep literal). The `universal_login.body` may reference an asset; no secret.
- **`auth0_attack_protection` — light; composite singleton.** Combines
  `breached_password_detection` / `brute_force_protection` / `suspicious_ip_throttling` into one
  resource; each has `enabled` + thresholds/allowlists. Prune computed. No secret.
- **`auth0_prompt` — trivial.** `universal_login_experience`, `identifier_first`,
  `webauthn_platform_first_factor`. (Per-prompt custom text is a **separate** deferred resource.)
  No secret.
- **`auth0_guardian` — medium; provider SECRET.** MFA factor config (`phone`, `push`, `email`,
  `otp`, `webauthn_*`, `duo`, …). The SMS/push provider blocks carry **credentials** (Twilio
  `auth_token`, Duo `secret_key`, custom SNS/APNS keys) → **scrub** those. Prune computed factor
  state.

Until Phase B these are no-ops, so an Auth0 export is a breadth scaffold, not yet plan-clean (the
pipeline's repo-wide secret scan is the backstop for the `client_secret` / `signing_secret` /
connection `options.client_secret` / action-secret / email-provider-credential / guardian-provider
material that generate-config-out may emit before the scrub rules land).

## Write-only / secret resources (EXCLUDE / scrub)

The credential plane is where Auth0's secrets live — scrub the value (keep the block, re-supply
out-of-band) or exclude the field/resource, exactly like Okta's `client_secret` / hook
`auth_scheme.value` / Datadog's `datadog_api_key`:

- **`auth0_client.client_secret`** — the confidential-application secret (Sensitive; returned on
  read) → **scrub the value**, keep the app block. The most common Auth0 secret. Also prune the
  read-only `signing_keys` / `encryption_key` (cert material, not adoptable).
- **`auth0_connection.options.client_secret` + `options.credentials`** — social/enterprise/OIDC
  connection secrets, SAML/SCIM signing + provisioning creds, and the database connection's
  `options.configuration` (encrypted custom-DB env) → **scrub** the secret-bearing option fields,
  keep the connection.
- **`auth0_resource_server.signing_secret`** — the HS256 API signing secret (computed/Sensitive)
  → **scrub the value**, keep the resource server.
- **`auth0_action.secrets`** — action secret *values* are **write-only** (API returns only the
  names) → **exclude the values** (keep the names).
- **`auth0_email_provider.credentials`** — the SMTP/API email credentials (password / api_key /
  AWS secret) are **write-only** → **EXCLUDE the credentials block** (adopt the provider shell).
- **`auth0_guardian` provider credentials** — Twilio `auth_token`, Duo `secret_key`, custom
  SMS/push provider keys → **scrub**.
- **`auth0_log_stream.sink` tokens** — sink `datadog_api_key` / `splunk_token` / HTTP
  `authorization` → **scrub** the token fields, keep the stream.
- **`auth0_client_credentials`** (a distinct resource, DEFERRED/EXCLUDED) — it *is* the client
  authentication secret (`client_secret` / `private_key_jwt`) → do not adopt; it is pure secret
  material.
- **`auth0_signing_keys` / tenant signing + encryption keys** (DEFERRED/EXCLUDED) — rotation
  state / read-only cert material; not adoptable.
- **The provider credentials themselves** — `AUTH0_CLIENT_SECRET` and the minted `access_token`
  (or `AUTH0_API_TOKEN`) are the tenant-wide master credentials; they live **only** in the
  `/oauth/token` body / `Authorization` header, never in generated config, state comments,
  errors, or logs. (There is no round-trippable "API token" resource to adopt.)
- **Not secret, do not over-scrub:** `auth0_resource_server.identifier` (a public audience URL),
  `auth0_connection` non-secret `options` (domain/client_id/scopes), `auth0_email_template` body
  (content, not a credential), `auth0_tenant`/`_branding`/`_prompt` settings, log-stream non-token
  sink config.

## Deliberately out of scope

- **User plane** (`auth0_user`, `auth0_user_permission`, `auth0_user_role`, `auth0_user_roles`) —
  bulk + PII; Terraformer enumerates users, but adopting a tenant's users as IaC is rarely wanted
  and is a large N with privacy weight. A much-later increment; Phase A adopts the config, not the
  directory.
- **The `::`-composite relationship/membership plane** — `auth0_connection_client(s)`,
  `auth0_organization_connection(s)`, `auth0_organization_member(s)`,
  `auth0_organization_member_role(s)`, `auth0_role_permission(s)`, `auth0_trigger_actions` — the
  who-is-enabled/assigned-for-what plane: N×M `::` composites (varying depth) with their own
  import ids, and adopting them fights the individual resources. The increment right after Phase A
  (establishes the `::` encoding); Phase A imports the bare client/connection/role/org and NOT the
  joins. **`auth0_trigger_actions` (the action→flow binding) is the first of these**, adopted just
  after `auth0_action`.
- **Deprecated rules/hooks** (`auth0_rule`, `auth0_rule_config`, `auth0_hook`) — superseded by
  `auth0_action` (covered). Terraformer still generates them; **do not** — new tenants have none
  and Auth0 is sunsetting them.
- **Custom domains** (`auth0_custom_domain`, `auth0_custom_domain_verification`) — DNS +
  certificate lifecycle, verification-gated, and the Management API audience gotcha (above). A
  later increment.
- **Singleton SUB-settings** (`auth0_prompt_custom_text`, `auth0_prompt_screen_partial(s)`,
  `auth0_pages`, `auth0_branding_theme`, `auth0_phone_provider`, `auth0_email_templates` beyond
  the core set) — per-prompt/per-language/per-screen content and additional tenant settings; a
  large low-value content surface. Later increments (Phase A covers the six *top-level*
  singletons).
- **Guardian SUB-factors as separate resources** — none exist; `auth0_guardian` is one combined
  resource (covered). No fan-out here.
- **Actions extensibility depth** (`auth0_flow`, `auth0_form`, `auth0_flow_vault_connection`,
  `auth0_token_exchange_profile`, `auth0_self_service_profile`, `auth0_encryption_key(s)`) —
  newer Auth0 Forms/Flows + key-management planes; dedicated later increments.
- **Client credential/config depth** (`auth0_client_credentials`, `auth0_connection_scim_configuration`,
  `auth0_connection_keys`) — pure secret/rotation resources; excluded above.
- **Cloud-IAM depth** (`Capabilities.IAM=false`): `auth0_role` is modeled at breadth (no
  secrets), but role→permission assignments, user→role assignments, and org-member roles are the
  deferred `::` plane, not Phase A.
- **Log / data planes** — the tenant logs (`GET /api/v2/logs`, checkpoint-paginated), user
  sessions, stats, jobs, and grants-as-data are runtime DATA behind the config, per scope. Out of
  scope (config only). `auth0_log_stream` (the *sink definition*) is in scope; the log *events*
  are not.
- **`auth0.v5` SDK dependency** — Terraformer pulls `gopkg.in/auth0.v5/management`; TerraLift uses
  a raw `net/http` client (smaller, matches Okta/Opsgenie, and the v5 SDK predates
  connections/organizations/attack-protection anyway). A deliberate non-adoption.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)

The **recommended Phase-A CONFIG CORE** (~15 TF types across ~11 enumeration passes):
`auth0_client`, `auth0_resource_server`, `auth0_connection`, `auth0_role`, `auth0_action`,
`auth0_organization`, `auth0_client_grant`, `auth0_email_template`, `auth0_log_stream`, and the
six settings singletons `auth0_tenant`, `auth0_branding`, `auth0_attack_protection`,
`auth0_prompt`, `auth0_guardian`, `auth0_email_provider`.

BEACHHEAD `auth0_client` + `auth0_resource_server` + `auth0_connection` (the application/API/
identity core essentially every tenant manages as IaC — apps, the APIs they call, and the
identity providers/databases behind login; all three are keyed+total lists, they exercise the
**OAuth2 client-credentials token exchange** + the `page`/`per_page`/`include_totals` pager on the
tenant's largest lists, and they anchor the **secret-scrub** discipline — `client_secret` /
`signing_secret` / connection `options.client_secret` — plus the `client_id`-not-`id` import
subtlety, the system-object skip, and the `connection.strategy` curation discriminator, the
provider's defining hazard) → INC-1 `auth0_role` + `auth0_action` (the RBAC + extensibility
core; role is a trivial bare-id list, action adds the **write-only `secrets`** exclude and the JS
`code` literal-string hazard; sets up the `auth0_trigger_actions` `::` binding that follows) →
INC-2 `auth0_organization` + `auth0_client_grant` (the B2B-org + M2M-grant plane; both bare-id
keyed+total lists — organizations also proves the checkpoint fallback exists for >1000-org
tenants) → INC-3 the six **settings singletons** (`auth0_tenant`, `auth0_branding`,
`auth0_attack_protection`, `auth0_prompt`, `auth0_guardian`, `auth0_email_provider`) + the
`auth0_email_template` name fan-out (establishes the **singleton-object GET** shape, the
**arbitrary-placeholder import id** with a stable sentinel, the three-GET `attack_protection`
combine, the `email_provider` **credentials-exclude**, the guardian provider-secret scrub, and
the fixed-name `email-templates` loop) → INC-4 `auth0_log_stream` (the lone **bare-array**
endpoint; sink-token scrub) → LATER the user plane, the `::`-composite relationship/membership
plane (`auth0_trigger_actions` first, then connection_client / organization_connection+member /
role_permission), deprecated rules/hooks, custom domains, the singleton sub-settings
(prompt_custom_text / pages / branding_theme / phone_provider), the Forms/Flows + key-management
planes, and the log/data planes.
