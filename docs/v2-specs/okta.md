# Okta provider — build spec

Research artifact for the `okta` provider (Phase A scaffold; TF provider source is
`okta/okta`, product "Okta" — the identity/access platform). Sources: Terraformer's
`providers/okta/` (built on the `okta/okta-sdk-golang` v2+v5 Go SDKs — ~40 generators),
the `okta/okta` registry docs (import formats + schema, **verified per-resource below**
against the provider's `website/docs/r/*.markdown`), and the Okta Core (Management) REST
API (`https://<org>.okta.com/api/v1/…`). Build mirrors the **Opsgenie** provider
(`internal/providers/opsgenie/`) and **PagerDuty** (`internal/providers/pagerduty/`) — a
flat, org-scoped, single-container REST provider (a direct `net/http` client, no CLI,
`terraform plan -generate-config-out` for export) — plus the same **per-parent fan-out**
pattern for its sub-resources. This is **REST, Opsgenie/PagerDuty-style, NOT GraphQL.**

**Okta is a HUGE provider (100+ resources). This spec scopes Phase A to a tractable
CONFIG CORE (~20 resource types across ~9 enumeration passes)** — the identity/access
config that essentially every Okta org manages as IaC — and explicitly DEFERS the long
tail (profile-schema properties, brand/theme/email-templates, factors/authenticators/
captcha, the app-user/app-group assignment + membership composites, behaviors, and the
other policy families) to later increments (see Build order). Five facts set Okta apart
from every prior provider, all load-bearing and called out below:

1. **Auth is the literal `Authorization: SSWS <api-token>` header** — *not* `Bearer`, *not*
   a custom `X-…-Key` header. The string `SSWS ` (with the trailing space) is a real prefix
   — the Okta analogue of Opsgenie's `GenieKey ` and PagerDuty's `Token token=`. Read from
   `OKTA_API_TOKEN`.
2. **The base URL is CONSTRUCTED from two env vars** — `https://<OKTA_ORG_NAME>.<OKTA_BASE_URL>`
   (org name + domain, both user-supplied, the Grafana-style host-from-config pattern), not a
   single URL and not a fixed region table.
3. **Pagination is LINK-HEADER (RFC 5988), a NEW pattern** — list bodies are BARE JSON arrays
   `[...]` with **no envelope**; the next-page URL lives in the HTTP **`Link` header**
   (`rel="next"`). The client's `Do` must **expose response headers**, parse the `rel="next"`
   link, and follow it until absent. The next URL is server-supplied → **host-validate it
   before re-sending the SSWS token** (the Opsgenie `paging.next` / Fastly `links.next`
   lesson).
4. **MANY type discriminators — the #1 classification hazard.** Several list endpoints return
   heterogeneous objects discriminated by a `type`/`signOnMode` field: apps by `signOnMode`,
   policies by `type` (and you MUST pass `?type=`), IdPs by `type`, zones by `type`.
5. **Fan-out composites go up to THREE parts.** `okta_auth_server` fans out to
   scopes/claims/policies, and policies fan out again to rules — so
   `okta_auth_server_policy_rule` imports by a **three-part** `<auth_server_id>/<policy_id>/<rule_id>`;
   most other resources import by a bare id.

## Version pin (load-bearing)

Pin `okta/okta ~> 4.x` (current major; org is lowercase `okta`, product "Okta"). Naming
facts that matter (the Terraformer-vs-current divergences):

- **Terraformer's coverage is broad but dated.** Its generators (built on the mixed
  `okta-sdk-golang` v2 **and** v5 SDKs) cover users, groups, group rules, the app subtypes,
  auth servers (+ scopes/claims/policies/rules), the signon/password/mfa policies (+ rules),
  network zones, trusted origins, inline/event hooks, IdPs (oidc/saml/social), user types,
  factors, authenticators, and the user/app **schema** properties. Phase A keeps the
  identity/access **config** core and drops schema/factors/authenticators/social-idp to
  later increments (below). **Do NOT pull the `okta/okta-sdk-golang` SDK** the way
  Terraformer did — a raw `net/http` client is smaller and matches the Opsgenie/PagerDuty
  providers (a deliberate non-adoption).
- Terraformer reads `OKTA_ORG_NAME` + `OKTA_BASE_URL` + `OKTA_API_TOKEN` and builds the org
  URL as `https://<org_name>.<base_url>`. The **TF provider** reads the *same* three
  (`org_name` / `base_url` / `api_token`, envs `OKTA_ORG_NAME` / `OKTA_BASE_URL` /
  `OKTA_API_TOKEN`), plus an optional `OKTA_HTTP_PROXY`. The REST endpoints below are
  provider-version-independent.
- **The TF provider ALSO supports OAuth2 private-key-JWT** (`client_id` + `private_key` /
  `private_key_id` + `scopes`) for scoped API access. **Base the scaffold on the SSWS
  api_token** (simpler, matches Terraformer, and is what most orgs script with). Note the
  OAuth path as a later auth-mode option; do not build it in Phase A.
- **`default`-named singletons map to distinct TF resources.** The auth server named
  `default` is `okta_auth_server_default`, the auth-server claim named `sub` is
  `okta_auth_server_claim_default`, and the built-in MFA policy `Default Policy` /
  password/signon default policies are the `okta_*_default` variants (Terraformer already
  branches on these). Phase A emits the **plain** `okta_auth_server` / `okta_policy_*` for
  non-default objects and **flags the default singletons** as adopt-in-place (they cannot be
  created/destroyed) — VERIFY the exact `_default` resource names per registry at build.

## Shape

