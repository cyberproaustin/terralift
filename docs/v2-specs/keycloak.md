# Keycloak provider — build spec

Research artifact for the `keycloak` provider (Phase A scaffold; TF provider source is
`keycloak/keycloak`, formerly `mrparkers/keycloak` — the Terraform provider for a self-hosted
**Keycloak** identity server / OIDC+SAML SSO). Sources: Terraformer's `providers/keycloak/`
(built on the dated `github.com/mrparkers/terraform-provider-keycloak/keycloak` Go SDK — a
**single** `realms` service generator that walks the whole server), the `keycloak/keycloak`
registry docs (import formats + schema, **verified per-resource below** against the provider's
`docs/resources/*.md`), and the Keycloak Admin REST API (`<KEYCLOAK_URL>/admin/realms/…`). Build
mirrors **three** prior providers at once — **Auth0** (`internal/providers/auth0/`) for the
**OAuth2 token-exchange** connect-time auth, **LaunchDarkly** (`internal/providers/launchdarkly/`)
for the **per-parent FAN-OUT** spine (Keycloak is *realm*-scoped exactly as LaunchDarkly is
*project*-scoped), and **Okta** (`internal/providers/okta/`) for the **type-discriminator + composite
import-depth** machinery. This is **REST, Auth0/Okta/LaunchDarkly-style, NOT GraphQL.**

**Keycloak is a large surface (realms × clients × roles × groups × scopes × flows × idps ×
federations, plus a deep mapper/sub-component plane). This spec scopes Phase A to the tractable
CONFIG CORE (~11 TF types)** — the realm/client/role/group/scope/flow/idp/federation config that
essentially every Keycloak server manages as IaC — and explicitly DEFERS the long tail (the user
plane, the many per-client/scope protocol mappers, the default-scope/default-group assignment
resources, the authentication sub-flow/execution/execution-config depth, and the LDAP-mapper /
sub-component plane) to later increments (see Build order). **Six facts set Keycloak apart from
every prior provider, all load-bearing and called out below:**

1. **Auth is an OAuth2 token EXCHANGE with TWO grant modes and a FORM-encoded body — the Auth0
   pattern, diverged.** Like Auth0, the Admin REST API is Bearer-authenticated with a short-lived
   token minted at connect time; **unlike** Auth0 the token endpoint takes
   `application/x-www-form-urlencoded` (not JSON) and there are **two** grant modes
   (client-credentials **or** password).
2. **The base URL is the user-supplied self-hosted server** (`KEYCLOAK_URL`, like Grafana/Okta) —
   **plus a `base_path`** (`/auth` on legacy Wildfly Keycloak; empty on 17+/Quarkus) that the
   Admin API paths hang off.
3. **The spine is a REALM FAN-OUT** — `GET /admin/realms` (parent) → per realm the
   clients/roles/groups/scopes/flows/idps/federations; and a **two-level realm×client** fan-out
   for client roles. One flat container = the Keycloak *server*.
4. **Pagination is `first`/`max` (offset/limit) on BARE JSON arrays** — list endpoints return a
   raw `[...]` (no envelope) and page by `?first=<offset>&max=<limit>`; a few are unpaged.
5. **MANY objects are keyed by an internal UUID, NOT the human name — and clients are the trap.**
   The import composite for a client is `<realm>/<client_UUID>` where the UUID is the object's
   `id`, **NOT** the human `clientId`. Same UUID-vs-name split for roles/groups/scopes/flows/
   federations; idps/required-actions are keyed by `alias`.
6. **Composite import ids are REALM-PREFIXED `/` composites** — bare `<realm>` for the realm
   itself, otherwise `<realm>/<leaf>` (2-part). **There is no 3-part id in the Phase-A set** (see
   the `keycloak_role` correction below — realm *and* client roles are the same 2-part TF type).

## Version pin (load-bearing)

Pin `keycloak/keycloak ~> 5.x` (**VERIFY the current major at build** — the provider moved org
from `mrparkers/keycloak` to `keycloak/keycloak`; Terraformer still imports the old
`github.com/mrparkers/terraform-provider-keycloak/keycloak` module path, but the registry source is
now `keycloak/keycloak`). Naming facts that matter (the Terraformer-vs-current + task-provisional
divergences):

- **`keycloak_role` is ONE resource for BOTH realm roles and client roles — there is NO separate
  `keycloak_realm_role` / `keycloak_client_role` (correct the provisional split).** The provider
  has a single `keycloak_role`; a *client* role is the same type with a `client_id` attribute set
  to the client's internal UUID (a realm role omits it). Terraformer confirms this — `role.go`
  emits `keycloak_role` for both, tagging client roles via `ContainerId`. **Consequently the
  client-role import is the same 2-part `<realm>/<role_id>` as a realm role** (the `role_id` is a
  globally-unique UUID), **NOT** a 3-part `<realm>/<client_uuid>/<role_id>` — that provisional
  3-part guess is wrong; VERIFY, but the registry has always used `{{realm_id}}/{{role_id}}`. The
  client UUID is carried as the resource's `client_id` *attribute*, not in the import id.
- **Clients split by `protocol` into two TF types.** Terraformer only emits `keycloak_openid_client`
  (it walks `GetOpenidClients`), but the same `GET …/clients` list also returns SAML clients
  (`protocol == "saml"`), which map to **`keycloak_saml_client`** — a discriminator, covered here
  from the registry + API directly (the Okta `signOnMode` precedent). Emit both.
- **Client scopes likewise split by `protocol`.** `keycloak_openid_client_scope` (protocol
  `openid-connect`) is the Phase-A target; `keycloak_saml_client_scope` (protocol `saml`) is its
  SAML analogue (same enumeration, later increment).
