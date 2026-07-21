# Grafana provider — build spec

Research artifact for the `grafana` provider (Phase A scaffold). Sources: Terraformer's
`providers/grafana/` (the `grafana/grafana-api-golang-client` — "gapi" — based generators
for dashboards + folders), the `grafana/grafana` registry docs (import formats + nested
schema, **verified per-resource below against the provider repo's `docs/resources/*.md` on
`main`**), and the Grafana HTTP API (`https://<instance>/api/...`). Build mirrors the
**Datadog** provider (`internal/providers/datadog/`) — a flat, org-scoped, single-container
REST provider driven by a direct `net/http` client (NOT the GraphQL `newrelic` shape) —
with **two** wrinkles beyond Datadog:

1. **The API host is the operator's own instance, read from `GRAFANA_URL`.** This is the
   first provider whose base URL is *user-supplied* rather than a fixed vendor host (Datadog
   at least constrained `DD_HOST` to a known site list; Grafana can be `https://myorg.grafana.net`
   *or* `https://grafana.mycorp.internal`). The base URL must be parsed + validated, and the
   redirect-refusing client still applies.
2. **Org-scoped composite import IDs.** Unlike Datadog (every import a *bare* token), the
   current `grafana/grafana` provider prefixes almost every import ID with the org id —
   `{{ orgID }}:{{ uid }}`, `{{ orgID }}:{{ name }}`, and a *three-part*
   `{{ orgID }}:{{ folderUID }}:{{ title }}` for rule groups. This is the #1 hazard (§ its
   own section) and the reason enumeration must capture **both** the org id and each
   resource's uid/id.

## Version pin (load-bearing)

Pin `grafana/grafana ~> 3.x` (current major; org is lowercase `grafana`). The
**org-id-prefixed composite import IDs are a v2+ behaviour** and are the default the current
provider emits — do **not** copy Terraformer's bare-uid/bare-numeric-id import style (it
targets the pre-v2 provider). Naming facts that matter:
- Terraformer only ships generators for **`grafana_dashboard`** and **`grafana_folder`**;
  everything else in the catalog below is covered from the registry + the HTTP API directly.
- Terraformer emits `grafana_dashboard.folder = <folderID>` (the **legacy numeric** folder
  id). The current provider prefers **`folder = <folderUID>`** (uid). Emit the uid.
- **`grafana_alert_notification`** (legacy) is **deprecated** → superseded by
  `grafana_contact_point` (unified-alerting provisioning API). Emit `grafana_contact_point`;
  do not adopt the legacy notification channel.
- The HTTP API paths below are provider-version-independent.

## Shape

- **Base URL — user-supplied, from `GRAFANA_URL` (the hard divergence).** Grafana is
  self-hosted *or* Grafana Cloud, so the API host is the **operator's own instance**. Read
  `GRAFANA_URL` (the TF provider reads the same env var) — e.g. `https://myorg.grafana.net`
  or `https://grafana.mycorp.internal`. Validate it before first use (mirror datadog's
  `datadogBase`, but stricter since nothing is assumed):
  - Parse with `url.Parse`; require scheme ∈ {`http`,`https`} and a non-empty host.
  - Trim a single trailing `/`. **Reject a query string or fragment** ("no trailing junk").
  - **Allow a sub-path** — Grafana can be served under a `root_url` sub-path (e.g.
    `https://grafana.mycorp.internal/grafana`); API paths append to the full base, so keep
    the path. Store the resolved base once; every request is `base + "/api/..."`.
  - Reject an empty/malformed `GRAFANA_URL` in preflight (there is no default host to fall
    back to — this is the divergence from every prior provider).
