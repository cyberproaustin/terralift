# Microsoft Entra ID (azuread) provider — build spec

Research artifact for the `azuread` provider (Phase A scaffold; TF provider source is
**`hashicorp/azuread`** — the official Terraform provider for **Microsoft Entra ID** (formerly Azure
Active Directory / Azure AD), the directory/identity plane behind an Azure tenant, driven entirely
through the **Microsoft Graph API**). Sources: Terraformer's `providers/azuread/` (a NARROW generator
covering five types — `app_role_assignment`, `application`, `group`, `service_principal`, `user` —
authenticating via `ARM_TENANT_ID`/`ARM_CLIENT_ID`/`ARM_CLIENT_SECRET`), the `hashicorp/azuread`
registry docs (import formats + schema, **verified per-resource below** against the provider repo's
`docs/resources/*.md` on `main`), and the Microsoft Graph REST API (`learn.microsoft.com/graph`,
`https://graph.microsoft.com/v1.0/…`, **verified per-endpoint below** against the v1.0 reference).
Build mirrors **Keycloak** (`internal/providers/keycloak/`) most closely — the connect-time
**OAuth2 client-credentials token exchange with a FORM-encoded body** and the short-lived-Bearer /
mid-run-refresh discipline are the same shape (Keycloak diverged from Auth0's JSON exchange; Entra
diverges again — the exchange host is Microsoft's login endpoint, and the scope is `.default`) — and
mirrors **Opsgenie/Fastly** for the **server-supplied ABSOLUTE next-URL** pagination (Graph's
`@odata.nextLink` is a full `https://graph.microsoft.com/…` URL that MUST be host-validated before the
Bearer is re-sent, exactly the Fastly/Opsgenie next-link lesson). This is **REST, Keycloak-style, NOT
GraphQL** (the "Graph" in Microsoft Graph is a product name, not a query language).

**Entra's DEFINING HAZARD is the v3 import ID.** In `azuread` **v3.x** every object-scoped resource's
import id was changed from a **bare UUID** to a **Graph resource-path form with a leading `/` and a
collection segment** — `azuread_group` imports as **`/groups/<object-id>`** (not `<object-id>`),
`azuread_user` as **`/users/<object-id>`**, `azuread_service_principal` as
**`/servicePrincipals/<object-id>`**, `azuread_application`/`azuread_application_registration` as
**`/applications/<object-id>`**, and the conditional-access/directory objects as their FULL Graph path
(`/identity/conditionalAccess/policies/<id>`, `/directory/administrativeUnits/<id>`). The
**relationship composites are their own shapes** — `azuread_group_member` is
**`<group-id>/member/<member-id>`** (NO leading slash), and `azuread_app_role_assignment` is
**`/servicePrincipals/<resourceSpId>/appRoleAssignedTo/<assignmentId>`** (leading `/servicePrincipals/`,
separator **`appRoleAssignedTo`** — NOT `appRoleAssignment` — and the SP is the assignment's
**resource/target** SP). And **`azuread_directory_role_assignment` is a BARE opaque id with no prefix**,
while **`azuread_directory_role` is not importable at all.** Every prefix, every `/` separator, every
part-count, and whether the leaf is a UUID vs an opaque id MUST be verified per-TF-type and encoded
explicitly in `importid.go` — never inferred. Six facts set Entra apart, all load-bearing:

1. **Auth is a connect-time OAuth2 CLIENT-CREDENTIALS token exchange with a FORM-encoded body (the
   Keycloak pattern, diverged) — but the provider itself is KEYLESS via the Azure SDK credential
   chain.** The TF provider authenticates with the Azure SDK credential chain (a client-secret app via
   `ARM_CLIENT_ID`/`ARM_CLIENT_SECRET`/`ARM_TENANT_ID`, or a client certificate, managed identity, or
   `az` CLI) and **never inlines the secret into config**. The SCAFFOLD's enumerator mints a Bearer at
   connect time: `POST https://login.microsoftonline.com/<tenant>/oauth2/v2.0/token`, body
   `application/x-www-form-urlencoded`
   (`grant_type=client_credentials&client_id=…&client_secret=…&scope=https://graph.microsoft.com/.default`)
   → `{"access_token":"<jwt>","expires_in":3599,…}`; then `Authorization: Bearer <jwt>` on every Graph
   call. **TWO hosts** (login + graph), one tenant, one app.
2. **Base is `https://graph.microsoft.com/v1.0`** — a FIXED vendor host (no user-supplied server, unlike
   Keycloak/Grafana). A handful of features live only under **`/beta`** (flag per-endpoint; the Phase-A
   set is entirely **v1.0** — see the catalog). National clouds (US Gov / China) use different
   login+graph hosts (§ Base URL) — Phase A targets the **public** cloud, Warn otherwise.
3. **The response family is the OData v4 envelope `{"value":[…],"@odata.nextLink":"<absolute-url>"}`** —
   decode `.value` into `[]T`; there is no `count`/`total` (unlike VSTS). Single-object GETs return the
   bare object (no envelope).
4. **Pagination follows the SERVER-SUPPLIED ABSOLUTE `@odata.nextLink` URL** — a full
   `https://graph.microsoft.com/v1.0/…?$skiptoken=…` URL. **Host-validate it stays on the Graph host
   before re-sending the Bearer** (token-exfil guard — the Fastly/Opsgenie next-link lesson); loop until
   `@odata.nextLink` is absent. `$top`/`$select` bound/trim the page.
5. **Graph is PERMISSION-SCOPED — many 403s are EXPECTED, not systemic.** Each list requires a distinct
   application permission (`Group.Read.All`, `Application.Read.All`, `Policy.Read.All`,
   `RoleManagement.Read.Directory`, `AdministrativeUnit.Read.All`, `User.Read.All`, or the catch-all
   `Directory.Read.All`), admin-consented. A 403/404 on one list is a permission/feature-absent **skip**,
   not a run failure (§ Status handling) — a preflight concern for which permissions the app was granted.
6. **Applications and service principals are adopted as SHELLS — their credential material is
   NEVER decoded.** `passwordCredentials` (client secrets) and `keyCredentials` (certificates) on an
   application or service principal, and any minted token, are secret → never pulled into a list element
   (§ Write-only, the Keycloak `client_secret` / Vault `bind_credential` precedent).

## Version pin (load-bearing)

Pin `hashicorp/azuread ~> 3.x` (current is **v3.9.0**, June 2026; **VERIFY the current major at build**).
Naming/behaviour facts that matter (the Terraformer-vs-current + v2→v3 divergences):

- **The v3 IMPORT-ID CHANGE is the single most important fact in this spec.** In v2.x, object resources
  imported by a **bare UUID** (`terraform import azuread_user.x 00000000-…`); in v3.x they import by a
  **Graph resource-path id** (`terraform import azuread_user.x /users/00000000-…`). Confirmed by
  hashicorp/terraform-provider-azuread#1508 ("Expected a User ID that matched (containing 2 segments):
  `/users/userId`") and verified per-resource below. **This means TerraLift's import blocks MUST emit the
  v3 prefixed form, not the raw object id off the wire** — the enumerator reads the bare Graph `id`
  (a UUID) and `importid.go` prepends the correct `/collection[/sub]/` prefix per TF type. Getting the
  prefix wrong makes every import block for that type un-importable. **If the pinned provider is still
  v2.x at build, the ids are bare UUIDs instead — VERIFY the major and branch the prefix logic on it.**
- **`azuread_application_registration` is a NEW lightweight resource, NOT a rename of
  `azuread_application`.** BOTH exist in v3. `azuread_application` is the "kitchen-sink" resource
  (app + api/oauth2 scopes + app roles + optional-claims + credentials in one block);
  `azuread_application_registration` (added ~v2.47, the v3-forward direction) is the **bare app-object
  shell**, with API-access / app-roles / permission-scopes / redirect-URIs / owners / credentials split
  into separate `azuread_application_*` resources. **Both import as `/applications/<object-id>`.** For a
  Phase-A SHELL, target **`azuread_application_registration`** (no secret sub-blocks, cleanest
  round-trip) and defer the decomposed `azuread_application_*` companions. VERIFY the beachhead resource
  name against the pinned provider (if it must be v2.x, fall back to `azuread_application`).
- **Terraformer covers only FIVE types and does NOT inline the secret (the welcome divergence).** Its
  `azuread` generator reads `ARM_TENANT_ID`/`ARM_CLIENT_ID`/`ARM_CLIENT_SECRET` from env and emits
  `azuread_application`, `azuread_group`, `azuread_service_principal`, `azuread_user`,
  `azuread_app_role_assignment` — keyless. It predates v3, so **its recorded import/state ids are the
  bare-UUID v2 form** — do NOT copy them verbatim; re-derive the v3 prefixed id. **Do NOT pull the
  `microsoftgraph/msgraph-sdk-go` or `hashicorp/go-azure-sdk` SDK** — a raw `net/http` client is smaller
  and matches Keycloak/Okta/Vault (a deliberate non-adoption; the same call the other providers made).
- The **TF provider** reads `ARM_TENANT_ID` / `ARM_CLIENT_ID` / `ARM_CLIENT_SECRET` (Azure SDK env, also
  the `AZURE_*` aliases and `tenant_id`/`client_id`/`client_secret` provider args) — TerraLift's emitted
  `providers.tf` stays **keyless** (empty `provider "azuread" {}`, env-authenticated). The Graph REST
  endpoints below are provider-version-independent; only the IMPORT-ID PREFIX is version-sensitive.

## Shape

- **Auth — a connect-time OAuth2 CLIENT-CREDENTIALS token exchange, FORM-encoded body (the Keycloak
  divergence, re-diverged for Microsoft's login endpoint).** The enumerator is Bearer-authenticated
  against Graph, and the Bearer is minted at connect time:
  - **Connect-time exchange:** `POST https://login.microsoftonline.com/<ARM_TENANT_ID>/oauth2/v2.0/token`,
    **`Content-Type: application/x-www-form-urlencoded`**, body
    `grant_type=client_credentials&client_id=<ARM_CLIENT_ID>&client_secret=<ARM_CLIENT_SECRET>&scope=https://graph.microsoft.com/.default`
    → `{"access_token":"<jwt>","token_type":"Bearer","expires_in":3599,"ext_expires_in":3599}`. This POST
    is itself **unauthenticated** (no Bearer); the `client_secret` rides in the **form body** only. The
    `.default` scope means "every application permission already consented to this app" — no per-scope
    enumeration needed. (VERIFY: a **client-certificate** credential — `client_assertion` +
    `client_assertion_type=…jwt-bearer` — is the cert path; Phase A can require the client-secret form and
    Warn/defer the cert/managed-identity/`az`-CLI paths the TF provider also supports.)
  - **Then, on every Graph request:** `Authorization: Bearer <access_token>` + `Accept: application/json`.
    **The token is short-lived** (`expires_in` ~3600s) — cache it and **re-mint when it would elapse
    mid-run, and on a mid-run 401** (a large tenant enumeration can outlive one token; the refresh path
    is not optional — the Keycloak precedent).
  - **Secret discipline (mirror Keycloak/Vault):** the `client_secret` appears ONLY in the `/token` form
    body; the `access_token` ONLY on the `Authorization` header. Neither ever appears in a URL, query,
    request body, error, log, generated config, or state. A direct `net/http` client (mirror
    `keycloakapi.go`); **no `az`/`az ad` CLI, no Graph/Azure SDK.** Use a **redirect-refusing** client for
    **BOTH** hosts (login + graph) — Go does NOT strip the Authorization header on a cross-host 3xx, and a
    `@odata.nextLink` or a login redirect toward another host would replay the Bearer/secret; refuse and
    require the expected host.
- **Base URL — a FIXED Microsoft vendor host (no user server), TWO hosts.** The token host is
  `https://login.microsoftonline.com`; the API host is **`https://graph.microsoft.com`** with base path
  **`/v1.0`**. Build every list URL as `https://graph.microsoft.com/v1.0<path>`. A small number of
  features are **beta-only** under `/beta` (flag per-endpoint — the Phase-A set is entirely `/v1.0`; if a
  later resource needs `/beta`, pin it explicitly, never silently). **National clouds** swap both hosts:
  US Gov → `login.microsoftonline.us` + `graph.microsoft.us`; China (21Vianet) →
  `login.chinacloudapi.cn` + `microsoftgraph.chinacloudapi.cn`. Phase A hard-targets the **public**
  cloud (`ARM_ENVIRONMENT` unset/`public`); Warn + defer the sovereign-cloud host tables (VERIFY the
  `ARM_ENVIRONMENT` → host mapping at build). The pager builds URLs from base+path EXCEPT when following a
  server-supplied `@odata.nextLink` (which is host-validated — see Pagination).
- **Scope — one Entra TENANT = one flat container.** The app credential authenticates against exactly one
  tenant (the `<ARM_TENANT_ID>` in the token URL); there is no sub-tenant resolution — the credential
  simply **is** the tenant. `model.ScopeTenant`. Resolve the container id/name **best-effort**: the id is
  `ARM_TENANT_ID`; the display name can come from `GET /v1.0/organization` (`value[0].displayName`,
  best-effort — needs `Organization.Read.All`/`Directory.Read.All`; fall back to the tenant GUID).
  Administrative units create a directory *sub-scope*, but that is an enumeration/RBAC detail, not a
  TerraLift container tree — `Capabilities.Hierarchy` stays **false**. `Capabilities{IAM:false,
  Exposure:false, Hierarchy:false}` (IAM could be argued **true** — this IS the identity plane — but the
  role/group *assignment* depth is deferred, so keep it false at Phase A, mirroring Keycloak).
- **Response family — the OData v4 envelope `{"value":[…],"@odata.nextLink":…}` (the key structural
  fact).** Every list endpoint returns
  `{"@odata.context":"…","value":[ {…}, … ],"@odata.nextLink":"<absolute-url>"?}`. Decode a generic
  wrapper `struct{ Value json.RawMessage `json:"value"`; NextLink string `json:"@odata.nextLink"` }`,
  then unmarshal `Value` into `[]T`. **There is NO `count`/`total`** (a `@odata.count` appears only with
  `$count=true` + the `ConsistencyLevel: eventual` header — not used by plain enumeration). A
  single-object GET (`GET /v1.0/groups/<id>`) returns the **bare object** (no `value` wrapper) — a
  separate `graphGet` helper. Decode ONLY the `id` (and, for the composites, the parent/child object ids)
  — **never a `passwordCredentials`/`keyCredentials` field** (§ Write-only).
- **Pagination — follow the SERVER-SUPPLIED ABSOLUTE `@odata.nextLink` URL (the Fastly/Opsgenie
  lesson).** When a collection exceeds one page, the response carries
  `"@odata.nextLink":"https://graph.microsoft.com/v1.0/<path>?$skiptoken=<opaque>"` — a **full absolute
  URL**. **HOST-VALIDATE it before re-sending the Bearer**: parse it, require scheme `https` and host
  **exactly `graph.microsoft.com`** (or the resolved national-cloud graph host), reject any other host —
  a server that hands back a next-URL on a different host must never receive the Bearer (token-exfil
  guard; the redirect-refusing client is the backstop, this is the belt). Follow it verbatim (do NOT
  reconstruct the query — the `$skiptoken` is opaque), accumulate `.value`, and **loop while
  `@odata.nextLink` is present** (absent ⇒ last page). Add `&$top=<N>` on the FIRST request to bound page
  size (999 for `/users`,`/groups`,`/servicePrincipals`,`/applications`; `/identity/conditionalAccess/*`
  and `/directory/administrativeUnits` cap lower — **VERIFY the max `$top` per endpoint**; an over-large
  `$top` 400s). Optionally `$select=id,displayName` to trim payloads. Bound the loop defensively
  (`graphMaxPages`). Some lists return everything in one page (no `@odata.nextLink`) — handled as a single
  iteration.
- **Status handling (mirror `keycloak/enumerate.go`'s `list`/`subList`; carry the status on the error).**
  Graph errors are the OData error envelope **`{"error":{"code":"<symbolic>","message":"<human>",
  "innerError":{…}}}`** — parse `error.code` + `error.message`, **never echo the request** (the Bearer is
  header-only, but redact any URL/query before logging — `$skiptoken`/`$filter` are noise). Rules:
  - **401** (token expired/invalid) → **refresh once mid-run and retry** (Graph tokens are ~1h, so a
    mid-run 401 is *expected* on a long enumeration and re-mintable — only fatal if the re-mint itself
    fails); a **preflight 401** is fatal (bad `ARM_CLIENT_ID`/`ARM_CLIENT_SECRET`/`ARM_TENANT_ID`).
  - **403** (`Authorization_RequestDenied` — the app lacks the application permission for THIS list, or
    admin consent was never granted) → best-effort **Verbose skip**. **Graph is permission-scoped, so
    MANY 403s are EXPECTED** — an app granted only `Group.Read.All` 403s on `/applications`,
    `/identity/conditionalAccess/policies`, etc. A 403 is a permission-absent skip, **not** a systemic
    failure.
  - **404** (object/feature absent, or a licensing-gated feature like Conditional Access without Entra ID
    P1/P2) → **Verbose skip**.
  - **429** (Graph throttles aggressively — `Retry-After` header) / **5xx** / **network** → enumeration
    may be silently incomplete → **Warn + hardFails++** (tell a systemic failure apart from an empty
    tenant); honour `Retry-After` on 429 before the retry. The Bearer/secret never appears in errors/logs.
  - **Systemic guard:** the top-level `list` (each enumeration root — groups, applications, SPs, CA
    policies, named locations, admin units, role assignments) owns `hardFails`; the per-parent `subList`
    (a group's members, an SP's `appRoleAssignedTo`) does **NOT** bump `hardFails` (sub-lists multiply by
    group/SP count — one group's 403 on members must not fail the run). If nothing was found AND the roots
    failed with real (non-403/404) errors, surface a systemic failure rather than shipping an empty
    inventory (same guard as Keycloak/Vault).
- **Preflight**: `terraform` present + `ARM_TENANT_ID` + `ARM_CLIENT_ID` + `ARM_CLIENT_SECRET` set + the
  **token exchange succeeds** + a lightweight Graph probe returns 200. Use **`GET
  /v1.0/organization?$select=id,displayName`** (or `GET /v1.0/groups?$top=1`) as the probe — the token
  exchange is the first real check (a 401/`invalid_client` from `/token` means bad app credentials or
  wrong tenant); then the Graph probe confirms the Bearer actually reaches Graph and the app has *some*
  directory read permission. A **403 on the probe** means the app authenticated but was granted no
  directory-read permission (or consent is pending) — surface it as an actionable preflight failure
  ("grant + admin-consent `Directory.Read.All` (or the per-resource `*.Read.All`) to the app"). Report
  which of the per-resource permissions are missing as a **Warn**, not a hard failure (a partial-permission
  app can still adopt the resources it CAN read).
- **Connect**: run the token exchange, best-effort read `GET /v1.0/organization` for the tenant display
  name, and set the single flat container (id = `ARM_TENANT_ID`, name = the org displayName or the GUID).

## The v3 Graph-path import IDs + relationship fan-out + the secret line — the CRITICAL determination

This is Entra's analogue of Keycloak's "realm-prefixed composite depth" call fused with Vault's "never
read a secret value" line — but with a twist unique to Entra: **the import id is a Graph RESOURCE PATH,
not a bare id.** The load-bearing per-resource facts are **(a) the import-id SHAPE — a single
`/collection/<uuid>` (or a multi-segment `/identity/conditionalAccess/policies/<uuid>`), a bare opaque
id, a `<parent>/member/<child>` composite, or a `/servicePrincipals/<sp>/appRoleAssignedTo/<id>`
composite; (b) whether the resource is an ENUMERATION ROOT (a top-level Graph list) or a per-parent
FAN-OUT child (group members, SP app-role-assignments); and (c) whether the object carries SECRET
credential material** (`passwordCredentials`/`keyCredentials` on apps/SPs — enumerate the shell, never
the value). Get (a) wrong and every import block for that type is un-importable; get (b) wrong and you
never reach the relationships (or re-list SPs per assignment); get (c) wrong and you leak a client secret
into the inventory/HCL/state. All three are **verified against `docs/resources/*.md`** and pinned
per-resource. The rules:

- **Object roots → a SINGLE Graph-path import.** Each top-level object list (`/groups`, `/applications`,
  `/servicePrincipals`, `/users`, `/identity/conditionalAccess/namedLocations`,
  `/identity/conditionalAccess/policies`, `/directory/administrativeUnits`) yields one resource per
  object, imported by its Graph path:
  | resource | v3 import id (VERIFIED) |
  |---|---|
  | `azuread_group` | `/groups/<object-id>` |
  | `azuread_application_registration` / `azuread_application` | `/applications/<object-id>` |
  | `azuread_service_principal` | `/servicePrincipals/<object-id>` |
  | `azuread_user` | `/users/<object-id>` |
  | `azuread_named_location` | `/identity/conditionalAccess/namedLocations/<id>` |
  | `azuread_conditional_access_policy` | `/identity/conditionalAccess/policies/<id>` |
  | `azuread_administrative_unit` | `/directory/administrativeUnits/<object-id>` |
  The enumerator reads the bare `id` (a UUID) off each list element; `importid.go` prepends the
  per-TF-type prefix. **The prefix is NOT `graph.microsoft.com/v1.0` and NOT a full URL** — it is the
  path *after* `/v1.0`, starting at `/` (e.g. `/groups/`), matching the provider's parser exactly.
- **`azuread_directory_role_assignment` → a BARE opaque id (the oddball).** Its import is the bare
  `unifiedRoleAssignment.id` — an opaque base64-ish string like
  `ePROZI_iKE653D_d6aoLHyr-lKgHI8ZGiIdz8CLVcng-1`, **NOT** UUID-shaped and **NOT** prefixed. Copy the
  `id` field verbatim; do not prepend `/roleManagement/…`.
- **`azuread_directory_role` → NOT importable.** The resource "activates" a built-in role template and the
  provider documents "This resource does not support importing." **Exclude it from Phase A** (you cannot
  emit an import block for it). Adopt `azuread_directory_role_assignment` instead (the assignments ARE
  importable), and treat activated roles as adopt-in-place.
- **Relationship composites (the two fan-out children) → their OWN shapes, both VERIFIED:**
  - **`azuread_group_member` = `<group-object-id>/member/<member-object-id>`** — 3-segment, **NO leading
    slash**, literal separator `member`. Enumerated per group: `GET /v1.0/groups/<group-id>/members`.
    (This is a Terraform-unique composite, unchanged in v3 — do NOT add a `/groups/` prefix.)
  - **`azuread_app_role_assignment` = `/servicePrincipals/<resourceSpId>/appRoleAssignedTo/<assignmentId>`**
    — leading `/servicePrincipals/`, separator **`appRoleAssignedTo`** (NOT `appRoleAssignment`!), and the
    SP in the path is the assignment's **RESOURCE/TARGET** SP (the API you were granted a role ON), not
    the assignee. Enumerated per SP: `GET /v1.0/servicePrincipals/<sp-id>/appRoleAssignedTo`; the
    `<assignmentId>` is the `appRoleAssignment.id` (an opaque string, e.g.
    `41W1zT6z1U-kJxf62svfp1HFE8pMZhxDun-ThPczmJE`). **The task's provisional
    `<sp>/appRoleAssignment/<id>` is WRONG on two counts** (missing `/servicePrincipals/` prefix; wrong
    separator word) — pin the verified form.
- **Encode the id as an explicit per-TF-type switch in `importid.go`** (mirror Keycloak's `rawImportID` /
  Vault's six-shape switch) — never infer the prefix, the separator, the part-count, or whether the leaf
  is a UUID vs an opaque id. **Template-escape the whole id on emit** (`util.EscapeHCLTemplate` — the
  other providers' rule): display names never enter the id, but the uniform escape is the safe default and
  an opaque assignment id can contain `-`/`_`.

## Enumeration spine

Flat tenant scope (one container = the Entra tenant). The spine is a set of **object roots** plus a
**two-level fan-out** for the two relationship composites. Best-effort per list (403 permission-absent /
404 feature-absent → Verbose skip; 401 → refresh-once-then-fatal; 429/5xx/network → Warn + count). The
Bearer/secret never appears in errors/logs. (Mirror `keycloak/enumerate.go`: a top-level `list` helper
owns the systemic-failure count for the roots; a `subList` helper for the per-group / per-SP fan-out does
NOT bump the count.) **Decode ONLY the `id` (+ the parent/child ids for composites) — never a
`passwordCredentials`/`keyCredentials`** (§ Write-only — the enumeration struct simply omits them). Every
request carries `Authorization: Bearer …` and is built as `https://graph.microsoft.com/v1.0<path>`.

- **Root — groups:** `GET /v1.0/groups?$top=999&$select=id,displayName` (paged via `@odata.nextLink`) →
  `{value:[{id,displayName,…}]}` → `azuread_group` (import `/groups/<id>`). Capture each group `id` — the
  fan-out key for members.
- **Root — applications:** `GET /v1.0/applications?$top=999&$select=id,displayName,appId` (paged) →
  `azuread_application_registration` (import `/applications/<id>` — the **object id**, NOT `appId`). Skip
  Microsoft first-party/built-in apps if they cannot round-trip (VERIFY). **Never decode
  `passwordCredentials`/`keyCredentials`.**
- **Root — service principals:** `GET /v1.0/servicePrincipals?$top=999&$select=id,displayName,appId,servicePrincipalType`
  (paged) → `azuread_service_principal` (import `/servicePrincipals/<id>`). Capture each SP `id` — the
  fan-out key for app-role-assignments. **Consider filtering `servicePrincipalType`** — a tenant has
  hundreds of Microsoft-first-party SPs (`servicePrincipalType == "Application"` with a Microsoft
  `appOwnerOrganizationId`) that are noise to adopt; VERIFY whether to keep only tenant-owned SPs.
  **Never decode `passwordCredentials`/`keyCredentials`.**
- **Root — named locations:** `GET /v1.0/identity/conditionalAccess/namedLocations` (paged) →
  `azuread_named_location` (import `/identity/conditionalAccess/namedLocations/<id>`). Requires
  `Policy.Read.All`; 403 if the app lacks it, 404/empty if Conditional Access is unlicensed → skip.
- **Root — conditional access policies:** `GET /v1.0/identity/conditionalAccess/policies` (paged) →
  `azuread_conditional_access_policy` (import `/identity/conditionalAccess/policies/<id>`). `Policy.Read.All`;
  licensing-gated (Entra ID P1/P2) → skip if absent.
- **Root — administrative units:** `GET /v1.0/directory/administrativeUnits` (paged) →
  `azuread_administrative_unit` (import `/directory/administrativeUnits/<id>`). `AdministrativeUnit.Read.All`.
- **Root — directory role assignments:** `GET /v1.0/roleManagement/directory/roleAssignments` (paged) →
  `{value:[{id,principalId,roleDefinitionId,directoryScopeId}]}` → `azuread_directory_role_assignment`
  (import = the **bare** `id`). `RoleManagement.Read.Directory` (or `Directory.Read.All`). (Skip
  `azuread_directory_role` — not importable; the assignments carry the `roleDefinitionId` reference.)
- **Root (DEFER / opt-in) — users:** `GET /v1.0/users?$top=999&$select=id,userPrincipalName,displayName`
  (paged) → `azuread_user` (import `/users/<id>`). `User.Read.All`. **PII + potentially very large N** —
  off by default; opt-in with a note (§ Deferrals). `$select` to avoid pulling full user profiles.
- **Fan-out — per group `<group-id>`, members:** `GET /v1.0/groups/<group-id>/members?$select=id` (paged,
  `subList`) → `{value:[{id, "@odata.type":"#microsoft.graph.user|group|servicePrincipal|…"}]}` →
  `azuread_group_member` (import `<group-id>/member/<member-id>`). **Members can be users/groups/SPs/
  devices/orgContacts** — the `azuread_group_member` resource takes any directory-object member; the
  import uses the bare member object `id`. Large groups (dynamic membership, "All users") are a big N →
  the `subList` cost is why this is a fan-out, and why it does not bump `hardFails`. (VERIFY whether to
  cap/skip very large groups.)
- **Fan-out — per service principal `<sp-id>`, app-role-assignments:** `GET
  /v1.0/servicePrincipals/<sp-id>/appRoleAssignedTo` (paged, `subList`) →
  `{value:[{id,appRoleId,principalId,principalType,resourceId}]}` → `azuread_app_role_assignment` (import
  `/servicePrincipals/<sp-id>/appRoleAssignedTo/<assignment-id>` — the `<sp-id>` is the RESOURCE SP being
  iterated). Note the Graph doc's replication-delay caveat (eventual consistency) — a just-created
  assignment may lag; tolerate.

If nothing was found AND the roots failed with real (non-403/404) errors, surface a systemic failure
rather than shipping an empty inventory (same guard as Keycloak/Vault).

## Resource catalog

Import IDs verified against the current `hashicorp/azuread` registry docs (`docs/resources/*.md`, v3) and
cross-checked against the Microsoft Graph v1.0 reference. All scope = tenant. "list endpoint → shape" is
the OData `{value,@odata.nextLink}` list. "root / fan-out" names the enumeration root or the parent
fan-out. The **import ID** column is the #1 hazard — **the v3 Graph-path prefix, the separator word, and
UUID vs opaque leaf.**

| native key | TF type | list endpoint → shape | root / fan-out | import ID (v3) | shape |
|---|---|---|---|---|---|
| azuread:group | azuread_group | `GET /v1.0/groups` → `{value}` | ROOT | `/groups/<object-id>` | **prefixed-single, UUID leaf** |
| azuread:application | azuread_application_registration | `GET /v1.0/applications` → `{value}` | ROOT | `/applications/<object-id>` | **prefixed-single, UUID leaf** (**secrets: never decode passwordCredentials/keyCredentials**) |
| azuread:service_principal | azuread_service_principal | `GET /v1.0/servicePrincipals` → `{value}` | ROOT | `/servicePrincipals/<object-id>` | **prefixed-single, UUID leaf** (**secrets: passwordCredentials/keyCredentials**) |
| azuread:named_location | azuread_named_location | `GET /v1.0/identity/conditionalAccess/namedLocations` | ROOT | `/identity/conditionalAccess/namedLocations/<id>` | **prefixed-single (multi-segment), UUID leaf** |
| azuread:conditional_access_policy | azuread_conditional_access_policy | `GET /v1.0/identity/conditionalAccess/policies` | ROOT | `/identity/conditionalAccess/policies/<id>` | **prefixed-single (multi-segment), UUID leaf** |
| azuread:administrative_unit | azuread_administrative_unit | `GET /v1.0/directory/administrativeUnits` | ROOT | `/directory/administrativeUnits/<object-id>` | **prefixed-single (multi-segment), UUID leaf** |
| azuread:directory_role_assignment | azuread_directory_role_assignment | `GET /v1.0/roleManagement/directory/roleAssignments` | ROOT | `<assignment-id>` (bare opaque) | **BARE opaque id (no prefix)** |
| azuread:group_member | azuread_group_member | `GET /v1.0/groups/<gid>/members` | ← group | `<group-id>/member/<member-id>` | **composite, NO leading slash, `member` sep** |
| azuread:app_role_assignment | azuread_app_role_assignment | `GET /v1.0/servicePrincipals/<sid>/appRoleAssignedTo` | ← service principal | `/servicePrincipals/<resourceSpId>/appRoleAssignedTo/<assignmentId>` | **composite, `/servicePrincipals/` prefix, `appRoleAssignedTo` sep, opaque leaf** |
| azuread:user (DEFER) | azuread_user | `GET /v1.0/users` → `{value}` | ROOT (opt-in) | `/users/<object-id>` | **prefixed-single, UUID leaf** (**PII + scale → defer**) |

**`azuread_directory_role` is NOT in the catalog** — the resource "activates" a built-in role template and
**does not support import** (verified in `docs/resources/directory_role.md`); Phase A adopts the
importable `azuread_directory_role_assignment` instead and treats activated roles as adopt-in-place.

### Import-format quirks (§ do not get wrong)

1. **v3 prefixes the object id with its Graph collection path — this is the provider's defining hazard.**
   The wire `id` is a bare UUID; the import id is `/<collection>/<uuid>` (or a multi-segment
   `/identity/conditionalAccess/policies/<uuid>`). Encode the prefix per TF type in `importid.go`; never
   infer it, and **branch on the provider major** — a v2.x pin wants the bare UUID, a v3.x pin wants the
   prefixed path (§ Version pin).
2. **`azuread_app_role_assignment` — the separator is `appRoleAssignedTo`, NOT `appRoleAssignment`, and
   the id is `/servicePrincipals/<resourceSpId>/appRoleAssignedTo/<assignmentId>`.** The full
   `/servicePrincipals/` prefix IS part of the import id, and the SP is the assignment's RESOURCE/target
   SP. The provisional `<sp>/appRoleAssignment/<id>` is wrong on both the prefix and the separator word —
   pin the verified form.
3. **`azuread_group_member` has NO leading slash — `<group-id>/member/<member-id>`.** Unlike every object
   root (which gained a `/collection/` prefix in v3), this Terraform-unique composite stays un-prefixed.
   Do NOT "normalize" it by adding `/groups/`.
4. **`azuread_directory_role_assignment` is a BARE opaque id (no prefix, not UUID-shaped).** Copy the
   `unifiedRoleAssignment.id` verbatim. It is the lone bare-id resource among the roots.
5. **`azuread_application` uses the OBJECT id (`id`), not the CLIENT id (`appId`).** `/applications/<id>`
   where `<id>` is the directory object UUID — an application also has an `appId` (the client id) which is
   a DIFFERENT UUID; the import uses `id`. Same object-id-not-appId rule for `azuread_service_principal`.
   Copy the `id` field, not `appId` (the client-id trap — the Keycloak `id`-not-`clientId` precedent).
6. **All ids are opaque strings off the wire — no numeric stringify** (unlike Datadog/Azure DevOps INTs).
   UUIDs and opaque role/app-role-assignment ids copy verbatim; `importid.go` only prepends the
   path prefix and joins the composite separators. Template-escape the whole id on emit.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a live tenant —
prune computed via `hcl.WalkResourceBlocks`; scrub/exclude credential fields. **The recurring hazards are
the application/SP `passwordCredentials`/`keyCredentials` (never read at enumeration; generate-config-out
would try to author empty credential blocks), the conditional-access-policy sprawl, and list-ordering
churn on the many string-array fields.**

- **`azuread_application_registration` — light shell (the reason to prefer it over `azuread_application`).**
  Core: `display_name`, `sign_in_audience`, `description`, `notes`. The decomposed sub-resources
  (`azuread_application_api_access`, `_app_role`, `_permission_scope`, `_redirect_uris`, `_owner`,
  `_password`, `_certificate`, `_federated_identity_credential`) are SEPARATE resources → deferred. **No
  secret on the registration shell itself** — the credentials live in `_password`/`_certificate` (deferred,
  § Write-only). Prune computed (`object_id`, `client_id`, `publisher_domain`).
- **`azuread_application` (if used instead) — HEAVY + SECRET.** The kitchen-sink resource: `display_name`,
  `api{oauth2_permission_scope…}`, `app_role`, `required_resource_access` (the API-permission graph),
  `optional_claims`, `web`/`spa`/`public_client` redirect blocks — AND inline `password`/`certificate`
  credential blocks. **`password`/`certificate` values are write-only/secret → scrub** (the Graph list
  returns credential metadata but never the secret text). This is why the SHELL
  (`azuread_application_registration`) is the Phase-A target. `required_resource_access` ordering churns →
  sort.
- **`azuread_service_principal` — light shell + SECRET.** Core: `client_id` (references the app), `use_existing`,
  `app_role_assignment_required`, `feature_tags`/`tags`, `login_url`, `notification_email_addresses`. **A
  service principal also carries `passwordCredentials`/`keyCredentials` (SAML signing certs, secrets) →
  never decode; the SP `_password`/`_certificate` are separate deferred resources.** Prune computed
  (`object_id`, `application_tenant_id`, `oauth2_permission_scope_ids`).
- **`azuread_conditional_access_policy` — HEAVY (the Entra `datadog_dashboard`/`keycloak_realm` analogue).**
  A deep nested `conditions{users,applications,platforms,locations,client_app_types,…}` +
  `grant_controls{built_in_controls,operator,…}` + `session_controls{…}` object (see the verified response
  sample — dozens of nested arrays). Defaults over-emit heavily; prune computed (`id`); the many
  `include*/exclude*` string arrays (`includeUsers`,`excludeGroups`,`includeRoles`,…) come back in server
  order → **sort for reproducible diffs**. No secret, but the biggest curation surface.
- **`azuread_named_location` — medium.** Two shapes discriminated by `@odata.type`
  (`#microsoft.graph.ipNamedLocation` → `ip{ip_ranges,trusted}`;
  `#microsoft.graph.countryNamedLocation` → `country{countries_and_regions,…}`). The TF resource has
  `ip{…}` and `country{…}` sub-blocks — emit the one matching the object type. `ip_ranges` ordering churns
  → sort. No secret. (VERIFY whether the discriminator drives one TF type with two blocks, or is a
  clean single type — the registry shows a single `azuread_named_location` with optional `ip`/`country`.)
- **`azuread_administrative_unit` — light.** `display_name`, `description`, `visibility`,
  `membership_type`/`membership_rule` (dynamic AUs). Members are a SEPARATE resource
  (`azuread_administrative_unit_member`, deferred). No secret. Prune computed (`object_id`).
- **`azuread_directory_role_assignment` — trivial.** `role_id` (the roleDefinition), `principal_object_id`,
  `directory_scope_id`/`app_scope_id`. References resolve to other objects' ids. No secret. The bare-id
  import (§ quirk 4).
- **`azuread_group` — light.** `display_name`, `mail_enabled`, `security_enabled`, `types`
  (`Unified`/`DynamicMembership`), `description`, `visibility`, `mail_nickname`. **Members/owners are
  SEPARATE resources** (`azuread_group_member` is Phase A; owners deferred). Dynamic groups have
  `dynamic_membership{rule}`. Prune computed (`object_id`, `mail`, `proxy_addresses`, `onpremises_*`). No
  secret. (VERIFY: importing a group whose membership is *also* managed by `azuread_group_member` can
  produce a members-diff — like the Azure DevOps `initialization` trap; may need `ignore_changes` on group
  membership, or model members ONLY via `azuread_group_member`.)
- **`azuread_group_member` / `azuread_app_role_assignment` — trivial relationship rows.** `group_object_id`
  + `member_object_id`; `app_role_id` + `principal_object_id` + `resource_object_id`. No secret. Large N;
  the composites (§ quirks 2–3) are the whole hazard.
- **`azuread_user` (if opted-in) — PII-heavy.** `user_principal_name`, `display_name`, `mail_nickname`,
  `account_enabled`, plus a large profile surface (job/contact/address). `password` is write-only/required
  on create → generate-config-out cannot author it (masked) → not plan-clean; and the profile is PII.
  Adopt the shell only with the scrub, or defer entirely (§ Deferrals).

Until Phase B these are no-ops, so an Entra export is a breadth scaffold, not yet plan-clean (the
pipeline's repo-wide secret scan is the backstop for any `password`/`key_credential`/`client_secret`
material generate-config-out might emit before the scrub rules land — though enumeration NEVER reads a
credential value in the first place, which is the paramount backstop).

## Write-only / secret resources (EXCLUDE / scrub)

The credential/integration plane is where Entra's secrets live — never decode the value; adopt the object
as a shell (re-supply credentials out-of-band), exactly like Keycloak's `client_secret` / Vault's
`bind_credential` non-decode:

- **`azuread_application` / `azuread_application_registration` — `passwordCredentials` (client secrets) +
  `keyCredentials` (certificates)** — the app's credential material. **Never decode into a list element;
  adopt the app as a SHELL.** The dedicated credential resources — **`azuread_application_password`**
  (client secret; `value` is write-only, returned only at creation) and
  **`azuread_application_certificate`** — are **DEFERRED** (a Phase-B scrub increment): their reason to
  exist is a secret that Graph never returns on read, so a scaffold nulls them.
- **`azuread_service_principal` — `passwordCredentials` + `keyCredentials`** (SP secrets, SAML signing
  certs) — same rule; adopt the SP shell, defer **`azuread_service_principal_password`** /
  **`azuread_service_principal_certificate`** (and the SAML `azuread_service_principal_token_signing_certificate`).
- **`azuread_synchronization_secret` / `azuread_application_federated_identity_credential`** — provisioning
  secrets and federated-credential material → deferred with the secret plane.
- **`azuread_user.password`** — write-only, required on create, never returned on read → if `azuread_user`
  is opted-in, scrub/omit the password (adopt the profile shell only).
- **The provider credential itself** — `ARM_CLIENT_SECRET` (and the minted `access_token`) is the
  tenant-wide app credential; it lives **only** in the `/token` form body / the `Authorization` header,
  never in generated config, state, errors, or logs. There is no round-trippable "app credential" resource
  to adopt. **`providers.tf` stays keyless** (Terraformer already doesn't inline it — keep that posture;
  if a future tool version tries to, refuse it — the GitLab/Vault leak precedent).
- **Not secret, do not over-scrub:** group/app/SP/user `display_name`, group `mail_enabled`/`security_enabled`/
  `types`, application `sign_in_audience`/`required_resource_access` (the permission GRAPH is config, not a
  secret), SP `tags`/`login_url`, conditional-access-policy conditions/controls (policy config, not a
  credential), named-location IP ranges/countries, administrative-unit membership rules, role-assignment
  role/principal/scope ids, group/app-role membership object ids. These are directory *structure/config* —
  adopt them.

## Deliberately out of scope

- **Application/SP CREDENTIAL resources** (`azuread_application_password`, `azuread_application_certificate`,
  `azuread_service_principal_password`, `azuread_service_principal_certificate`,
  `azuread_service_principal_token_signing_certificate`,
  `azuread_application_federated_identity_credential`, `azuread_synchronization_secret`) — each exists to
  hold a secret Graph never returns on read → a scaffold nulls them; a dedicated Phase-B scrub increment,
  not the beachhead.
- **The decomposed `azuread_application_*` sub-resources** (`_api_access`, `_app_role`, `_permission_scope`,
  `_pre_authorized`, `_redirect_uris`, `_owner`, `_optional_claims`, `_known_clients`,
  `_from_template`) — the pieces `azuread_application_registration` splits out; a later increment after the
  app shell is solid (each has its own `<app-object-id>/…` composite — VERIFY per type).
- **`azuread_directory_role`** — not importable (activates a built-in template); Phase A adopts the
  importable `azuread_directory_role_assignment` and treats activated roles as adopt-in-place.
- **The wider RBAC/relationship plane** (`azuread_group_owner`, `azuread_administrative_unit_member`,
  `azuread_administrative_unit_role_member`, `azuread_custom_directory_role`,
  `azuread_directory_role_eligibility_schedule_request` / PIM, `azuread_group_role_management_policy`) —
  the who-owns/who-is-eligible-for-what plane (N×M composites, several PIM-gated). Phase A adopts the two
  core relationships (`azuread_group_member`, `azuread_app_role_assignment`) and defers the rest
  (`Capabilities.IAM=false`).
- **User plane at scale** (`azuread_user`, `azuread_users` data) — PII + potentially tens of thousands of
  objects in a large tenant; the `/users` list is the reason the `@odata.nextLink` pager must be robust,
  but adopting a tenant's user directory as IaC is rarely wanted. Off by default; opt-in with a scrub note.
- **Access-governance / entitlement-management plane** (`azuread_access_package*`,
  `azuread_conditional_access_*` templates beyond policies, `azuread_authentication_strength_policy`,
  `azuread_claims_mapping_policy`, `azuread_named_location` sub-variants, `azuread_custom_security_attribute*`)
  — later increments after the CA-policy shell is solid.
- **Provisioning / synchronization** (`azuread_synchronization_job`, `_secret`, B2B/B2C flows,
  `azuread_invitation`) — the sync/guest-onboarding plane; secret-bearing, deferred.
- **Directory settings & tenant config** (`azuread_directory_role_definition` custom roles,
  `azuread_named_location` country/IP is IN scope, but `azuread_authentication_method_policy`,
  password-protection, tenant branding) — tenant-wide singletons, better authored by hand; later
  increments.
- **Sovereign / national clouds** (US Gov, China 21Vianet) — different login+graph hosts; Phase A targets
  the public cloud, defers the `ARM_ENVIRONMENT` host table (VERIFY).
- **`/beta` Graph surfaces** — the Phase-A set is all `/v1.0`; any resource that requires `/beta` is
  deferred and pinned explicitly when adopted (never silently switch base).
- **Data planes** — sign-in/audit logs, directory audit, group/user *activity*, token issuance. DATA
  behind the config. Out of scope (config only).
- **The Graph/Azure SDKs + the `az` CLI** — Terraformer pulls `ARM_*` env but the provider ecosystem
  ships heavy SDKs; TerraLift uses a raw `net/http` client (smaller, matches Keycloak/Okta/Vault). A
  deliberate non-adoption.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)

The **recommended Phase-A CONFIG CORE** (~9 TF types across the object roots + two relationship fan-outs):
`azuread_group`, `azuread_application_registration`, `azuread_service_principal`, `azuread_named_location`,
`azuread_conditional_access_policy`, `azuread_administrative_unit`, `azuread_directory_role_assignment`,
`azuread_group_member`, `azuread_app_role_assignment`. **Defer** `azuread_user` (PII/scale — opt-in), all
credential resources (`azuread_application_password`/`_certificate`, SP secrets — a scrub increment),
`azuread_directory_role` (not importable), and the decomposed `azuread_application_*` / owner / PIM planes.

BEACHHEAD `azuread_group` + `azuread_application_registration` + `azuread_service_principal` (the
group/app/SP core essentially every Entra tenant manages as IaC — `azuread_group` establishes the
**`GET /v1.0/groups` root** and the **`/groups/<id>` v3 prefixed import**,
`azuread_application_registration` establishes the **`/applications/<id>` import** with the
**object-id-not-appId** subtlety and the **`passwordCredentials`/`keyCredentials` never-decode** shell
discipline, and `azuread_service_principal` establishes the **`/servicePrincipals/<id>` import** and is
the **fan-out key** for app-role-assignments — and this trio exercises the **OAuth2 client-credentials
FORM token exchange** against `login.microsoftonline.com`, the **`.default` scope**, the **OData
`{value,@odata.nextLink}` envelope decode**, the **host-validated absolute-`@odata.nextLink` pager**, the
**Bearer refresh-on-401**, the **`GET /v1.0/organization` preflight**, and the **permission-scoped-403 =
skip** taxonomy without touching a single credential value) → INC-1 `azuread_group_member` (the **per-group
members fan-out** + the **`<group>/member/<member>` no-leading-slash composite** + the `list`-vs-`subList`
systemic-count split) + `azuread_app_role_assignment` (the **per-SP `appRoleAssignedTo` fan-out** + the
**`/servicePrincipals/<sp>/appRoleAssignedTo/<id>` composite** — the provider's defining separator hazard)
→ INC-2 `azuread_conditional_access_policy` + `azuread_named_location` (the **Conditional Access plane** on
`/identity/conditionalAccess/*`, `Policy.Read.All`, the heavy CA-policy curation surface + the
`ip`/`country` named-location discriminator) → INC-3 `azuread_administrative_unit` +
`azuread_directory_role_assignment` (the **directory/RBAC plane** — `/directory/administrativeUnits` and
`/roleManagement/directory/roleAssignments`, the **bare-opaque-id** import oddball) → INC-4 (opt-in)
`azuread_user` (the **`/users` PII/scale** root, off by default, password-scrubbed) → INC-5 the CREDENTIAL
scrub increment (`azuread_application_password`/`_certificate`, SP secrets — per-type scrub + re-supply) →
LATER the decomposed `azuread_application_*` plane, the owner/PIM/entitlement RBAC planes, provisioning/
sync, sovereign clouds, `/beta` surfaces, and the data planes. **NEVER: inline `ARM_CLIENT_SECRET` into
`providers.tf` (Terraformer doesn't — keep it keyless); and NEVER decode a `passwordCredentials` /
`keyCredentials` / `password` into a list element.**
