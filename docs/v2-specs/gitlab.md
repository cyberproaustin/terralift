# GitLab provider — build spec

Research artifact for the `gitlab` provider (Phase A scaffold; TF provider source is
**`gitlabhq/gitlab`** — the official Terraform provider for **GitLab**, the DevOps/SCM platform, SaaS at
`gitlab.com` or self-managed CE/EE). Sources: Terraformer's `providers/gitlab/` (four files —
`gitlab_provider.go` + `gitlab_service.go` + `group.go` + `project.go`, built on the `xanzy/go-gitlab` Go
SDK), the `gitlabhq/gitlab` registry docs (import formats + schema, **verified per-resource below** against
the provider repo's `docs/resources/*.md`), and the GitLab REST API v4
(`https://gitlab.com/api/v4/…`). Build mirrors **two** prior providers at once — **Keycloak**
(`internal/providers/keycloak/`) for the **parent→child FAN-OUT** spine (GitLab fans out `groups → children`
and `projects → children` exactly as Keycloak fans out `realms → sub-objects`, with the same
`list`/`subList` split — the top-level `list` owns the systemic-failure count, the per-parent `subList`
does not) and its **offset PAGER** (Keycloak's `first`/`max` → GitLab's `page`/`per_page`), and **Vault**
(`internal/providers/vault/`) for the **custom-header client + secret-data-is-off-limits discipline** (the
`X-Vault-Token` custom header → GitLab's `PRIVATE-TOKEN`; Vault's "never read a secret value" → GitLab's
"never decode a CI/CD variable `value`"). This is **REST, Keycloak/Vault-style, NOT GraphQL** (GitLab has a
GraphQL API but the TF provider and Terraformer both use REST v4).

**GitLab's DEFINING HAZARD is the import ID — it uses MANY colon-composite ids with part-counts from 1 to 4,
including a THREE-part CI/CD-variable id with an environment scope and a FOUR-part LDAP-link id with an
empty segment. Every separator, part-count, and whether the leaf is a numeric id vs a name/path MUST be
verified per-TF-type and encoded explicitly in `importid.go` — never inferred.** Six facts set GitLab
apart, all load-bearing and called out below:

1. **Auth is a single custom header — `PRIVATE-TOKEN: <token>` (exact casing/hyphen confirmed) — from
   `GITLAB_TOKEN`.** The Vault `X-Vault-Token` / Logz.io `X-API-TOKEN` custom-header shape. A Personal,
   Project, Group, or CI-Job Access Token rides ONLY on that header — never in the URL, query, body, errors,
   logs, config, or state, and **never inlined into `providers.tf`** (Terraformer inlines it — the leak we
   refuse). GitLab also accepts the OAuth form **`Authorization: Bearer <token>`** for the same tokens
   (confirmed); pin the canonical `PRIVATE-TOKEN`.
2. **Base URL already contains `/api/v4` — do NOT re-append a version prefix** (contrast Vault's `/v1/`
   which we append). `GITLAB_BASE_URL` (default `https://gitlab.com/api/v4/`) is the full API endpoint and
   "must end with a slash"; the self-managed override is the same env with a different host
   (`https://gitlab.mycorp.internal/api/v4/`). https, redirect-refusing client.
3. **The spine is a TWO-ROOT FAN-OUT** — `GET /groups?membership=true` (parent) → per-group
   variables/labels/hooks/members/ldap-links, and `GET /projects?membership=true` (parent) → per-project
   variables/labels/hooks/branches/tags/deploy-keys/members/milestones/shared-groups. One flat container =
   the GitLab *instance*. A PAT/token sees only what its owner can access — the membership root is the scope.
4. **Pagination is offset `page`/`per_page` (max 100) driven by the `X-Next-Page` response header** (and the
   RFC5988 `Link` header). `X-Total`/`X-Total-Pages` exist but are **omitted for collections > 10 000** — so
   the pager loops on `X-Next-Page` (empty ⇒ last page), NOT on a total. **Keyset pagination**
   (`pagination=keyset`) exists for very large collections but is **not needed for Phase A** (offset to 100
   pages is ample for a token-scoped group/project set).
5. **Numeric ids everywhere in the composites** — a project/group/hook/label/member/deploy-key id is a
   NUMBER off the wire (Terraformer does `strconv.FormatInt(int64(x.ID), 10)`). The top-level
   `gitlab_group`/`gitlab_project` import accepts EITHER the numeric id OR the full path
   (`richardc/example`), but the sub-resource composites embed the **numeric** parent id — pin the numeric
   `id` for parity with Terraformer, never the path (a path contains `/` which would collide with the future
   composite parser). A handful of leaves are NAMES not numbers (branch/tag name, ldap cn/provider) — flagged
   per-type.
6. **Colon-composite import ids with 1–4 parts and an empty-segment case** (§ CRITICAL) — bare `<id>`
   (group/project), 2-part `<parent>:<leaf>` (hooks/labels/members/deploy-keys/branches/tags/milestones/
   share), 3-part `<parent>:<key>:<environment_scope>` (CI/CD variables — the env scope!), and 4-part
   `<group>:<provider>:<cn>:<filter>` (ldap link, cn XOR filter — one segment is always EMPTY). **Encode per
   TF type; never infer the separator or the part-count.**

## Version pin (load-bearing)

Pin `gitlabhq/gitlab ~> 17.x` (current provider is **17.x/18.x** tracking the GitLab release train — **VERIFY
the current major at build**; the provider moved from `xanzy/go-gitlab` to `gitlab.com/gitlab-org/api/client-go`
under the hood, but the registry source is `gitlabhq/gitlab` and the REST endpoints below are
version-independent). Naming/behaviour facts that matter (the Terraformer-vs-current divergences):

- **Terraformer INLINES the GitLab token into the provider block** — `GetConfig()` returns
  `"token": cty.StringVal(p.token)` (and `"base_url"`), writing the token straight into HCL. **This is a
  secret leak.** TerraLift MUST NOT inline the token; the emitted `providers.tf` authenticates via
  `GITLAB_TOKEN` / `GITLAB_BASE_URL` env only (keyless). This is the #1 "do NOT copy Terraformer" item.
- **Terraformer's resource set is NARROW — only 6 types across two generators.** `group.go` fetches a
  **single** group (`Groups.GetGroup`, not a list) and emits `gitlab_group`. `project.go` lists a group's
  projects (`Groups.ListGroupProjects`, `PerPage:100`, loop until `NextPage==0`) and emits `gitlab_project`
  plus, per project, `gitlab_project_variable` (`project.ID:key:environment_scope`), `gitlab_branch_protection`
  (`project.ID:branch`), `gitlab_tag_protection` (`project.ID:tag`), and `gitlab_project_membership`
  (`project.ID:member_id`). It does **NOT** cover labels, hooks, deploy-keys, group variables/labels/hooks,
  memberships-by-group, share-group, ldap-links, or milestones — those are covered here from the API +
  registry. **Do NOT pull the `go-gitlab` SDK** — a raw `net/http` client is smaller and matches
  Vault/Keycloak/Logz.io (a deliberate non-adoption).
- **Terraformer's enumeration ROOT is a single named group passed as `--resources` args** (`g.Args["group"]`),
  not a discovery pass. TerraLift's root is the token's OWN membership — `GET /groups?membership=true` +
  `GET /projects?membership=true` — so it adopts everything the token can reach without a hand-supplied group
  list (the Keycloak `GET /admin/realms` precedent).
- Terraformer reads `token` + `base_url` (env `GITLAB_TOKEN`, default base
  `https://gitlab.com/api/v4/`). The **TF provider** reads the same via `GITLAB_TOKEN` / `GITLAB_BASE_URL`
  (base "must end with a slash" and include `/api/v4/`). The REST endpoints below are provider-version-independent.

## Shape

- **Auth — the `PRIVATE-TOKEN` header (the Vault `X-Vault-Token` / Logz.io `X-API-TOKEN` custom-header
  shape).** GitLab authenticates with **`PRIVATE-TOKEN: <token>`** on every request (exact hyphenated casing
  confirmed in the API auth doc; the OAuth `Authorization: Bearer <token>` form is also accepted for the same
  personal/project/group tokens, but pin the canonical `PRIVATE-TOKEN`). Plus `Accept: application/json`.
  Read the token from **`GITLAB_TOKEN`**. NOT a query param, NOT the body. The token rides **only** on the
  `PRIVATE-TOKEN` header — never in the URL, query, request body, errors, logs, config, or state (redact any
  URL/query that could appear in a message — mirror `keycloakapi.go`'s `redactURL`, which strips `?…`). A
  direct `net/http` client; **no `glab` CLI, no `go-gitlab` SDK**. Use a **redirect-refusing** client (mirror
  Vault/Keycloak `*HTTPClient` — Go does NOT strip a custom `PRIVATE-TOKEN` header on a cross-host 3xx, so an
  auto-followed redirect would leak the token; self-managed GitLab behind a reverse proxy can 3xx — refuse and
  require `GITLAB_BASE_URL` point at the canonical API host).
- **Base URL — the user-supplied endpoint, https, ALREADY carrying `/api/v4` (the key divergence from
  Vault).** Default **`https://gitlab.com/api/v4`** (SaaS); override via **`GITLAB_BASE_URL`** for
  self-managed (e.g. `https://gitlab.mycorp.internal/api/v4`). Strip any trailing slash (the provider doc
  says the value "must end with a slash" — we normalize it OFF and re-add per route). **Do NOT append a
  version prefix** — unlike Vault's `/v1/`, the base already includes `/api/v4`; if `GITLAB_BASE_URL` is set
  to a BARE host with no `/api/v4`, append exactly one `/api/v4` (normalize so the base ends in `/api/v4`
  with no duplication — VERIFY the normalization against both `https://host` and `https://host/api/v4/`
  inputs). **Force https** (the token is a secret — upgrade a bare host / explicit `http://`, mirror
  `forceHTTPS`) UNLESS the host is `localhost`/`127.0.0.1` where a dev instance on `http://` is plausible
  (allow http there with a **Warn**, the Keycloak local-dev divergence). Guard the host charset (reject
  `@`/userinfo-splice, the `validDomain` guard). All routes are `<base>/<path>` where `<base>` ends in
  `/api/v4`; we never follow a server-supplied next-URL (the pager builds URLs from `base+path+?page=`).
- **Scope — one GitLab INSTANCE = one flat container; groups/projects are a FAN-OUT KEY, not a hierarchy.**
  The token authenticates against the instance and sees exactly what its owner can access; there is no
  sub-instance resolution — the token simply **is** the instance scope. `model.ScopeTenant`. **Groups and
  projects are fan-out keys, not a container tree** — like Keycloak realms / Vault mounts, they live *under*
  the one instance container, so `Capabilities.Hierarchy` stays **false**. **GitLab groups genuinely nest
  (subgroups)**, but that tree is an enumeration detail, not a container hierarchy — and Phase A does NOT
  need manual recursion: `GET /groups?membership=true` returns a **flat** list of every group the token can
  reach (top-level AND subgroups), so there is no `subGroups` tree to flatten (contrast Keycloak). Resolve
  the container id/name **best-effort** from the `GITLAB_BASE_URL` host (there is no "instance name" endpoint;
  `GET /version` or `GET /user` proves connectivity). `Capabilities{IAM:false, Exposure:false,
  Hierarchy:false}`.
- **Response family — BARE JSON ARRAYS with `page`/`per_page` offset pagination (the Keycloak
  `first`/`max` shape; unlike Vault's map/keys).** Almost every list endpoint returns the collection as a
  **bare `[...]` array** with NO envelope; unmarshal straight into `[]T`. The pager is offset/limit query
  params, `?page=<n>&per_page=<N>` (**`per_page` max 100**): fetch `page=1&per_page=100`, accumulate, and
  loop `page++` **while the `X-Next-Page` response header is non-empty** (empty ⇒ last page). Do NOT rely on
  `X-Total`/`X-Total-Pages` — GitLab **omits** them (and the `rel="last"` Link) for collections > 10 000.
  Implement one generic **offset pager** (`gitlabList[T]`, reads `X-Next-Page`; the whole list surface) plus
  a **single-object GET** helper for the one non-list read (`GET /projects/:id` for share-groups). Bound the
  loop defensively (`glMaxPages`). Singletons (a single group/project object) decode as one bare object.
- **Pagination — offset only for Phase A; keyset deferred.** Offset `page`/`per_page` (default 20, **max
  100**) covers a token-scoped group/project set comfortably. **Keyset pagination**
  (`pagination=keyset&order_by=id&sort=asc`, cursor in the `Link`/`X-Next-Cursor` header, no `X-Total`) is
  GitLab's answer for very large collections and would only matter if a single group had > tens of thousands
  of projects/members — **ignore for Phase A** (VERIFY at build that no beachhead list forces keyset; if one
  does, the offset pager just needs the cursor fallback). Treat every list as *potentially* paged; never
  truncate.
- **Status handling (mirror `keycloak/enumerate.go`'s `list`/`subList`; carry the status on the error).**
  GitLab errors are **`{"message": …}`** for any HTTP status ≥ 400 — and `message` is EITHER a string
  (`{"message":"401 Unauthorized"}`, `{"message":"404 Project Not Found"}`) OR an object of field→errors
  (`{"message":{"name":["is too long"]}}`) — parse the `message` tolerantly (string first, else JSON-encode
  the object), **never echo the request** (the token could be nowhere near the body, but the URL/query might
  be). A few validation paths use `{"error":"…"}` (e.g. OAuth) — accept both keys. Rules:
  - **401** (missing/invalid/expired/revoked token) → **fatal** in preflight; a mid-enumeration 401 → fatal
    (every remaining list will fail; a PAT does not auto-refresh).
  - **403** (the token's owner can *see* but not *manage* the object, OR the feature is unlicensed — e.g.
    ldap-links/hooks on CE, an epic/audit feature on Free) → best-effort **Verbose SKIP** (do not fail the
    run).
  - **404** (group/project/feature absent, OR a sub-resource collection the token cannot reach) → **Verbose
    skip**. (GitLab returns 404 rather than 403 for objects the token cannot even see — treat both as a skip.)
  - **429** (GitLab rate-limits aggressively — `RateLimit-*`/`Retry-After` headers) / **5xx** / **network**
    → enumeration may be silently incomplete → **Warn + hardFails++** (tell a systemic failure apart from an
    empty instance). The token never appears in errors/logs.
  - **Systemic guard:** the top-level `list` (groups root, projects root) owns `hardFails`; the per-parent
    `subList` (variables/labels/hooks/… per group/project) does **NOT** bump `hardFails` (sub-lists multiply
    by group/project count — a single project's 403 on hooks must not fail the run). If nothing was found AND
    the roots failed with real (non-403/404) errors, surface a systemic failure rather than shipping an empty
    inventory (same guard as Keycloak/Vault).
- **Preflight**: `terraform` present + `GITLAB_TOKEN` set + (`GITLAB_BASE_URL` valid-or-default) + a
  lightweight auth probe succeeds. Use **`GET /user`** as the auth probe — a valid token can ALWAYS read its
  own user (returns the token owner's id/username — identity metadata, not a secret), so a **401 there means
  a genuinely bad/expired/revoked token**. (For a CI-Job token, `GET /user` may 403 — VERIFY; fall back to
  `GET /version`, which most tokens can read.) A 403 on `GET /groups?membership=true` in preflight is a Warn,
  not a failure (the token may still reach *some* projects directly).
- **Connect**: run `GET /user` (or `GET /version`) to validate the token, best-effort read the instance
  name/host from `GITLAB_BASE_URL`, and set the single flat container (id/name = the host, e.g. `gitlab.com`).

## Two-root FAN-OUT + colon-composite import DEPTH — the CRITICAL determination

This is GitLab's analogue of Keycloak's "realm fan-out + composite import depth" call, fused with Vault's
"never read a secret value" line. The load-bearing per-resource facts are **(a) which of the TWO roots the
resource fans out from (group vs project) — or is it a root itself; (b) the import-id PART COUNT and
separator (bare `<id>` / 2-part `<parent>:<leaf>` / 3-part `<parent>:<key>:<env_scope>` / 4-part
`<group>:<provider>:<cn>:<filter>`); and (c) whether each id part is a NUMERIC id vs a NAME/path.** Get (a)
wrong and you never reach the sub-objects (or list them under the wrong parent); get (b) wrong and every
import block for that type is un-importable; get (c) wrong (path instead of numeric id, or name instead of
id) and the same. All three are **verified against the registry `docs/resources/*.md`** and pinned
per-resource in the catalog. The rules:

- **Root level (the parents) → BARE import id.**
  - `GET /groups?membership=true` (paged) → each is a `gitlab_group` whose **import id is the numeric group
    `id`** (the registry accepts the full path too — `terraform import gitlab_group.example example` — but
    pin the numeric `id` for composite parity). The group `id` (numeric) + `full_path` are the **fan-out
    keys** for group children.
  - `GET /projects?membership=true` (paged) → each is a `gitlab_project` whose **import id is the numeric
    project `id`** (registry example `richardc/example` shows the path form works, but pin the numeric `id`).
    The project `id` (numeric) is the fan-out key for project children.
- **Group-scoped children (one-level fan-out per group) → composite import.** Per group, list
  variables/labels/hooks/members/ldap-links (see spine). Import ids are `<group_id>:<leaf>` (2-part) EXCEPT
  the 3-part variable (`<group>:<key>:<env_scope>`) and the 4-part ldap-link.
- **Project-scoped children (one-level fan-out per project) → composite import.** Per project, list
  variables/labels/hooks/deploy-keys/protected-branches/protected-tags/members/milestones and read
  share-groups (see spine). Import ids are `<project_id>:<leaf>` (2-part) EXCEPT the 3-part variable.
- **The import-id part-count + separator is the #1 hazard — FOUR shapes, encode per-TF-type, never infer:**
  1. **BARE `<id>`** — `gitlab_group`, `gitlab_project` (numeric id; path also accepted).
  2. **2-part `<parent>:<leaf>`** (single colon) — `gitlab_group_hook`/`gitlab_project_hook`
     (`<parent>:<hook_id>`), `gitlab_group_label`/`gitlab_project_label` (`<parent>:<label_id>`),
     `gitlab_group_membership`/`gitlab_project_membership` (`<parent>:<user_id>`), `gitlab_deploy_key`
     (`<project>:<deploy_key_id>`), `gitlab_branch_protection` (`<project>:<branch>` — leaf is the branch
     NAME), `gitlab_tag_protection` (`<project>:<tag>` — leaf is the tag NAME), `gitlab_project_share_group`
     (`<project>:<group_id>`), `gitlab_project_milestone` (`<project>:<milestone_id>`).
  3. **3-part `<parent>:<key>:<environment_scope>`** (two colons — THE trap) — `gitlab_project_variable`
     (`<project>:<key>:<environment_scope>`, e.g. `12345:my_key:*`) and `gitlab_group_variable`
     (`<group>:<key>:<environment_scope>`, e.g. `12345:my_key:*`). **The env scope is part of the id** — two
     variables with the same key but different scopes (`*` vs `production`) are DIFFERENT resources; the id
     MUST carry the third segment (default scope is `*`). A CI/CD variable list returns each entry's
     `environment_scope` — capture it.
  4. **4-part `<group>:<ldap_provider>:<cn>:<filter>`** (three colons, ONE empty segment) —
     `gitlab_group_ldap_link` (`12345:ldapmain:testcn:` when CN-based, filter empty; or `12345:ldapmain::testfilter`
     when filter-based, cn empty). **cn XOR filter — exactly one is populated, the other segment is
     literally empty between two colons.** The composite builder must NOT collapse the empty segment.
  - The whole composite is `util.EscapeHCLTemplate`-wrapped before emit (a label name / variable key / branch
    name can contain `$`/`{`). Encode the id as an explicit per-TF-type switch in `importid.go` (mirror
    Vault's six-shape switch / Keycloak's depth switch).

## Enumeration spine

Flat instance scope (one container = the GitLab host). The spine is a **two-root fan-out**: list the group
root and the project root, then per group/project its children. Best-effort per list (403/404 → Verbose
skip; 401 → fatal; 429/5xx → Warn + count). The token never appears in errors/logs. (Mirror
`keycloak/enumerate.go`: a top-level `list` helper owns the systemic-failure count for the two roots; a
`subList` helper for the per-parent fan-out does NOT bump the count, since sub-lists multiply by
group/project count.) **Decode ONLY the id/name/scope fields needed for the import composite — never the
secret `value`/`token` fields** (§ Write-only, the Vault precedent — the enumeration struct simply omits
them).

- **Root A — groups:** `GET /groups?membership=true&min_access_level=40&per_page=100` (paged on
  `X-Next-Page`) → bare array `[{id, full_path, name}]` → `gitlab_group` (bare import, numeric `id`).
  **`min_access_level=40` (Maintainer) or `50` (Owner)** scopes the root to groups the token can actually
  MANAGE, not merely view — adopt what you can import cleanly (VERIFY the level; without it you enumerate
  read-only groups whose children you cannot manage). Capture each group `id` — the fan-out key.
- **Root B — projects:** `GET /projects?membership=true&min_access_level=40&per_page=100` (paged) → bare
  array `[{id, path_with_namespace, name}]` → `gitlab_project` (bare import, numeric `id`). Capture each
  project `id`. (Skip archived projects — VERIFY; an archived project is read-only and not usefully adopted.)
- **Per group `<g>` (one-level fan-out):**
  - `GET /groups/<g>/variables` (paged) → `[{key, environment_scope, …}]` → `gitlab_group_variable`
    (`<g>:<key>:<environment_scope>` — **3-part**). **NEVER decode `value`** (the secret).
  - `GET /groups/<g>/labels` (paged) → `[{id, name, …}]` → `gitlab_group_label` (`<g>:<label_id>` — VERIFY
    numeric id vs name; registry example `12345:fixme` shows a NAME, but the list returns numeric `id` —
    capture the numeric `id` and confirm the import accepts it, else use `name`).
  - `GET /groups/<g>/hooks` (paged) → `[{id, url, …}]` → `gitlab_group_hook` (`<g>:<hook_id>`). **NEVER
    decode `token`** (write-only; the API does not return it anyway).
  - `GET /groups/<g>/members` (paged; **direct** members, NOT `/members/all` which includes inherited) →
    `[{id, username, …}]` → `gitlab_group_membership` (`<g>:<user_id>`).
  - `GET /groups/<g>/ldap_group_links` (paged) → `[{provider, cn, filter, group_access}]` →
    `gitlab_group_ldap_link` (`<g>:<provider>:<cn>:<filter>` — **4-part, one empty segment**). **Owner/admin +
    Premium/Ultimate + LDAP configured** — expect **403/404 on gitlab.com SaaS / CE → Verbose skip** (guarded,
    not a failure). Include it but do not rely on it.
- **Per project `<p>` (one-level fan-out):**
  - `GET /projects/<p>/variables` (paged) → `[{key, environment_scope, …}]` → `gitlab_project_variable`
    (`<p>:<key>:<environment_scope>` — **3-part**). **NEVER decode `value`.**
  - `GET /projects/<p>/labels` (paged) → `[{id, name, …}]` → `gitlab_project_label` (`<p>:<label_id>` —
    registry example `12345:101010` shows a **numeric** `label_id`; capture the numeric `id`).
  - `GET /projects/<p>/hooks` (paged) → `[{id, url, …}]` → `gitlab_project_hook` (`<p>:<hook_id>`). **NEVER
    decode `token`.**
  - `GET /projects/<p>/deploy_keys` (paged) → `[{id, title, key, …}]` → `gitlab_deploy_key`
    (`<p>:<deploy_key_id>`). `key` is the PUBLIC ssh key (safe, but note it is emitted verbatim).
  - `GET /projects/<p>/protected_branches` (paged) → `[{name, …}]` → `gitlab_branch_protection`
    (`<p>:<branch>` — leaf is the branch **NAME**).
  - `GET /projects/<p>/protected_tags` (paged) → `[{name, …}]` → `gitlab_tag_protection` (`<p>:<tag>` — leaf
    is the tag **NAME**).
  - `GET /projects/<p>/members` (paged; **direct** members) → `[{id, username, …}]` →
    `gitlab_project_membership` (`<p>:<user_id>`).
  - `GET /projects/<p>/milestones` (paged) → `[{id, iid, title, …}]` → `gitlab_project_milestone`
    (`<p>:<milestone_id>` — VERIFY whether `milestone_id` is the global `id` or the per-project `iid`;
    example `12345:11` is ambiguous — capture both and confirm against the import).
  - **Share-groups (NOT a dedicated list endpoint):** `GET /projects/<p>` (single object) →
    `shared_with_groups: [{group_id, group_access_level, …}]` → one `gitlab_project_share_group` per entry
    (`<p>:<group_id>`). This is the ONE single-object read in the spine (no `/shares` list endpoint) — VERIFY
    the field name `shared_with_groups`.

**Groups DO NOT need `subGroups` recursion** — `membership=true` already returns subgroups flat (contrast
Keycloak's group tree). If nothing was found AND the two roots failed with real (non-403/404) errors,
surface a systemic failure rather than shipping an empty inventory (same guard as Keycloak/Vault).

## Resource catalog

Import IDs verified against the current `gitlabhq/gitlab` registry docs (`docs/resources/*.md`) and
cross-checked against Terraformer's `project.go`. All scope = instance. "list endpoint → shape" is the fan-out
list (bare JSON array, `page`/`per_page`). "fan-out" names the parent. The **id shape** column is the #1
hazard — **part count + separator + numeric-vs-name leaf**.

| native key | TF type | list endpoint → shape | fan-out | import ID | id shape |
|---|---|---|---|---|---|
| gitlab:group | gitlab_group | `GET /groups?membership=true` → bare array | root | `<id>` (numeric; path ok) | **bare** |
| gitlab:project | gitlab_project | `GET /projects?membership=true` → bare array | root | `<id>` (numeric; path ok) | **bare** |
| gitlab:group_variable | gitlab_group_variable | `GET /groups/<g>/variables` | ← group | `<group>:<key>:<environment_scope>` | **3-part** (env scope!; **value SECRET**) |
| gitlab:project_variable | gitlab_project_variable | `GET /projects/<p>/variables` | ← project | `<project>:<key>:<environment_scope>` | **3-part** (env scope!; **value SECRET**) |
| gitlab:group_label | gitlab_group_label | `GET /groups/<g>/labels` | ← group | `<group_id>:<label_id>` | **2-part** (VERIFY id vs name) |
| gitlab:project_label | gitlab_project_label | `GET /projects/<p>/labels` | ← project | `<project_id>:<label_id>` | **2-part** (numeric label id) |
| gitlab:group_hook | gitlab_group_hook | `GET /groups/<g>/hooks` | ← group | `<group_id>:<hook_id>` | **2-part** (**token write-only**) |
| gitlab:project_hook | gitlab_project_hook | `GET /projects/<p>/hooks` | ← project | `<project>:<hook_id>` | **2-part** (**token write-only**) |
| gitlab:deploy_key | gitlab_deploy_key | `GET /projects/<p>/deploy_keys` | ← project | `<project>:<deploy_key_id>` | **2-part** (key = PUBLIC ssh) |
| gitlab:branch_protection | gitlab_branch_protection | `GET /projects/<p>/protected_branches` | ← project | `<project_id>:<branch>` | **2-part** (leaf = branch NAME) |
| gitlab:tag_protection | gitlab_tag_protection | `GET /projects/<p>/protected_tags` | ← project | `<project_id>:<tag>` | **2-part** (leaf = tag NAME) |
| gitlab:project_membership | gitlab_project_membership | `GET /projects/<p>/members` (direct) | ← project | `<project_id>:<user_id>` | **2-part** |
| gitlab:group_membership | gitlab_group_membership | `GET /groups/<g>/members` (direct) | ← group | `<group_id>:<user_id>` | **2-part** |
| gitlab:project_share_group | gitlab_project_share_group | `GET /projects/<p>` → `shared_with_groups[]` | ← project (single-obj) | `<project_id>:<group_id>` | **2-part** (no list endpoint) |
| gitlab:project_milestone | gitlab_project_milestone | `GET /projects/<p>/milestones` | ← project | `<project>:<milestone_id>` | **2-part** (VERIFY id vs iid) |
| gitlab:group_ldap_link | gitlab_group_ldap_link | `GET /groups/<g>/ldap_group_links` | ← group | `<group>:<provider>:<cn>:<filter>` | **4-part, empty segment** (owner/admin; CE→skip) |

**No `gitlab_group_milestone` resource exists** — GitLab has a group-milestones REST API
(`GET /groups/:id/milestones`) but the TF provider only ships `gitlab_project_milestone`. Do NOT emit a
group-milestone resource (VERIFY at build against the registry; enumerate only project milestones).

### Import-format quirks (§ do not get wrong)

1. **FOUR part-counts — encode per TF type, never infer the separator or the count.** bare `<id>` /
   2-part `<parent>:<leaf>` / 3-part `<parent>:<key>:<env_scope>` / 4-part `<group>:<provider>:<cn>:<filter>`.
   This is the provider's defining hazard (the Vault six-shape / Keycloak depth-switch precedent).
2. **The 3-part CI/CD variable id carries the ENVIRONMENT SCOPE.** `gitlab_project_variable` /
   `gitlab_group_variable` = `<parent>:<key>:<environment_scope>` (confirmed `12345:my_key:*`). The scope is
   NOT optional in the id — the default `*` (all environments) must be emitted as the third segment, and a
   key that exists in two scopes (`*` and `production`) is two distinct resources. Capture
   `environment_scope` off each list entry; never assume `*`.
3. **The 4-part LDAP-link id has an EMPTY segment (cn XOR filter).** `gitlab_group_ldap_link` =
   `<group>:<provider>:<cn>:<filter>` where exactly one of cn/filter is populated and the other is the empty
   string between two colons (`12345:ldapmain:testcn:` or `12345:ldapmain::testfilter`). The composite builder
   must preserve the empty segment — do not trim trailing/adjacent colons.
4. **Leaf is NUMERIC for most, NAME for branch/tag, path-ish for ldap cn.** hook/label/member/deploy-key/
   share-group/milestone leaves are numeric ids off the wire (Terraformer `strconv.FormatInt(int64(x.ID))`);
   `gitlab_branch_protection`/`gitlab_tag_protection` leaves are the branch/tag NAME; ldap cn/provider are
   directory strings. Capture the right field per type (a label's `id`, a branch's `name`).
5. **The PARENT id is NUMERIC in every composite — never the path.** Even though `gitlab_group`/`gitlab_project`
   ACCEPT a full path (`richardc/example`) as their own bare import id, the sub-resource composites embed the
   **numeric** parent id (`12345:…`). Using the path as the parent segment would inject a `/` and break the
   colon parser. Pin the numeric `id` for the parent segment (Terraformer parity).
6. **All ids/names are opaque strings on emit — but the numeric ids must be stringified** (the Datadog
   `strconv`-of-numeric precedent, NOT the Keycloak opaque-UUID case). `int64(id)` → decimal string. Branch
   names / label names / variable keys copy verbatim. Template-escape the whole composite on emit
   (`util.EscapeHCLTemplate`) — a label name or variable key can contain `$`/`{`.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a live instance —
prune computed via `hcl.WalkResourceBlocks`; scrub credential fields. **The recurring hazard is the CI/CD
variable `value` and the hook `token`** — both are secrets; the enumeration never reads them, but
generate-config-out will try to author them (variable `value` is returned by the API on read — the paramount
scrub; hook `token` is write-only so it is nulled). The repo-wide secret scan is the backstop.

- **`gitlab_group` / `gitlab_project` — the fan-out parents; medium curation.** Group: `name`, `path`,
  `description`, `visibility`, `parent_id` (subgroups — references the parent group numeric id),
  `request_access_enabled`, LFS/2FA toggles. Project: `name`, `path`, `namespace_id`, `visibility`,
  `default_branch`, `description`, feature toggles (`issues_enabled`, `merge_requests_enabled`, …), `topics`.
  **`gitlab_project` `import_url_username`/`import_url_password` are write-only (not importable) → scrub;
  `runners_token` is a SECRET (registration token) → scrub.** Prune computed (`http_url_to_repo`,
  `ssh_url_to_repo`, `web_url`, timestamps). `topics`/`shared_with_groups` come back in server order → sort.
- **`gitlab_project_variable` / `gitlab_group_variable` — light shell + the paramount SECRET.** `key`,
  `value` (**THE secret — masked/unmasked CI/CD variable value; returned by the variables API on read →
  scrub the value, keep the block**), `variable_type` (`env_var`/`file`), `protected`, `masked`, `raw`,
  `environment_scope` (part of the id). Never pull `value` into the inventory struct (§ Write-only). The `$`
  in a variable value would also hit the `${…}` interpolation hazard — but since we scrub it, moot.
- **`gitlab_project_hook` / `gitlab_group_hook` — light; write-only SECRET.** `url`, the per-event booleans
  (`push_events`, `merge_requests_events`, `pipeline_events`, …), `enable_ssl_verification`. **Secret:**
  `token` (webhook auth token — **write-only, NOT returned by the API on read** → generate-config-out nulls it
  → scrub/flag re-supply), and `custom_headers`/`url_variables`/`signing_token` are likewise write-only. Not a
  read leak (masked), but not plan-clean until re-supplied.
- **`gitlab_project_label` / `gitlab_group_label` — trivial.** `name`, `color`, `description`. No secret.
- **`gitlab_deploy_key` — light; PUBLIC key only.** `title`, `key` (the PUBLIC ssh key — safe to emit; NOT a
  private key), `can_push`. No secret (the private half never touches GitLab). Note the id is `<project>:<key_id>`.
- **`gitlab_branch_protection` / `gitlab_tag_protection` — light; nested access levels.** `branch`/`tag`,
  `push_access_level`, `merge_access_level`, `allow_force_push`, `code_owner_approval_required`, and nested
  `allowed_to_push`/`allowed_to_merge` blocks (user/group access lists — come back in server order → sort).
  No secret.
- **`gitlab_project_membership` / `gitlab_group_membership` — trivial.** `project_id`/`group_id`, `user_id`,
  `access_level` (`10`/`20`/`30`/`40`/`50`), optional `expires_at`. No secret (user_id is an identifier, not
  PII payload). **Enumerate DIRECT members only** — `/members/all` includes inherited members that are NOT
  manageable at this level (importing them fights the parent's membership).
- **`gitlab_project_share_group` — trivial.** `project_id`, `group_id`, `group_access` (level),
  `expires_at`. Sourced from the project object's `shared_with_groups` (no list endpoint). No secret.
- **`gitlab_project_milestone` — light.** `project_id`, `title`, `description`, `due_date`, `start_date`,
  `state`. No secret. VERIFY the id is `id` vs `iid`.
- **`gitlab_group_ldap_link` — light; directory config.** `group_id`, `ldap_provider`, `cn` XOR `filter`,
  `group_access`. No secret (LDAP bind creds live on the instance, not the link). Expect it absent on
  CE/SaaS (403/404 skip).

Until Phase B these are no-ops, so a GitLab export is a breadth scaffold, not yet plan-clean. The pipeline's
repo-wide secret scan is the backstop for the variable `value` / hook `token` / project `runners_token` /
`import_url_password` fields that generate-config-out nulls-or-emits before the scrub rules land — and the
paramount backstop is that enumeration NEVER reads a variable `value` or hook `token` in the first place.

## Write-only / secret resources (EXCLUDE / scrub)

Two tiers: **HARD-EXCLUDE the token-minting resources entirely** (adopting them creates/returns a live
credential), and **scrub the field-level secrets** on the adoptable config shells.

**HARD-EXCLUDE (secret-minting — never enumerate/read/import):**
- **Access-token resources** — `gitlab_personal_access_token`, `gitlab_project_access_token`,
  `gitlab_group_access_token`, `gitlab_deploy_token`, `gitlab_pipeline_schedule` trigger tokens,
  `gitlab_cluster_agent_token`. These **create and RETURN a live token secret** (the `token` attribute is
  populated only at create time and cannot be read back) — adopting them is nonsensical (there is no value to
  import) and any enumeration would surface a credential. **Never enumerate** the `GET
  …/access_tokens` collections. (The Vault "dynamic-credential" hard-exclude precedent.)
- **The GitLab token itself** — `GITLAB_TOKEN` lives ONLY on the `PRIVATE-TOKEN` header, never in generated
  config, state, errors, or logs. **Do NOT inline it into `providers.tf` (Terraformer does — refuse it).**
  There is no round-trippable "token" resource to adopt.

**Scrub the value, keep the config shell:**
- **`gitlab_project_variable.value` / `gitlab_group_variable.value`** — the CI/CD variable value (a secret;
  the variables API DOES return it on read → **the paramount scrub**; never decode into the inventory struct
  — mirror Keycloak's `bind_credential` / LaunchDarkly env-key non-decode).
- **`gitlab_project_hook.token` / `gitlab_group_hook.token`** (+ `custom_headers`, `url_variables`,
  `signing_token`) — webhook secrets, **write-only** (not returned on read) → scrub, flag re-supply.
- **`gitlab_project.runners_token`** — the project's runner registration token (secret) → scrub.
  `import_url_username`/`import_url_password` — write-only, not importable → scrub.
- **Not secret, do not over-scrub:** `gitlab_deploy_key.key` (PUBLIC ssh key — emit it), label
  colors/names, branch/tag protection access levels, membership `access_level`/`user_id`, variable
  `key`/`environment_scope`/`protected`/`masked` flags (only the `value` is secret), share-group access
  levels, milestone titles/dates, ldap cn/provider/filter. These are config/structure — adopt them.

## Deliberately out of scope

- **Access-token / secret-minting resources** — personal/project/group access tokens, deploy tokens,
  pipeline-trigger tokens, cluster-agent tokens. Not "deferred" — **permanently excluded** (they mint/return
  a live credential; there is no config value to round-trip). The Vault dynamic-credential precedent.
- **User plane** (`gitlab_user`, `gitlab_user_sshkey`, `gitlab_user_gpgkey`, `gitlab_user_runner`) —
  `GET /users` is **admin-only** and the user directory is bulk + PII. A much-later increment; Phase A adopts
  group/project config, not the instance's user directory.
- **Instance/admin-level resources** — `gitlab_instance_variable`, `gitlab_application_settings`,
  `gitlab_system_hook` (`GET /hooks`), `gitlab_instance_cluster`, `gitlab_runner` registration, license.
  All require **admin** on a self-managed instance; deferred/guarded (a non-admin token 403s → skip).
- **CI/CD depth** (`gitlab_pipeline_schedule`(+`_variable`/`_trigger`), `gitlab_pipeline_trigger`,
  `gitlab_project_runner_enablement`, `gitlab_project_environment`, `gitlab_project_protected_environment`,
  `gitlab_project_freeze_period`, `gitlab_project_mirror`) — the pipeline/runner/environment plane, several
  secret-bearing; a later increment after the variable/hook shells are solid.
- **Merge-request / approval plane** (`gitlab_project_approval_rule`, `gitlab_group_approval_rule`,
  `gitlab_project_level_mr_approvals`, `gitlab_branch`, `gitlab_project_issue*`, `gitlab_project_badge`) —
  the MR-governance and issue/board config; a later increment.
- **Integrations plane** (the many `gitlab_service_*` / `gitlab_integration_*` — Slack, Jira, Microsoft
  Teams, Pipelines-email, …) — each carries an integration credential (token/password/webhook) → scrub-heavy;
  a dedicated later increment with the secret-scrub rules.
- **Group-level org depth** (`gitlab_group_project_file_template`, `gitlab_group_saml_link`,
  `gitlab_group_epic_board`, `gitlab_group_custom_attribute`, `gitlab_group_share_group`,
  `gitlab_group_protected_environment`) and **cross-group subgroup recursion beyond the flat membership
  list** — later increments (SAML/epic features are Premium/Ultimate-gated).
- **SAML/SCIM identity links** (`gitlab_group_saml_link`, SCIM) — Premium/Ultimate + admin; deferred with
  the ldap-link (which IS in Phase A but expected-absent on CE/SaaS).
- **Data planes** — commits, MR diffs, pipeline runs, job logs, artifacts, issue/epic content, container
  registry images. DATA behind the config. Out of scope (config only).
- **The `go-gitlab` SDK + the `glab` CLI** — Terraformer pulls the SDK; TerraLift uses a raw `net/http`
  client (smaller, matches Vault/Keycloak/Logz.io). A deliberate non-adoption. (Also non-adopted:
  Terraformer's token-inlining and its hand-supplied single-group root.)
- **Cloud-IAM depth** (`Capabilities.IAM=false`) — group/project memberships are modeled at breadth, but
  role/permission-matrix depth, protected-environment approver rules, and SAML/SCIM group mapping are the
  deferred identity/governance planes.

## Build order (Phase B increments; Phase A builds the CONFIG CORE all at once)

The **recommended Phase-A CONFIG CORE** (~16 TF types across the two-root fan-out): `gitlab_group`,
`gitlab_project`, `gitlab_group_variable`, `gitlab_project_variable`, `gitlab_group_label`,
`gitlab_project_label`, `gitlab_group_hook`, `gitlab_project_hook`, `gitlab_deploy_key`,
`gitlab_branch_protection`, `gitlab_tag_protection`, `gitlab_project_membership`, `gitlab_group_membership`,
`gitlab_project_share_group`, `gitlab_project_milestone`, `gitlab_group_ldap_link` (expected-absent on
CE/SaaS).

BEACHHEAD `gitlab_group` + `gitlab_project` + `gitlab_project_variable` (the group/project/variable core
essentially every GitLab org manages as IaC — `gitlab_group` and `gitlab_project` establish the **two
`membership=true` roots** and the **bare numeric-id (or path) import**, and `gitlab_project_variable`
establishes the **`GET /projects/<p>/variables` fan-out**, the **3-part `<project>:<key>:<environment_scope>`
composite** (the env-scope trap — the provider's defining import hazard), and the **paramount `value`-never-
decode scrub** — and this trio exercises the **`PRIVATE-TOKEN` custom-header client**, the **`GET /user`
preflight**, the **`X-Next-Page` offset pager**, and the **`list`-vs-`subList` systemic-count split** without
touching a single secret value) → INC-1 `gitlab_project_label` + `gitlab_project_hook` + `gitlab_deploy_key`
(the rest of the per-project 2-part composites — the **hook `token` write-only scrub** and the deploy-key
public-key note) → INC-2 `gitlab_group_variable` + `gitlab_group_label` + `gitlab_group_hook` +
`gitlab_group_membership` (the GROUP fan-out mirroring the project children — the second 3-part variable) →
INC-3 `gitlab_branch_protection` + `gitlab_tag_protection` + `gitlab_project_membership` +
`gitlab_project_share_group` + `gitlab_project_milestone` (the **branch/tag NAME-leaf** composites, the
**share-group single-object read** with no list endpoint, and the milestone id-vs-iid VERIFY) → INC-4
`gitlab_group_ldap_link` (the **4-part empty-segment composite** + the **Premium/admin 403→skip guard**) →
LATER the CI/CD-depth plane (pipeline schedules/triggers/environments), the MR/approval plane, the
integrations plane (scrub-heavy), the instance/admin-level resources, the user plane, and SAML/SCIM identity
links. **NEVER: the access-token / secret-minting resources — permanently excluded (they return a live
credential); and NEVER inline the token into `providers.tf` (Terraformer does — refuse it).**
