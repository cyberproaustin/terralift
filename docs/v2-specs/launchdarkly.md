# LaunchDarkly provider — build spec

Research artifact for the `launchdarkly` provider (Phase A scaffold; TF provider source is
`launchdarkly/launchdarkly`, product "LaunchDarkly" — the feature-flag / experimentation
platform). Sources: Terraformer's `providers/launchdarkly/` (built on the
`launchdarkly/api-client-go` Go SDK — only `project` / `featureFlag`(+`feature_flag_environment`)
/ `segment` generators), the `launchdarkly/launchdarkly` registry docs (import formats +
schema, **verified per-resource below** against the provider's `docs/resources/*.md`), and
the LaunchDarkly REST API v2 (`https://app.launchdarkly.com/api/v2/…`). Build mirrors the
**Honeycomb** provider (`internal/providers/honeycomb/`) — a flat, single-container REST
provider whose spine is a **per-parent FAN-OUT** — and the **Opsgenie**
(`internal/providers/opsgenie/`) + **Okta** (`internal/providers/okta/`) providers for the
**host-validated server-supplied next-URL pager** and the **bare / 2-part / 3-part composite
import** machinery. This is **REST, Honeycomb/Opsgenie-style, NOT GraphQL.** LaunchDarkly is
to *projects/environments* what Honeycomb is to *datasets*: most resources are enumerated via
a `GET /api/v2/projects` (parent) → per-project (and, for env-scoped resources, per
project×environment) fan-out. Five facts set it apart from every prior provider, all
load-bearing and called out below:

1. **Auth is a RAW token on the `Authorization` header — NO scheme prefix.** LaunchDarkly
   sends `Authorization: <LAUNCHDARKLY_ACCESS_TOKEN>` — the token **directly** as the header
   value: **not** `Bearer <token>`, **not** `Token token=…`, **not** `GenieKey `/`SSWS `,
   **not** a custom `X-…-Key` header. Getting a scheme prefix *added* is a silent 401. Read
   from `LAUNCHDARKLY_ACCESS_TOKEN`.
2. **The base URL is a fixed vendor host** — `https://app.launchdarkly.com`, all paths under
   `/api/v2/` — with a federal/custom override (`LAUNCHDARKLY_API_HOST`), not a region table.
3. **The spine is a project→environment FAN-OUT** — `GET /api/v2/projects` (parent) → per
   project the environments/flags/metrics; and for **env-scoped** resources (segments,
   destinations, flag-environments) a **two-level** project×environment fan-out. One flat
   container = the account.
4. **Pagination is HATEOAS `_links.next.href`** — a **server-supplied next URL** (a relative
   PATH *or* a full URL) that must be **host-validated before the token is re-sent** (the
   Opsgenie `paging.next` / Okta `Link`-header lesson).
5. **Composite import IDs go up to THREE `/`-separated parts, and the key ORDER is not
   uniform** — `feature_flag_environment` is `<project>/<env>/<flag>` (env in the **middle**),
   which does **not** match its own `flag_id` attribute (`<project>/<flag>`). The separator,
   the part-count, and the key order are the #1 hazard.

## Version pin (load-bearing)

Pin `launchdarkly/launchdarkly ~> 2.x` (current major; org is lowercase `launchdarkly`,
product "LaunchDarkly"). Naming facts that matter (the Terraformer-vs-current divergences):

- **Terraformer's coverage is a tiny subset.** Its generator covers only `project`,
  `feature_flag` (+ the per-flag `feature_flag_environment` fan-out), and `segment` — via the
  `launchdarkly/api-client-go` SDK. It does **not** cover `environment` (as a standalone
  resource), `webhook`, `team`, `custom_role`, `destination`, or `metric`. Those are covered
  here from the registry + REST API directly (as we did for the Honeycomb/Okta resources
  Terraformer lacked). **Do NOT pull the `launchdarkly/api-client-go` SDK** the way
  Terraformer did — a raw `net/http` client is smaller and matches the Honeycomb/Opsgenie/
  Okta providers (a deliberate non-adoption, same call as dropping the Honeycomb/Okta SDKs).
- **`launchdarkly_environment` is BOTH a nested block on `launchdarkly_project` AND a
  standalone resource — VERIFY which to emit.** The provider lets you declare environments
  inline inside `launchdarkly_project { environments { … } }` *or* as separate
  `launchdarkly_environment` resources, but **not both for the same env** (a known conflict —
  an env managed inline cannot also be a standalone resource). Terraformer emits `project`
  with environments *implicit* and never emits a standalone `launchdarkly_environment`.
  **Phase A emits standalone `launchdarkly_environment` resources** (cleaner adoption, one
  resource per env, and it anchors the env-scoped fan-out) and imports the bare
  `launchdarkly_project` **without** inline environments — VERIFY the project resource does not
  also try to manage the same envs (else a plan conflict). This is the LaunchDarkly analogue
  of Opsgenie's "team roster is inline, not a resource" call, inverted.
- **`launchdarkly_feature_flag_environment` is a SEPARATE resource, not part of the flag.**
  `launchdarkly_feature_flag` manages the flag *definition* (key, name, `variations`,
  `defaults`, `temporary`, `tags`); the per-environment targeting/rules/fallthrough/prerequisites
  live in the distinct `launchdarkly_feature_flag_environment` resource (one per flag ×
  environment). Confirmed against both Terraformer (`loadFeatureFlagEnv` emits
  `launchdarkly_feature_flag_environment` per env) and the registry. Emit both.
- **Optional but recommended: pin `LD-API-Version`.** The REST API is date-versioned via an
  `LD-API-Version: <YYYYMMDD>` header (Terraformer sends `20191212`; newer dates exist). Send
  a pinned version header so the response contract does not drift under us — **VERIFY the
  current recommended version at build**; absent the header the API uses the token's default
  version. Not a resource; a client stability knob.
- Terraformer reads `LAUNCHDARKLY_ACCESS_TOKEN`. The **TF provider** reads the *same*
  `LAUNCHDARKLY_ACCESS_TOKEN` (config `access_token`) plus `LAUNCHDARKLY_API_HOST` (config
  `api_host`) for the base host. The REST endpoints below are provider-version-independent.

## Shape

- **Auth — a RAW token on the `Authorization` header, NO scheme prefix (the hard
  divergence).** Every request carries:
  - `Authorization: <LAUNCHDARKLY_ACCESS_TOKEN>` — the token is the **entire** header value.
    **Do NOT prepend `Bearer `, `Token token=`, `GenieKey `, or `SSWS `**, and do NOT move it
    to a custom header. This is the opposite trap from Opsgenie/Okta/PagerDuty (where a
    literal prefix is required) — here **any** prefix is wrong. Getting it wrong is a silent
    401 (`{"message":"invalid access token","code":"unauthorized"}`).
  - `LD-API-Version: <YYYYMMDD>` (recommended, see version pin) + `Content-Type:
    application/json` (harmless on GET).
  Read the token from `LAUNCHDARKLY_ACCESS_TOKEN`. A direct `net/http` client (mirror
  `honeycombapi.go` / `opsgenieapi.go`); **no LaunchDarkly CLI**, and **no**
  `launchdarkly/api-client-go`. The token rides **only** on the `Authorization` header,
  **never** in the URL, errors, or logs (same discipline as the GenieKey / SSWS / DD-API-KEY).
  Force `https`; **refuse redirects** (mirror `ogHTTPClient` / `oktaHTTPClient`) so the token
  cannot be replayed to another host on a 3xx.
- **Base URL — a fixed vendor host, with a federal/custom override (must be read from env).**
  Default `https://app.launchdarkly.com`; **all** API paths are under `/api/v2/`. There is no
  region table — but a **federal / dedicated / custom** instance is reachable via
  `LAUNCHDARKLY_API_HOST` (the TF provider's `api_host`; e.g. the FedRAMP host
  `https://app.launchdarkly.us`). Read `LAUNCHDARKLY_API_HOST`, default `app.launchdarkly.com`,
  **force https**, strip any scheme/slashes a user pastes, store the resolved base host once,
  and **host-validate every `_links.next` follow against it** before re-sending the token
  (mirror `isOpsgenieURL` → `isLaunchDarklyURL`). The redirect-refusing client still applies.
- **Scope — one LaunchDarkly account = one flat container.** The access token is
  **account-scoped**; there is no sub-account and no multi-account resolution — the token
  simply **is** the account. `model.ScopeTenant`. **Projects are a fan-out KEY, not a
  container hierarchy** — like Honeycomb's datasets, the projects/environments live *under*
  the one account container, so `Capabilities.Hierarchy` stays **false** (the fan-out is an
  enumeration detail, not a scope tree). Resolve the container id/name **best-effort** via a
  lightweight `GET /api/v2/members/me` (returns the caller's `{_id, email, role}` — no account
  *name* is exposed, so use the email/host as the display id) or, if that is gated, `GET
  /api/v2/projects?limit=1` for validation; fall back to the API host string. As flat as
  Opsgenie/Honeycomb: the token is the account, no id lookup required.
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Response family — ONE dominant shape: the `{items, _links, totalCount}` envelope (the key
  structural fact).** Almost every list endpoint returns:
  ```
  { "items": [ … ], "_links": { "self": {"href":…}, "next": {"href":"/api/v2/…"} }, "totalCount": n }
  ```
  — the array lives under a fixed **`items`** key (not a resource-named key), and the pager is
  a **HATEOAS `_links.next.href`** cursor: follow it until **absent** (single-page endpoints
  simply omit `_links.next`). Implement one generic **items/`_links.next`** helper (the whole
  client surface for the common case) that fetches the first page with `?limit=<N>` and then
  loops on `_links.next.href`. A handful of singletons (`GET /api/v2/members/me`, a single
  `GET …/flags/<proj>/<flag>`) return a **bare object**, not an envelope — decode separately.
- **`_links.next.href` is a SERVER-SUPPLIED URL — host-validate it before re-sending the token
  (do not miss this).** Unlike a pager where *we* build every URL from `base()+path`,
  LaunchDarkly hands back the next cursor in `_links.next.href`, which is **usually a
  base-relative PATH** (e.g. `/api/v2/flags/my-project?limit=20&offset=20`) but **may be a
  full URL**. We then issue a GET to it with the raw `Authorization` token, and this is **not**
  an HTTP redirect (`CheckRedirect` never fires, Go does not strip the header), so a next-link
  pointing at another host would leak the account token. **Resolve `href` against the
  configured base if it is relative, then validate the resulting scheme+host** (mirror
  Opsgenie's `isOpsgenieURL` and Okta's `rel="next"` host-check): require `https` and host ==
  the resolved base host (`app.launchdarkly.com` or the `LAUNCHDARKLY_API_HOST` value); refuse
  and error otherwise. This is the single most important safety rule of the client.
- **Pagination — `_links.next` everywhere; a few endpoints are single-page.** The
  `_links.next` helper handles both uniformly (single-page responses omit it). `limit`/`offset`
  are also accepted on the first page; `totalCount` bounds the loop defensively (`ldMaxPages`).
  Empirically **paged and potentially large:** `flags/<proj>` (a big account has **thousands**
  of flags per project — see scale note), `segments/<proj>/<env>`, `members`, `teams`,
  `projects`. Usually single-page but **always honour `_links.next` if present:**
  `environments`, `metrics/<proj>`, `webhooks`, `roles`, `destinations`. **VERIFY** the max
  `limit` per endpoint at build; treat every list as *potentially* paged so a large account is
  never truncated.
- **Scale — the feature-flag / flag-environment plane is HUGE (flag it).** Feature flags are
  the dominant object type: a mature account routinely has **hundreds to thousands of flags
  per project**, and each flag has **one `feature_flag_environment` per environment**. So the
  flag-environment plane is `flags × environments` — **tens of thousands** of resources on a
  big account (the largest inventory this provider produces by far, dwarfing every other
  type). Two consequences: (a) enumeration of flag-environments should read the **`environments`
  map already embedded in each flag object** from `GET /api/v2/flags/<proj>` rather than a
  per-flag `GET …/flags/<proj>/<flag>` call (Terraformer does a per-flag call — a rate-limit
  hazard at scale; prefer the embedded map, VERIFY it carries every env); (b) expect to brush
  the rate limit on a bulk flag enumeration (see below). Segments (`projects × environments`)
  are the second multiplier.
- **Rate limiting is per-token and per-route (flag it).** LaunchDarkly returns `429` with
  `X-Ratelimit-Route-Limit` / `X-Ratelimit-Reset` (epoch-**milliseconds**) and `Retry-After`.
  A bulk flag/segment enumeration WILL hit it. Read these off the response and back off; bound
  + honour the reset. Do not hammer.
- **Status handling (mirror `list` / `honeycombAPIError`).** LaunchDarkly errors are
  `{"message":…,"code":…}` (HTTP status carries the meaning). **401** (`unauthorized`, bad/
  revoked token) → fatal, surfaced in preflight; if it appears mid-run every remaining list
  fails too → treat as fatal, not a partial inventory. **403** (`forbidden` — the token's role
  lacks the read, e.g. a reader token or a member without project access) → best-effort
  Verbose skip. **404** (project/env absent, or a feature not on the plan — teams/destinations
  are Enterprise) → Verbose skip. **429** → rate-limited → honour `X-Ratelimit-Reset` /
  `Retry-After` and back off. **5xx / network** → enumeration may be silently incomplete →
  Warn + count (tell a systemic failure apart from an empty account). The token never appears
  in errors/logs; strip any query string before a URL is logged (belt-and-suspenders, mirror
  `redactURL`).
- **Preflight**: `terraform` present + `LAUNCHDARKLY_ACCESS_TOKEN` set + a lightweight
  authenticated call succeeds. Use `GET /api/v2/members/me` (any token can read its own
  member) as the validation probe, falling back to `GET /api/v2/projects?limit=1`. `members/me`
  doubles as the identity probe (email/role) so later 403 skips are explained rather than
  surprising.
- **Connect**: no real resolution — the token is the account. Validate the probe succeeds,
  resolve a display id from `members/me` (email; else the API host string), and set the single
  flat container (`model.ScopeTenant`).

## Project/environment FAN-OUT scope + composite import DEPTH — the CRITICAL determination

This is LaunchDarkly's analogue of Honeycomb's "dataset-scoped vs env-wide + composite-vs-bare"
call, Okta's "composite depth", and Opsgenie's "bare-vs-slash + parent identifier" call. The
load-bearing per-resource facts are **(a) is the resource ACCOUNT-wide, PROJECT-scoped
(one-level fan-out via `GET /api/v2/projects`), or ENV-scoped (two-level project×environment
fan-out); (b) is the import id a BARE key, a 2-part `/` composite, or a 3-part `/` composite —
and, for composites, what is the exact key ORDER; and (c) every `_links.next` follow is
host-validated.** Get (a) wrong and you fan out to the wrong endpoint (or never reach the
env-scoped resources); get (b) wrong and every import block for that type is un-importable.
All are **verified against the registry `docs/resources/*.md`** and pinned per-resource in the
catalog. The rules:

- **ACCOUNT-wide (flat list, no fan-out) → BARE import id.** `launchdarkly_webhook`
  (`GET /api/v2/webhooks`, id `<webhook_id>` — a 24-hex object id), `launchdarkly_team`
  (`GET /api/v2/teams`, id `<team_key>`), `launchdarkly_custom_role` (`GET /api/v2/roles`, id
  `<role_key>`). These are the Honeycomb "env/team-wide bare-import" analogue. `launchdarkly_project`
  is also account-wide but is the **fan-out parent** (imported by the bare `<project_key>`).
- **PROJECT-scoped (one-level fan-out) → 2-part `<project_key>/<key>` import id.**
  `launchdarkly_feature_flag` (`GET /api/v2/flags/<project_key>`, import `<project_key>/<flag_key>`),
  `launchdarkly_metric` (`GET /api/v2/metrics/<project_key>`, import `<project_key>/<metric_key>`),
  and `launchdarkly_environment` (`GET /api/v2/projects/<project_key>/environments`, import
  `<project_key>/<env_key>`). First `GET /api/v2/projects` (the parent list), then per project
  the sub-list — exactly Honeycomb's `GET /1/datasets` → per-dataset fan-out.
- **ENV-scoped (TWO-level project×environment fan-out) → 3-part composite import id.** The
  subtle plane: these need **both** a project AND an environment key, so the fan-out is
  `projects → per project its environments → per (project, env) the sub-list`:
  - `launchdarkly_segment` (`GET /api/v2/segments/<project_key>/<env_key>`, import
    **`<project_key>/<env_key>/<segment_key>`**).
  - `launchdarkly_destination` (`GET /api/v2/destinations/<project_key>/<env_key>` — or the
    account-wide `GET /api/v2/destinations`; VERIFY, below — import
    **`<project_key>/<env_key>/<destination_id>`**).
  - `launchdarkly_feature_flag_environment` — the flag's per-env targeting, derived from each
    flag's embedded `environments` map (flag × env), import
    **`<project_key>/<env_key>/<flag_key>`**.
- **The 3-part key ORDER is the #1 trap — env is in the MIDDLE, and it does not match the
  resource's own attributes.** All three 3-part ids are `<project>/<env>/<leaf>`. But
  `launchdarkly_feature_flag_environment` is the killer: its **import id** is
  `<project>/<env>/<flag>` (env in the middle), while its **`flag_id` attribute** is
  `<project>/<flag>` (no env) and its **`env_key` attribute** is the env — i.e. the import
  composite interleaves the flag's own composite attribute with the env. Terraformer confirms
  the import id `projectKey + "/" + envKey + "/" + flagKey` while setting `flag_id =
  projectKey + "/" + flagKey`. Encode the three parts + the order per TF type in `importid.go`;
  never infer them, and never reuse the `flag_id` attribute as the import id.

The account-vs-project-vs-env scope (which drives the fan-out depth), the composite depth
(bare / 2-part / 3-part), and the 3-part key order are the things we cannot get wrong —
enumerated per-resource in the catalog and re-verified against the registry docs at build.
Encode the import id as an explicit per-TF-type switch in `importid.go` (mirror Okta's
`rawImportID` depth switch and Opsgenie's parent-identifier switch) — never infer the
separator, the part-count, or the key order.

## Enumeration spine

Flat account scope. The spine is a **project fan-out**: `GET /api/v2/projects` (parent) → per
project the environments/flags/metrics; and a **second-level** per-(project, environment)
fan-out for segments/destinations, plus the flag×env derivation for flag-environments. Best-
effort per list (403 role-absent / 404 feature-or-plan-absent → Verbose skip; 401 → fatal;
other → Warn + count, so a systemic failure is told apart from an empty account). Each list is
tagged with its envelope + pager per the catalog. The token never appears in errors/logs;
every `_links.next` follow is host-validated first. (Mirror `honeycomb/enumerate.go`: a
top-level `list` helper owns the systemic-failure count; a `subList` helper for the fan-out
does not, since sub-lists multiply by project/env count.)

- **Parent:** `GET /api/v2/projects` → `items[{key, name}]` (paged). Each `key` becomes a
  `launchdarkly_project` (bare import `<project_key>`), and the `key` is the fan-out key for
  every sub-list below. Optionally `?expand=environments` embeds each project's environments
  in one pass (avoids the per-project environments call — VERIFY it returns the full env list).
- **Per project `<proj>` (one-level fan-out):**
  - `GET /api/v2/projects/<proj>/environments` → `items[{key, name, apiKey, mobileKey,
    _links…}]` → `launchdarkly_environment` (import `<proj>/<env_key>`). **Capture each env
    `key`** — it is the second fan-out key for segments/destinations/flag-environments.
    **Scrub `apiKey`/`mobileKey`/`clientSideId`** (SDK keys — see EXCLUDE).
  - `GET /api/v2/flags/<proj>` → `items[{key, name, environments:{<envKey>:{…}}}]` (paged;
    **note the path is `/api/v2/flags/<proj>`, NOT `/api/v2/projects/<proj>/flags`**) →
    `launchdarkly_feature_flag` (import `<proj>/<flag_key>`). For **each** flag, read its
    embedded `environments` map → one `launchdarkly_feature_flag_environment` per env
    (import `<proj>/<envKey>/<flag_key>`) — the flag×env multiplier (see scale note). Prefer
    the embedded map over a per-flag `GET …/flags/<proj>/<flag>` call.
  - `GET /api/v2/metrics/<proj>` → `items[{key, name}]` → `launchdarkly_metric`
    (import `<proj>/<metric_key>`). Experimentation feature.
- **Per (project `<proj>`, environment `<env>`) (two-level fan-out):**
  - `GET /api/v2/segments/<proj>/<env>` → `items[{key, name}]` (paged) →
    `launchdarkly_segment` (import `<proj>/<env>/<segment_key>`).
  - `GET /api/v2/destinations/<proj>/<env>` → `items[{_id, name, kind}]` →
    `launchdarkly_destination` (import `<proj>/<env>/<destination_id>`). **VERIFY** whether the
    canonical list is per-(proj,env) or the account-wide `GET /api/v2/destinations` (which
    returns each destination's project/env in its body); either yields the same 3-part import.
    Data Export is an Enterprise feature (404 on lower plans → Verbose skip). **Config carries
    credentials → scrub** (see EXCLUDE).
- **Account-wide flat lists (no fan-out):**
  - `GET /api/v2/webhooks` → `items[{_id, name, secret}]` → `launchdarkly_webhook` (bare import
    `<webhook_id>`). **`secret` is signing material → scrub.**
  - `GET /api/v2/teams` → `items[{key, name}]` (paged) → `launchdarkly_team` (bare import
    `<team_key>`). Enterprise (404 → Verbose skip).
  - `GET /api/v2/roles` → `items[{key, name}]` (paged) → `launchdarkly_custom_role` (bare
    import `<role_key>`).

If nothing was found AND lists failed with real (non-403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as the Honeycomb/Opsgenie/Okta
`enumerate.go`).

## Resource catalog

Import IDs verified against the current `launchdarkly/launchdarkly` registry docs
(`docs/resources/*.md`). All scope = account. "endpoint → shape" is the list path and its
envelope (`items` + `_links.next` unless noted). "scope / fan-out" names the fan-out depth
(account-wide / project / project×env). The **sep** column is the #1 hazard —
**bare / 2-part / 3-part**, all `/`.

| native key | TF type | endpoint → shape | scope / fan-out | id field | import ID | sep |
|---|---|---|---|---|---|---|
| launchdarkly:project | launchdarkly_project | `GET /api/v2/projects` → `items` (paged) | account-wide (parent) | `key` | `<project_key>` | **bare** |
| launchdarkly:environment | launchdarkly_environment | `GET /api/v2/projects/<proj>/environments` → `items` | ← project | `key` | `<project_key>/<env_key>` | **2-part** (SDK keys SECRET) |
| launchdarkly:feature_flag | launchdarkly_feature_flag | `GET /api/v2/flags/<proj>` → `items` (paged) | ← project | `key` | `<project_key>/<flag_key>` | **2-part** |
| launchdarkly:feature_flag_environment | launchdarkly_feature_flag_environment | flag's embedded `environments{}` map (flag × env) | ← project → flag × env | `env_key`+`flag_key` | `<project_key>/<env_key>/<flag_key>` | **3-part** (env in MIDDLE) |
| launchdarkly:segment | launchdarkly_segment | `GET /api/v2/segments/<proj>/<env>` → `items` (paged) | ← project × env | `key` | `<project_key>/<env_key>/<segment_key>` | **3-part** |
| launchdarkly:destination | launchdarkly_destination | `GET /api/v2/destinations/<proj>/<env>` (or account-wide) | ← project × env | `_id` | `<project_key>/<env_key>/<destination_id>` | **3-part** (config SECRET; Enterprise) |
| launchdarkly:metric | launchdarkly_metric | `GET /api/v2/metrics/<proj>` → `items` | ← project | `key` | `<project_key>/<metric_key>` | **2-part** |
| launchdarkly:webhook | launchdarkly_webhook | `GET /api/v2/webhooks` → `items` | account-wide | `_id` | `<webhook_id>` | **bare** (`secret` SECRET) |
| launchdarkly:team | launchdarkly_team | `GET /api/v2/teams` → `items` (paged) | account-wide | `key` | `<team_key>` | **bare** (Enterprise) |
| launchdarkly:custom_role | launchdarkly_custom_role | `GET /api/v2/roles` → `items` (paged) | account-wide | `key` | `<role_key>` | **bare** |

### Import-format quirks (§ do not get wrong)
1. **Composite DEPTH tracks the fan-out depth — bare / 2-part / 3-part, all SLASH.** Every
   LaunchDarkly composite uses a forward slash `/`. **Account-wide** resources import **bare**
   (`<project_key>` / `<webhook_id>` / `<team_key>` / `<role_key>`). **Project-scoped**
   resources are **2-part** (`<project_key>/<key>`): environment, feature_flag, metric.
   **Env-scoped** resources are **3-part** (`<project_key>/<env_key>/<leaf>`): segment,
   destination, feature_flag_environment. Encode the part-count per TF type in `importid.go`;
   never infer it from the object.
2. **The 3-part ORDER is `<project>/<env>/<leaf>` — env in the MIDDLE — and
   `feature_flag_environment` does NOT reuse its own composite attribute.** The import id is
   `<project_key>/<env_key>/<flag_key>`, but the resource's `flag_id` attribute is
   `<project_key>/<flag_key>` (no env) and `env_key` is separate. **Do not** build the import
   id by appending the env to `flag_id` (that would give `<project>/<flag>/<env>` — wrong
   order) — build it as `project`, then `env`, then `flag`. This is the single most
   error-prone id in the provider (Terraformer: `projectKey+"/"+envKey+"/"+flagKey`).
3. **`launchdarkly_environment` imports 2-part `<project_key>/<env_key>`, not bare** — even
   though an environment key is unique within its project, the import composite carries the
   project prefix (the env is project-scoped). Store both keys off the environment object.
4. **The BARE-import resources key on different id fields.** `project`/`team`/`custom_role`
   import by their human **`key`** (a slug you set); `webhook` imports by its server-assigned
   **`_id`** (a 24-hex object id, NOT a key). `destination` likewise keys on the server
   **`_id`** as its leaf. Copy the right field: `key` for project/team/role/segment/flag/metric/
   environment leaves, `_id` for webhook and destination.
5. **All keys/ids are opaque strings off the wire** — no numeric stringify step (unlike
   Datadog's numeric monitor ids). Copy the field verbatim. Keys are user-chosen slugs
   (`[a-z0-9._-]`), so template-escape on emit for safety (mirror the other providers'
   `EscapeHCLTemplate`), even though they rarely contain `${…}`.
6. **`launchdarkly_project` must be imported WITHOUT inline environments in Phase A.** Because
   an env can be managed inline on the project *or* as a standalone `launchdarkly_environment`
   (not both), emit the project as a bare shell and let the standalone environment resources
   own the envs — VERIFY the generated project config does not also declare `environments {}`
   blocks for the same envs (else a plan conflict). See version-pin note.

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a
live account — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the
Honeycomb/Okta providers. LaunchDarkly has **no single monster resource** (contrast Datadog's
`datadog_dashboard`); the weight is spread across the flag/flag-environment plane (by
*volume*, not depth), and the recurring hazards are the **environment SDK keys**, the
**destination config credentials**, the **webhook secret**, and the sheer **scale** of the
flag×env plane.

- **`launchdarkly_feature_flag` — light per-resource, heavy by volume.** `key`, `name`,
  `variations` (typed value list — bool/string/number/json), `variation_type`, `defaults`
  (on/off variation indexes), `temporary`, `tags`, `maintainer_id`. Terraformer's exact
  quirk to reproduce: **`IgnoreKeys "include_in_snippet"`** — the deprecated
  `include_in_snippet` over-emits / conflicts with `client_side_availability` (prune it).
  Prune computed `_id`/timestamps. The variation `value` for JSON flags is a raw JSON string
  → keep literal (template-escape hazard). No secret.
- **`launchdarkly_feature_flag_environment` — the volume driver; targeting tree.** Per flag ×
  env: `on`, `fallthrough` (variation or rollout), `off_variation`, `targets` (individual
  user/context-key targeting), `rules` (clause trees — `attribute`/`op`/`values`),
  `prerequisites`, `context_targets`. This is the largest count in the inventory (see scale);
  each is individually small but the plan will be enormous. Prune computed `_id`. No secret,
  but the `targets`/`rules` can carry user keys (PII-ish — adopt, do not over-scrub). Rule
  ordering is significant — preserve it.
- **`launchdarkly_environment` — SECRET (scrub the SDK keys).** `key`, `name`, `color`,
  `default_ttl`, `secure_mode`, `default_track_events`, `require_comments`,
  `confirm_changes`, `tags`. **Computed secrets returned on read → scrub:** `api_key` (the
  **server-side SDK key** — a live credential that reads/streams all flag data), `mobile_key`
  (mobile SDK key), and `client_side_id` (client SDK id — lower-sensitivity but scrub for
  safety). These are the LaunchDarkly analogue of the Opsgenie `api_integration.api_key` /
  Okta hook `value` scrub — the provider's defining secret. **VERIFY** whether the TF resource
  exposes these as writable/computed (they are Sensitive/computed in the schema); keep the
  block, scrub the values, re-supply/rotate out-of-band.
- **`launchdarkly_segment` — medium; targeting.** `key`, `name`, `description`, `tags`,
  `included`/`excluded` (explicit context keys), `rules` (clause trees, same as flag rules),
  `unbounded` (big-segment flag). Prune computed `_id`/`creation_date`. No secret; context
  keys are user identifiers (adopt).
- **`launchdarkly_metric` — light; experimentation.** `key`, `name`, `kind` (`pageview`/
  `click`/`custom`), `event_key`, `is_active`, `is_numeric`, `unit`, `success_criteria`,
  `selector`/`urls` (for click/pageview). Prune computed. No secret.
- **`launchdarkly_webhook` — SECRET (scrub).** `url`, `name`, `on`/`enabled`, `tags`,
  `statements` (policy filter), and **`secret`** — the HMAC signing secret LaunchDarkly uses
  to sign webhook payloads → **scrub the value**, keep the block. The `url` is not itself
  secret (unless it embeds a token — flag if it does). Import by the bare `_id`.
- **`launchdarkly_destination` — SECRET (scrub the config; Enterprise).** `project_key`,
  `env_key`, `name`, `kind` (`kinesis`/`google-pubsub`/`mparticle`/`segment`/`azure-event-hubs`),
  `enabled`, and a **`config` map that carries the sink credentials** per kind: mParticle
  (`api_key`, `secret`), Segment (`write_key`), Azure Event Hubs (`namespace`/`policy_key`),
  Kinesis (a role ARN — not itself a secret but scope-sensitive), Pub/Sub (project/topic).
  **Scrub the credential fields in `config`**, keep the block. This is the second most
  important scrub after the environment SDK keys.
- **`launchdarkly_team` — light; IAM-ish breadth (Enterprise).** `key`, `name`, `description`,
  `member_ids` (member-id refs), `maintainers`, `custom_role_keys` (role refs),
  `role_attributes`. **Team membership** (`member_ids`) is carried on the team (like Opsgenie's
  inline roster) — VERIFY whether the provider splits members into a separate
  `launchdarkly_team_members` resource in the current version. No secret. **CAUTION:** your own
  member (behind the token) may appear — adopt but do not lock yourself out.
- **`launchdarkly_custom_role` — light; IAM-ish breadth.** `key`, `name`, `description`,
  `policy`/`policy_statements` (resource/action/effect trees — the LaunchDarkly RBAC grammar),
  `base_permissions`. The policy statements reference resources by LaunchDarkly's
  `proj/*:env/*:flag/*` resource-specifier grammar → keep literal (a `*`/`:`-heavy string;
  template-escape hazard is low but VERIFY the writer does not choke). No secret.
- **`launchdarkly_project` — light shell.** `key`, `name`, `tags`, `default_client_side_availability`,
  `include_in_snippet` (deprecated — same `IgnoreKeys` prune as the flag). Emit as a bare shell
  WITHOUT inline `environments {}` in Phase A (standalone `launchdarkly_environment` resources
  own the envs — the plan-conflict note above). Prune computed `_id`.

Until Phase B these are no-ops, so a LaunchDarkly export is a breadth scaffold, not yet
plan-clean (the pipeline's repo-wide secret scan is the backstop for the environment SDK keys,
destination config, and webhook secret that generate-config-out emits before the scrub rules
land).

## Write-only / secret resources (EXCLUDE / scrub)

The credential/integration plane is where LaunchDarkly's secrets live — scrub the value (keep
the block, re-supply out-of-band) or exclude the resource, exactly like Honeycomb's
`api_key` / Opsgenie's `api_integration.api_key` / Okta's hook `value` / Datadog's
`datadog_api_key`:

- **`launchdarkly_access_token` — EXCLUDE entirely.** The token *value* (`token`) is
  **write-only** (returned once at creation, never on read) — it is the actual master
  credential material (the same kind of token as the `LAUNCHDARKLY_ACCESS_TOKEN` the client
  itself uses). There is nothing round-trippable to adopt; surface it, adopt/rotate
  out-of-band. This is the LaunchDarkly analogue of `datadog_api_key` / `honeycombio_api_key`.
- **`launchdarkly_environment` SDK keys — SCRUB (the notable one).** `api_key` (server-side
  SDK key), `mobile_key`, and `client_side_id` are **computed credentials returned on read**
  that read/stream all flag evaluations for the environment → **scrub the values**, keep the
  environment block. The SDK key is the single most commonly-leaked LaunchDarkly secret; do
  not emit it into generated config or state comments. (If a future provider version marks them
  purely computed/read-only and they never appear in generate-config-out, nothing to do — but
  VERIFY, and scrub if present.)
- **`launchdarkly_destination.config` credentials — SCRUB.** The `config` map carries the sink
  credentials per `kind` — mParticle `api_key`/`secret`, Segment `write_key`, Azure Event Hubs
  `policy_key`, etc. → scrub the credential fields, keep the block. Data Export is Enterprise;
  the whole plane is 404 on lower plans.
- **`launchdarkly_webhook.secret` — SCRUB.** The HMAC signing secret → scrub the value, keep
  the webhook block. The callback `url` is not itself secret.
- **Not secret, do not over-scrub:** `launchdarkly_feature_flag` / `_feature_flag_environment`
  targeting (context/user keys are identifiers, not credentials — adopt), `launchdarkly_segment`
  included/excluded keys (identifiers), `launchdarkly_metric` (event config), `launchdarkly_custom_role`
  policy statements (RBAC grammar, no secret), `launchdarkly_team` member refs (ids, not
  secrets). The `client_side_id` is technically a public identifier used in browser SDKs, but
  scrub it with the other env keys for a conservative default.
- **The provider `access_token` (`LAUNCHDARKLY_ACCESS_TOKEN`) itself** — the account-wide
  master credential; it lives **only** on the raw `Authorization` header, never in generated
  config, state comments, errors, or logs.

## Deliberately out of scope
- **`launchdarkly_access_token`** — write-only token value, EXCLUDE (above); no round-trippable
  material. Surface as a gap; author/rotate out-of-band.
- **Relay Proxy config / `launchdarkly_relay_proxy_configuration`** — a Relay Proxy auto-config
  key resource whose `full_key` is write-only (a secret returned once) → excluded with the
  access-token plane; a later increment at best.
- **Approvals / workflows / release pipelines** (`launchdarkly_flag_trigger` — carries a
  write-only trigger `url`; approval-request and workflow objects) — a separate governance
  plane, several carrying write-only trigger URLs; later increment.
- **Audit log / flag evaluation / experiment results / usage DATA planes** — flag evaluation
  events, experiment results, audit-log entries, and Data Export *stream contents* are runtime
  DATA behind the config, per scope. Out of scope (config only).
- **Team/member IAM depth** (`Capabilities.IAM=false`) — `launchdarkly_team` /
  `launchdarkly_custom_role` are modeled at breadth, but individual `launchdarkly_team_member`/
  member management, SCIM/SSO provisioning, and account-level role assignments are not.
- **Context kinds / big-segment (unbounded) backing stores** — `launchdarkly_context_kind`
  (a small config object) and the unbounded-segment external store are adjacent surfaces; a
  small later increment (`context_kind`) / out of scope (backing store is infra).
- **Inline environments on `launchdarkly_project`** — Phase A emits standalone
  `launchdarkly_environment` resources; the inline `environments {}` block on the project is a
  deliberate non-adoption (the two conflict; see version pin + quirks).
- **LaunchDarkly SDK dependency** — Terraformer pulls `launchdarkly/api-client-go`; TerraLift
  uses a raw `net/http` client (smaller, matches Honeycomb/Opsgenie/Okta). A deliberate
  non-adoption.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `launchdarkly_project` + `launchdarkly_environment` + `launchdarkly_feature_flag`
(the feature-flag core essentially every LaunchDarkly account manages as IaC — project is the
bare fan-out parent, environment establishes the per-project fan-out + the 2-part
`<project>/<env>` composite **and** the SDK-key **secret-scrub** (the provider's defining
secret), and feature_flag establishes the `GET /api/v2/flags/<proj>` project fan-out + the
2-part `<project>/<flag>` import and the `include_in_snippet` prune) → INC-1
`launchdarkly_feature_flag_environment` (the flag×env volume plane — establishes the flag's
embedded `environments` map derivation, the **3-part** `<project>/<env>/<flag>` import with env
in the MIDDLE, and the targeting/rules tree; the largest inventory this provider produces) →
INC-2 `launchdarkly_segment` (the two-level project×environment fan-out and the 3-part
`<project>/<env>/<segment>` import — the segment targeting tree) → INC-3
`launchdarkly_webhook` + `launchdarkly_custom_role` (the account-wide bare-import plane — the
webhook `secret` scrub and the RBAC policy grammar) → INC-4 `launchdarkly_metric` +
`launchdarkly_team` (experimentation metrics — project fan-out, 2-part — and the Enterprise
team plane — account-wide bare `<team_key>` with the inline member roster and the self-adoption
caution) → INC-5 `launchdarkly_destination` (Data Export — Enterprise, the second two-level
project×environment fan-out, the 3-part `<project>/<env>/<destination_id>` import keyed on the
server `_id`, and the **config-credential scrub** per sink kind) → LATER/BLOCKED
`launchdarkly_access_token` + `launchdarkly_relay_proxy_configuration` (write-only secrets,
EXCLUDE), `launchdarkly_flag_trigger` / approvals / workflows (governance plane, write-only
trigger urls), `launchdarkly_context_kind`, the inline-project-environments non-adoption, and
the audit-log / evaluation / experiment-results data planes.