- **Auth — the distinctive `Authorization: SSWS <api-token>` header (the hard divergence).**
  Every request carries:
  - `Authorization: SSWS <OKTA_API_TOKEN>` — note the literal `SSWS ` prefix (with the
    trailing space before the raw token). This is **not** `Bearer <token>` and **not** a
    custom header; getting the prefix wrong is a silent 401 (`E0000011 Invalid token
    provided`). It is the Okta analogue of Opsgenie's `GenieKey ` / PagerDuty's `Token
    token=`.
  - `Accept: application/json` + `Content-Type: application/json` (harmless on GET).
  Read the token from `OKTA_API_TOKEN`. A direct `net/http` client (mirror `opsgenieapi.go`
  / `pagerdutyapi.go`); **no Okta CLI**, and **no** `okta-sdk-golang`. The token rides **only**
  on the `Authorization` header, **never** in the URL, errors, or logs (same discipline as the
  GenieKey / DD-API-KEY). Force `https`; **refuse redirects** (mirror `ogHTTPClient` /
  `pdHTTPClient`) so the token cannot be replayed to another host on a 3xx.
- **Base URL — CONSTRUCTED from org name + domain (must be read from env; the Grafana-style
  host-from-config pattern).** There is no fixed base and no region table: the org URL is
  built as `https://<OKTA_ORG_NAME>.<OKTA_BASE_URL>` — e.g. `OKTA_ORG_NAME=dev-12345` +
  `OKTA_BASE_URL=okta.com` → `https://dev-12345.okta.com`; the preview cell uses
  `OKTA_BASE_URL=oktapreview.com` → `https://dev-12345.oktapreview.com`. All Management API
  paths are under `…/api/v1/`. Construction rules:
  - Require both `OKTA_ORG_NAME` and `OKTA_BASE_URL`; **force https**; build the host as
    exactly `org_name + "." + base_url` (strip any scheme/slashes a user pastes into either).
  - Store the resolved base host once and **host-validate every server-supplied next-page URL
    against it** before re-sending the SSWS token (see pagination).
  - `OKTA_HTTP_PROXY` (optional) selects an HTTP proxy; it does not change the host validation
    target. Custom-domain orgs (vanity `id.example.com`) exist but are out of scope — Phase A
    assumes the `<org>.<base_url>` construction the TF provider uses.
- **Scope — one Okta org = one flat container.** The token is **org-scoped**; there is no
  sub-org and no multi-tenant resolution — the token simply **is** the org.
  `model.ScopeTenant`, `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`. Resolve the
  container id/name **best-effort** via a lightweight `GET /api/v1/org` (returns
  `{"id","subdomain","companyName",…}` — the org display name); if that endpoint is gated
  (needs `okta.orgs.read`), fall back to `GET /api/v1/users?limit=1` for validation and use
  `OKTA_ORG_NAME` / the host string as the display id. As flat as Opsgenie: the token is the
  org, no id lookup required.
- **Response family — BARE JSON ARRAYS + LINK-HEADER pagination (the key structural fact,
  unlike every prior provider).** Almost every list endpoint returns the collection as a
  **bare `[...]` array in the body with NO envelope** — no `data`/`paging` wrapper (Opsgenie),
  no keyed offset/`more` (PagerDuty), no JSON:API `meta` (Datadog). Unmarshal the body
  straight into `[]T`. The next-page cursor is **NOT in the body** — it is in the HTTP
  **`Link` header** (RFC 5988):
  ```
  Link: <https://dev-12345.okta.com/api/v1/users?after=00u...&limit=200>; rel="next"
  Link: <https://dev-12345.okta.com/api/v1/users?limit=200>; rel="self"
  ```
  A response commonly carries **multiple** `Link` headers (`self`, `next`, sometimes `prev`);
  follow **only `rel="next"`**, ignore `self`/`prev`, and stop when no `rel="next"` is present.
  **This forces a client-shape change from Opsgenie: `Do` must return the response HEADERS
  (at minimum the `Link` header set), not just body + status.** Implement one generic
  **bare-array + Link-next** helper: fetch `?limit=200`, unmarshal `[]T`, parse the `Link`
  header for `rel="next"`, GET that URL, repeat. Bound every loop defensively (`oktaMaxPages`).