- **Auth — dual Bearer/Basic, from `GRAFANA_AUTH` (the second divergence).** One env var,
  two schemes (the TF provider + Terraformer's gapi client read the same var):
  - **Token** (API key or **service-account token**, e.g. `glsa_…` / a JWT-ish `eyJ…`) →
    `Authorization: Bearer <token>`.
  - **Basic** `user:pass` → `Authorization: Basic base64(user:pass)`.
  - **Detection (mirror gapi):** `strings.SplitN(GRAFANA_AUTH, ":", 2)`; **two parts → Basic
    `user:pass`, else Bearer**. This is safe because Grafana tokens never contain a `:`
    (`glsa_`/base64url/JWT), so a colon unambiguously means basic creds. The token/password
    rides **only** on the `Authorization` header, **never** in errors/logs (redact like
    datadog's `redactURL` + keyless error bodies).
- **Org scope + the `X-Grafana-Org-Id` header.** Grafana has *orgs* inside an instance. The
  token is org-scoped (or, for basic auth / a multi-org token, the org is selected via the
  **`X-Grafana-Org-Id: <id>`** header). One flat container = the **current org**. Read
  `GRAFANA_ORG_ID` (optional; Terraformer defaults it to `1`); if set, send it as
  `X-Grafana-Org-Id` on every request. **Resolve the org via `GET /api/org` → `{id, name}`**
  — the numeric `id` is load-bearing: it is the `orgID` in every composite import ID below.
  `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- **Optional self-hosted TLS wrinkle.** A self-hosted instance behind a private CA / mTLS may
  need a custom root or client cert. The TF provider reads `GRAFANA_CA_CERT` /
  `GRAFANA_TLS_CERT` / `GRAFANA_TLS_KEY` / `GRAFANA_INSECURE_SKIP_VERIFY` (Terraformer reads
  the `HTTPS_*` equivalents). Support these as optional transport config; **do not** default
  to `InsecureSkipVerify` (opt-in only). Not needed for Grafana Cloud.
- **Redirect-refusing client (still applies).** A self-hosted instance behind an auth proxy
  can `302` to an SSO/login host; a bare `GRAFANA_URL` typo can 3xx to a marketing page. Go
  strips `Authorization` on a *cross-host* redirect but **not** the `X-Grafana-Org-Id` header
  and **not** a same-host scheme/port change — and silently following a redirect would decode
  a login-page HTML body as JSON. So **refuse redirects** (mirror `datadogHTTPClient`): the
  list endpoints answer `200` directly, so a 3xx is a hard, clearly-surfaced error.
- **Response families — four (classify per resource, § next section).**
  1. **Bare array** — body is a raw `[...]`. `GET /api/folders`, `/api/datasources`,
     `/api/playlists`, `/api/annotations`, all `/api/v1/provisioning/*` collections
     (contact-points, mute-timings, templates, alert-rules), `/api/access-control/roles`,
     `/api/reports`, `/api/orgs`. Unmarshal straight into `[]T`.
  2. **Keyed + paged object** — array under a named key with a `totalCount`:
     `GET /api/teams/search` → `{"teams":[...],"totalCount":n,"page":p,"perPage":pp}`,
     `GET /api/serviceaccounts/search` → `{"serviceAccounts":[...],"totalCount":n,...}`.
     `GET /api/library-elements` is **double-nested**:
     `{"result":{"elements":[...],"totalCount":n,"page":p,"perPage":pp}}`.
  3. **Search array** — `GET /api/search?type=dash-db` → a bare array of hits
     `[{"uid","title","folderUid","folderId","type"}]` (paged). The hit has **no dashboard
     model**; fetch the model per-uid via `GET /api/dashboards/uid/<uid>` (Terraformer does
     exactly this two-step).
  4. **Singleton object** — `GET /api/v1/provisioning/policies` returns a *single* object
     (the org's whole notification-policy tree), not a list.
- **Pagination — several mechanisms; per-resource (§ catalog).**
  - **Unpaged (one call returns everything):** `/api/datasources`, `/api/playlists`, every
    `/api/v1/provisioning/*`, `/api/access-control/roles`, `/api/reports`, `/api/orgs`.
  - **`/api/search`** (dashboards): `?type=dash-db&limit=<=5000&page=<n>`, **`page` is
    1-based**; stop on a short/empty page.
  - **`/api/folders`**: `?limit=<n>&page=<n>` (1-based; newer Grafana paginates folders);
    stop on a short page.
  - **`/api/teams/search`, `/api/serviceaccounts/search`**: `?perpage=<n>&page=<n>`
    (1-based); loop until `len(all) >= totalCount` (or a short page).
  - **`/api/library-elements`**: `?kind=1&perPage=<n>&page=<n>`; bound by `result.totalCount`.
  - Bound every loop defensively (`grafanaMaxPages`).
- **Status handling (mirror `datadogAPIError`):** 401 (token/basic invalid or expired) →
  fatal, surfaced in preflight/enumerate; 403 (org role lacks the scope, or Enterprise-only
  endpoint on OSS) → feature/permission absent → best-effort skip at Verbose; 404 on a
  collection (endpoint absent on this Grafana version / OSS-vs-Enterprise) → skip at Verbose;
  429 / 5xx / network → enumeration may be silently incomplete → Warn + count. Auth material
  never in errors/logs.
- **Preflight:** `terraform` present + `GRAFANA_URL` set **and** valid (http/https, host,
  no junk) + `GRAFANA_AUTH` set + `GET /api/org` (or `GET /api/user`) returns `200`. Unlike
  Datadog there is no `/validate` endpoint — a `200` from `/api/org` *is* the validation
  (it exercises both the URL and the auth in one call). No Grafana CLI dependency.
- **Connect:** resolve the flat org scope via `GET /api/org` → `{id, name}`. Set the single
  flat container **with `ID` = the numeric org id as a string** (this is the value reused as
  the `orgID` prefix in every import ID — see the classification section). `Name` = the org
  name (fall back to the id). No sub-account resolution: the token *is* the org.

## Org-scoped composite import IDs + response-shape — the CRITICAL classification

This is Grafana's analogue of Datadog's "which API version / shape / pager" call, but the
load-bearing per-resource fact is **the import-ID composite** (Datadog was all bare tokens;
Grafana is almost all composites). Get it wrong and `terraform apply` fails to import even
though enumeration succeeded. The rules:

- **The org id prefixes almost every import ID.** The current provider accepts a bare token
  *or* an `{{ orgID }}:`-prefixed one; the **prefixed form is what it emits and what we must
  generate** (a bare id lands the resource in the caller's *default* org, which may be wrong).
  Enumeration resolves the org id **once** (in Connect, from `GET /api/org`) and stores it as
  each resource's `Container`; `deriveImportID` prepends `r.Container + ":"`. So the composite
  is built at export time, not carried per-resource.
- **Three composite shapes, plus one bare exception:**
  - **`orgID:<token>`** (the norm) — where `<token>` is a **uid** (dashboard, folder,
    data_source, playlist, library_panel, role), a **name** (contact_point, message_template,
    mute_timing), a **numeric id** (team, service_account, annotation, report), or a **parent
    uid** (dashboard_permission → dashboardUID, folder_permission → folderUID).
  - **`orgID:folderUID:title`** (three-part) — **`grafana_rule_group` only.** Rule groups are
    identified by *folder uid + group title*, both prefixed by the org. This is the single
    three-token import in the catalog; get the separator count right.
  - **`orgID:<anyString>`** — **`grafana_notification_policy`** (singleton). The token after
    the org is arbitrary (the provider ignores it — there is one policy per org); emit a
    stable placeholder, e.g. `orgID:policy`.
  - **bare `id` (NO org prefix)** — **`grafana_organization`** only. It is *instance-scoped*
    (server-admin API); the id it imports by literally *is* an org id, so there is no prefix.
- **The token type varies per resource** (uid / name / numeric id / parent uid) — pinned in
  the catalog. Numeric ids (team, service_account, annotation, report) come off the wire as
  JSON numbers → **stringify** before building the composite.
- **`:` is the separator, and names are free text.** contact_point / message_template /
  mute_timing import by **name**, and rule_group by **title** — all user-authored strings
  that *could* contain a `:` and would then break the composite parse. This is a provider
  constraint (it `SplitN`s on `:`), not ours to fix; note it as a curation caveat and emit
  what the provider expects. Import IDs also carry template metacharacters (a rule-group
  title or contact-point name with `${…}`), so `deriveImportID` must **`EscapeHCLTemplate`
  the finished composite** (mirror datadog's `importid.go`).

## Enumeration spine

Flat org scope; **one parent fan-out only** (dashboards: `/api/search` → per-uid model
fetch — the Terraformer two-step; contrast Datadog's fully flat lists). Every list is a
single best-effort org-level call (Verbose + continue on 403/404; 401 fatal; other errors →
Warn + count), each tagged with its shape + pager per the catalog. The org id is fixed for
the whole run (resolved in Connect) and stamped as every resource's `Container`.

- **Bare array (unpaged):** `GET /api/datasources`, `GET /api/playlists`,
  `GET /api/v1/provisioning/contact-points`, `GET /api/v1/provisioning/mute-timings`,
  `GET /api/v1/provisioning/templates`, `GET /api/v1/provisioning/alert-rules` (**group the
  returned rules by `(folderUID, ruleGroup)` → one `grafana_rule_group` per pair**),
  `GET /api/access-control/roles` (**skip fixed/managed roles** — uid `fixed:*` or
  `global==true` built-ins; keep custom), `GET /api/reports`, `GET /api/annotations`.
- **Bare array (paged):** `GET /api/folders` (`?limit=&page=`, 1-based; **skip the "General"
  folder** — empty uid / id 0, not a real folder).
- **Search array (paged, then fan-out):** `GET /api/search?type=dash-db` (`?limit=<=5000&page=`,
  1-based) → for each hit `GET /api/dashboards/uid/<uid>` for the model.
- **Keyed + paged object:** `GET /api/teams/search` (`teams`, `?perpage=&page=`, bound by
  `totalCount`), `GET /api/serviceaccounts/search` (`serviceAccounts`, same pager),
  `GET /api/library-elements?kind=1` (`result.elements`, `?perPage=&page=`, bound by
  `result.totalCount`).
- **Singleton object:** `GET /api/v1/provisioning/policies` → the one notification-policy
  tree (emit a single `grafana_notification_policy`; **skip if the tree is empty/default**).
- **Companion permissions (parent-driven, no list-all):** `grafana_dashboard_permission` /
  `grafana_folder_permission` have **no collection endpoint** — derive them from the already-
  enumerated dashboards/folders (one permission resource per dashboard uid / folder uid; the
  ACL is fetched per parent via `GET /api/dashboards/uid/<uid>/permissions` /
  `GET /api/folders/<uid>/permissions` if we need to confirm non-empty). Fan out from the
  parents, not a standalone list.
- **Instance-scoped (server-admin, likely out of scope for an org-scoped token):**
  `GET /api/orgs` → `grafana_organization`. Requires server admin (basic auth admin); skip at
  Verbose on 403.

If nothing was found AND lists failed with real (non-401/403/404) errors, surface a systemic
failure rather than shipping an empty inventory (same guard as datadog's `enumerate.go`).

## Resource catalog

Import IDs **verified** against the current `grafana/grafana` registry docs
(`docs/resources/*.md` on `main`). All scope = org unless noted. "shape" = the response
family the enumeration endpoint uses. The `orgID` in every import ID is the numeric org id
resolved in Connect.

| native key | TF type | endpoint | shape | id/token | import ID |
|---|---|---|---|---|---|
| grafana:dashboard | grafana_dashboard | `GET /api/search?type=dash-db` → per-uid `GET /api/dashboards/uid/<uid>` | search array (paged) | `uid` | `{{orgID}}:{{uid}}` |
| grafana:folder | grafana_folder | `GET /api/folders` | bare array (paged) | `uid` | `{{orgID}}:{{uid}}` |
| grafana:data_source | grafana_data_source | `GET /api/datasources` | bare array | `uid` | `{{orgID}}:{{uid}}` |
| grafana:dashboard_permission | grafana_dashboard_permission | per-dashboard (fan-out from dashboards) | — | dashboard `uid` | `{{orgID}}:{{dashboardUID}}` |
| grafana:folder_permission | grafana_folder_permission | per-folder (fan-out from folders) | — | folder `uid` | `{{orgID}}:{{folderUID}}` |
| grafana:contact_point | grafana_contact_point | `GET /api/v1/provisioning/contact-points` | bare array | `name` | `{{orgID}}:{{name}}` |
| grafana:notification_policy | grafana_notification_policy | `GET /api/v1/provisioning/policies` | **singleton** | — | `{{orgID}}:{{anyString}}` (e.g. `orgID:policy`) |
| grafana:message_template | grafana_message_template | `GET /api/v1/provisioning/templates` | bare array | `name` | `{{orgID}}:{{name}}` |
| grafana:mute_timing | grafana_mute_timing | `GET /api/v1/provisioning/mute-timings` | bare array | `name` | `{{orgID}}:{{name}}` |
| grafana:rule_group | grafana_rule_group | `GET /api/v1/provisioning/alert-rules` (group by folderUID+group) | bare array | `folderUID`+`title` | **`{{orgID}}:{{folderUID}}:{{title}}`** (3-part) |
| grafana:team | grafana_team | `GET /api/teams/search` | keyed `teams` (paged) | `id` (int) | `{{orgID}}:{{id}}` |
| grafana:service_account | grafana_service_account | `GET /api/serviceaccounts/search` | keyed `serviceAccounts` (paged) | `id` (int) | `{{orgID}}:{{id}}` |
| grafana:playlist | grafana_playlist | `GET /api/playlists` | bare array | `uid` | `{{orgID}}:{{uid}}` |
| grafana:library_panel | grafana_library_panel | `GET /api/library-elements?kind=1` | keyed `result.elements` (paged) | `uid` | `{{orgID}}:{{uid}}` |
| grafana:annotation | grafana_annotation | `GET /api/annotations` | bare array | `id` (int) | `{{orgID}}:{{id}}` |
| grafana:role | grafana_role | `GET /api/access-control/roles` (skip fixed) | bare array | `uid` | `{{orgID}}:{{uid}}` — **Enterprise** |
| grafana:report | grafana_report | `GET /api/reports` | bare array | `id` (int) | `{{orgID}}:{{id}}` — **Enterprise** |
| grafana:organization | grafana_organization | `GET /api/orgs` (server-admin) | bare array | `id` (int) | `{{id}}` — **bare, instance-scope** |

### Import-format quirks (§ do not get wrong)
1. **Almost everything is an `{{orgID}}:` composite** — the inverse of Datadog. The org id
   comes from Connect (`GET /api/org`), stored as `Container`; the export builds
   `Container + ":" + <token>`. The bare-token form the provider *also* accepts is **not**
   what we emit (it defaults to the caller's active org).
2. **`grafana_rule_group` is the only three-part id:** `{{orgID}}:{{folderUID}}:{{title}}`.
   Two colons, three tokens. Rule groups are synthesised by grouping
   `/api/v1/provisioning/alert-rules` on `(folderUID, ruleGroup)`.
3. **`grafana_notification_policy` is a singleton** — one per org, import token after the org
   is arbitrary (`anyString`); emit `orgID:policy`. Do not try to enumerate more than one.
4. **`grafana_contact_point` / `grafana_message_template` / `grafana_mute_timing` import by
   NAME**; **`grafana_rule_group` by title.** These are free-text and could contain a `:`
   that breaks the provider's `SplitN` parse (a caveat to surface, not fixable here).
5. **`grafana_data_source` imports by `uid`** (not the numeric `id`), per the current docs —
   even though the API object has both. Store + emit the uid.
6. **`grafana_team` / `grafana_service_account` / `grafana_annotation` / `grafana_report`
   import by NUMERIC id** (stringify the JSON number before composing).
7. **`grafana_organization` is the lone bare import** (`{{id}}`, no org prefix) — it is
   instance-scoped (server-admin API), and the id it takes *is* an org id.
8. **`deriveImportID` must `EscapeHCLTemplate` the finished composite** — import IDs can hold
   `${…}`/`%{…}` (a rule-group title, a contact-point name), and `hcl.ImportBlock` renders
   with `%q`, which does not neutralise template metacharacters (mirror datadog's
   `importid.go`).

## Curation gotchas (Phase B, when live)

Confirmed shapes/gotchas to verify against real `terraform plan -generate-config-out` on a
live org — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets as below.
**`grafana_dashboard` is the heaviest curation surface** (the giant model JSON), the Grafana
analogue of `fastly_service_vcl` / `datadog_dashboard`.

- **`grafana_dashboard` — the big one.** `config_json` is the entire dashboard **model JSON**
  (Terraformer writes it to a `data/dashboard-<slug>.json` file and refs it via `file(...)`;
  the current provider takes an inline JSON string). **Template hazard:** the model is full of
  `${var}` / `[[var]]` dashboard-variable syntax and `$__rate_interval` / `${datasource}`
  macros → the generated HCL must keep these **literal** (`EscapeHCLTemplate` the blob;
  verify terraform's writer does the equivalent — this is the #1 thing to check on real
  output). Prune churny computed model fields (`id`, `version`, `iteration`, `dashboard_id`,
  `url`, `slug`); the provider ships a diff-suppress for these but generate-config-out may
  still emit them. `folder` refs the **folder uid**. Phase-B-heavy; treat Phase-A export as a
  breadth scaffold.
- **`grafana_folder`.** Light: `title` + `uid`. Prune computed `id`, `url`. **Skip the
  "General" folder** on enumeration (empty uid / id 0). Nested folders: `parent_folder_uid`
  refs another folder.
- **`grafana_data_source` — nested secrets.** `type`/`name`/`url`/`json_data_encoded` are the
  shell. **Write-only material** never returned by `GET /api/datasources` (Grafana redacts to
  a `secureJsonFields` bool map): `secure_json_data_encoded` (passwords, API keys, TLS client
  key), `http_headers`, `basic_auth_password`, `password` → scrub / re-supply out-of-band
  (see EXCLUDE). Import by `uid`; prune computed `id`.
- **`grafana_contact_point` — redacted secrets.** Nested per-type integration blocks
  (email/slack/pagerduty/webhook/opsgenie/…) share one `name`. The provisioning GET redacts
  secret settings to **`[REDACTED]`** (Slack `url`/`token`, PagerDuty `integrationKey`,
  webhook `password`/`authorization_credentials`) unless called with `?decrypt=true` (admin +
  a config flag) → the generated config would carry literal `[REDACTED]` → **scrub AND flag
  for re-supply**. `disable_resolve_message` is carried.
- **`grafana_notification_policy` — singleton tree.** The whole routing tree: nested `policy`
  blocks, `contact_point` refs (by name), `group_by`, `matcher`. One per org. No secrets;
  prune nothing sensitive. Adopting it means the org's *entire* alert routing is Terraform-
  owned — flag that.
- **`grafana_message_template` — pure template text.** `name` + `template` is a raw
  Go/Alertmanager template body (`{{ define }}`, `{{ .Labels }}`, `{{ range }}`) → the
  **worst literal-`{{…}}` hazard** in the provider; `EscapeHCLTemplate` is mandatory or the
  HCL breaks. Light otherwise.
- **`grafana_mute_timing`.** `name` + `intervals` (time ranges/weekdays/months). Light.
- **`grafana_rule_group` — synthesised + template-heavy.** `folder_uid` + `name` (group
  title) + nested `rule` blocks; each rule carries `data` (a query-model JSON with
  `$__interval`/`${…}` macros — same literal hazard), `for`, `condition`, `annotations`,
  `labels`. Built by grouping `/api/v1/provisioning/alert-rules` on `(folderUID, ruleGroup)`.
  Prune per-rule computed `uid`. Medium-heavy.
- **`grafana_team`.** `name` + `email` + members. Prune computed `id`/`team_id`.
  `team_sync` (Enterprise) is a nested block. Light.
- **`grafana_service_account`.** `name` + `role` + `is_disabled`. **No token** (tokens
  excluded — see EXCLUDE). Prune computed `id`. **Caution:** if `GRAFANA_AUTH` is a
  service-account token, that SA appears in the list — adopt it but **do not disable it**
  (same self-adoption note as datadog's own-user caution).
- **`grafana_playlist`.** `name` + `interval` + `item` blocks. Prune computed `id`; import by
  `uid`. Light.
- **`grafana_library_panel`.** `name` + `model_json` (panel model JSON — same template hazard
  as dashboards) + `folder_uid`. Prune computed `id`, `version`. Medium.
- **`grafana_annotation` — data-plane-ish.** `text` + `dashboard_uid`/`time`. `/api/annotations`
  also returns *runtime* alert-state annotations, not just user-authored ones → low IaC
  value; adoption churns. Prune computed `id`. Later/optional.
- **`grafana_role` (Enterprise, IAM-ish breadth).** `name` + `uid` + `permissions` blocks.
  **Skip fixed/managed roles** (uid `fixed:*` / built-in `global` roles) — only custom roles
  are adoptable. Prune computed `version`, `id`. Enterprise-only endpoint (403/404 on OSS →
  skip).
- **`grafana_report` (Enterprise).** `name` + `dashboard_uid` + `schedule` + `recipients`.
  Prune computed `id`. Enterprise-only.
- **`grafana_organization` (instance-scope).** `name` + `admins`/`editors`/`viewers`. Needs
  server admin; bare-`id` import. Likely out of scope for an org-scoped token (below).

## Write-only / secret resources (EXCLUDE)

Grafana's secrets live in the credential/integration settings — exclude these (surface,
re-supply out-of-band), exactly like datadog's api_key / integration plane:
- **`grafana_service_account_token`** — the token `key` is write-only (returned **once** at
  creation, never on read) → **exclude entirely.** This is the actual secret material; adopt
  the parent `grafana_service_account` identity but never its tokens. (Same rule as
  `datadog_api_key`/`datadog_application_key`.)
- **`grafana_data_source` secure fields** — `secure_json_data_encoded` (DB passwords, cloud
  API keys, TLS client key), `http_headers`, `basic_auth_password`, `password`: Grafana never
  returns these (redacts to `secureJsonFields`), so they cannot be reverse-engineered →
  **scrub / re-supply.** Adopt the connection shell only. (The provider also splits secret
  config into **`grafana_data_source_config`** / `grafana_data_source_config_lbac_rules` —
  those secret-only companions are excluded.)
- **`grafana_contact_point` settings secrets** — Slack `url`/`token`, PagerDuty
  `integration_key`, webhook `password`/`authorization_credentials`, etc.: returned as
  `[REDACTED]` → scrub the redacted values and flag for re-supply (do **not** write
  `[REDACTED]` into applied config).
- **Grafana Cloud stack credentials** — `grafana_cloud_stack_service_account_token`,
  `grafana_cloud_api_key` (deprecated), `grafana_cloud_access_policy_token`: write-only cloud
  tokens on the **stack-management plane** (grafana.com API, a *different* credential than
  `GRAFANA_AUTH`) → out of scope entirely (below), and their secrets excluded.
- `GRAFANA_AUTH` (token or basic password) itself is never logged or echoed into config —
  the provider reads it from env (mirror datadog's providers.tf note).

## Deliberately out of scope
- **Grafana Cloud stack-management plane** (`grafana_cloud_stack`,
  `grafana_cloud_stack_service_account`(`_token`), `grafana_cloud_access_policy`(`_token`),
  `grafana_cloud_plugin_installation`, `grafana_cloud_org_member`, …) — a **separate API**
  (`grafana.com`) with a **separate Cloud API-key credential**, not the instance API driven by
  `GRAFANA_URL`/`GRAFANA_AUTH`. Different creds, different host → out of scope.
- **Synthetic Monitoring** (`grafana_synthetic_monitoring_check`/`_probe`/`_installation`) —
  the SM plugin API + its own access token. Separate creds → out of scope.
- **Grafana OnCall** (`grafana_oncall_*`: integrations, routes, schedules, escalation chains)
  — the OnCall API + its own token. Separate creds → out of scope.
- **SLO / Machine Learning plugins** (`grafana_slo`, `grafana_machine_learning_*`) — plugin
  APIs; dedicated later increments at best, not core config.
- **Instance-scoped / server-admin resources** (`grafana_organization`,
  `grafana_organization_preferences`, `grafana_user` (global users), `grafana_sso_settings`)
  — these need server-admin (basic-auth admin), operate on the *instance* not a single org,
  and fight an org-scoped token. Modeled at the edge (organization enumerated best-effort,
  bare import) but out of scope for the org-scoped beachhead; SSO/global-user depth is
  `Capabilities.IAM=false`.
- **Cloud-IAM depth**: team/service-account are breadth (no secrets); `grafana_team_external_group`,
  SAML/SCIM, and per-resource RBAC assignments (`grafana_role_assignment`,
  `grafana_*_permission_item`) are not (Enterprise, assignment-graph heavy).
- **Data planes**: dashboard-rendered panels, metric/query data, alert instances + state,
  bulk annotation events — the DATA behind the config, per scope.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `grafana_dashboard` + `grafana_folder` (what essentially every Grafana user manages
as IaC; folder is the parent container for dashboards, dashboard is the heaviest curation
surface — the model-JSON blob + the `${var}`/`[[var]]` template-escaping work — and the pair
exercises the `orgID:uid` composite, the `/api/search`→per-uid two-step, and paged
`/api/folders`) → INC-1 `grafana_data_source` + `grafana_folder_permission` +
`grafana_dashboard_permission` (the config companions; data source introduces the
secure-field scrub, permissions introduce the parent fan-out with no list-all) → INC-2
(unified alerting, provisioning API) `grafana_contact_point` + `grafana_notification_policy`
(singleton) + `grafana_message_template` + `grafana_mute_timing` + `grafana_rule_group` (the
whole `/api/v1/provisioning/*` family in one increment — the singleton, the `[REDACTED]`
secret-scrub, the `{{…}}`-template message bodies, and the three-part
`orgID:folderUID:title` composite) → INC-3 (IAM-ish + extras) `grafana_team` +
`grafana_service_account` (numeric-id composites; SA excludes tokens; self-adoption caution)
+ `grafana_playlist` + `grafana_library_panel` → INC-4 / LATER `grafana_annotation`
(data-plane-ish, low value), `grafana_role` + `grafana_report` (Enterprise — gated on an
Enterprise instance), `grafana_organization` (instance-scope, server-admin, bare import) →
OUT the Grafana Cloud stack plane, Synthetic Monitoring, OnCall, SLO/ML plugins (separate
APIs/creds).
