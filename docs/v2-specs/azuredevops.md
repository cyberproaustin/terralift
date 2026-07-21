# Azure DevOps provider — build spec

Research artifact for the `azuredevops` provider (Phase A scaffold; TF provider source is
**`microsoft/azuredevops`** — the official Terraform provider for **Azure DevOps Services**, Microsoft's
hosted DevOps/SCM/CI platform at `dev.azure.com/<org>`). Sources: Terraformer's `providers/azuredevops/`
(six files — `azuredevops_provider.go` + `azuredevops_service.go` + `project.go` + `git_repository.go` +
`group.go` + `helpers.go`, built on the `microsoft/azure-devops-go-api` SDK), the `microsoft/azuredevops`
registry docs (import formats + schema, **verified per-resource below** against the provider repo's
`website/docs/r/*.html.markdown`), and the Azure DevOps REST API
(`https://dev.azure.com/<org>/_apis/…`, plus the separate identity host
`https://vssps.dev.azure.com/<org>/_apis/graph/…`). Build mirrors **two** prior providers at once —
**GitLab** (`internal/providers/gitlab/`) for the **org → projects FAN-OUT spine** (Azure DevOps fans
out `projects → children` exactly as GitLab fans out `projects → children`, with the same `list`/`subList`
split — the top-level `list` owns the systemic-failure count, the per-project `subList` does not) and its
**redirect-refusing custom-auth client**, and **Vault** (`internal/providers/vault/`) for the
**secret-data-is-off-limits discipline** (Vault's "never read a secret value" → Azure DevOps's "never decode
a **service-connection authorization** or a **variable-group secret value**"). This provider introduces
**four genuinely NEW mechanics not seen in the prior REST providers**, all called out below: **(1)** the VSTS
**`{"count":N,"value":[…]}` envelope** decode (a generic wrapper, not a bare array like GitLab and not a
map like Vault); **(2)** the **`?api-version=` query param that MUST ride on EVERY request** (a missing
api-version silently changes or breaks the response — a per-request append helper); **(3)** the
**continuation-token pager** driven by the **`x-ms-continuationtoken` RESPONSE header** + a `continuationToken`
query param (not GitLab's `X-Next-Page` offset, not Vault's no-pagination); and **(4)** the **203/HTML
bad-PAT gotcha** — Azure DevOps answers a bad/under-scoped PAT with **`203 Non-Authoritative Information`
and an HTML sign-in page** instead of a clean `401` (a well-known trap that must be detected explicitly).

**Azure DevOps's DEFINING HAZARD is the import ID — it MIXES bare identifiers (a bare project GUID, a bare
integer agent-pool id, a bare opaque group *descriptor*) with `<project>/<child>` slash-composites whose
leaf is sometimes a UUID (repo, team, service-endpoint) and sometimes an INT (build definition, variable
group, environment, agent queue, policy). Every separator (`/`), part-count, and whether the leaf is a
UUID vs an int vs a descriptor MUST be verified per-TF-type and encoded explicitly in `importid.go` — never
inferred.** Six facts set Azure DevOps apart, all load-bearing and called out below:

1. **Auth is a Personal Access Token sent via HTTP Basic with an EMPTY username** — the header is
   `Authorization: Basic base64(":"+PAT)` (username blank, PAT as the password), from
   **`AZDO_PERSONAL_ACCESS_TOKEN`**. Confirmed by the MS REST docs' own sample
   (`string.Format("{0}:{1}", "", personalaccesstoken)`, curl `-u :{pat}`). The PAT NEVER appears in the
   URL, query, body, errors, logs, config, or state, and is **never inlined into `providers.tf`**.
   (Terraformer already does the right thing here — see § Version pin — but the discipline still holds.)
2. **Base URL is the org service URL** — **`AZDO_ORG_SERVICE_URL`** (e.g. `https://dev.azure.com/<org>`),
   **https**, and the **graph/identity APIs live on a SEPARATE host** `https://vssps.dev.azure.com/<org>`
   (groups/users/memberships). Two hosts, one org, one PAT.
3. **`?api-version=` is MANDATORY on every request** (e.g. `7.1`; some areas are still preview and need
   `7.1-preview.N`). A request without it does not fail cleanly — it can return an older shape or an error.
   A helper appends `?api-version=7.1` / `&api-version=7.1` to every route (§ Shape).
4. **The response family is the VSTS `{"count":N,"value":[…]}` ENVELOPE** — a generic wrapper object, not a
   bare array (GitLab) and not a map-keyed object (Vault). Decode `.value` into `[]T`; ignore `.count`.
5. **Pagination is a CONTINUATION TOKEN** — the response carries an **`x-ms-continuationtoken` header** when
   more data exists; re-issue the same list with `&continuationToken=<tok>` (and `$top` to bound page size).
   **The PRESENCE of the header is the only reliable "more data" signal** — loop until it is absent. A few
   collections (git repositories, agent pools) return everything in one page (no header) — the pager must
   treat an absent header as "done."
6. **The 203/HTML bad-PAT gotcha** — a bad, expired, or under-scoped PAT yields **`203 Non-Authoritative
   Information` with a `text/html` sign-in page**, NOT a `401`. Detect it (status 203 OR
   `Content-Type: text/html` on an `_apis` route) and treat it as an **auth failure** (§ Status handling).

## Version pin (load-bearing)

Pin `microsoft/azuredevops ~> 1.x` (current provider is **1.x**; **VERIFY the current major at build** — the
provider matured through 0.x and reached 1.0). Naming/behaviour facts that matter (the Terraformer-vs-current
divergences):