- **The `rel="next"` URL is SERVER-SUPPLIED — host-validate it before re-sending the token
  (do not miss this).** Okta hands back a **fully-qualified next URL** in the `Link` header,
  and we issue a GET to it with the `Authorization: SSWS` token. A next-link pointing at
  another host would leak the org token — and this is **not** an HTTP redirect, so
  `CheckRedirect` never fires and Go does not strip the header. **Validate the next URL's
  scheme+host against the constructed org base before each follow** (mirror `isOpsgenieURL`
  / Fastly's `isFastlyURL`): require `https` and host == the resolved `<org>.<base_url>`;
  refuse and error otherwise. This is the single most important safety rule of the client.
- **Pagination — Link-header everywhere; a few endpoints are single-page.** The Link-next
  helper handles both uniformly (single-page responses simply omit `rel="next"`). Empirically:
  users/apps/groups can be **large and deeply paged** (default `limit` is small — request
  `limit=200`, the common max; users especially can run to many pages); auth-server sub-lists,
  policies, zones, hooks, and IdPs are usually single-page but **always honour `rel="next"`
  if present** so a big org is never truncated. VERIFY the max `limit` per endpoint at build.
- **Rate limiting is aggressive and per-org (flag it).** Okta enforces org-wide per-endpoint
  rate limits and returns `429` with `X-Rate-Limit-Limit` / `X-Rate-Limit-Remaining` /
  `X-Rate-Limit-Reset` (epoch-seconds) headers (and `Retry-After` on 429). Read these off the
  response (the client already exposes headers for pagination) and back off — a bulk user/app
  enumeration WILL brush the limit. Bound + honour the reset; do not hammer.
- **Status handling (mirror `list` / `opsgenieAPIError`).** Okta errors are
  `{"errorCode":"E0000…","errorSummary":…,"errorId":…,"errorCauses":[…]}` (HTTP status carries
  the meaning). **401** (`E0000011`) → token invalid/expired → fatal, surfaced in preflight;
  if it appears mid-run every remaining list fails too → treat as fatal, not a partial
  inventory. **403** (`E0000006`) → the token's admin role lacks the read (e.g. a read-only
  vs super-admin token, or a feature the role can't see) → best-effort Verbose skip. **404**
  → feature/endpoint absent on the org edition → Verbose skip. **429** → rate-limited → honour
  `X-Rate-Limit-Reset`/`Retry-After` and back off. **5xx / network** → enumeration may be
  silently incomplete → Warn + count (tell a systemic failure apart from an empty org). The
  token never appears in errors/logs; strip any query string before a URL is logged
  (belt-and-suspenders, mirror `redactURL`).
- **Preflight**: `terraform` present + `OKTA_ORG_NAME` **and** `OKTA_BASE_URL` **and**
  `OKTA_API_TOKEN` set + a lightweight authenticated call succeeds. Use `GET
  /api/v1/users?limit=1` (any admin token can read it) as the validation probe, and
  best-effort `GET /api/v1/org` for the org display name (subdomain/companyName). `/org`
  doubling as the name probe means later 403/404 skips are explained rather than surprising.
- **Connect**: no real resolution — the token is the org. Validate the probe succeeds, resolve
  the org name from `/api/v1/org` (best-effort; else `OKTA_ORG_NAME` / host string), and set
  the single flat container (`model.ScopeTenant`).

## Type discriminators + fan-out composites — the CRITICAL determination

This is Okta's analogue of Opsgenie's "bare-vs-slash + team-scope" call and Datadog's "which
API version/shape" call. The load-bearing per-resource facts are **(a) which DISCRIMINATOR
field maps a heterogeneous list object to its TF type (and its value → type table); (b) is
the resource enumerated flat, or via a per-parent FAN-OUT (and how deep); and (c) is the
import id a BARE id or a SLASH composite — and if composite, is it TWO parts or THREE.** Get
(a) wrong and the wrong TF type (or none) is emitted for a mixed-list object; get (b) wrong
and you never reach the sub-resources (or you re-list apps N times); get (c) wrong and every
import block for that type is un-importable. All three are **verified against the registry
`website/docs/r/*.markdown`** and pinned per-resource in the catalog. The rules:

- **Apps — discriminate on `signOnMode` (one `GET /api/v1/apps`, fan the result into types).**
  A single list returns every application; map by `signOnMode`:
  | `signOnMode` | TF type |
  |---|---|
  | `OPENID_CONNECT` | `okta_app_oauth` |
  | `SAML_2_0` (and `SAML_1_1`) | `okta_app_saml` |
  | `AUTO_LOGIN` | `okta_app_auto_login` |
  | `BOOKMARK` | `okta_app_bookmark` |
  | `BASIC_AUTH` | `okta_app_basic_auth` |
  | `BROWSER_PLUGIN` | `okta_app_swa`, **or** `okta_app_three_field` when `name == "template_swa3field"` |
  | `SECURE_PASSWORD_STORE` | `okta_app_secure_password_store` |
  - **Skip Okta's own admin/dashboard/template apps by `name`**: `saasure` (the Okta admin
    console), `okta_enduser`, `okta_browser_plugin`, `template_wsfed`, `template_swa_two_page`
    (Terraformer's exact skip list) — plus any `WS_FEDERATION`/unmapped `signOnMode` (no clean
    TF type) → skip at Verbose. **BROWSER_PLUGIN needs a second discriminator** (`name`) to
    split SWA vs three-field — the one nested discriminator.