- **Terraformer's single `realms` generator does FAR more than Phase A** — it also walks users,
  every protocol-mapper type, default/optional client-scope *assignments*, service-account roles,
  group memberships/roles, default groups, authentication sub-flows/executions/execution-configs,
  and LDAP mappers. **Do NOT copy that breadth** and **do NOT pull the `mrparkers` SDK** — a raw
  `net/http` client is smaller and matches the Auth0/Okta/LaunchDarkly providers (a deliberate
  non-adoption). Phase A keeps the config core and defers the rest (Out of scope + Build order).
- Terraformer reads `url` + `base_path` + `client_id` + `client_secret` + `realm` +
  `client_timeout` (+ TLS knobs). The **TF provider** reads the *same* via `KEYCLOAK_URL` /
  `KEYCLOAK_BASE_PATH` / `KEYCLOAK_CLIENT_ID` / `KEYCLOAK_CLIENT_SECRET` / `KEYCLOAK_REALM` /
  `KEYCLOAK_CLIENT_TIMEOUT`, **or** the password grant via `KEYCLOAK_USER` / `KEYCLOAK_PASSWORD`.
  The REST endpoints below are provider-version-independent.

## Shape

- **Auth — OAuth2 token EXCHANGE, TWO grant modes, FORM-encoded body (the hard divergence from
  Auth0's JSON exchange).** Keycloak's Admin REST API is Bearer-authenticated, and the Bearer is a
  short-lived token minted at connect time from the token endpoint of the **`master` realm** (the
  configured auth realm, `KEYCLOAK_REALM`, default `master` — its token has cross-realm admin):
  - **Connect-time exchange:** `POST <KEYCLOAK_URL><base_path>/realms/master/protocol/openid-connect/token`,
    **`Content-Type: application/x-www-form-urlencoded`** (NOT JSON — the divergence from Auth0),
    body one of:
    - **client_credentials** (a service-account client, typically in the master realm):
      `grant_type=client_credentials&client_id=<KEYCLOAK_CLIENT_ID>&client_secret=<KEYCLOAK_CLIENT_SECRET>`
    - **password** (a bootstrap admin user via the built-in `admin-cli` client):
      `grant_type=password&client_id=admin-cli&username=<KEYCLOAK_USER>&password=<KEYCLOAK_PASSWORD>`
    → `{"access_token":"<jwt>","expires_in":60,"refresh_token":"…","refresh_expires_in":1800,…}`.
    This POST is itself **unauthenticated** (no Bearer); the `client_secret` / `password` ride in
    the **form body** only. **Precedence:** prefer client-credentials when
    `KEYCLOAK_CLIENT_ID`+`KEYCLOAK_CLIENT_SECRET` are set, else fall back to
    `KEYCLOAK_USER`+`KEYCLOAK_PASSWORD`.
  - **Then, on every `/admin/…` request:** `Authorization: Bearer <access_token>` +
    `Accept: application/json` (+ harmless `Content-Type: application/json` on GET). **The Keycloak
    access token is short-lived** (`expires_in` is often only **60s**, far shorter than Auth0's
    24h) — cache it and **re-mint (or use the `refresh_token`) when it would elapse mid-run**; a
    bulk multi-realm enumeration WILL outlive one token, so the refresh path is not optional here
    (contrast Auth0, where one exchange normally suffices).
  - **Secret discipline (mirror the Auth0 rule):** the `client_secret` / `password` appear ONLY in
    the `/token` form body; the `access_token`/`refresh_token` ONLY on the `Authorization` header.
    None ever appear in the URL, errors, or logs. A direct `net/http` client (mirror `auth0api.go`);
    **no Keycloak CLI (`kcadm.sh`)**, and **no** `mrparkers` SDK. Refuse redirects (mirror
    `auth0HTTPClient`) so no secret is replayed to another host on a 3xx.
- **Base URL — the user-supplied self-hosted server + a `base_path` (the Grafana/Okta
  host-from-config pattern, with a Keycloak twist).** There is no vendor host and no region table:
  - Require `KEYCLOAK_URL` (e.g. `https://keycloak.mycorp.internal`, `https://sso.example.com`).
    Strip any trailing slash; **prefer https**, but **allow `http` for a local-dev host**
    (`localhost` / `127.0.0.1` — Keycloak dev servers run on `http://localhost:8080`) **with a
    Warn**, rather than hard-forcing https (a deliberate divergence from Auth0/Okta, which force
    https unconditionally). Reject an `@`/path/port-splice host (the `validDomain` userinfo guard).
  - **`base_path`** (`KEYCLOAK_BASE_PATH`, default empty; `/auth` for legacy Wildfly Keycloak <17)
    sits between the host and the API path. The token endpoint is
    `<KEYCLOAK_URL><base_path>/realms/master/protocol/openid-connect/token`; Admin API paths are
    `<KEYCLOAK_URL><base_path>/admin/realms/…`. On modern Quarkus Keycloak (17+) `base_path` is
    empty; on Wildfly it is `/auth` (`…/auth/admin/realms/…`). Read it from env, default empty.
  - Store the resolved base once; the pager builds every URL from `base+path` (Keycloak does **not**
    hand back a next-URL — see pagination), so there is no server-supplied cross-host next-URL to
    validate. The redirect-refusing client is the backstop for the Bearer/secret.
- **Scope — one Keycloak SERVER = one flat container; realms are a FAN-OUT KEY, not a hierarchy.**
  The credentials authenticate against the whole server (the `master`-realm admin token spans all
  realms); there is no sub-server resolution — the credentials simply **are** the server.
  `model.ScopeTenant`. **Realms are a fan-out key, not a container tree** — like LaunchDarkly's
  projects / Honeycomb's datasets, the realms and their sub-objects live *under* the one server
  container, so `Capabilities.Hierarchy` stays **false** (the realm fan-out is an enumeration
  detail, not a scope tree; and Keycloak *groups* nest within a realm — that tree is likewise an
  enumeration detail, not a container hierarchy). Resolve the container id/name **best-effort** from
  the `KEYCLOAK_URL` host string (there is no "server name" endpoint; `GET /admin/realms` proves
  connectivity). `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response family — BARE JSON ARRAYS with `first`/`max` offset pagination (the key structural
  fact; unlike Auth0's keyed+total envelope).** Almost every list endpoint returns the collection
  as a **bare `[...]` array in the body with NO envelope** — no `items`/`_links` (LaunchDarkly),
  no `{"<key>":[…],"total"}` (Auth0), no JSON:API. Unmarshal the body straight into `[]T`. The
  pager is **offset/limit** query params, `?first=<offset>&max=<limit>`: fetch `first=0&max=N`,
  accumulate, and **loop `first += max` while a FULL page (`len == max`) returns** — stop on the
  first short/empty page (there is no `total` to bound against). Implement one generic
  **bare-array `first`/`max`** helper (the whole client surface for lists) plus a
  **bare-object GET** helper for singletons (`GET …/realms/<realm>` returns one realm object). A
  few endpoints are **unpaged** — they ignore `first`/`max` and return everything in one call.
- **Pagination — per-endpoint; flag which paginate.** **Paginated (`first`/`max`, potentially
  large):** `users` (the biggest — deferred, but the reason the pager exists), `clients`, `groups`
  (top-level; sub-groups nest), `roles` (realm and per-client). **Usually unpaged (one call
  returns all):** `realms`, `client-scopes`, `authentication/flows`, `identity-provider/instances`,
  `components`, `authentication/required-actions`. Treat every list as *potentially* paged
  (honour `first`/`max` if a full page returns) so a large realm is never truncated; bound every
  loop defensively (`kcMaxPages`). **VERIFY the max `max` per endpoint at build** (Keycloak has no
  universal cap; a `max` of 100–200 is safe).
- **Status handling (mirror `auth0Do` / `list`).** Keycloak Admin errors are
  `{"error":"…","error_description":"…"}` or `{"errorMessage":"…"}` (HTTP status carries the
  meaning). **401** (token invalid/expired) → refresh once mid-run (Keycloak tokens are very
  short-lived, so a mid-run 401 is *expected* and re-mintable — only fatal if the re-mint itself
  fails); a preflight 401 is fatal. **403** (the service-account/admin user lacks the realm-admin
  role for that realm/object — e.g. a client scoped to only some realms) → best-effort **Verbose
  skip**. **404** (realm/object absent, or a feature not enabled) → Verbose skip. **429 / 5xx /
  network** → enumeration may be silently incomplete → **Warn + count** (tell a systemic failure
  apart from an empty server). Secrets never appear in errors/logs; strip any query string before a
  URL is logged (`redactURL`).
- **Preflight**: `terraform` present + `KEYCLOAK_URL` set + (**either** `KEYCLOAK_CLIENT_ID` **and**
  `KEYCLOAK_CLIENT_SECRET` **or** `KEYCLOAK_USER` **and** `KEYCLOAK_PASSWORD`) set + the token
  exchange succeeds + `GET /admin/realms` returns 200. **The token exchange is itself the first
  real check** (a 401 from `/token` means bad client_id/secret or username/password, or the M2M
  client is not a service account); then `GET /admin/realms` confirms the token actually has admin
  scope (an authenticated-but-unprivileged token exchanges fine but 403s on `/admin/…`).
- **Connect**: run the token exchange, validate `GET /admin/realms` succeeds (it also enumerates
  the realms for the spine), and set the single flat container (id/name = the `KEYCLOAK_URL` host).

## Realm FAN-OUT + protocol/providerId discriminators + composite import DEPTH — the CRITICAL determination

This is Keycloak's analogue of LaunchDarkly's "account-vs-project-vs-env scope + composite depth"
call and Okta's "discriminator + fan-out + composite-depth" call. The load-bearing per-resource
facts are **(a) is the resource enumerated at the SERVER level (just `GET /admin/realms`),
REALM-scoped (one-level fan-out per realm), or REALM×CLIENT-scoped (two-level fan-out); (b) which
DISCRIMINATOR field maps a heterogeneous list object to its TF type; and (c) is the import id the
BARE `<realm>` or a 2-part `<realm>/<leaf>` composite — and is the leaf a UUID or an alias/name.**
Get (a) wrong and you never reach the sub-objects (or re-list clients per role); get (b) wrong and
the wrong TF type is emitted for a mixed-list object; get (c) wrong and every import block for that
type is un-importable. All three are **verified against the registry `docs/resources/*.md`** and
pinned per-resource in the catalog. The rules:

- **Server level (the parent) → BARE import id.** `GET /admin/realms` → one bare array of realm
  representations `[{id, realm, …}]`; each becomes a `keycloak_realm` whose **import id is the bare
  realm NAME** (the `realm` field, e.g. `my-realm`) — **not** a UUID and **not** the realm's
  internal `id`. The `realm` name is also the **fan-out key** and the value every sub-resource
  carries as `realm_id`. (Skip the built-in **`master`** realm from adoption, or flag it — it is the
  admin realm and adopting it fights the provider's own auth; VERIFY.)
- **REALM-scoped (one-level fan-out) → 2-part `<realm>/<leaf>` import id.** Per realm, list the
  sub-objects: clients, roles, groups, client-scopes, authentication flows, identity providers,
  user federations, required actions. First `GET /admin/realms` (parent), then per realm the
  sub-list — exactly LaunchDarkly's `GET /api/v2/projects` → per-project fan-out.
- **REALM×CLIENT-scoped (TWO-level fan-out) → still a 2-part `<realm>/<role_id>` import.** Client
  roles need **both** a realm AND a client, so the fan-out is
  `realms → per realm its clients → per client its roles`
  (`GET /admin/realms/<realm>/clients/<client_uuid>/roles`). **But the import id stays 2-part**
  `<realm>/<role_id>` (the `role_id` UUID is globally unique) — the client UUID is carried as the
  resource's `client_id` *attribute*, not in the import id. This is the LaunchDarkly-inverse: a
  deeper fan-out that does **not** deepen the import composite (contrast Okta's auth-server→policy→
  rule, where the 3rd fan-out level *does* add a 3rd import part). **Get this right: two-level
  enumeration, 2-part import.**
- **Discriminators (the Okta `signOnMode`/`type` precedent) — three of them:**
  | list endpoint | discriminator field | value → TF type |
  |---|---|---|
  | `…/clients` | `protocol` | `openid-connect` → `keycloak_openid_client`; `saml` → `keycloak_saml_client` |
  | `…/client-scopes` | `protocol` | `openid-connect` → `keycloak_openid_client_scope`; `saml` → `keycloak_saml_client_scope` (later) |
  | `…/identity-provider/instances` | `providerId` | `oidc`/`keycloak-oidc` → `keycloak_oidc_identity_provider`; `saml` → `keycloak_saml_identity_provider`; social (`google`/`github`/…) → deferred |
  | `…/components?type=…UserStorageProvider` | `providerId` | `ldap` → `keycloak_ldap_user_federation`; `kerberos` → deferred |
- **Leaf id form is per-type — UUID vs alias/name (the #1 subtlety alongside the composite depth):**
  - **UUID leaf** (the object's internal `id`): `keycloak_openid_client` / `keycloak_saml_client`
    (client `id` — **NOT** the human `clientId`), `keycloak_role` (role `id`), `keycloak_group`
    (group `id`), `keycloak_openid_client_scope` (scope `id`), `keycloak_authentication_flow` (flow
    `id`), `keycloak_ldap_user_federation` (component `id`).
  - **alias/name leaf**: `keycloak_oidc_identity_provider` / `keycloak_saml_identity_provider`
    (idp `alias`), `keycloak_required_action` (`alias`). And `keycloak_realm` — the bare realm
    `name`.
  - **The client UUID trap:** a client is keyed by an internal UUID `id` (`dcbc61b1-…`), **not** its
    human `clientId` (`my-app`). The import is `<realm>/<client_uuid>`; using `clientId` makes every
    client import fail (registry: "…where `openid_client_id` is the unique ID Keycloak assigns…
    this value is *not* the same as the client_id"). Copy the `id` field, not `clientId`.

The server/realm/realm×client scope (which drives the fan-out depth), the three discriminators, and
the import composite (bare `<realm>` vs 2-part `<realm>/<leaf>`, UUID vs alias leaf) are the things
we cannot get wrong — enumerated per-resource in the catalog and re-verified against the registry
docs at build. Encode the import id as an explicit per-TF-type switch in `importid.go` (mirror
Okta's `rawImportID` / LaunchDarkly's depth switch) — never infer the separator, the part-count, or
whether the leaf is the UUID or the human name.

## Enumeration spine

Flat server scope. The spine is a **realm fan-out**: `GET /admin/realms` (parent) → per realm the
clients/roles/groups/scopes/flows/idps/federations/required-actions; and a **second-level**
per-(realm, client) fan-out for client roles. Best-effort per list (403 role-absent / 404
feature-absent → Verbose skip; 401 → refresh-once-then-fatal; other → Warn + count, so a systemic
failure is told apart from an empty server). Each list is tagged with its discriminator + pager per
the catalog. Secrets never appear in errors/logs. (Mirror `launchdarkly/enumerate.go`: a top-level
`list` helper owns the systemic-failure count; a `subList` helper for the fan-out does not, since
sub-lists multiply by realm/client count.)

- **Parent:** `GET /admin/realms` → bare array `[{id, realm}]` (unpaged). Each `realm` becomes a
  `keycloak_realm` (bare import `<realm>`), and the `realm` name is the fan-out key for every
  sub-list below. Skip/flag the built-in `master` realm.
- **Per realm `<realm>` (one-level fan-out):**
  - `GET /admin/realms/<realm>/clients` (**paged** `first`/`max`) → bare array
    `[{id, clientId, protocol, …}]` → **discriminate on `protocol`**: `openid-connect` →
    `keycloak_openid_client`, `saml` → `keycloak_saml_client` (import `<realm>/<client_uuid>` — the
    UUID `id`). **Capture each client `id` (UUID) + `clientId`** — the UUID is the second fan-out
    key for client roles. **Skip Keycloak's built-in clients by `clientId`**: `account`,
    `account-console`, `admin-cli`, `broker`, `realm-management`, `security-admin-console`
    (the Okta `saasure`-skip analogue — they exist in every realm and cannot be usefully adopted).
  - `GET /admin/realms/<realm>/roles` (**paged**) → `[{id, name}]` → `keycloak_role` (realm role;
    import `<realm>/<role_id>`).
  - `GET /admin/realms/<realm>/groups` (**paged**, top-level) → `[{id, name, subGroups:[…]}]` →
    `keycloak_group` (import `<realm>/<group_id>`). **Groups are a TREE** — recursively flatten
    `subGroups` (Terraformer's `flattenGroups`); on newer Keycloak `subGroups` may be empty in the
    brief list and require a per-group children fetch (`?briefRepresentation=false` or
    `…/groups/<id>/children`) — VERIFY at build. Each nested group is still a flat 2-part import.
  - `GET /admin/realms/<realm>/client-scopes` (unpaged) → `[{id, name, protocol}]` →
    **discriminate on `protocol`**: `openid-connect` → `keycloak_openid_client_scope` (import
    `<realm>/<scope_id>`); `saml` → deferred `keycloak_saml_client_scope`. Flag Keycloak's built-in
    default scopes (`profile`, `email`, `roles`, `web-origins`, `offline_access`,
    `microprofile-jwt`, `role_list`, …) — adopt-in-place, not freely creatable; VERIFY.
  - `GET /admin/realms/<realm>/authentication/flows` (unpaged) → `[{id, alias, builtIn, topLevel}]`
    → `keycloak_authentication_flow` (import `<realm>/<flow_id>`). **Skip `builtIn: true` flows**
    (browser/direct-grant/registration/… are Keycloak-managed — adopt-in-place only); emit the
    custom top-level flows. The sub-flow/execution/execution-config depth is DEFERRED (below).
  - `GET /admin/realms/<realm>/identity-provider/instances` (unpaged) → `[{alias, providerId}]` →
    **discriminate on `providerId`**: `oidc`/`keycloak-oidc` → `keycloak_oidc_identity_provider`,
    `saml` → `keycloak_saml_identity_provider` (import `<realm>/<alias>`). Social providers
    (`google`/`github`/`facebook`/…) → deferred. **`client_secret` in the config → scrub** (EXCLUDE).
  - `GET /admin/realms/<realm>/components?type=org.keycloak.storage.UserStorageProvider` (unpaged) →
    `[{id, name, providerId, config}]` → **filter `providerId == "ldap"`** →
    `keycloak_ldap_user_federation` (import `<realm>/<component_id>`). `kerberos` federations and
    the LDAP-mapper sub-components are DEFERRED. **`config.bindCredential` → scrub** (EXCLUDE).
  - `GET /admin/realms/<realm>/authentication/required-actions` (unpaged) →
    `[{alias, name, providerId, enabled}]` → `keycloak_required_action` (import `<realm>/<alias>`).
- **Per (realm `<realm>`, client `<client_uuid>`) (two-level fan-out):**
  - `GET /admin/realms/<realm>/clients/<client_uuid>/roles` (**paged**) → `[{id, name}]` →
    `keycloak_role` (client role; import `<realm>/<role_id>` — **2-part**, with the resource's
    `client_id` attribute = `<client_uuid>`). **Skip the built-in clients** here too (their
    `realm-management`/`account` roles are Keycloak-managed).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic failure
rather than shipping an empty inventory (same guard as the Auth0/LaunchDarkly/Okta `enumerate.go`).

## Resource catalog

Import IDs verified against the current `keycloak/keycloak` registry docs (`docs/resources/*.md`).
All scope = server. "endpoint → shape" is the list path (bare JSON array + `first`/`max` unless
noted). "disc / fan-out" names the discriminator field or the parent fan-out. The **sep** column is
the #1 hazard — **bare / 2-part slash**; **leaf** flags UUID vs alias/name.

| native key | TF type | endpoint → shape | disc / fan-out | leaf id | import ID | sep |
|---|---|---|---|---|---|---|
| keycloak:realm | keycloak_realm | `GET /admin/realms` (bare array, unpaged) | parent (skip `master`) | `realm` name | `<realm>` | **bare** (smtp password SECRET) |
| keycloak:openid_client | keycloak_openid_client | `…/realms/<realm>/clients` (paged) | ← realm; `protocol=openid-connect` | `id` (UUID) | `<realm>/<client_uuid>` | **2-part** (**client_secret SECRET**; UUID≠clientId) |
| keycloak:saml_client | keycloak_saml_client | `…/realms/<realm>/clients` (paged) | ← realm; `protocol=saml` | `id` (UUID) | `<realm>/<client_uuid>` | **2-part** (signing key material) |
| keycloak:role (realm) | keycloak_role | `…/realms/<realm>/roles` (paged) | ← realm | `id` (UUID) | `<realm>/<role_id>` | **2-part** |
| keycloak:role (client) | keycloak_role | `…/clients/<client_uuid>/roles` (paged) | ← realm → client | `id` (UUID) | `<realm>/<role_id>` | **2-part** (NOT 3-part; `client_id` is an attr) |
| keycloak:group | keycloak_group | `…/realms/<realm>/groups` (paged, tree) | ← realm (recursive subGroups) | `id` (UUID) | `<realm>/<group_id>` | **2-part** |
| keycloak:openid_client_scope | keycloak_openid_client_scope | `…/realms/<realm>/client-scopes` (unpaged) | ← realm; `protocol=openid-connect` | `id` (UUID) | `<realm>/<scope_id>` | **2-part** |
| keycloak:authentication_flow | keycloak_authentication_flow | `…/realms/<realm>/authentication/flows` (unpaged) | ← realm (skip `builtIn`) | `id` (UUID) | `<realm>/<flow_id>` | **2-part** |
| keycloak:oidc_identity_provider | keycloak_oidc_identity_provider | `…/realms/<realm>/identity-provider/instances` (unpaged) | ← realm; `providerId=oidc` | `alias` | `<realm>/<alias>` | **2-part** (**client_secret SECRET**) |
| keycloak:saml_identity_provider | keycloak_saml_identity_provider | `…/realms/<realm>/identity-provider/instances` (unpaged) | ← realm; `providerId=saml` | `alias` | `<realm>/<alias>` | **2-part** (signing key material) |
| keycloak:ldap_user_federation | keycloak_ldap_user_federation | `…/realms/<realm>/components?type=…UserStorageProvider` (unpaged) | ← realm; `providerId=ldap` | `id` (UUID) | `<realm>/<component_id>` | **2-part** (**bind_credential SECRET**) |
| keycloak:required_action | keycloak_required_action | `…/realms/<realm>/authentication/required-actions` (unpaged) | ← realm | `alias` | `<realm>/<alias>` | **2-part** |

### Import-format quirks (§ do not get wrong)

1. **Every id is REALM-PREFIXED except the realm itself — bare `<realm>` vs 2-part `<realm>/<leaf>`,
   all SLASH.** The realm imports by its **bare name** (`my-realm`); everything else is
   `<realm>/<leaf>`. **There is NO 3-part id in the Phase-A set.** Encode the part-count per TF type
   in `importid.go`; never infer it. (Terraformer confirms the `<realm>/<leaf>` shape for
   required-actions `realmId+"/"+alias`, group-memberships/roles `realm/group_id`, etc.)
2. **`keycloak_role` (realm AND client roles) is 2-part `<realm>/<role_id>`, NOT 3-part.** The
   provisional `<realm>/<client_uuid>/<role_id>` was wrong: the `role_id` is a globally-unique UUID,
   so the registry imports both realm and client roles as `{{realm_id}}/{{role_id}}`. The client
   UUID lives in the resource's `client_id` *attribute* (set during the realm×client fan-out), never
   in the import id. **This is the single most important correction in this spec.**
3. **The client leaf is the internal UUID `id`, NOT the human `clientId`.** `<realm>/<client_uuid>`
   uses the object's `id` (`dcbc61b1-e39b-…`), not `clientId` (`my-app`). Same UUID-leaf rule for
   roles/groups/scopes/flows/federations. Getting this wrong is un-importable. Copy the `id` field.
4. **IdPs and required-actions key on `alias` (a human slug), not a UUID.**
   `<realm>/<alias>` — e.g. `my-realm/my-oidc-idp`, `my-realm/CONFIGURE_TOTP`. Store the `alias`
   off the object, not any `internalId`.
5. **`realm_id` everywhere is the realm NAME, not the realm's internal `id`.** Sub-resources
   reference their realm by its `realm` name (`my-realm`), which is also the `keycloak_realm` import
   id — Keycloak uses the realm name as the effective id in the Admin API path (`/admin/realms/<name>/…`).
   Carry the realm `name` as the fan-out key, not the realm object's `id`.
6. **All ids/aliases are opaque strings off the wire — no numeric stringify** (unlike Datadog).
   UUIDs and slugs alike copy verbatim. Template-escape on emit (mirror the other providers'
   `EscapeHCLTemplate`) — realm/client names and role names can contain `$`/`{`/`}` (see the
   `$`→`$$` curation gotcha below).

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a live
server — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the Auth0/Okta
providers. **`keycloak_realm` is the heaviest curation surface** (a sprawling settings object — the
Keycloak analogue of `datadog_dashboard` / `auth0_connection`), and the recurring hazards are the
**client/idp/ldap/smtp secrets**, the **`$`→`$$` literal-escape** hazard Terraformer had to fix by
hand, and the **built-in/`master`-realm singletons**.

- **`keycloak_realm` — the big one; over-emit + SECRET + list-ordering churn.** Core surface:
  `realm` (name), `enabled`, `display_name`, token/session lifespans (`access_token_lifespan`,
  `sso_session_idle_timeout`, …), `password_policy`, `security_defenses` (brute-force/headers),
  `internationalization` (`supported_locales` — **sort for reproducibility**, Terraformer does),
  `smtp_server` (host/port/from — and **`smtp_server.password` is a SECRET → scrub**),
  `web_authn_policy`, `otp_policy`. Defaults over-emit heavily; prune computed. The SMTP password is
  the realm's one credential. Phase-B-heavy; treat Phase-A export as a breadth scaffold.
- **`keycloak_openid_client` — medium; SECRET + list churn.** Core: `client_id` (the human id),
  `name`, `access_type` (`CONFIDENTIAL`/`PUBLIC`/`BEARER-ONLY`), `standard_flow_enabled`,
  `service_accounts_enabled`, `valid_redirect_uris`, `web_origins`, `root_url`. **Secret:**
  `client_secret` — returned on read for CONFIDENTIAL clients, Sensitive → **scrub** (PUBLIC /
  BEARER-ONLY clients have none). **Sort `valid_redirect_uris` + `web_origins`** (Terraformer sorts
  them for reproducible diffs). **`$`→`$$` hazard** on `root_url`/`name` (below). Prune computed
  `service_account_user_id`.
- **`keycloak_saml_client` — medium; key material.** `client_id`, `name`, `sign_documents`,
  `sign_assertions`, `signing_certificate` / `signing_private_key` / `encryption_certificate` — the
  **private key is signing material → scrub/exclude** (the SAML analogue of Okta's app signing key;
  the cert is public, the private key is not). `valid_redirect_uris` sorting applies.
- **`keycloak_role` — light; composite refs.** `name`, `description`, `client_id` (UUID attr for
  client roles; absent for realm roles — the discriminator), `composite_roles` (references other
  role UUIDs — **sort** for reproducibility, Terraformer does). No secret.
- **`keycloak_group` — light; tree refs.** `name`, `realm_id`, `parent_id` (references the parent
  group's UUID for nested groups — map through the flattened tree), `attributes`. Group
  memberships/roles/default-groups are SEPARATE resources (deferred). No secret. Prune computed
  `path`.
- **`keycloak_openid_client_scope` — light.** `name`, `description`, `consent_screen_text`
  (**`$`→`$$` hazard**), `include_in_token_scope`, `gui_order`. The scope's protocol-mapper children
  are a SEPARATE deferred plane. Built-in default scopes are adopt-in-place. No secret.
- **`keycloak_authentication_flow` — light shell (the depth is deferred).** `alias`, `description`,
  `provider_id` (`basic-flow`/`client-flow`). The flow *shell* only — its sub-flows, executions, and
  execution-configs (Terraformer's `keycloak_authentication_subflow` / `_execution` /
  `_execution_config`, an ordering-and-`depends_on`-sensitive tree) are DEFERRED. Skip `builtIn`.
  No secret.
- **`keycloak_oidc_identity_provider` — medium; SECRET.** `alias`, `authorization_url`, `token_url`,
  `client_id`, `default_scopes`, `trust_email`, `sync_mode`. **Secret:** `client_secret` (the
  external IdP client secret, Sensitive) → **scrub**, keep the block. IdP protocol mappers are a
  separate deferred plane.
- **`keycloak_saml_identity_provider` — medium; key material.** `alias`, `single_sign_on_service_url`,
  `entity_id`, `signing_certificate`, `name_id_policy_format`. The `signing_certificate` is public;
  a request-signing private key (if configured) is key material → scrub. No client_secret.
- **`keycloak_ldap_user_federation` — medium; SECRET.** `name`, `connection_url`, `users_dn`,
  `bind_dn`, `edit_mode`, `search_scope`, `import_enabled`. **Secret:** `bind_credential` (the LDAP
  bind password, in the component `config.bindCredential`) → **scrub**, keep the block. LDAP mappers
  are a separate deferred plane. Terraformer only adopts LDAP with a non-empty bind credential —
  note the bind_dn is parsed from the `cn=` component.
- **`keycloak_required_action` — trivial.** `alias`, `name`, `enabled`, `default_action`, `priority`.
  `priority` ordering may churn (tolerate). No secret.
- **The `$`→`$$` literal-escape hazard (Terraformer's `PostConvertHook`).** Keycloak free-text
  fields — `consent_screen_text`, client/scope `name`, `description`, `root_url` — can contain a
  literal `$`, which Terraform would try to interpolate (`${…}`). Terraformer replaces `$`→`$$`
  on those fields before emitting. **Verify `terraform plan -generate-config-out` escapes these**
  (the Okta `${…}`-EL / Datadog `%{`-grok precedent) — else the generated HCL is a plan error. This
  is the #1 thing to check on real output.
- **List-ordering churn.** `internationalization.supported_locales`, client `valid_redirect_uris` /
  `web_origins`, and role `composite_roles` come back in server order and churn across runs —
  sort them on emit for stable diffs (Terraformer sorts all four).

Until Phase B these are no-ops, so a Keycloak export is a breadth scaffold, not yet plan-clean (the
pipeline's repo-wide secret scan is the backstop for the `client_secret` / `bind_credential` / idp
`client_secret` / realm `smtp_server.password` / SAML private-key material that generate-config-out
may emit before the scrub rules land).

## Write-only / secret resources (EXCLUDE / scrub)

The credential/integration plane is where Keycloak's secrets live — scrub the value (keep the block,
re-supply out-of-band) or exclude the field, exactly like Auth0's `client_secret` /
`email_provider.credentials` / Okta's hook `auth_scheme.value`:

- **`keycloak_openid_client.client_secret`** — the confidential-client secret (Sensitive; returned
  on read for `CONFIDENTIAL` clients) → **scrub the value**, keep the client block. The most common
  Keycloak secret. PUBLIC / BEARER-ONLY clients have none.
- **`keycloak_ldap_user_federation.bind_credential`** — the LDAP bind password (in the component
  `config.bindCredential`) → **scrub the value**, keep the federation block. Never pull it into the
  inventory (mirror LaunchDarkly's env-key non-decode — the enumeration struct simply omits the
  field).
- **`keycloak_oidc_identity_provider.client_secret`** (and any social-IdP client secret) — the
  external IdP client secret → **scrub the value**, keep the idp block.
- **`keycloak_realm.smtp_server.password`** — the realm's SMTP password (the outbound-email
  credential) → **scrub the value**, keep the `smtp_server` block.
- **`keycloak_saml_client` / `keycloak_saml_identity_provider` signing/encryption private keys** —
  `signing_private_key` / `encryption_private_key` are key material → **scrub / do not round-trip**
  (the public certificate is fine to emit). The Okta SAML-signing-key precedent.
- **The provider credentials themselves** — `KEYCLOAK_CLIENT_SECRET` / `KEYCLOAK_PASSWORD` and the
  minted `access_token`/`refresh_token` are the server-wide master credentials; they live **only**
  in the `/token` form body / the `Authorization` header, never in generated config, state comments,
  errors, or logs. (There is no round-trippable "admin credential" resource to adopt.)
- **Not secret, do not over-scrub:** `keycloak_openid_client` `valid_redirect_uris` / `web_origins`
  / `root_url` (public URLs), `keycloak_saml_*` public certificates, `keycloak_oidc_identity_provider`
  `authorization_url`/`token_url`/`client_id` (public), `keycloak_group` attributes,
  `keycloak_required_action` config, realm non-SMTP settings. Group/role names and LDAP `users_dn` /
  `bind_dn` are directory structure, not credentials — adopt.

## Deliberately out of scope

- **User plane** (`keycloak_user`, `keycloak_user_roles`, `keycloak_group_memberships`) — bulk +
  PII; Terraformer enumerates users (`GET /admin/realms/<realm>/users`, the reason the `first`/`max`
  pager exists), but adopting a realm's users as IaC is rarely wanted and is a large N with privacy
  weight. A much-later increment; Phase A adopts the config, not the directory.
- **Protocol mappers** (the many `keycloak_openid_*_protocol_mapper` / `keycloak_saml_*_protocol_mapper`
  / `keycloak_generic_client_protocol_mapper` / `keycloak_*_client_scope_*_mapper` types) — per-client
  and per-client-scope, a large N (Terraformer walks ~14 OIDC mapper types plus SAML) with their own
  `<realm>/<client_id|scope_id>/<mapper_id>` composites. The single biggest deferred plane; a
  dedicated later increment after the client/scope shells are solid.
- **Default-scope / default-group ASSIGNMENT resources** (`keycloak_openid_client_default_scopes` /
  `_optional_scopes`, `keycloak_default_groups`, `keycloak_group_roles`,
  `keycloak_openid_client_service_account_role` / `_realm_role`) — the who-is-assigned-what
  relationship plane (N×M, several are `<realm>/<client_id>` composites). Adopting them fights the
  individual client/scope/group/role resources; a later increment. Phase A imports the bare
  clients/scopes/groups/roles and NOT the assignments.
- **Authentication sub-flow / execution / execution-config depth**
  (`keycloak_authentication_subflow`, `keycloak_authentication_execution`,
  `keycloak_authentication_execution_config`, `keycloak_authentication_bindings`) — an
  ordering-and-`depends_on`-sensitive tree under each custom flow (Terraformer builds it with a level
  stack + `depends_on` wiring). Phase A adopts the flow *shell*; the executions are a later increment.
- **LDAP-mapper + other sub-components** (`keycloak_ldap_*_mapper` — full-name/group/role/
  hardcoded/user-attribute/msad, `keycloak_custom_user_federation`, kerberos federation, the
  `components?parent=…` sub-plane) — the mapper/sub-component tree under each federation and the
  identity-provider mappers. Deferred with the mapper plane.
- **SAML client scopes** (`keycloak_saml_client_scope`) — the SAML analogue of
  `keycloak_openid_client_scope` (same `client-scopes` list, `protocol=saml`); a small later
  increment alongside the SAML clients.
- **Social identity providers** (`keycloak_oidc_google_identity_provider` and the social
  `providerId` variants) — each carries a `client_secret`; deferred with the secret-bearing IdP work.
- **Realm sub-singletons** (`keycloak_realm_events`, `keycloak_realm_keystore_*`,
  `keycloak_realm_user_profile`, `keycloak_realm_default_client_scopes`) — additional per-realm
  settings objects, better authored by hand once the realm shell is adopted. Later increments.
- **Cloud-IAM depth** (`Capabilities.IAM=false`): `keycloak_role` / `keycloak_group` are modeled at
  breadth, but role→role composites, group→role assignments, service-account roles, and user→role
  assignments are the deferred assignment plane, not Phase A.
- **Data planes** — user sessions, events, and the runtime auth flow *state* are DATA behind the
  config, per realm. Out of scope (config only).
- **`mrparkers` SDK + Keycloak CLI** — Terraformer pulls the `mrparkers` provider SDK; TerraLift
  uses a raw `net/http` client (smaller, matches Auth0/Okta/LaunchDarkly), and there is no `kcadm.sh`
  dependency. A deliberate non-adoption.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)

The **recommended Phase-A CONFIG CORE** (~11 TF types across the realm fan-out): `keycloak_realm`,
`keycloak_openid_client`, `keycloak_saml_client`, `keycloak_role` (realm + client),
`keycloak_group`, `keycloak_openid_client_scope`, `keycloak_authentication_flow`,
`keycloak_oidc_identity_provider`, `keycloak_saml_identity_provider`, `keycloak_ldap_user_federation`,
`keycloak_required_action`.

BEACHHEAD `keycloak_realm` + `keycloak_openid_client` + `keycloak_role` (the realm/client/role core
essentially every Keycloak server manages as IaC — realm is the bare fan-out parent and the heaviest
curation surface with the **SMTP-password scrub**, `keycloak_openid_client` establishes the
**`GET /admin/realms → per-realm /clients` fan-out**, the **`protocol` discriminator**, the
**`<realm>/<client_uuid>` UUID-not-clientId** import subtlety, and the **`client_secret` scrub**,
and `keycloak_role` establishes both the realm-role list AND the **two-level realm×client fan-out**
for client roles with its **2-part-not-3-part** import — the provider's defining classification
hazard, and it exercises the **OAuth2 token exchange** + **`first`/`max` bare-array pager** on the
realm's larger lists) → INC-1 `keycloak_group` + `keycloak_openid_client_scope` (the group TREE
recursive-flatten and the second `protocol`-discriminated list — the client-scope shell) → INC-2
`keycloak_saml_client` + `keycloak_saml_identity_provider` (the SAML side of the client/idp
discriminators — SAML signing-key material) + `keycloak_oidc_identity_provider` (the idp `providerId`
discriminator + the **idp `client_secret` scrub** + the `<realm>/<alias>` alias-leaf import) → INC-3
`keycloak_ldap_user_federation` (the `components?type=…` provider-filtered list + the
**`bind_credential` scrub**) + `keycloak_authentication_flow` (the flow shell, `builtIn` skip) +
`keycloak_required_action` (the light alias-leaf tail) → LATER the user plane, the protocol-mapper
plane (the biggest deferred surface), the default-scope/default-group/service-account ASSIGNMENT
composites, the authentication sub-flow/execution/execution-config depth, the LDAP-mapper /
sub-component plane, SAML client scopes, social IdPs, the realm sub-singletons, and the data planes.