- **Terraformer does NOT inline the PAT** (the welcome divergence). `azuredevops_provider.go` reads
  `os.Getenv("AZDO_ORG_SERVICE_URL")` / `os.Getenv("AZDO_PERSONAL_ACCESS_TOKEN")` into **private struct
  fields**, and **`GetProviderData()` returns an empty map** — credentials are NOT written into the generated
  HCL. TerraLift keeps this posture: the emitted `providers.tf` authenticates via `AZDO_ORG_SERVICE_URL` /
  `AZDO_PERSONAL_ACCESS_TOKEN` env only (keyless). **If a future Terraformer/tool version inlines the PAT,
  refuse it** (the GitLab/Vault leak precedent — assert, don't assume).
- **Terraformer's resource set is NARROW — only THREE types across three generators.** `project.go` lists
  projects (`client.GetProjects`, continuation-token paged) → `azuredevops_project` (state id = **bare
  project UUID**). `git_repository.go` lists repos (`client.GetRepositories`) → `azuredevops_git_repository`
  (state id = **bare repo UUID**). `group.go` lists graph groups (`client.ListGroups`, continuation-token
  paged) → `azuredevops_group` (id = the group **descriptor**). It does **NOT** cover build definitions,
  variable groups, service endpoints, agent pools/queues, teams, environments, or policies — those are
  covered here from the API + registry. **Do NOT pull the `azure-devops-go-api` SDK** — a raw `net/http`
  client is smaller and matches GitLab/Vault (a deliberate non-adoption).
- **State id vs import-command id diverge for the composites.** Terraformer records the repo/project **state
  id** as the bare UUID (`id.String()`), but TerraLift emits **`import` blocks** whose `id` must be the
  string `terraform import` PARSES — and for `azuredevops_git_repository` that is **`<project>/<repo>`**, not
  a bare UUID. So we pin the slash-composite the CLI accepts, not Terraformer's bare state id (§ CRITICAL).
- Terraformer reads `AZDO_ORG_SERVICE_URL` + `AZDO_PERSONAL_ACCESS_TOKEN`. The **TF provider** reads the same
  two (`org_service_url` / `personal_access_token` args, or those envs). The REST endpoints below are
  provider-version-independent (they are the same `_apis/…` routes the SDK wraps).

## Shape

- **Auth — a PAT via HTTP Basic with an EMPTY username (NEW auth shape).** Every request carries
  **`Authorization: Basic base64(":"+PAT)`** — username blank, the PAT as the password. Confirmed by the MS
  REST "get started" doc, whose C# sample builds the header from `string.Format("{0}:{1}", "",
  personalaccesstoken)` and whose curl form is `-u :{personalaccesstoken}`. Plus `Accept: application/json`.
  Read the PAT from **`AZDO_PERSONAL_ACCESS_TOKEN`**. NOT a query param, NOT the body. The PAT rides **only**
  on the Authorization header — never in the URL, query, request body, errors, logs, config, or state
  (redact any URL/query that could appear in a message — mirror `gitlabapi.go`'s `redactURL`, which strips
  `?…`; note that api-version and continuationToken live in the query, so redaction still applies). A direct
  `net/http` client; **no `az`/`az devops` CLI, no `azure-devops-go-api` SDK**. Use a **redirect-refusing**
  client (mirror `glHTTPClient` / Vault's `mkHTTPClient` — Go does NOT strip the Authorization header on a
  cross-host 3xx, and Azure DevOps CAN 302 an unauthenticated/HTML request toward a sign-in host, so an
  auto-followed redirect would replay the PAT off-org; refuse and require the org host).
- **Base URL — the org service URL, https, plus a SECOND host for graph/identity.** Require
  **`AZDO_ORG_SERVICE_URL`** (e.g. `https://dev.azure.com/<org>`); strip any trailing slash. Almost every
  route is `<AZDO_ORG_SERVICE_URL>/_apis/…` (org-level) or `<AZDO_ORG_SERVICE_URL>/<project>/_apis/…`
  (project-level). **The graph/identity APIs (groups, users, memberships) live on a DIFFERENT host** —
  `https://vssps.dev.azure.com/<org>/_apis/graph/…` — derived from the org URL by swapping the host to
  `vssps.dev.azure.com` (VERIFY the swap for on-prem/legacy `*.visualstudio.com` org URLs, where the graph
  host differs — e.g. `https://<org>.vssps.visualstudio.com`; Phase A can hard-require the `dev.azure.com`
  form and Warn on legacy). **Force https** (the PAT is a secret — upgrade a bare host / explicit `http://`,
  mirror `forceHTTPS`); Azure DevOps Services is https-only (no localhost dev-instance carve-out like
  GitLab/Vault, though Azure DevOps *Server* on-prem could be http — VERIFY, Warn if so). Guard the host
  charset (reject `@`/userinfo-splice, the `validDomain` guard). We never follow a server-supplied next-URL
  (the pager builds URLs from `base+path+?api-version=…&continuationToken=…`).
- **Scope — one Azure DevOps ORGANIZATION = one flat container; PROJECTS are a FAN-OUT KEY, not a
  hierarchy.** The PAT authenticates against the org and sees exactly what its owner can access; there is no
  sub-org resolution — the PAT simply **is** the org scope. `model.ScopeTenant`. **Projects are a fan-out
  key, not a container tree** — like GitLab groups/projects, they live *under* the one org container, so
  `Capabilities.Hierarchy` stays **false**. Resolve the container id/name **best-effort** from the
  `AZDO_ORG_SERVICE_URL` host (the last path segment is the org name; there is no cheap "org object" read
  beyond `GET /_apis/projects` proving connectivity). `Capabilities{IAM:false, Exposure:false,
  Hierarchy:false}`.