- **Policies — discriminate on `type`, and you MUST pass `?type=<TYPE>` (there is no list-all).**
  `GET /api/v1/policies` **requires** a `type` query parameter, so enumeration **loops the
  Phase-A policy types** and lists each:
  | `?type=` | TF type | rules → |
  |---|---|---|
  | `OKTA_SIGN_ON` | `okta_policy_signon` | `okta_policy_rule_signon` |
  | `PASSWORD` | `okta_policy_password` | `okta_policy_rule_password` |
  | `MFA_ENROLL` | `okta_policy_mfa` | `okta_policy_rule_mfa` |
  Each policy then **fans out to its rules** (`GET /api/v1/policies/<policy_id>/rules`), which
  map to the matching `okta_policy_rule_*` (the parent's type fixes the rule type). DEFERRED
  policy types (later increments, not Phase A): `ACCESS_POLICY` (app sign-on →
  `okta_app_signon_policy` + `_rule`), `PROFILE_ENROLLMENT`, `IDP_DISCOVERY`,
  `POST_AUTH_SESSION`, `POST_AUTH_SESSION`/device-assurance — VERIFY the current resource
  names when those increments land.
- **IdPs — discriminate on `type`.** `GET /api/v1/idps` (or the type-filtered `?type=`):
  `OIDC` → `okta_idp_oidc`, `SAML2` → `okta_idp_saml`. Social types (`GOOGLE`/`FACEBOOK`/
  `LINKEDIN`/`MICROSOFT`/`APPLE`) → `okta_idp_social` are **DEFERRED** (each carries a
  `client_secret`; later increment).
- **Network zones — discriminate on `type` for curation (single TF type).** `GET /api/v1/zones`
  returns `IP`, `DYNAMIC`, and `ENHANCED_DYNAMIC` zones — all map to the one `okta_network_zone`
  resource (the `type` field selects which sub-attributes are populated); no split.
- **Groups — filter, not discriminate.** `GET /api/v1/groups?filter=type eq "OKTA_GROUP"` →
  `okta_group`; **skip `BUILT_IN`** (the immutable "Everyone" group) and **`APP_GROUP`**
  (app-sourced, not directly manageable) — those cannot be adopted as `okta_group`.
- **Fan-outs (the Opsgenie/PagerDuty per-parent pattern) — the deepest is 3 levels:**
  - **auth_server → scopes / claims / policies**, and **policy → rules** (a *nested* fan-out):
    `GET /api/v1/authorizationServers` → per server `…/scopes`, `…/claims`, `…/policies`, and
    per policy `…/policies/<pid>/rules`. Import ids deepen with the nesting:
    `okta_auth_server` = bare `<auth_server_id>`; scope/claim/policy = **two-part**
    `<auth_server_id>/<child_id>`; policy rule = **THREE-part**
    `<auth_server_id>/<policy_id>/<rule_id>`.
  - **policy → rules** (top-level policies): per policy `GET /api/v1/policies/<pid>/rules` →
    `okta_policy_rule_*` = **two-part** `<policy_id>/<rule_id>`.
  The discriminator value→type tables, the fan-out shape, and the composite depth (bare / 2-part
  / 3-part) are the things we cannot get wrong — enumerated per-resource in the catalog and
  re-verified against the registry docs at build. Encode the import id as an explicit
  per-TF-type switch in `importid.go` (mirror Opsgenie's `rawImportID`) — never infer the
  separator or the parent count.

## Enumeration spine

Flat org scope. The pattern is: a set of top-level bare-array + Link-next lists, plus the
per-parent fan-outs above. Best-effort per list (403 role-absent / 404 feature-absent →
Verbose skip; 401 → fatal; other → Warn + count, so a systemic failure is told apart from an
empty org). Each list is tagged with its discriminator + pager. The SSWS token never appears
in errors/logs; every `rel="next"` follow is host-validated first.

- **Top-level flat lists:**
  - `GET /api/v1/apps?limit=200` → one bare array → discriminate on `signOnMode` into the app
    types (skip `saasure`/`okta_enduser`/`okta_browser_plugin`/`template_wsfed`/
    `template_swa_two_page`; split BROWSER_PLUGIN on `name`). **One list, seven TF types** — do
    NOT re-list per type.
  - `GET /api/v1/groups?filter=type eq "OKTA_GROUP"&limit=200` → `okta_group` (skip
    BUILT_IN/APP_GROUP). Can be large — page it.
  - `GET /api/v1/groups/rules?limit=200` → `okta_group_rule`.
  - `GET /api/v1/users?limit=200` → `okta_user`. **Potentially the largest list in the org** —
    page hard and expect to brush the rate limit; PII (see curation).
  - `GET /api/v1/meta/types/user` → `okta_user_type` (single page; the `default` type is
    built-in — flag as adopt-in-place, not creatable).
  - `GET /api/v1/zones?limit=200` → `okta_network_zone` (types IP/DYNAMIC/ENHANCED_DYNAMIC → one
    TF type).
  - `GET /api/v1/trustedOrigins?limit=200` → `okta_trusted_origin`.
  - `GET /api/v1/authorizationServers?limit=200` → `okta_auth_server` (+ scope/claim/policy/rule
    fan-outs below; the `default` server → flag as `okta_auth_server_default`).
  - `GET /api/v1/idps?limit=200` → discriminate on `type`: `OIDC` → `okta_idp_oidc`, `SAML2` →
    `okta_idp_saml` (skip social/other → deferred).
  - `GET /api/v1/inlineHooks` → `okta_inline_hook`.
  - `GET /api/v1/eventHooks` → `okta_event_hook`.
  - **Policies (per-type — MUST pass `?type=`):** loop
    `GET /api/v1/policies?type=OKTA_SIGN_ON` → `okta_policy_signon`,
    `?type=PASSWORD` → `okta_policy_password`, `?type=MFA_ENROLL` → `okta_policy_mfa`. Capture
    each policy id for the rules fan-out.
- **Per-parent fan-outs:**
  - per **auth_server**: `…/scopes` → `okta_auth_server_scope` (`<as_id>/<scope_id>`);
    `…/claims` → `okta_auth_server_claim` (`<as_id>/<claim_id>`; claim `sub` → flag
    `okta_auth_server_claim_default`); `…/policies` → `okta_auth_server_policy`
    (`<as_id>/<policy_id>`); per auth-server policy `…/policies/<pid>/rules` →
    `okta_auth_server_policy_rule` (**three-part** `<as_id>/<pid>/<rule_id>`).
  - per **policy** (top-level signon/password/mfa): `GET /api/v1/policies/<pid>/rules` →
    `okta_policy_rule_signon` / `_password` / `_mfa` (`<policy_id>/<rule_id>`).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as the Opsgenie/PagerDuty
`enumerate.go`).

## Resource catalog

Import IDs verified against the current `okta/okta` registry docs
(`website/docs/r/*.markdown`). All scope = org. "endpoint → shape" is the list path (bare
JSON array + Link-next unless noted). "disc / fan-out" names the discriminator field or the
parent fan-out. The **sep** column is the #1 hazard — **bare / 2-part slash / 3-part slash**.

| native key | TF type | endpoint → shape | disc / fan-out | id field | import ID | sep |
|---|---|---|---|---|---|---|
| okta:user | okta_user | `GET /api/v1/users` (bare array) | — | `id` | `<user_id>` | bare |
| okta:group | okta_group | `GET /api/v1/groups?filter=type eq "OKTA_GROUP"` | skip BUILT_IN/APP_GROUP | `id` | `<group_id>` | bare |
| okta:group_rule | okta_group_rule | `GET /api/v1/groups/rules` | — | `id` | `<group_rule_id>` | bare |
| okta:user_type | okta_user_type | `GET /api/v1/meta/types/user` | — | `id` | `<user_type_id>` | bare (`default` built-in) |
| okta:app_oauth | okta_app_oauth | `GET /api/v1/apps` | signOnMode `OPENID_CONNECT` | `id` | `<app_id>` | bare (**client_secret SECRET**) |
| okta:app_saml | okta_app_saml | `GET /api/v1/apps` | signOnMode `SAML_2_0`/`SAML_1_1` | `id` | `<app_id>` | bare (signing creds) |
| okta:app_auto_login | okta_app_auto_login | `GET /api/v1/apps` | signOnMode `AUTO_LOGIN` | `id` | `<app_id>` | bare (SWA creds) |
| okta:app_bookmark | okta_app_bookmark | `GET /api/v1/apps` | signOnMode `BOOKMARK` | `id` | `<app_id>` | bare |
| okta:app_basic_auth | okta_app_basic_auth | `GET /api/v1/apps` | signOnMode `BASIC_AUTH` | `id` | `<app_id>` | bare (SWA creds) |
| okta:app_swa | okta_app_swa | `GET /api/v1/apps` | signOnMode `BROWSER_PLUGIN` (name≠three-field) | `id` | `<app_id>` | bare (SWA creds) |
| okta:app_three_field | okta_app_three_field | `GET /api/v1/apps` | `BROWSER_PLUGIN` + name `template_swa3field` | `id` | `<app_id>` | bare (SWA creds) |
| okta:app_secure_password_store | okta_app_secure_password_store | `GET /api/v1/apps` | signOnMode `SECURE_PASSWORD_STORE` | `id` | `<app_id>` | bare (shared creds SECRET) |
| okta:trusted_origin | okta_trusted_origin | `GET /api/v1/trustedOrigins` | — | `id` | `<trusted_origin_id>` | bare |
| okta:network_zone | okta_network_zone | `GET /api/v1/zones` | type IP/DYNAMIC/ENHANCED_DYNAMIC → one type | `id` | `<zone_id>` | bare |
| okta:auth_server | okta_auth_server | `GET /api/v1/authorizationServers` | parent (name `default` → `_default`) | `id` | `<auth_server_id>` | bare |
| okta:auth_server_scope | okta_auth_server_scope | `…/authorizationServers/<id>/scopes` | ← auth_server | `id` | `<auth_server_id>/<scope_id>` | **2-part slash** |
| okta:auth_server_claim | okta_auth_server_claim | `…/authorizationServers/<id>/claims` | ← auth_server (claim `sub` → `_default`) | `id` | `<auth_server_id>/<claim_id>` | **2-part slash** |
| okta:auth_server_policy | okta_auth_server_policy | `…/authorizationServers/<id>/policies` | ← auth_server | `id` | `<auth_server_id>/<policy_id>` | **2-part slash** |
| okta:auth_server_policy_rule | okta_auth_server_policy_rule | `…/policies/<pid>/rules` | ← auth_server → policy | `id` | `<auth_server_id>/<policy_id>/<rule_id>` | **3-part slash** |
| okta:policy_signon | okta_policy_signon | `GET /api/v1/policies?type=OKTA_SIGN_ON` | type discriminator (`?type=` required) | `id` | `<policy_id>` | bare |
| okta:policy_password | okta_policy_password | `GET /api/v1/policies?type=PASSWORD` | `?type=` required | `id` | `<policy_id>` | bare |
| okta:policy_mfa | okta_policy_mfa | `GET /api/v1/policies?type=MFA_ENROLL` | `?type=` required (Default → `_default`) | `id` | `<policy_id>` | bare |
| okta:policy_rule_signon | okta_policy_rule_signon | `…/policies/<pid>/rules` | ← policy_signon | `id` | `<policy_id>/<rule_id>` | **2-part slash** |
| okta:policy_rule_password | okta_policy_rule_password | `…/policies/<pid>/rules` | ← policy_password | `id` | `<policy_id>/<rule_id>` | **2-part slash** |
| okta:policy_rule_mfa | okta_policy_rule_mfa | `…/policies/<pid>/rules` | ← policy_mfa | `id` | `<policy_id>/<rule_id>` | **2-part slash** |
| okta:inline_hook | okta_inline_hook | `GET /api/v1/inlineHooks` | — | `id` | `<inline_hook_id>` | bare (**auth header SECRET**) |
| okta:event_hook | okta_event_hook | `GET /api/v1/eventHooks` | — | `id` | `<event_hook_id>` | bare (**auth header SECRET**) |
| okta:idp_oidc | okta_idp_oidc | `GET /api/v1/idps` | type `OIDC` | `id` | `<idp_id>` | bare (**client_secret SECRET**) |
| okta:idp_saml | okta_idp_saml | `GET /api/v1/idps` | type `SAML2` | `id` | `<idp_id>` | bare (signing creds) |

### Import-format quirks (§ do not get wrong)
1. **Composite DEPTH is the #1 hazard — bare / 2-part / 3-part, all SLASH.** Every Okta
   composite uses a forward slash `/`. Most resources are **bare** (`<id>`). The auth-server
   sub-resources are **2-part** (`<auth_server_id>/<child_id>`) — scope, claim, policy. The
   top-level policy rules are **2-part** (`<policy_id>/<rule_id>`). **`okta_auth_server_policy_rule`
   is the one THREE-part id** (`<auth_server_id>/<policy_id>/<rule_id>`) — it lives two fan-out
   levels down (server → policy → rule), so both parents must be carried through. Encode the
   part-count per TF type in `importid.go`; never infer it.
2. **The parent id ORDER is outermost-first.** `<auth_server_id>/<policy_id>/<rule_id>` —
   server, then policy, then rule (matching the fan-out nesting). Store the left/mid/right in
   import order as you descend the fan-out (Terraformer already carries `auth_server_id` +
   `policy_id` as the resource's parent attributes).
3. **Apps import by the bare `<app_id>` — the `signOnMode` only chooses the TF *type*, not the
   id.** A misclassified `signOnMode` emits the wrong resource type for a correct id (a plan
   error, not an import error). Some app resources additionally accept optional import suffixes
   (`<app_id>/skip_users`, `/skip_groups`, `/skip_roles`) to skip pulling assignments on
   import — Phase A imports the **bare `<app_id>`** and does NOT adopt assignments (those are
   deferred composites, below).
4. **`default`-named singletons are distinct resources and cannot be created/destroyed.** The
   `default` auth server (`okta_auth_server_default`), the `sub` claim
   (`okta_auth_server_claim_default`), and the built-in default policies (`okta_policy_*_default`)
   are adopt-in-place only. Phase A flags them (still import by the same bare/2-part id) and
   emits the plain resource for everything non-default — VERIFY the exact `_default` type name
   per registry.
5. **All Okta ids are opaque strings off the wire** (`00u…` users, `00g…` groups, `0oa…` apps,
   `aus…` auth servers, etc.) — copy `id` verbatim, no numeric stringify (unlike Datadog's
   numeric monitor ids).
6. **Groups need the `type eq "OKTA_GROUP"` filter, not post-filtering.** Pass the filter in
   the query so BUILT_IN/APP_GROUP never enter the array — an APP_GROUP imported as `okta_group`
   is un-appliable.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a
live org — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the
Opsgenie/PagerDuty providers. Okta has **no single monster resource** (contrast Datadog's
`datadog_dashboard`); the weight is spread across the app/policy/rule config, and the
recurring hazards are the **app/idp/hook secrets**, the **`${…}` Okta Expression Language
templates** (Terraformer needed an `escapeDollar` `${` → `$${` hook), and the **default/
built-in singletons**.

- **App resources (`okta_app_oauth` / `_saml` / `_swa` / `_auto_login` / `_basic_auth` /
  `_three_field` / `_secure_password_store` / `_bookmark`) — medium; secrets + EL templates.**
  Common surface: `label`, `status`, `visibility`, `hide_ios`/`hide_web`. **Secrets by type:**
  `okta_app_oauth.client_secret` (confidential OIDC clients — Sensitive, returned on read →
  SCRUB); the SWA-family credential scheme (`SHARED_USERNAME_AND_PASSWORD` → `shared_password`
  / shared username; `EXTERNAL_PASSWORD_SYNC`) → scrub the shared password. **EL-template
  hazard:** SAML `attribute_statements` and OIDC/SWA field mappings carry Okta Expression
  Language `${user.…}` / `${app.…}` strings → the generated HCL must keep these **literal**
  (Terraformer's `escapeDollar` turned `${` into `$${`; VERIFY terraform's writer does the
  equivalent — else plan interpolation breaks). Prune computed `sign_on_mode`/`name`
  (server-assigned app name)/`logo_url`/`id`. **App assignments (users/groups) are NOT adopted**
  in Phase A (deferred composites) — import the bare app.
- **`okta_user` — light config, but PII + write-only creds.** `profile` (`first_name`/
  `last_name`/`email`/`login`), `status`, `custom_profile_attributes`. **No credential is
  round-trippable:** `password` / `recovery_question`+`answer` are **write-only** (never
  returned on read) → EXCLUDE the credentials block (adopt the user shell). PII (emails/names)
  is not a secret — adopt, but note the volume. **CAUTION:** your own admin user (behind the
  token) appears — adopt but do not deactivate/lock yourself out.
- **`okta_group` / `okta_group_rule` — light.** Group: `name`, `description`,
  `custom_profile_attributes`. Group rule: `expression_value` (Okta EL — `${…}` literal
  hazard), `group_assignments` (group-id refs), `status`. Group **memberships** are a separate
  resource (deferred). Prune computed.
- **`okta_auth_server` (+ scope/claim/policy/rule) — medium; the deepest fan-out.** Auth
  server: `audiences`, `issuer_mode`; the signing `credentials` rotate and are Okta-managed
  (prune computed `kid`/`credentials`). Scope: `name`/`consent`/`metadata_publish`. Claim:
  `value` (Okta EL `${…}` literal hazard), `claim_type`, `value_type`. Policy: `client_whitelist`
  (client-id refs). Rule: `grant_type_whitelist`, `scope_whitelist`, token lifetimes. The
  `default` server + `sub` claim are adopt-in-place singletons. Rule ordering (`priority`)
  is significant — preserve it.
- **`okta_policy_*` + `okta_policy_rule_*` — medium; defaults + ordering.** Policy:
  `status`, `priority`, `groups_included` (group refs). Rule: per-type conditions
  (`network_connection`/`network_includes`, `factor_sequence`/`enroll` for MFA, password age/
  complexity for password, session/behaviors for signon). **`priority` ordering churns** and
  the **`Default Policy` / default rule are built-in** (adopt-in-place, cannot be
  created/deleted) — flag them. Prune computed `id`/`system`.
- **`okta_network_zone` — light.** `type` (IP/DYNAMIC/ENHANCED_DYNAMIC), `gateways`/`proxies`
  (IP), `dynamic_locations`/`asns` (dynamic). Prune computed. No secret.
- **`okta_trusted_origin` — trivial.** `origin`, `scopes` (CORS/REDIRECT). No secret.
- **`okta_inline_hook` / `okta_event_hook` — SECRET (scrub).** Both carry a `channel` block
  with `channel.config.headers` (custom auth headers) and an `channel.config.auth_scheme`
  (`type=HEADER`, `key`, **`value`**) — the **`value` is the write-only bearer/secret token**
  Okta sends to authenticate itself to your endpoint → **scrub the value**, keep the block.
  `channel.config.uri` (the callback URL) is not itself secret. Event hook additionally lists
  `events`; inline hook has a `type` (e.g. `com.okta.oauth2.tokens.transform`). This is the
  hook-plane analogue of the Opsgenie `api_key` scrub.
- **`okta_idp_oidc` / `okta_idp_saml` — SECRET (oidc) + refs.** OIDC IdP: **`client_secret`**
  (write-only-on-read Sensitive → SCRUB), `client_id`, the `authorization`/`token`/`user_info`/
  `jwks` endpoint URLs, `issuer_url`, `scopes`. SAML IdP: `sso_url`, `sso_binding`,
  `issuer`, and a signing/trust `kid` (references an IdP key — the request-signing key is
  Okta-managed, the IdP's cert is public, not a secret). `provisioning_action`/`deprovisioned_action`
  and `subject_match_type`/`match_attribute` are config. Prune computed status/ids.
- **`okta_user_type` — trivial.** `name`/`display_name`/`description`. The `default` type is
  built-in (adopt-in-place). No secret.

Until Phase B these are no-ops, so an Okta export is a breadth scaffold, not yet plan-clean
(the pipeline's repo-wide secret scan is the backstop for the app/idp `client_secret` and the
hook auth-header `value` that generate-config-out emits before the scrub rules land).

## Write-only / secret resources (EXCLUDE / scrub)

The credential/integration plane is where Okta's secrets live — scrub the value (keep the
block, re-supply out-of-band) or exclude the field, exactly like Opsgenie's
`api_integration.api_key` / PagerDuty's `integration_key` / Datadog's `datadog_api_key`:

- **`okta_app_oauth.client_secret`** — the confidential-OIDC-client secret (Sensitive; returned
  on read) → **scrub the value**, keep the app block. The most common app secret.
- **SWA-family shared credentials** (`okta_app_swa`, `okta_app_three_field`,
  `okta_app_basic_auth`, `okta_app_secure_password_store`, `okta_app_auto_login`) — the
  `SHARED_USERNAME_AND_PASSWORD` scheme's **`shared_password`** (and per-user credential
  push) is write-only → scrub. The shared username is not itself a secret.
- **`okta_app_saml` / `okta_idp_saml` signing credentials** — the app/IdP request-signing
  **private key is Okta-managed and never exported** (only a `kid` / public cert is returned);
  nothing to scrub in the app, but do NOT attempt to round-trip a private key. The IdP's
  verification cert is public.
- **`okta_idp_oidc.client_secret`** — the external OIDC IdP client secret (write-only on read)
  → **scrub the value**, keep the block.
- **`okta_inline_hook` / `okta_event_hook` — `channel.config.auth_scheme.value` + custom
  `headers`** — the write-only bearer/secret token Okta presents to your callback endpoint →
  **scrub the value**. The callback `uri` is not secret.
- **`okta_user` credentials** — `credentials.password` (write-only, never returned) and
  `credentials.recovery_question.answer` → **EXCLUDE the credentials block** (adopt the user
  shell only; passwords/recovery are set out-of-band).
- **The provider `api_token` (`OKTA_API_TOKEN`) itself** — the SSWS token is the org-wide
  master credential; it lives **only** on the `Authorization` header, never in generated
  config, state comments, errors, or logs. (There is no round-trippable "API token" resource to
  adopt.)
- **Not secret, do not over-scrub:** `okta_user` profile (emails/names are PII, not
  credentials — adopt), `okta_trusted_origin` (origins are public), `okta_network_zone`
  (IP/ASN lists are not secret), `okta_idp_saml` public cert / `sso_url`.

## Deliberately out of scope
- **Profile-schema plane** (`okta_user_schema_property`, `okta_user_base_schema_property`,
  `okta_app_user_schema_property`, `okta_group_schema_property`) — the custom-attribute schema
  is a separate, order-sensitive surface (Terraformer covers it) with its own composite ids
  (`<schema_id>/<property_index>`-style); a later increment once the core objects are solid.
- **Brand / theme / email plane** (`okta_brand`, `okta_theme`, `okta_email_customization`,
  `okta_email_template_settings`, `okta_email_domain`, `okta_email_sender`) — the org
  customization surface, largely content + verification, and several carry per-brand composite
  ids. Later increment.
- **Factors / authenticators / captcha** (`okta_factor`, `okta_authenticator`, `okta_captcha`,
  `okta_captcha_org_wide_settings`, `okta_policy_device_assurance_*`) — the MFA-enrollment
  *mechanism* plane (Terraformer covers `okta_factor`/`okta_authenticator`); captcha carries a
  `secret_key`. Later increment.
- **App-assignment + membership composites** (`okta_app_group_assignment` =
  `<app_id>/<group_id>`, `okta_app_user` = `<app_id>/<user_id>`, `okta_group_memberships` /
  `okta_user_group_memberships`, `okta_group_role`, `okta_app_group_assignments`) — the
  who-is-assigned-to-what relationship plane: large N×M composites with their own slash import
  ids, and adopting them fights the individual resources. A later increment; Phase A imports the
  bare app/group/user and NOT the assignments.
- **Behaviors** (`okta_behavior`) — behavioral-detection rules referenced by signon policy
  rules; a small later increment.
- **The other policy families** — `ACCESS_POLICY` (app sign-on: `okta_app_signon_policy` +
  `_rule`), `PROFILE_ENROLLMENT`, `IDP_DISCOVERY`, `POST_AUTH_SESSION` — additional
  `?type=` values with their own resources; later increments (Phase A covers signon/password/
  mfa only).
- **Social IdPs** (`okta_idp_social` — Google/Facebook/LinkedIn/Microsoft/Apple) — each carries
  a `client_secret`; deferred with the secret-bearing IdP work. SMS templates
  (`okta_template_sms`) likewise deferred.
- **Admin-role / IAM depth** (`Capabilities.IAM=false`) — users/groups/user-types are modeled
  at breadth, but admin-role assignments (`okta_admin_role_targets`, `okta_group_role`,
  `okta_role_subscription`), SSO/SCIM provisioning, and org-level identity settings are not.
- **Org settings / domains / rate-limit config** — `okta_org_configuration`, `okta_domain`,
  `okta_rate_limiting`, `okta_threat_insight_settings`: org-singleton settings, better authored
  by hand; adopting them fights per-object config. Out of scope.
- **Data planes** — the System Log, user sessions, enrolled factors, group memberships as data,
  and app assignment *state* are runtime DATA behind the config, per scope. Out of scope
  (config only).
- **Okta SDK dependency** — Terraformer pulls `okta/okta-sdk-golang` (v2 + v5); TerraLift uses
  a raw `net/http` client (smaller, matches Opsgenie/PagerDuty). A deliberate non-adoption.
- **OAuth2 private-key-JWT auth mode** — the TF provider supports it, but Phase A bases the
  scaffold on the SSWS `api_token`. A later auth-mode option.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)
