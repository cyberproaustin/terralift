# Vault provider — build spec

Research artifact for the `vault` provider (Phase A scaffold; TF provider source is
**`hashicorp/vault`** — the official Terraform provider for **HashiCorp Vault**, the secrets-and-encryption
platform). Sources: Terraformer's `providers/vault/` (only two files —
`vault_provider.go` + `vault_service_generator.go` — built on the official `hashicorp/vault/api` Go SDK),
the `hashicorp/vault` registry docs (import formats + schema, **verified per-resource below** against the
provider repo's `website/docs/r/*.html.md`), and the Vault HTTP API
(`<VAULT_ADDR>/v1/…`, e.g. `https://127.0.0.1:8200/v1/sys/mounts`). Build mirrors **three** prior
providers at once — **Logz.io / Mackerel** (`internal/providers/{logzio,mackerel}/`) for the **flat,
single-container, custom-header** REST client driven by a raw `net/http` client (NOT the vendor SDK),
**Keycloak** (`internal/providers/keycloak/`) for the **type-DISCRIMINATOR + per-parent FAN-OUT** spine
(a mount's `type` selects which role-list endpoint and which TF type, exactly as Keycloak's
`protocol`/`providerId` does), and it introduces **one genuinely NEW decode shape not seen in any prior
provider** — the **map-keyed-by-path** object (`sys/mounts`/`sys/auth`/`sys/audit` return
`{"data":{"secret/":{…},"sys/":{…}}}`, an object whose KEYS are the resource paths, not an array).

## ⚠ THE PARAMOUNT CONSTRAINT — secret DATA vs config (read this first)

**Vault is a secrets store. TerraLift adopts Vault CONFIGURATION and MUST NEVER enumerate or read secret
DATA.** This is the single load-bearing design decision and it overrides every convenience the prior
providers taught. Reading a secret's *value* would write it into the inventory, the generated HCL, state,
and logs — a catastrophic leak. Concretely:

- **ADOPT (safe config):** secret-engine **mounts** (`vault_mount`), auth-method **mounts**
  (`vault_auth_backend`), **ACL policies** (`vault_policy`), **audit devices** (`vault_audit`),
  **namespaces** (`vault_namespace`, Enterprise), and backend **roles / configs by NAME**
  (`vault_pki_secret_backend_role`, `vault_database_secret_backend_role`,
  `vault_aws_secret_backend_role`, `vault_ldap_auth_backend`, `vault_jwt_auth_backend_role`,
  `vault_approle_auth_backend_role`, `vault_token_auth_backend_role`, `vault_github_team`, …).
- **HARD-EXCLUDE (secret DATA — never enumerate, never read, never import):** `vault_generic_secret`,
  `vault_kv_secret`, `vault_kv_secret_v2` and any KV path contents; generated/dynamic credentials
  (`<pki>/issue/*`, `<pki>/cert/*`, `<db>/creds/*`, `<aws>/creds/*`, `<transit>/export/*`, token
  creation); the root token, unseal/recovery keys, and anything under `sys/unseal` / `sys/generate-root`
  / `sys/rekey`; cubbyhole; transit/PKI private-key material.
- **The enumeration design must ONLY hit config/metadata endpoints** — `sys/*`, and backend
  **config/role LISTs by NAME** (`<backend>/roles` → `{"data":{"keys":[…]}}`). It must **NEVER** hit a
  data path: never `LIST <kv_mount>/metadata`, never `LIST <kv_mount>/`, never `GET <kv_mount>/data/<p>`,
  never `GET <pki>/issue|cert`, never `GET <db>/creds`, never `GET <aws>/creds`. **Terraformer does the
  wrong thing here — it enumerates `vault_generic_secret` by LISTing `{mount}/` and adopting each secret
  path — this is the #1 "do NOT copy Terraformer" item, alongside its token-inlining.**
- Every resource that *straddles* the line (a config resource whose read *might* return credential
  material) is flagged **VERIFY** in the catalog and handled as a Phase-B field-level **scrub**, never a
  data read. Vault masks write-only credentials on read (returns them null), so the config shells are
  safe to adopt — but the enumeration must still never walk into a mount's data plane to reach them.

Six more facts set Vault apart, all load-bearing and called out below:

1. **Auth is a single custom header — `X-Vault-Token: <token>` (exact casing confirmed) — from
   `VAULT_TOKEN`.** The Logz.io/Mackerel custom-header shape. The token NEVER appears in the URL, query,
   body, errors, logs, config, or state, and is **never inlined into `providers.tf`** (Terraformer inlines
   it — the leak we refuse). Optional Enterprise **`X-Vault-Namespace: <ns>`** from `VAULT_NAMESPACE`.
2. **Base URL is the user-supplied cluster** (`VAULT_ADDR`, default `https://127.0.0.1:8200`), **https**,
   with every route under the **`/v1/`** prefix. `VAULT_SKIP_VERIFY` exists but TLS verification is **NOT**
   disabled by default.
3. **The spine is a MOUNT-TYPE discriminator + FAN-OUT.** `GET sys/mounts` / `GET sys/auth` (the parents)
   return a **map keyed by path**; each mount's **`type`** selects the backend role-list endpoint and the
   TF type (pki/database/aws → `vault_*_secret_backend_role`; jwt/approle/token → `vault_*_auth_backend_role`)
   — the Keycloak `protocol`/`providerId` precedent.
4. **THREE distinct response shapes** (§ Shape): **map-keyed-by-path** (`sys/mounts`/`sys/auth`/`sys/audit`
   — NEW), **LIST-keys** (`{"data":{"keys":[…]}}`, trailing `/` marks a sub-path), and **single-object**
   (`{"data":{…}}`).
5. **Import IDs are HETEROGENEOUS path composites** (§ CRITICAL) — bare path, bare name, `<backend>/roles/<name>`,
   `auth/<backend>/role/<name>`, `auth/token/roles/<name>`, `auth/<backend>/map/teams/<team>` — **four
   different composite shapes, encode per-TF-type, never infer.**
6. **`LIST` is a real HTTP verb** (or `GET …?list=true`) and **an empty directory 404s** (a 404 on
   `<backend>/roles` usually just means "no roles" — a normal skip, not an error).

## Version pin (load-bearing)

Pin `hashicorp/vault ~> 5.0` (current is **5.x** — the `auth_backend` docs resolve under
`…/hashicorp/vault/5.7.0/…`; **VERIFY the current major at build**). Naming/behaviour facts that matter
(the Terraformer-vs-current divergences):

- **Terraformer INLINES the Vault token into the provider block** — `GetConfig()` returns
  `"token": cty.StringVal(p.token)` (and `"address": …`), writing the token straight into HCL. **This is a
  secret leak.** TerraLift MUST NOT inline the token; the emitted `providers.tf` authenticates via
  `VAULT_ADDR` / `VAULT_TOKEN` (+ optional `VAULT_NAMESPACE`) env only. This + the `vault_generic_secret`
  data-read are the two "do not copy Terraformer" items.
- **Terraformer enumerates `vault_generic_secret`** — it LISTs `{mount}/` to walk KV paths and adopts each
  secret. **HARD-EXCLUDE** (§ Paramount constraint); TerraLift never LISTs a mount's contents.
- **Terraformer's resource set is broad (~37 types) but its ENUMERATION is only two files** — a single
  `vault_service_generator.go` walks `sys/mounts`, `sys/auth`, `sys/policies/acl`, then per-mount
  `{mount}/roles` and per-auth `/auth/{backend}/{entity}` (roles/users/groups). It does NOT cover
  `sys/audit`, `sys/namespaces`, or the mount/auth *config* resources (ldap/github) directly — those are
  covered here from the API + registry. **Do NOT pull the `hashicorp/vault/api` SDK** — a raw `net/http`
  client is smaller and matches Logz.io/Mackerel/Keycloak (a deliberate non-adoption).
- Terraformer reads `VAULT_ADDR` + `VAULT_TOKEN` (env), CLI args override. The **TF provider** reads the
  same `VAULT_ADDR` / `VAULT_TOKEN` (+ `VAULT_NAMESPACE`, `VAULT_CACERT`, `VAULT_SKIP_VERIFY`, …). The REST
  endpoints below are provider-version-independent.

## Shape

- **Auth — the `X-Vault-Token` header (the Logz.io `X-API-TOKEN` / Mackerel `X-Api-Key` custom-header
  shape).** Vault authenticates with **`X-Vault-Token: <token>`** on every request (exact casing confirmed
  in the API index; the `Authorization: Bearer <token>` form is also accepted, but pin the canonical
  `X-Vault-Token`). Plus `Accept: application/json`. Read the token from **`VAULT_TOKEN`**. NOT a query
  param, NOT the body. The token rides **only** on the `X-Vault-Token` header — never in the URL, query,
  request body, errors, logs, config, or state (redact any URL/query that could appear in a message —
  mirror `logzioapi.go`/`mackerelapi.go`'s `redactURL`). Optional Enterprise **`X-Vault-Namespace: <ns>`**
  from **`VAULT_NAMESPACE`** on every request (scopes the whole session to one namespace). A direct
  `net/http` client; **no `vault` CLI, no `hashicorp/vault/api` SDK**. Use a **redirect-refusing** client
  (mirror `mkHTTPClient` — Go does NOT strip a custom `X-Vault-Token` header on a cross-host 3xx, so an
  auto-followed redirect would leak the token; Vault also issues **307 redirects from a standby to the
  active node** — we must refuse and require `VAULT_ADDR` point at the active node / a routable endpoint,
  rather than replay the token).
- **Base URL — the user-supplied cluster, https, `/v1/` prefix.** Require **`VAULT_ADDR`** (default
  `https://127.0.0.1:8200`); strip any trailing slash. **Force https** (the token is a secret — upgrade a
  bare host / explicit `http://`, mirror `forceHTTPS`) UNLESS the host is `localhost`/`127.0.0.1` where a
  dev server on `http://` is common (allow http there with a **Warn**, the Keycloak local-dev divergence).
  Guard the host charset (reject `@`/path/port-splice, the `validRegion`/`validDomain` userinfo guard). All
  routes are `<VAULT_ADDR>/v1/<path>`. **TLS matters** — do NOT set `InsecureSkipVerify` by default;
  honour `VAULT_CACERT` / `VAULT_SKIP_VERIFY` only if explicitly set (and Warn loudly if verification is
  disabled). URLs are always built from `base+"/v1/"+path`; we never follow a server-supplied next-link.
- **Scope — one Vault CLUSTER (+ one NAMESPACE) = one flat container.** The token authenticates against the
  cluster; `VAULT_NAMESPACE` (default root) fixes the namespace. There is no sub-cluster resolution — the
  token *is* the cluster. `model.ScopeTenant`. **Namespaces are a fan-out key, not a container tree** (like
  Keycloak realms / LaunchDarkly projects) — even though Vault namespaces genuinely nest (Enterprise),
  `Capabilities.Hierarchy` stays **false** and Phase A does **not** recurse cross-namespace (it operates
  within the one configured namespace and adopts child namespaces as `vault_namespace` LEAF resources
  without descending into them — the recursion is a deferred increment). Resolve the container id/name
  **best-effort** from `GET sys/health` (`cluster_name`/`cluster_id`, no auth-scope needed), falling back to
  the `VAULT_ADDR` host. `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response family — THREE distinct shapes (the key structural fact; wider than any prior provider).**
  Everything is under the `/v1/` prefix and (in modern Vault) wrapped in the standard envelope
  `{"request_id":…,"data":{…},"warnings":…}`. **The decode must be tolerant of BOTH the `data`-wrapped
  form AND a top-level form** — a few `sys/*` endpoints historically return their payload at the TOP LEVEL
  (no `data` wrapper), e.g. the deprecated `sys/policy`, and older Vault returned the `sys/mounts`/`sys/auth`
  maps at top level too. Decode: try `data`, fall back to top-level (mirror `decodeEnvelope`'s
  wrapped-then-bare fallback). The three shapes:
  1. **MAP-KEYED-BY-PATH (NEW — no prior provider).** `GET sys/mounts`, `GET sys/auth`, `GET sys/audit`
     return an OBJECT whose **keys are the mount/auth/audit paths** and whose values are the config
     objects — **NOT an array**:
     ```json
     { "data": { "secret/": {"type":"kv","accessor":"kv_…"},
                 "pki/":    {"type":"pki"},
                 "sys/":    {"type":"system"} } }
     ```
     Decode into `map[string]struct{ Type, Accessor, Description string }`; **the KEY is the path** (carries
     a trailing `/` in the API — strip it for the import id, § CRITICAL). VERIFY at build whether `sys/audit`
     is `data`-wrapped and trailing-slash-keyed like `sys/mounts` (its API-doc sample shows a bare
     `{"file":{…}}` — likely a doc simplification of the wrapped `{"data":{"file/":{…}}}`; the tolerant
     decode handles either).
  2. **LIST-KEYS.** `LIST sys/policies/acl`, `LIST <backend>/roles`, `LIST auth/<backend>/role`,
     `LIST sys/namespaces` return `{"data":{"keys":["a","b/"]}}` — a `keys` array of names; a **trailing `/`
     on a key marks a SUB-PATH** ("directory"), not a leaf. Decode `data.keys` (fall back to top-level
     `keys`; `sys/policies/acl` may also carry a parallel `policies` array — prefer `keys`). Confirmed for
     PKI: `LIST /v1/pki/roles` → `{"data":{"keys":["dev","prod"]}}`.
  3. **SINGLE-OBJECT.** `GET sys/policies/acl/<name>` → `{"data":{"name":"deploy","policy":"<hcl>"}}`,
     `GET sys/mounts/<path>`, `GET <backend>/roles/<name>` → one config object under `data`. Phase-A
     enumeration needs shapes 1 + 2 (paths + names); the single-object read is only for optional
     label/curation and MUST stay on config fields (the policy document is config, not a secret).
- **Pagination — NONE.** Vault list endpoints return the full `keys`/map in one call (no cursor/limit). The
  fan-out multiplies by mount/auth count, not by page. Bound any accidental loop defensively; re-VERIFY at
  build that no `sys/*` list silently caps (they do not).
- **`LIST` semantics + empty-directory 404.** Vault's list is the HTTP **`LIST`** verb OR **`GET …?list=true`**
  (equivalent) — pick one and pin it (prefer `GET …?list=true` for maximal proxy/client compatibility, but
  `redactURL` must strip the `?list=true` before any log). **A LIST of an empty/absent directory returns
  404** (not an empty `keys`) — so a 404 on `<backend>/roles` normally means "no roles configured on that
  mount," a **benign Verbose skip**, indistinguishable from "feature absent." Do not treat it as a failure.
- **Status handling (mirror `mackerelapi.go`/`enumerate.go`'s `list`; carry the status on the error).** Vault
  errors are **`{"errors":["msg","msg2"]}`** for any HTTP status ≥ 400 (a string array, NOT an object — a
  different envelope than Mackerel's `{"error":{"message"}}`; parse the `errors[]` array, never echo the
  request). Rules:
  - **401** (missing/invalid token) → fatal in preflight; a mid-enumeration 401 (token revoked/expired) →
    fatal (every remaining list will fail).
  - **403 — the Vault-specific subtlety (Vault returns 403 for permission-denied AND for a bad token).**
    The chosen rule: **a 403 on a specific mount/role/leaf sub-list = the token's policy does not grant that
    path → best-effort Verbose SKIP** (do not fail the run). **A 403 on the sys/* BACKBONE (`sys/mounts`
    AND `sys/auth`) = the token lacks broad `read`/`list` on `sys/*` → systemic**: Warn + count, and if
    both backbone lists 403 and nothing was found, surface a systemic failure (do NOT ship an empty
    inventory — same guard as the other providers). I.e. leaf-403 → skip; backbone-403-with-zero-results →
    fail. (Preflight's `auth/token/lookup-self` probe disambiguates a truly-bad token — which 403s
    everywhere — from a merely under-privileged one — which 403s on `sys/*` but succeeds on lookup-self.)
  - **404** (mount/feature/namespace absent, OR an empty LIST directory) → Verbose skip. `sys/namespaces`
    404s on Vault OSS (Enterprise-only) → skip cleanly.
  - **429 / 5xx / network** → enumeration may be silently incomplete → **Warn + hardFails++** (tell a
    systemic failure apart from an empty cluster). The token never appears in errors/logs.
- **Preflight**: `terraform` present + `VAULT_ADDR` + `VAULT_TOKEN` set + a lightweight auth probe succeeds.
  Use **`GET auth/token/lookup-self`** as the auth probe — a valid token can ALWAYS look itself up (it
  returns the token's own policies/ttl/accessor — **metadata, not a secret**), so a **403/401 there means a
  genuinely bad/expired token**, whereas a 403 on `sys/mounts` means merely under-privileged. Then attempt
  `GET sys/mounts` to confirm config-read scope (a 403 here is a Warn, not a preflight failure — a scoped
  token may still enumerate *some* backends it is granted). There is no dedicated `/validate` endpoint;
  lookup-self is the auth check.
- **Connect**: run `auth/token/lookup-self` to validate the token, best-effort `GET sys/health` for the
  container name (`cluster_name`, fall back to the `VAULT_ADDR` host — a locked/standby node may 429/503
  on health), fix the namespace from `VAULT_NAMESPACE`, and set the single flat container.

## Mount-TYPE discriminator + backend FAN-OUT + heterogeneous import IDs — the CRITICAL determination

This is Vault's analogue of Keycloak's "discriminator + fan-out + composite-import-depth" call, fused with
the paramount config-vs-data line. The load-bearing per-resource facts are **(a) is the resource a
sys-backbone object (`sys/mounts`/`sys/auth`/`sys/policies`/`sys/audit`/`sys/namespaces`) or a
backend-scoped role reached by a per-mount FAN-OUT; (b) which mount `type` DISCRIMINATES a heterogeneous
mount list into its role TF type; and (c) which of the SIX import-id shapes the resource uses.** Get (a)
wrong and you either miss the roles or — far worse — walk into a data plane; get (b) wrong and you emit the
wrong `vault_*_backend_role` type or hit the wrong list path; get (c) wrong and every import block is
un-importable. All three are pinned per-resource in the catalog and re-verified against the registry
`website/docs/r/*.html.md`.

- **Sys-backbone (the parents) → bare-path / bare-name import.**
  - `GET sys/mounts` (map keyed by path) → each entry is a `vault_mount` (import = **bare `<path>`**, trailing
    slash **stripped**). **Skip the built-in system mounts** — `sys/`, `identity/`, `cubbyhole/` (and the
    default `secret/` KV only if it is the auto-created one — VERIFY the skip set; these cannot be managed as
    a `vault_mount`). The mount **`type`** is the fan-out discriminator.
  - `GET sys/auth` (map keyed by path) → each entry is a `vault_auth_backend` (import = **bare `<path>`**).
    **Skip the built-in `token/`** auth method as a `vault_auth_backend` (it is always present and not
    createable) — but DO fan out `auth/token/roles` for `vault_token_auth_backend_role`. The auth **`type`**
    discriminates the auth-role/config TF type.
  - `LIST sys/policies/acl` (keys = policy names) → `vault_policy` (import = **bare `<name>`**). **Skip the
    built-in `root` and `default`** policies (not manageable).
  - `GET sys/audit` (map keyed by path) → `vault_audit` (import = **bare `<path>`**, trailing slash stripped).
  - `LIST sys/namespaces` (Enterprise; 404 on OSS → skip; keys carry trailing `/`) → `vault_namespace`
    (import = **bare `<name>`/path without trailing slash**; the resource's `id` attribute keeps the slash
    but the import id does not). LEAF only — do NOT recurse into child namespaces in Phase A.
- **Backend-scoped roles (per-mount FAN-OUT) → `type`-selected list path + `type`-selected TF type + the
  role composite import.** For each mount from `sys/mounts`/`sys/auth`, branch on `type`:
  | mount source | `type` | LIST path | TF type | import id |
  |---|---|---|---|---|
  | sys/mounts | `pki` | `LIST <mount>roles` | `vault_pki_secret_backend_role` | `<backend>/roles/<name>` |
  | sys/mounts | `database` | `LIST <mount>roles` | `vault_database_secret_backend_role` | `<backend>/roles/<name>` |
  | sys/mounts | `aws` | `LIST <mount>roles` | `vault_aws_secret_backend_role` | `<backend>/roles/<name>` |
  | sys/auth | `jwt`/`oidc` | `LIST auth/<mount>role` | `vault_jwt_auth_backend_role` | `auth/<backend>/role/<name>` |
  | sys/auth | `approle` | `LIST auth/<mount>role` | `vault_approle_auth_backend_role` | `auth/<backend>/role/<name>` |
  | sys/auth | `token` (built-in `token/`) | `LIST auth/token/roles` | `vault_token_auth_backend_role` | `auth/token/roles/<name>` |
  | sys/auth | `github` | `LIST auth/<mount>map/teams` | `vault_github_team` | `auth/<backend>/map/teams/<team>` |
  Note the `<backend>` in the import id is the **mount path** (e.g. `pki`, `aws`, `jwt`), i.e. the map key
  with its trailing slash stripped. **VERIFY each LIST sub-path per type at build** — secret backends and
  token use `roles` (plural) while jwt/approle use `role` (singular); github uses `map/teams`.
- **Auth/secret MOUNT-CONFIG resources (no fan-out — the mount object itself IS the resource).** Some auth
  types map their *mount* to a dedicated config TF type instead of (or in addition to) a generic
  `vault_auth_backend`:
  - `type == "ldap"` (auth) → `vault_ldap_auth_backend` (import = **bare `<path>`**, e.g. `ldap`). Its
    `bindpass` is a write-only secret Vault does not return on read (→ Phase-B scrub, not a data leak).
    ldap users/groups (`vault_ldap_auth_backend_{user,group}`) are deferred.
  - `type == "github"` (auth) → `vault_github_auth_backend` (the mount config; import `<path>`) PLUS the
    `vault_github_team` fan-out above.
  - `type == "kubernetes"`/`"aws"`/`"gcp"`/`"okta"` (auth) → their `vault_*_auth_backend` config resources —
    **later increments** (each has its own config secret to scrub).
- **The import-id heterogeneity is the #1 hazard — SIX shapes, encode per-TF-type, never infer:**
  bare path (`vault_mount`/`vault_auth_backend`/`vault_audit`/`vault_ldap_auth_backend`), bare name
  (`vault_policy`/`vault_namespace`), `<backend>/roles/<name>` (secret-backend roles — `roles` plural, NO
  `auth/` prefix), `auth/<backend>/role/<name>` (jwt/approle — `role` SINGULAR, WITH `auth/` prefix),
  `auth/token/roles/<name>` (the ODD one — `roles` plural WITH `auth/token/` prefix), and
  `auth/<backend>/map/teams/<team>` (github). Encode the import id as an explicit per-TF-type switch in
  `importid.go` (mirror Okta's `rawImportID` / Keycloak's depth switch) — the whole composite is
  `util.EscapeHCLTemplate`-wrapped before emit.

## Enumeration spine

Flat cluster scope (one namespace from `VAULT_NAMESPACE`). The spine is a **sys-backbone pass then a
mount-type fan-out**: list the five sys-backbone collections, then per mount/auth (branching on `type`) its
roles. Best-effort per list (leaf-403 / 404-empty → Verbose skip; backbone-403 / 5xx → Warn + count; 401 →
fatal). The token never appears in errors/logs. (Mirror `mackerel/enumerate.go`: a top-level `list` helper
owns the systemic-failure count; a `subList` helper for the per-mount fan-out does NOT bump the count, since
sub-lists multiply by mount count — the LaunchDarkly/Mackerel pattern.) **The map-keyed decode is new**
(`decodeMountMap` → `map[string]mountInfo`; the KEY is the path).

- **Sys-backbone (one call each):**
  - `GET sys/mounts` → map keyed by path → `vault_mount` (bare `<path>`, strip trailing `/`; skip
    `sys/`/`identity/`/`cubbyhole/`). **Capture each (path, type)** — the fan-out keys for secret-backend
    roles.
  - `GET sys/auth` → map keyed by path → `vault_auth_backend` (bare `<path>`; skip `token/`). **Capture each
    (path, type)** — the fan-out keys for auth-backend roles + the ldap/github config resources.
  - `LIST sys/policies/acl` → `data.keys` → `vault_policy` (bare `<name>`; skip `root`/`default`).
  - `GET sys/audit` → map keyed by path → `vault_audit` (bare `<path>`).
  - `LIST sys/namespaces` → `data.keys` (trailing `/`) → `vault_namespace` (bare `<name>`; **404 on OSS →
    skip**; LEAF only, no recursion).
- **Per secret mount `<m>` (fan-out on `sys/mounts` type):**
  - `type==pki` → `LIST <m>roles` → `data.keys` → `vault_pki_secret_backend_role` (`<m>/roles/<name>`).
  - `type==database` → `LIST <m>roles` → `vault_database_secret_backend_role` (`<m>/roles/<name>`).
  - `type==aws` → `LIST <m>roles` → `vault_aws_secret_backend_role` (`<m>/roles/<name>`).
- **Per auth mount `<a>` (fan-out on `sys/auth` type):**
  - `type==jwt|oidc` → `LIST auth/<a>role` → `vault_jwt_auth_backend_role` (`auth/<a>/role/<name>`).
  - `type==approle` → `LIST auth/<a>role` → `vault_approle_auth_backend_role` (`auth/<a>/role/<name>`).
  - `type==token` (the `token/` mount) → `LIST auth/token/roles` → `vault_token_auth_backend_role`
    (`auth/token/roles/<name>`).
  - `type==ldap` → emit `vault_ldap_auth_backend` for the mount (bare `<path>`); users/groups deferred.
  - `type==github` → emit `vault_github_auth_backend` for the mount + `LIST auth/<a>map/teams` →
    `vault_github_team` (`auth/<a>/map/teams/<team>`).

**NEVER, in any branch, LIST or GET a mount's DATA plane** (`<kv>/`, `<kv>/metadata`, `<kv>/data/*`,
`<pki>/certs`, `<db>/creds`, `<aws>/creds`, `<transit>/export`). The fan-out only ever touches
`.../roles`, `.../role`, `.../roles`, `.../map/teams` — config/name endpoints. If nothing was found AND the
sys backbone failed with real (non-leaf-403/non-404) errors, surface a systemic failure rather than
shipping an empty inventory (same guard as the other providers).

## Resource catalog

Import IDs verified against the current `hashicorp/vault` registry docs (`website/docs/r/*.html.md`). All
scope = cluster/namespace. "list" = the enumeration mechanism; "disc / fan-out" names the mount-`type`
discriminator or the parent. The **id shape** column is the #1 hazard.

| native key | TF type | list endpoint → shape | disc / fan-out | import ID | id shape |
|---|---|---|---|---|---|
| vault:mount | vault_mount | `GET sys/mounts` → map-by-path | parent (skip sys/identity/cubbyhole) | `<path>` (no `/`) | **bare path** |
| vault:auth_backend | vault_auth_backend | `GET sys/auth` → map-by-path | parent (skip token/) | `<path>` | **bare path** |
| vault:policy | vault_policy | `LIST sys/policies/acl` → data.keys | — (skip root/default) | `<name>` | **bare name** |
| vault:audit | vault_audit | `GET sys/audit` → map-by-path | — | `<path>` | **bare path** |
| vault:namespace | vault_namespace | `LIST sys/namespaces` → data.keys | — (Enterprise; leaf, no recurse) | `<name>` | **bare name** |
| vault:pki_secret_backend_role | vault_pki_secret_backend_role | `LIST <m>roles` → data.keys | ← mount `type=pki` | `<backend>/roles/<name>` | **`/roles/` composite** |
| vault:database_secret_backend_role | vault_database_secret_backend_role | `LIST <m>roles` → data.keys | ← mount `type=database` | `<backend>/roles/<name>` | **`/roles/` composite** |
| vault:aws_secret_backend_role | vault_aws_secret_backend_role | `LIST <m>roles` → data.keys | ← mount `type=aws` | `<backend>/roles/<name>` | **`/roles/` composite** |
| vault:ldap_auth_backend | vault_ldap_auth_backend | `GET sys/auth` (type=ldap) | ← auth `type=ldap` | `<path>` | **bare path** (bindpass write-only) |
| vault:jwt_auth_backend_role | vault_jwt_auth_backend_role | `LIST auth/<a>role` → data.keys | ← auth `type=jwt/oidc` | `auth/<backend>/role/<name>` | **`auth/…/role/` composite** |
| vault:approle_auth_backend_role | vault_approle_auth_backend_role | `LIST auth/<a>role` → data.keys | ← auth `type=approle` | `auth/<backend>/role/<name>` | **`auth/…/role/` composite** |
| vault:token_auth_backend_role | vault_token_auth_backend_role | `LIST auth/token/roles` → data.keys | ← auth `type=token` | `auth/token/roles/<name>` | **`auth/token/roles/` composite (odd)** |
| vault:github_auth_backend | vault_github_auth_backend | `GET sys/auth` (type=github) | ← auth `type=github` | `<path>` | **bare path** (INC — VERIFY) |
| vault:github_team | vault_github_team | `LIST auth/<a>map/teams` → data.keys | ← auth `type=github` | `auth/<backend>/map/teams/<team>` | **`map/teams/` composite** (VERIFY LIST path) |

### Import-format quirks (§ do not get wrong)

1. **SIX import shapes — encode per TF type, never infer the separator or part-count.** bare path / bare
   name / `<backend>/roles/<name>` / `auth/<backend>/role/<name>` / `auth/token/roles/<name>` /
   `auth/<backend>/map/teams/<team>`. This is the provider's defining hazard.
2. **Secret-backend roles vs auth-backend roles differ in BOTH the prefix and the plural.** Secret backends:
   `<backend>/roles/<name>` — `roles` **plural**, **NO** `auth/` prefix (confirmed `pki/roles/my_role`,
   `postgres/roles/my-role`, `aws/roles/deploy`). Auth backends (jwt/approle): `auth/<backend>/role/<name>`
   — `role` **singular**, **WITH** `auth/` prefix (confirmed `auth/jwt/role/test-role`,
   `auth/approle/role/test-role`).
3. **`vault_token_auth_backend_role` is the ODD one — `auth/token/roles/<name>`** (`roles` **plural** AND
   the `auth/token/` prefix — confirmed). Do NOT assume the jwt/approle `role`-singular form for token.
4. **Mount / auth / audit ids STRIP the trailing slash.** The API map keys carry a trailing `/`
   (`secret/`, `github/`, `file/`); the import id is the path WITHOUT it (`terraform import vault_mount.x
   secret`, `…vault_audit.x syslog`). `vault_namespace` likewise: the object `id`/`path` show a trailing
   slash but the import id is the bare name (`example2`). **Strip `/` on emit; keep it as the map lookup
   key during enumeration.**
5. **`vault_github_team` = `auth/<backend>/map/teams/<team>`** (confirmed `auth/github/map/teams/terraform-developers`),
   and the LIST path to enumerate teams is `auth/<backend>/map/teams` — **VERIFY the LIST path** (github team
   mappings live under `.../map/teams`).
6. **All ids/paths are opaque strings off the wire — no numeric stringify** (unlike Datadog). Mount paths,
   role names, policy names copy verbatim; a path segment may itself contain `/` (nested mount paths) — the
   composite build must not double-escape those. Template-escape the whole composite on emit
   (`util.EscapeHCLTemplate`) for parity — Vault policy names / mount paths can contain `$`/`{`.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a live cluster —
prune computed via `hcl.WalkResourceBlocks`; scrub credential fields. **The recurring hazard is
credential-bearing CONFIG fields** (bindpass, connection_url passwords, engine root keys) — Vault masks
most on read (returns null), so the risk is generate-config-out **nulling** them (not plan-clean until
re-supplied) rather than leaking them, but the repo-wide secret scan is the backstop.

- **`vault_mount` / `vault_auth_backend` — trivial config; the fan-out parents.** `path`, `type`,
  `description`, `default_lease_ttl_seconds`, `max_lease_ttl_seconds`, `options`. No secret. Prune computed
  `accessor`. The KV mount is adopted as a MOUNT (engine config) — never touch its data.
- **`vault_policy` — light.** `name`, `policy` (the ACL HCL document — config, read via
  `GET sys/policies/acl/<name>`; contains **path grants, not secrets**). The policy string is HCL-in-HCL →
  **template-escape `$`/`{`** (a policy body has `path "secret/*" { capabilities = [...] }` — the `${…}`
  interpolation hazard, the Keycloak `$`→`$$` precedent). No secret.
- **`vault_audit` — light.** `path`, `type` (`file`/`syslog`/`socket`), `options` (`file_path`, …). No
  secret (audit device config, not audit *logs* — which are DATA, out of scope).
- **`vault_namespace` — trivial.** `path`. No secret. Leaf; recursion deferred.
- **`vault_pki_secret_backend_role` / `_database_ / `_aws_` — medium; NO secret on read (confirmed).** The
  role defines the *rules* for minting dynamic creds (`allowed_domains`, `ttl`, `creation_statements`,
  `credential_type`, `policy_arns`), NOT the creds — "No additional attributes are exported." Adopt cleanly;
  prune computed. **The generated CREDENTIALS (`<pki>/issue`, `<db>/creds`, `<aws>/creds`) are DATA and are
  never touched.**
- **`vault_ldap_auth_backend` — medium; write-only SECRET.** `path`, `url`, `binddn`, `userdn`, `groupdn`,
  `userattr`. **Secret:** `bindpass` (the LDAP bind password) — Vault does **not** return it on read, so
  generate-config-out nulls it → **scrub / flag re-supply**, keep the block (also a `bindpass_wo` write-only
  variant). Not a read leak (masked), but not plan-clean until re-supplied.
- **`vault_database_secret_backend_connection` (later inc) — connection_url embeds a password** →
  **scrub**. `vault_aws_secret_backend` root config holds `access_key`/`secret_key` (engine root creds,
  not returned on read) → **scrub**. Prefer IAM-role/instance-profile variants where possible.
- **`vault_jwt_auth_backend_role` / `_approle_ / `_token_` — light; no secret on read.** `role_name`,
  `token_policies`, `token_ttl`, bound-claims / bound-cidrs. AppRole's `secret_id` is a SEPARATE resource
  (deferred, and it mints a secret — excluded). No secret on the role read.
- **`vault_github_team` — trivial.** `backend`, `team`, `policies`. No secret.

Until Phase B these are no-ops, so a Vault export is a breadth scaffold, not yet plan-clean. The pipeline's
repo-wide secret scan is the backstop for the `bindpass` / `connection_url` password / engine-root-key
fields that generate-config-out nulls-or-leaks before the scrub rules land — and the paramount backstop is
that enumeration NEVER reads a secret value in the first place.

## Write-only / secret resources (EXCLUDE / scrub)

Two tiers: **HARD-EXCLUDE the secret-DATA plane entirely** (never enumerate/read/import), and **scrub the
field-level write-only credentials** on the adoptable config shells.

**HARD-EXCLUDE (secret DATA — the paramount rule):**
- **`vault_generic_secret` / `vault_kv_secret` / `vault_kv_secret_v2`** — the CONTENTS of a KV store.
  Adopting any of these reads the secret VALUE into inventory/state/HCL. Never enumerate (never LIST a KV
  mount's paths, `<kv>/`, `<kv>/metadata`), never import. **Terraformer adopts `vault_generic_secret` — the
  leak we refuse.**
- **Generated / dynamic credentials** — `<pki>/issue/*`, `<pki>/cert/*`, `<db>/creds/*`, `<aws>/creds/*`,
  `<transit>/export/*`, `auth/*/login`, token creation. These MINT or RETURN live secrets. Never read.
- **Root token, unseal / recovery keys, seal state** — `sys/unseal`, `sys/generate-root`, `sys/rekey`,
  `sys/seal`. Never touch.
- **Cubbyhole** (`cubbyhole/`), **transit key material**, **PKI CA private keys** — key material. Excluded.
- **The Vault token itself** — `VAULT_TOKEN` lives ONLY on the `X-Vault-Token` header, never in generated
  config, state, errors, or logs. **Do NOT inline it into `providers.tf` (Terraformer does — refuse it).**
  There is no round-trippable "token" resource to adopt.

**Scrub the value, keep the config shell (Vault masks these on read → generate-config-out nulls them):**
- **`vault_ldap_auth_backend.bindpass`** — LDAP bind password → scrub, flag re-supply.
- **`vault_database_secret_backend_connection` connection_url password** → scrub.
- **`vault_aws_secret_backend` `secret_key` / azure/gcp engine root creds** → scrub (prefer IAM-role
  variants).
- **`vault_*_auth_backend` client secrets** (github/jwt/oidc `oidc_client_secret`, kubernetes token) →
  scrub as the config resources land.
- **`X-Vault-Namespace`** is not a secret (it's a routing header) — safe to emit as the `VAULT_NAMESPACE`
  env note in `providers.tf`.
- **Not secret, do not over-scrub:** mount/auth `type`/`path`/`description`/ttls, policy documents (ACL
  grants), audit device `options` (file paths), pki/db/aws role RULES (allowed_domains, creation_statements,
  policy_arns), namespace paths, github team→policy maps, jwt bound-claims. These are config, adopt them.

## Deliberately out of scope

- **The entire secret-DATA plane** — `vault_generic_secret`, `vault_kv_secret*`, dynamic-credential reads,
  cubbyhole, transit/PKI key material, root/unseal keys. Not "deferred" — **permanently excluded** (the
  paramount constraint). Adopting them is a leak, not a feature.
- **Identity plane** (`vault_identity_entity`, `_entity_alias`, `_group`, `_group_alias`,
  `_oidc_*`) — the internal identity store (entities/groups/aliases). Config, but a large N with its own
  fan-out; a much-later increment. Phase A adopts mounts/auth/policies, not identities.
- **Per-auth users/groups** (`vault_ldap_auth_backend_user`/`_group`, `vault_okta_auth_backend_user`/`_group`,
  `vault_cert_auth_backend_role`, `vault_kubernetes_auth_backend_role`, …) — the deeper per-auth role/user
  plane beyond the jwt/approle/token/github beachhead; later increments (each auth type is its own fan-out).
- **Secret-engine CONNECTIONS / configs** (`vault_database_secret_backend_connection`,
  `vault_aws_secret_backend` root config, `vault_pki_secret_backend_config_*`, `vault_pki_secret_backend_cert`
  issuance) — the credential-bearing engine configs (scrub-heavy) and the cert *issuance* (DATA). A later
  increment after the mount + role shells are solid.
- **Cross-namespace recursion** (Enterprise) — Phase A operates within one `VAULT_NAMESPACE` and adopts child
  `vault_namespace` objects as leaves; recursing `sys/namespaces` into each child (a namespace fan-out with
  per-namespace `X-Vault-Namespace`) is a deferred increment. `Capabilities.Hierarchy=false`.
- **Enterprise-only policy types** (`vault_egp_policy`, `vault_rgp_policy` — Sentinel governance) and
  `vault_password_policy` / `vault_rotation_policy` — later increments alongside the ACL `vault_policy`
  beachhead.
- **Runtime/DATA endpoints** — leases (`sys/leases`), tokens (`auth/token/*` beyond lookup-self and role
  LISTs), metrics, audit *logs*, replication state, seal status. DATA/operational, not config. Out of scope.
- **The `hashicorp/vault/api` SDK + the `vault` CLI** — Terraformer pulls the SDK; TerraLift uses a raw
  `net/http` client (smaller, matches Logz.io/Mackerel/Keycloak). A deliberate non-adoption. (Also
  non-adopted: Terraformer's token-inlining and its `vault_generic_secret` data-read.)
- **Cloud-IAM depth** (`Capabilities.IAM=false`) — Vault ACL policies + token roles are modeled at breadth,
  but entity→group→policy assignment and Sentinel governance are the deferred identity/governance planes.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)

The **recommended Phase-A CONFIG CORE** (~11 TF types): `vault_mount`, `vault_auth_backend`, `vault_policy`,
`vault_audit`, `vault_namespace`, `vault_pki_secret_backend_role`, `vault_database_secret_backend_role`,
`vault_aws_secret_backend_role`, `vault_ldap_auth_backend`, `vault_jwt_auth_backend_role`,
`vault_approle_auth_backend_role` (+ `vault_token_auth_backend_role` and the `vault_github_auth_backend`/
`vault_github_team` pair if the beachhead cluster runs token roles / github auth).

BEACHHEAD `vault_mount` + `vault_auth_backend` + `vault_policy` (the mount/auth/policy backbone essentially
every Vault cluster manages as IaC — `vault_mount` establishes the **map-keyed-by-path decode** (the new
shape) + the **bare-path trailing-slash-strip** import + the **system-mount skip**, `vault_auth_backend`
adds the second map-keyed list + the **`type` discriminator** capture, and `vault_policy` adds the
**LIST-keys decode** + the **policy-document `$`→`$$` escape** — and this trio exercises the
**`X-Vault-Token` custom-header client**, the **`auth/token/lookup-self` preflight**, and the
**403-leaf-vs-backbone rule** without touching a single data path) → INC-1 `vault_pki_secret_backend_role` +
`vault_database_secret_backend_role` + `vault_aws_secret_backend_role` (the **secret-mount fan-out** — the
`type`-selected `LIST <mount>roles` and the **`<backend>/roles/<name>` composite** import; confirms roles
read config-only, no creds) → INC-2 `vault_jwt_auth_backend_role` + `vault_approle_auth_backend_role` +
`vault_token_auth_backend_role` (the **auth-mount fan-out** — the **`auth/<backend>/role/<name>`** and the
**odd `auth/token/roles/<name>`** composites) + `vault_ldap_auth_backend` (the auth-config resource + the
**`bindpass` scrub**) → INC-3 `vault_audit` + `vault_namespace` (the last two sys-backbone lists — audit map
+ Enterprise namespace LIST) + `vault_github_auth_backend`/`vault_github_team` (the **`map/teams`** fan-out)
→ LATER the identity plane, per-auth users/groups + more auth-role types, the secret-engine
connection/config plane (scrub-heavy), cross-namespace recursion, the Enterprise/Sentinel policy types, and
the runtime/DATA endpoints. **NEVER: the secret-DATA plane (`vault_generic_secret`/`vault_kv_secret*`/
dynamic creds/unseal keys) — permanently excluded, the paramount constraint.**