- **Response family — the VSTS `{"count":N,"value":[…]}` ENVELOPE (NEW; the key structural fact).** Almost
  every list endpoint returns `{"count":<n>,"value":[…]}`. Decode a generic wrapper
  `struct{ Count int `json:"count"`; Value json.RawMessage `json:"value"` }`, then unmarshal `Value` into
  `[]T`; **`count` is advisory — never treat it as the total across pages** (it is the count in THIS page).
  A single-object GET (e.g. `GET /_apis/git/repositories/<id>`) returns the bare object (no envelope) — a
  separate `azdoGet` helper. **The `descriptor`, `id`, `name`, and (for the fan-out) `project.id` fields are
  all that Phase A decodes — never a secret field** (§ Write-only, the Vault precedent).
- **api-version — MANDATORY, appended to EVERY request (NEW mechanic).** Every route needs
  `?api-version=<v>` (or `&api-version=<v>` when the path already has a query). Pin **`7.1`** as the default
  (GA on Azure DevOps Services); a handful of areas are still **preview** and require a `-preview.N` suffix
  (VERIFY per area at build): **graph** (`7.1-preview.1`), **service endpoints**
  (`serviceendpoint/endpoints`, `7.1-preview.4`), **service hooks**, and some **teams** routes
  (`7.1-preview.3`). Implement a single `withAPIVersion(path, ver)` helper that picks the right separator
  and lets the caller override the version per-area; **redactURL still strips the query before any log**
  (the continuationToken must not surface, and the version is noise). A missing api-version is a silent
  correctness bug, not a clean error — never omit it.
- **Pagination — CONTINUATION TOKEN via the `x-ms-continuationtoken` RESPONSE header (NEW pager).** When a
  collection exceeds one page, the response sets **`x-ms-continuationtoken: <opaque>`** (Go canonicalizes to
  `resp.Header.Get("X-Ms-Continuationtoken")`); re-issue the SAME list URL with
  `&continuationToken=<opaque>` appended, accumulate `.value`, and **loop while the header is present**
  (absent ⇒ last page). Add `&$top=<N>` (e.g. 200) to bound page size where supported. **The header's
  presence is the only reliable signal** — do NOT rely on `count`, and do NOT assume a fixed page size.
  Implement one generic **continuation-token pager** (`azdoList[T]`, reads `X-Ms-Continuationtoken`; the
  whole list surface) plus a **single-object GET** helper. Some lists never paginate (git repositories,
  agent pools return all in one call, no header) — the pager handles that as a single iteration. A few older
  routes use `$top`/`$skip` offset instead of a token (VERIFY per area — e.g. teams); the pager can carry a
  `$skip` fallback if a beachhead list forces it. Bound the loop defensively (`azdoMaxPages`).