The **recommended Phase-A CONFIG CORE** (~20 TF types across ~9 enumeration passes):
`okta_user`, `okta_group`, `okta_group_rule`, `okta_user_type`, the app family
(`okta_app_oauth`/`_saml`/`_auto_login`/`_bookmark`/`_basic_auth`/`_swa`/`_three_field`/
`_secure_password_store` — one `/apps` list, discriminated), `okta_trusted_origin`,
`okta_network_zone`, `okta_auth_server` (+ `_scope`/`_claim`/`_policy`/`_policy_rule`
fan-outs), `okta_policy_signon`/`_password`/`_mfa` (+ `okta_policy_rule_*` fan-out),
`okta_inline_hook`, `okta_event_hook`, `okta_idp_oidc`, `okta_idp_saml`.

BEACHHEAD `okta_user` + `okta_group` + `okta_group_rule` (the identity core essentially every
org manages as IaC — all three are simple flat bare-id lists, they exercise the bare-array +
**Link-header pagination** on the largest lists in the org, and they anchor the group refs the
policies/apps point at; establishes the user credential-EXCLUDE and the group `type` filter) →
INC-1 the **app family** via one `GET /api/v1/apps` (the **`signOnMode` discriminator** — one
list into seven TF types, the BROWSER_PLUGIN→swa/three-field name split, the Okta-own-app skip
list, and the `client_secret`/SWA-shared-credential **secret-scrub** + the `${…}` Okta-EL
template escaping — the provider's defining classification hazard) → INC-2 `okta_auth_server`
(+ `_scope` / `_claim` / `_policy` / `_policy_rule`) (the **deepest fan-out** — establishes the
2-part `<auth_server_id>/<child_id>` composites and the one **3-part**
`<auth_server_id>/<policy_id>/<rule_id>` import, plus the `default` server / `sub` claim
adopt-in-place singletons) → INC-3 `okta_policy_signon` + `_password` + `_mfa` (+
`okta_policy_rule_*`) (the **`?type=`-required policy discriminator** — one loop over the three
types, each fanning out to its rules as the top-level 2-part `<policy_id>/<rule_id>` composite;
the `Default Policy` built-in flag and the `priority` ordering churn) → INC-4
`okta_network_zone` + `okta_trusted_origin` + `okta_user_type` (the light bare-id config tail;
zone `type` collapses to one resource) → INC-5 `okta_inline_hook` + `okta_event_hook` +
`okta_idp_oidc` + `okta_idp_saml` (the hook + IdP plane — the `channel.config.auth_scheme.value`
+ IdP `client_secret` **secret-scrub**, and the IdP `type` discriminator) → LATER the
profile-schema plane, brand/theme/email, factors/authenticators/captcha, the app-assignment +
membership composites, behaviors, the other policy families (`ACCESS_POLICY`/`PROFILE_ENROLLMENT`/
`IDP_DISCOVERY`), social IdPs, admin-role/IAM depth, org-settings singletons, and the data
planes.