- **Status handling (mirror `gitlab/enumerate.go`'s `list`/`subList`; carry the status on the error).** Azure
  DevOps errors are the VSTS envelope **`{"$id":…,"message":…,"typeName":…,"typeKey":…,"errorCode":…,
  "eventId":…}`** for a clean 4xx/5xx — parse **`message`** (fall back to `typeKey`), **never echo the
  request** (the PAT is nowhere near the body, but the URL/query might be). Rules:
  - **203 / HTML sign-in page (THE gotcha) → treat as AUTH FAILURE.** A bad/expired/under-scoped PAT does
    NOT return 401 — it returns **`203 Non-Authoritative Information`** and/or a **`text/html`** sign-in
    body on an `_apis` route. Detect: `status == 203` **OR** (`Content-Type` starts with `text/html` on a
    JSON API route). In **preflight** → fatal; **mid-enumeration** → fatal (every remaining list will fail;
    a PAT does not auto-refresh). Do NOT try to JSON-decode the HTML.
  - **401 / 403** (missing/insufficient PAT scope — the token's owner can *see* but not read that area, or
    the area is unlicensed) → in preflight, 401 is fatal; a **403 on a specific project sub-list** →
    best-effort **Verbose SKIP** (do not fail the run). (Azure DevOps also 401s a genuinely-bad PAT on some
    routes even as it 203s on others — treat both as auth failures at the backbone.)
  - **404** (project/feature/area absent, OR a sub-resource collection the PAT cannot reach) → **Verbose
    skip**.
  - **429** (Azure DevOps rate-limits via TSTUs — `Retry-After`/`X-RateLimit-*` headers) / **5xx** /
    **network** → enumeration may be silently incomplete → **Warn + hardFails++** (tell a systemic failure
    apart from an empty org). The PAT never appears in errors/logs.
  - **Systemic guard:** the top-level `list` (the projects root, the org-level pools/groups roots) owns
    `hardFails`; the per-project `subList` (repos/definitions/variable-groups/… per project) does **NOT**
    bump `hardFails` (sub-lists multiply by project count — a single project's 403 on service endpoints must
    not fail the run). If nothing was found AND the roots failed with real (non-403/404) errors, surface a
    systemic failure rather than shipping an empty inventory (same guard as GitLab/Vault).
- **Preflight**: `terraform` present + `AZDO_ORG_SERVICE_URL` valid + `AZDO_PERSONAL_ACCESS_TOKEN` set + a
  lightweight auth probe succeeds. Use **`GET /_apis/projects?api-version=7.1&$top=1`** as the auth probe — a
  valid PAT with the minimal (Project & Team: Read) scope can list projects, so a **203/HTML or 401 there
  means a genuinely bad/expired/under-scoped PAT**. (A 200 with an empty `value` is a valid-but-empty org, not
  a failure.) A 403 on a *specific* later area in preflight is a Warn, not a failure (the PAT may still reach
  most areas).
- **Connect**: run the `GET /_apis/projects` probe to validate the PAT, best-effort read the org name from
  the `AZDO_ORG_SERVICE_URL` host/path (last segment), and set the single flat container (id/name = the org).

## org → projects FAN-OUT + heterogeneous import IDs + the secret line — the CRITICAL determination

This is Azure DevOps's analogue of GitLab's "project fan-out + composite import depth" call, fused with
Vault's "never read a secret value" line. The load-bearing per-resource facts are **(a) whether the resource
is ORG-level (no project — agent pools, groups) or a per-PROJECT fan-out child; (b) the import-id shape —
BARE (`<guid>` / `<int>` / `<descriptor>`) vs `<project>/<child>` slash-composite — and for the composite,
whether the LEAF is a UUID vs an INT; and (c) whether the resource is SECRET-bearing** (service connections,
variable-group secret values — enumerate the shell, never the value). Get (a) wrong and you list under the
wrong root (or hit a project route for an org object); get (b) wrong and every import block for that type is
un-importable; get (c) wrong and you leak a credential into the inventory/HCL/state. All three are **verified
against the registry `website/docs/r/*.html.markdown`** and pinned per-resource in the catalog. The rules:

- **Root — projects.** `GET /_apis/projects` (continuation-token paged) → each is an `azuredevops_project`
  whose **import id is the bare project GUID** (the registry accepts the project NAME too —
  `terraform import azuredevops_project.x "Example Project"` — but pin the **GUID** for stability; a name can
  change and can contain spaces/slashes). Capture each project `id` (UUID) + `name` — the **fan-out keys**.
- **Org-level resources (no project fan-out) → BARE import.**
  - `GET /_apis/distributedtask/pools` → `azuredevops_agent_pool` (import = **bare integer** pool id, e.g.
    `0`). Org-scoped; there is no project segment.
  - `GET https://vssps.dev.azure.com/<org>/_apis/graph/groups` (the **graph host**, continuation-token paged)
    → `azuredevops_group` (import = the opaque **descriptor**, e.g.
    `vssgp.Uy0xLTkt…` / `aadgp.…` / `ungrp.T3c`). The org-scope graph list returns BOTH org-level and
    project-scoped groups (e.g. `[MyFirstProject]\Contributors`) flat — one call covers all groups. Capture
    `descriptor`.
- **Per-project children (one-level fan-out per project) → `<project>/<child>` slash-composite import.** For
  each project id `<p>`, list its repos/definitions/variable-groups/service-endpoints/queues/teams/
  environments/policies (see spine). The composite is **always 2-part `<project>/<child>`** — but the leaf
  type varies:
  - **UUID leaf:** `azuredevops_git_repository` (`<project>/<repoId>`), `azuredevops_team`
    (`<project_id>/<team_id>` — BOTH UUIDs), `azuredevops_serviceendpoint_*`
    (`<project>/<endpointId>` — endpoint UUID).
  - **INT leaf:** `azuredevops_build_definition` (`<project>/<definitionId>`), `azuredevops_variable_group`
    (`<project>/<groupId>`), `azuredevops_environment` (`<project>/<environmentId>`), `azuredevops_agent_queue`
    (`<project>/<queueId>`), `azuredevops_branch_policy_*` / `azuredevops_repository_policy_*`
    (`<project>/<policyId>` — the policy CONFIGURATION id).
- **The POLICY plane needs a TYPE DISCRIMINATOR (the Vault/Keycloak precedent).** `azuredevops_branch_policy_*`
  and `azuredevops_repository_policy_*` are ALL sourced from ONE list — `GET
  /<project>/_apis/policy/configurations` — and every entry carries a **`type.id` (a policy-type UUID)** and a
  **scope**. To map a configuration to the RIGHT TF type you must branch on `type.id` (min-reviewers, build
  validation, comment-requirements, work-item-linking, file-path-pattern, case-enforcement, …) AND on whether
  the scope is a branch (→ `azuredevops_branch_policy_*`) or a repository/project (→
  `azuredevops_repository_policy_*`). This is a real discriminator; Phase A can either (i) beachhead ONE
  policy type (min_reviewers) and defer the rest, or (ii) encode the full `type.id → TF type` table. **The
  import id is the same for all of them** (`<project>/<configId>`), so a scaffold that gets the TF type
  slightly wrong still emits an importable block — but the generated HCL would be for the wrong resource, so
  pin the mapping before enabling a type. Recommend deferring the policy plane past the beachhead.
- **The import-id shape is the #1 hazard — FOUR shapes, encode per-TF-type, never infer:**
  1. **BARE GUID** — `azuredevops_project` (name also accepted; pin the GUID).
  2. **BARE INT** — `azuredevops_agent_pool` (org-level pool id).
  3. **BARE DESCRIPTOR** — `azuredevops_group` (opaque `vssgp.`/`aadgp.`/`ungrp.` string; template-escape it).
  4. **2-part `<project>/<child>`** (single `/`) — everything else, with the leaf a **UUID** (repo, team,
     service endpoint) or an **INT** (build definition, variable group, environment, agent queue, policy).
  - The project segment is the **project GUID** in every composite (the registry ALSO accepts the project
    NAME — `"Example Project/10"` — but pin the GUID; a name with a space/slash would break the `/` parser).
  - The whole composite is `util.EscapeHCLTemplate`-wrapped before emit (a descriptor is opaque; a project
    name — if ever used — can contain `$`/`{`). Encode the id as an explicit per-TF-type switch in
    `importid.go` (mirror Vault's six-shape / GitLab's four-shape switch).

## Enumeration spine

Flat org scope (one container = the Azure DevOps org). The spine is a **single-root fan-out**: list the
projects root and the two org-level roots (agent pools, graph groups), then per project its children.
Best-effort per list (403/404 → Verbose skip; 401/203/HTML → fatal; 429/5xx → Warn + count). The PAT never
appears in errors/logs. (Mirror `gitlab/enumerate.go`: a top-level `list` helper owns the systemic-failure
count for the roots; a `subList` helper for the per-project fan-out does NOT bump the count.) **Decode ONLY
the id/name/descriptor/project.id fields needed for the import composite — never a service-connection
`authorization` or a variable-group secret `value`** (§ Write-only, the Vault precedent — the enumeration
struct simply omits them). Every request carries `?api-version=…` and the Basic-PAT header.

- **Root — projects:** `GET /_apis/projects?api-version=7.1` (continuation-token paged) → envelope
  `{count,value:[{id,name,…}]}` → `azuredevops_project` (bare GUID import). Capture each project `id`(UUID) +
  `name` — the fan-out keys.
- **Org root — agent pools:** `GET /_apis/distributedtask/pools?api-version=7.1` → `{count,value:[{id,name}]}`
  → `azuredevops_agent_pool` (bare INT import). (Filter the built-in hosted pools? VERIFY — the built-in
  "Azure Pipelines" hosted pool may not be cleanly manageable as a `azuredevops_agent_pool`; Warn/skip if
  the import rejects it.)
- **Org root — groups (the GRAPH host):** `GET https://vssps.dev.azure.com/<org>/_apis/graph/groups?
  api-version=7.1-preview.1` (continuation-token paged, `x-ms-continuationtoken`) →
  `{count,value:[{descriptor,displayName,principalName,…}]}` → `azuredevops_group` (descriptor import).
- **Per project `<p>` (one-level fan-out):**
  - `GET /<p>/_apis/git/repositories?api-version=7.1` → `{count,value:[{id,name,project}]}` →
    `azuredevops_git_repository` (`<p>/<repoId>` — **UUID leaf**). Usually single-page (no continuation).
  - `GET /<p>/_apis/build/definitions?api-version=7.1` (continuation-token paged) →
    `{count,value:[{id,name}]}` → `azuredevops_build_definition` (`<p>/<definitionId>` — **INT leaf**).
  - `GET /<p>/_apis/distributedtask/variablegroups?api-version=7.1` → `{count,value:[{id,name,variables,…}]}`
    → `azuredevops_variable_group` (`<p>/<groupId>` — **INT leaf**). **NEVER decode a variable `value` /
    `secretValue`** (the secret; § Write-only). Groups linked to a Key Vault, or containing secret variables,
    may not round-trip cleanly (the registry warns secret groups can't be imported) → adopt the shell, flag.
  - `GET /<p>/_apis/serviceendpoint/endpoints?api-version=7.1-preview.4` → `{count,value:[{id,name,type,…}]}`
    → `azuredevops_serviceendpoint_*` (`<p>/<endpointId>` — **UUID leaf**). **SECRET-bearing — NEVER decode
    `authorization`/`data` credential fields.** The endpoint `type` discriminates the TF type
    (`azuredevops_serviceendpoint_github`/`_dockerregistry`/`_azurerm`/…) — a discriminator like the policy
    plane. **Recommend deferring the whole service-endpoint family to a Phase-B scrub increment** (see below).
  - `GET /<p>/_apis/distributedtask/queues?api-version=7.1` → `{count,value:[{id,name}]}` →
    `azuredevops_agent_queue` (`<p>/<queueId>` — **INT leaf**).
  - `GET /_apis/projects/<p>/teams?api-version=7.1` (org route, project in the path; VERIFY vs the org-wide
    `GET /_apis/teams?$mine=false&$top=…&$skip=…` which uses `$top`/`$skip` offset) →
    `{count,value:[{id,name,projectId}]}` → `azuredevops_team` (`<p>/<teamId>` — **both UUIDs**). Skip the
    project's default team if it fights the project resource — VERIFY.
  - `GET /<p>/_apis/distributedtask/environments?api-version=7.1` (continuation-token paged) →
    `{count,value:[{id,name}]}` → `azuredevops_environment` (`<p>/<environmentId>` — **INT leaf**).
    (The environments-list API lives under distributedtask, NOT pipelines — the pipelines route has
    no list endpoint.)
  - `GET /<p>/_apis/policy/configurations?api-version=7.1` (continuation-token paged) →
    `{count,value:[{id,type:{id},isEnabled,settings:{scope}}]}` → `azuredevops_branch_policy_*` /
    `azuredevops_repository_policy_*` (`<p>/<configId>` — **INT leaf**), discriminated by `type.id` + scope
    (§ CRITICAL). Defer past the beachhead.

If nothing was found AND the roots failed with real (non-403/404) errors, surface a systemic failure rather
than shipping an empty inventory (same guard as GitLab/Vault).

## Resource catalog

Import IDs verified against the current `microsoft/azuredevops` registry docs
(`website/docs/r/*.html.markdown`) and cross-checked against Terraformer's `project.go` / `git_repository.go`
/ `group.go`. All scope = org. "list endpoint → shape" is the `{count,value}` envelope list. "fan-out" names
the parent (project) or ORG. The **id shape** column is the #1 hazard — **bare vs `<project>/<child>`, and
UUID vs INT vs descriptor leaf.**

| native key | TF type | list endpoint → shape | fan-out | import ID | id shape |
|---|---|---|---|---|---|
| azuredevops:project | azuredevops_project | `GET /_apis/projects` → `{count,value}` | root | `<projectGuid>` (name also ok) | **bare GUID** |
| azuredevops:agent_pool | azuredevops_agent_pool | `GET /_apis/distributedtask/pools` | ORG | `<poolId>` | **bare INT** |
| azuredevops:group | azuredevops_group | `GET vssps …/_apis/graph/groups` (paged) | ORG (graph host) | `<descriptor>` | **bare descriptor** |
| azuredevops:git_repository | azuredevops_git_repository | `GET /<p>/_apis/git/repositories` | ← project | `<project>/<repoId>` | **2-part, UUID leaf** |
| azuredevops:build_definition | azuredevops_build_definition | `GET /<p>/_apis/build/definitions` (paged) | ← project | `<project>/<definitionId>` | **2-part, INT leaf** |
| azuredevops:variable_group | azuredevops_variable_group | `GET /<p>/_apis/distributedtask/variablegroups` | ← project | `<project>/<groupId>` | **2-part, INT leaf** (**secret values!**) |
| azuredevops:serviceendpoint | azuredevops_serviceendpoint_* | `GET /<p>/_apis/serviceendpoint/endpoints` | ← project (type-disc) | `<project>/<endpointId>` | **2-part, UUID leaf** (**SECRET-bearing → defer**) |
| azuredevops:agent_queue | azuredevops_agent_queue | `GET /<p>/_apis/distributedtask/queues` | ← project | `<project>/<queueId>` | **2-part, INT leaf** |
| azuredevops:team | azuredevops_team | `GET /_apis/projects/<p>/teams` | ← project | `<project_id>/<team_id>` | **2-part, BOTH UUID** |
| azuredevops:environment | azuredevops_environment | `GET /<p>/_apis/distributedtask/environments` (paged) | ← project | `<project>/<environmentId>` | **2-part, INT leaf** |
| azuredevops:branch_policy | azuredevops_branch_policy_* | `GET /<p>/_apis/policy/configurations` (paged) | ← project (type-disc) | `<project>/<configId>` | **2-part, INT leaf** (type.id disc) |
| azuredevops:repository_policy | azuredevops_repository_policy_* | `GET /<p>/_apis/policy/configurations` (paged) | ← project (type-disc) | `<project>/<configId>` | **2-part, INT leaf** (type.id + scope disc) |

**`azuredevops_area_permissions` / `azuredevops_iteration_permissions` are NOT in the catalog** — they are
**permission (ACL) resources**, keyed by `project_id` + a `principal` (group) descriptor + a path, sourced
from the Security/ACL API (`_apis/accesscontrollists/<namespaceId>`), not a clean list of objects with a
round-trippable import id. They belong to the deferred **permissions plane** (§ Deliberately out of scope),
alongside `azuredevops_git_permissions`, `azuredevops_project_permissions`, etc.

### Import-format quirks (§ do not get wrong)

1. **FOUR shapes — encode per TF type, never infer the separator or the leaf type.** bare GUID
   (`azuredevops_project`) / bare INT (`azuredevops_agent_pool`) / bare descriptor (`azuredevops_group`) /
   2-part `<project>/<child>` (everything else). The 2-part leaf is a **UUID** for repo/team/service-endpoint
   and an **INT** for build-definition/variable-group/environment/agent-queue/policy. This is the provider's
   defining hazard (the Vault six-shape / GitLab four-shape precedent).
2. **The project segment is the project GUID (pin it), even though the registry accepts the project NAME.**
   Every composite doc shows BOTH `"Example Project/10"` and `00000000-…/10`. Pin the **GUID** form: a
   project name can contain spaces or a `/`, which would break the composite `/` parser; the GUID is stable
   and separator-safe. (Terraformer records the bare project/repo GUID as the STATE id; the `terraform
   import` CLI parses the `<project>/<child>` composite — emit the composite in the import block.)
3. **`azuredevops_git_repository` import triggers an `initialization` diff — a curation note, not an import
   note.** The registry warns: after import, `terraform plan` shows a diff on the `initialization` block and
   would try to re-init the repo. Phase-B curation must **`lifecycle { ignore_changes = [initialization] }`**
   (or drop the block) so an imported repo is plan-clean. VERIFY the exact form of `<project>/<repoId>` (the
   docs example is `<projectName>/<repoName>` and `<projectName>/<repoId>`; pin `<projectGuid>/<repoId>` and
   confirm the GUID/GUID form imports at live round-trip, else fall back to `<projectName>/<repoId>`).
4. **The group `descriptor` is opaque and prefix-typed** (`vssgp.` VSTS group, `aadgp.` AAD group,
   `ungrp.` custom). It is NOT a UUID and NOT a name — capture the `descriptor` field verbatim off the graph
   list; do not derive it. Template-escape on emit (defensive; the base64-ish body is safe but the switch is
   uniform).
5. **The policy id is the CONFIGURATION id (an int), not the policy-TYPE id.** `azuredevops_branch_policy_*` /
   `azuredevops_repository_policy_*` import = `<project>/<configId>`; the `type.id` (a UUID) only
   DISCRIMINATES which TF type to emit — it is never the import leaf. Do not confuse the two.
6. **All ids are opaque strings on emit — the INT ids must be stringified** (the Datadog `strconv`-of-numeric
   precedent). A pool/definition/group/queue/environment/policy int → decimal string; a project/repo/team/
   endpoint UUID and a group descriptor copy verbatim. Template-escape the whole composite on emit.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a live org — prune
computed via `hcl.WalkResourceBlocks`; scrub credential fields. **The recurring hazard is the
service-connection `authorization`/credential blob and the variable-group secret `value`** — both are
secrets; the enumeration never reads them, but generate-config-out will try to author them (a variable-group
secret value is returned BLANK on read → not plan-clean until re-supplied; a service-connection secret is
also masked → nulled). The repo-wide secret scan is the backstop.

- **`azuredevops_project` — the fan-out root; light curation.** `name`, `description`, `visibility`
  (`private`/`public`), `version_control` (`Git`/`Tfvc`), `work_item_template`, `features`. Prune computed
  (`process_template_id` sometimes computed). No secret.
- **`azuredevops_git_repository` — light; the `initialization` trap.** `name`, `project_id`, `default_branch`.
  **`initialization` block causes a post-import diff → `ignore_changes`/drop** (§ quirk 3). Prune computed
  (`remote_url`, `ssh_url`, `web_url`, `size`, `disabled`). No secret (repo *contents* are DATA, out of
  scope).
- **`azuredevops_build_definition` — medium; the biggest shell.** `name`, `project_id`, `repository{…}`,
  `ci_trigger`/`pull_request_trigger`, `variable` blocks, `agent_pool_name`, YAML `path`. **`variable`
  blocks can be `is_secret = true` — the secret `value` is masked on read → scrub** (a build definition can
  carry inline secret variables, same hazard as the variable group). Prune computed (`revision`).
- **`azuredevops_variable_group` — light shell + the paramount SECRET.** `name`, `project_id`, `description`,
  `allow_access`, and `variable` blocks (`name`, `value`, `is_secret`, `secret_value`). **The secret variable
  `value`/`secret_value` is THE scrub** — the variablegroups API returns secret values BLANK on read, so
  generate-config-out nulls them → not plan-clean until re-supplied; never pull `value` into the inventory
  struct. `key_vault` block (linked Key Vault) → the values live in Azure Key Vault, not Azure DevOps; adopt
  the linkage, never the secret. The registry warns **secret-bearing groups may not import at all**.
- **`azuredevops_serviceendpoint_*` — SECRET-bearing; DEFER the family.** Every service connection carries an
  `authorization` block and type-specific credential fields (`azuredevops_serviceendpoint_dockerregistry`
  password, `_github` `auth_personal.personal_access_token`, `_azurerm` SPN key, `_generic` `password`, …).
  Masked on read → nulled by generate-config-out. **Recommend a dedicated Phase-B increment** (config shells
  + per-type scrub rules); not in the beachhead.
- **`azuredevops_agent_pool` / `azuredevops_agent_queue` — trivial.** Pool: `name`, `pool_type`,
  `auto_provision`, `auto_update`. Queue: `project_id`, `agent_pool_id` (or `name`). No secret.
- **`azuredevops_team` — light.** `project_id`, `name`, `description`, `administrators`, `members` (identity
  descriptors — identifiers, not PII payload). No secret. Members/admins come back in server order → sort.
- **`azuredevops_group` — light.** `scope`/`origin`/`display_name`/`description` (or `mail`/`origin_id` for
  an AAD-backed group). No secret. Note the id is the descriptor.
- **`azuredevops_environment` — trivial.** `project_id`, `name`, `description`. No secret (environment
  *checks*/approvals are separate resources, deferred).
- **`azuredevops_branch_policy_*` / `azuredevops_repository_policy_*` — medium; the type discriminator.**
  Each shape (`_min_reviewers`, `_build_validation`, `_comment_resolution`, `_work_item_linking`,
  `_auto_reviewers`, `_status_check`; repository: `_author_email_pattern`, `_file_path_pattern`,
  `_max_path_length`, `_max_file_size`, `_case_enforcement`, `_reserved_names`) has its own settings block.
  `_build_validation` references a `build_definition_id`; `_auto_reviewers` references reviewer descriptors.
  No secret, but the `type.id → TF type` mapping must be exact (§ CRITICAL). Defer past the beachhead.

Until Phase B these are no-ops, so an Azure DevOps export is a breadth scaffold, not yet plan-clean. The
pipeline's repo-wide secret scan is the backstop for the service-connection credentials / variable-group
secret values / build-definition secret variables that generate-config-out nulls-or-emits before the scrub
rules land — and the paramount backstop is that enumeration NEVER reads a service-connection authorization or
a secret variable value in the first place.

## Write-only / secret resources (EXCLUDE / scrub)

Two tiers: **DEFER the credential-dominated resources** (a service connection is mostly a credential;
adopting a scaffold that nulls it is low-value until the scrub rules land), and **scrub the field-level
secrets** on the adoptable config shells.

**DEFER (credential-dominated — a Phase-B scrub increment, not the beachhead):**
- **`azuredevops_serviceendpoint_*`** (the whole family — `_github`, `_azurerm`, `_dockerregistry`,
  `_kubernetes`, `_generic`, `_bitbucket`, `_npm`, `_nuget`, `_sonarqube`, `_ssh`, …). Each is a **service
  connection whose reason to exist is a stored credential** (`authorization`, `password`, `token`,
  `personal_access_token`, SPN key). Azure DevOps masks these on read → generate-config-out nulls them.
  Enumerate the SHELL is possible (`<project>/<endpointId>`), but adopt only in a dedicated increment with
  per-type scrub; **never decode the `authorization`/`data` credential fields into a list element.**
- **Personal access tokens / OAuth apps** — there is no round-trippable "PAT" resource; the PAT lives ONLY on
  the Authorization header, never in generated config, state, errors, or logs. **Do NOT inline it into
  `providers.tf`** (Terraformer already doesn't — keep it that way).

**Scrub the value, keep the config shell:**
- **`azuredevops_variable_group` secret `value`/`secret_value`** — the paramount scrub (masked/blank on read;
  never decode into the inventory struct — mirror GitLab's CI/CD-variable `value` / Vault's `bind_credential`
  non-decode).
- **`azuredevops_build_definition` `variable { is_secret = true }` values** — inline secret variables, masked
  on read → scrub.
- **`azuredevops_serviceendpoint_*` `authorization`/credential blocks** — if ever adopted, scrub per type
  (see DEFER above).
- **Not secret, do not over-scrub:** project name/visibility, repo name/default_branch (repo CONTENTS are DATA
  — out of scope, never read), build-definition non-secret variables/triggers/repository ref, variable-group
  non-secret variable names/values + `allow_access`, agent pool/queue names, team names + member descriptors,
  group descriptors/display names, environment names, policy settings (reviewer counts, patterns, scopes).
  These are config/structure — adopt them.

## Deliberately out of scope

- **Service connections' secret data** — the `azuredevops_serviceendpoint_*` credential fields are
  DEFERRED to a scrub increment (above), not adopted at the beachhead. Adopting a nulled credential is
  low-value until the scrub + re-supply flow exists.
- **Permissions / ACL plane** (`azuredevops_git_permissions`, `azuredevops_project_permissions`,
  `azuredevops_area_permissions`, `azuredevops_iteration_permissions`, `azuredevops_build_definition_permissions`,
  `azuredevops_serviceendpoint_permissions`, `azuredevops_team_administrators`/`_members`,
  `azuredevops_group_membership`) — ACL/membership resources keyed by a namespace + principal descriptor +
  token/path, sourced from the Security API, not a clean object list. Admin-heavy; a much-later identity
  increment (`Capabilities.IAM=false`).
- **User plane** (`azuredevops_user_entitlement`, `azuredevops_group_entitlement`,
  `azuredevops_service_principal_entitlement`) — org membership/licensing via the Member Entitlement
  Management API (`vsaex.dev.azure.com`, a THIRD host); bulk + PII. A later increment; Phase A adopts
  project/repo/pipeline config, not the org's user directory.
- **Pipeline/YAML depth** (`azuredevops_build_folder`, `azuredevops_build_definition_permissions`,
  `azuredevops_pipeline_authorization`, `azuredevops_check_*` environment checks/approvals,
  `azuredevops_resource_authorization`, `azuredevops_agent_pool_queue`) — the deeper CI plane and
  environment gates; a later increment after the definition/environment shells are solid.
- **Service hooks** (`azuredevops_servicehook_*` — storage-queue, permissions) — webhook subscriptions whose
  consumer inputs carry auth (basic-auth passwords, API tokens); scrub-bearing → a dedicated later increment.
- **Boards/work-item plane** (`azuredevops_workitem`, `azuredevops_area`/`_iteration` classification nodes,
  `azuredevops_team_settings`) and **wiki, dashboards, extensions** — product config beyond the
  repo/pipeline/policy core; later increments.
- **Data planes** — commits, PR diffs, build/pipeline RUNS, logs, artifacts, work-item CONTENT, repo file
  contents (`azuredevops_git_repository_file`), test results. DATA behind the config. Out of scope
  (config only).
- **Azure DevOps *Server* (on-prem TFS)** — the `{server:port}/tfs/{collection}` instance form,
  Windows-auth, and older api-versions. Phase A targets **Azure DevOps *Services*** (`dev.azure.com`); the
  on-prem base-URL/auth variants are a later carve-out (VERIFY the http/Windows-auth divergence).
- **The `azure-devops-go-api` SDK + the `az devops` CLI** — Terraformer pulls the SDK; TerraLift uses a raw
  `net/http` client (smaller, matches GitLab/Vault). A deliberate non-adoption. (Terraformer's PAT handling
  is already keyless — that part we KEEP, not refuse.)
- **Cloud-IAM depth** (`Capabilities.IAM=false`) — groups/teams are modeled at breadth, but membership,
  permission-matrix/ACL depth, and entitlement/licensing are the deferred identity/governance planes.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)

The **recommended Phase-A CONFIG CORE** (~9 TF types across the org → projects fan-out): `azuredevops_project`,
`azuredevops_git_repository`, `azuredevops_build_definition`, `azuredevops_variable_group` (shell; secret
values scrubbed), `azuredevops_agent_pool` (org), `azuredevops_agent_queue`, `azuredevops_team`,
`azuredevops_group` (org, graph host), `azuredevops_environment`. **Defer** the policy plane
(`azuredevops_branch_policy_*` / `azuredevops_repository_policy_*` — needs the `type.id` discriminator) and
the whole `azuredevops_serviceendpoint_*` family (secret-dominated) to dedicated increments.

BEACHHEAD `azuredevops_project` + `azuredevops_git_repository` + `azuredevops_build_definition` (the
project/repo/pipeline core essentially every Azure DevOps org manages as IaC — `azuredevops_project`
establishes the **`GET /_apis/projects` root** and the **bare-GUID import**, `azuredevops_git_repository`
establishes the **`GET /<p>/_apis/git/repositories` fan-out** and the **2-part `<project>/<repoId>`
UUID-leaf composite** (plus the `initialization`-diff curation note), and `azuredevops_build_definition`
establishes the **INT-leaf composite** `<project>/<definitionId>` — and this trio exercises the
**Basic-with-empty-username PAT client**, the **`?api-version=` per-request helper**, the
**`{count,value}` envelope decode**, the **`x-ms-continuationtoken` continuation pager**, the **203/HTML
bad-PAT detection**, the **`GET /_apis/projects` preflight**, and the **`list`-vs-`subList` systemic-count
split** without touching a single secret value) → INC-1 `azuredevops_variable_group` (the **secret-value
scrub** — the API returns secret values blank; adopt the shell) + `azuredevops_agent_queue` +
`azuredevops_environment` (the rest of the per-project INT-leaf composites) → INC-2 `azuredevops_agent_pool`
(the **org-level bare-INT root**) + `azuredevops_group` (the **org-level graph host** on
`vssps.dev.azure.com` + the **descriptor import**) + `azuredevops_team` (the **both-UUID composite**) →
INC-3 the POLICY plane `azuredevops_branch_policy_*` + `azuredevops_repository_policy_*` (the
**`GET /<p>/_apis/policy/configurations` list** + the **`type.id`/scope discriminator** → TF type) → INC-4
the `azuredevops_serviceendpoint_*` family (the **secret-scrub increment** — per-type authorization scrub) →
LATER the permissions/ACL plane, the user-entitlement plane (a third host `vsaex.dev.azure.com`), the deeper
pipeline/environment-check plane, service hooks, the boards/work-item plane, and Azure DevOps *Server*
(on-prem) support. **NEVER: inline the PAT into `providers.tf` (Terraformer doesn't — keep it keyless); and
NEVER decode a service-connection `authorization` or a variable-group secret `value` into a list element.**
