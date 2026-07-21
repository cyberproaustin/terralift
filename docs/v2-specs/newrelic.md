# New Relic provider — build spec

Research artifact for the `newrelic` provider (Phase A scaffold). Sources: Terraformer's
`providers/newrelic/` (which uses the legacy `newrelic-client-go` REST-ish clients — a
*deprecated-resource* reference only, see Version pin), the `newrelic/newrelic` registry
docs (import formats verified per-resource below against the provider repo's
`website/docs/r/*.html.markdown`), and the NerdGraph GraphQL schema
(`https://api.newrelic.com/graphql`). Build mirrors the flat, single-container shape of the
Datadog (`internal/providers/datadog/`) and Fastly providers — one scope, no parent
fan-out, `terraform plan -generate-config-out` for export — but with **one divergence that
touches every line of the client**:

## THE BIG DIVERGENCE — NerdGraph is a GraphQL API, not REST

Every provider built so far (Fastly, DigitalOcean, Datadog, Linode, NS1, Vultr…) talks to a
family of REST **GET list** endpoints and classifies the *response envelope*. New Relic has
**no such endpoints**. There is exactly **one** URL — the NerdGraph GraphQL endpoint — and
you POST a query to it. This inverts the client design:

- **The client is a single `nerdgraph(ctx, query, vars)` call, not a set of GET helpers.**
  It POSTs `{"query": "...", "variables": {...}}` (JSON body) to the region endpoint and
  decodes `{"data": {...}, "errors": [...]}`. Make it a package var (like Datadog's
  `datadogDo`) so tests substitute a fake. There is no per-resource path/shape/pager matrix
  the way Datadog had v1-bare / v1-keyed / v2-JSON:API — there is **one** transport and the
  per-resource variety lives entirely in the *query text* and the *response tree path*.
- **A 200 with a non-empty `errors` array is a FAILURE.** This is the single most important
  client rule and the thing that has no analogue in the REST providers. NerdGraph answers
  HTTP 200 for query-level errors and even for **partial** failures (`{"data": {…partial…},
  "errors": [{…}]}`). The client MUST inspect `errors` on every 200 and not trust `data`
  alone. Classify by `errors[].extensions.errorClass` (see Shape → status handling).
- **Pagination is NerdGraph's `nextCursor`, not `?page=`/`?offset=`.** List fields return
  `{ results/entities: [...], nextCursor: "..." }`; you resend the SAME query with the
  `cursor` variable set to the previous `nextCursor` and loop until `nextCursor` is null/
  empty. This is the New Relic analogue of Datadog's `fastlyListPaged`, but the cursor is a
  *value you echo back to the one fixed endpoint*, never a server-supplied follow-URL — so
  (unlike Datadog's v2 next-links) the auth header never risks travelling to another host.
- **The cursor lives at a different tree depth per query** (under `results` for
  entitySearch, under `policiesSearch`/`nrqlConditionsSearch`/`destinations`/`channels`/
  `workflows` for the dedicated queries). The pagination helper must be told the JSON path
  to the array and to the cursor, not assume a fixed envelope.

Everything below is downstream of this.

## Version pin (load-bearing)

Pin `newrelic/newrelic ~> 3.x` (current major; the org is `newrelic`, lowercase). Naming
facts that matter — **Terraformer's generators emit the deprecated resources; do not copy
them**:
- Terraformer emits **`newrelic_alert_channel`** (deprecated). The current
  alerting-notification stack is **`newrelic_notification_destination` +
  `newrelic_notification_channel` + `newrelic_workflow`**. Emit those three; drop
  `newrelic_alert_channel`.
- Terraformer emits **`newrelic_alert_condition`** (legacy APM condition) and
  **`newrelic_infra_alert_condition`** (legacy infra). The current alert condition is
  **`newrelic_nrql_alert_condition`**. Emit the NRQL condition; the two legacy ones are out
  of scope.
- Terraformer has no `newrelic_one_dashboard` generator (it predates it) and no generator
  for the current notification stack, workloads, service levels, obfuscation, key
  transactions, or drop rules — all covered here from NerdGraph + the registry directly.
- Terraformer's `newrelic_synthetics_monitor` uses the legacy REST synthetics API and emits
  a single flat monitor. The current provider splits synthetics into **six** typed
  resources (`_monitor` / `_script_monitor` / `_cert_check_monitor` /
  `_broken_links_monitor` / `_step_monitor`, plus `_private_location`) — see the CRITICAL
  classification.
- **`newrelic_dashboard`** (deprecated) → emit **`newrelic_one_dashboard`**.
- **`newrelic_nrql_drop_rule`** is **deprecated and scheduled for removal 2026-06-30**
  (superseded by `newrelic_pipeline_cloud_rule`). Given the removal date it may already be
  gone from newer provider builds — include it for back-compat but see build order/out of
  scope; prefer `newrelic_pipeline_cloud_rule` going forward.

The NerdGraph queries below are provider-version-independent (they hit the API directly).

## Shape

- **Auth — a User key on the `API-Key` header (the divergence from Datadog's two headers).**
  New Relic needs exactly one secret:
  - `API-Key: <NEW_RELIC_API_KEY>` — a **User key** (not a License key, not an Ingest key).
    The header name is literally `API-Key`.
  Plus `Content-Type: application/json`. Two non-secret inputs complete the triple:
  - **Account** via `NEW_RELIC_ACCOUNT_ID` (an integer; passed as a GraphQL `Int` variable,
    not string-concatenated into the query).
  - **Region** via `NEW_RELIC_REGION` (`US` default, or `EU`) — this selects the base URL,
    below.
  A direct `net/http` client that POSTs (mirror the structure of `datadogDo`, but one
  header and `http.MethodPost` with a body); no New Relic CLI. The TF provider reads the
  **same three** env vars (`NEW_RELIC_API_KEY` / `NEW_RELIC_ACCOUNT_ID` /
  `NEW_RELIC_REGION`). The key is only ever on the `API-Key` header — never in a query
  string, an error, or a log line. (The query body is static GraphQL and carries no secret,
  so it is safe to log at Verbose; the header is not.)
- **Region → base URL — must be read from env; there are exactly two data-center bases:**
  | `NEW_RELIC_REGION` | GraphQL endpoint |
  |---|---|
  | `US` (default) | `https://api.newrelic.com/graphql` |
  | `EU` | `https://api.eu.newrelic.com/graphql` |
  Resolve once and store. Every request — the probe, every list, every cursor follow — POSTs
  to this one URL; there are no per-resource paths and no server-supplied next-links, so the
  host is fixed for the whole run. Still refuse HTTP redirects (belt-and-suspenders so the
  `API-Key` header can never be replayed to another host). A FedRAMP/gov endpoint exists but
  is out of scope.
- **Scope: a single New Relic account = one flat container.** The `NEW_RELIC_ACCOUNT_ID`
  names exactly one account; there is no sub-account fan-out and no multi-account resolution
  in this provider. One flat container = the account (`model.ScopeTenant`, ID = the account
  id, Name = the resolved account name). `Capabilities{IAM:false, Exposure:false,
  Hierarchy:false}`. (New Relic *does* have a parent/sub-account hierarchy at the org level,
  but this provider is pinned to one account id, so `Hierarchy:false`.)
- **Response handling — one envelope, checked two ways.** Every call decodes
  `{"data": json.RawMessage, "errors": []nerdgraphErr}`. Rules:
  1. Transport error / HTTP ≥ 400 (a bad key can yield 401/403 at the HTTP layer) → hard
     error carrying the status.
  2. HTTP 200 **but `errors` non-empty** → a NerdGraph error. **Do not** silently use
     `data`. Classify by `errors[0].extensions.errorClass`:
     - `UNAUTHORIZED` / `FORBIDDEN` → the key is bad or lacks the product/account → on the
       preflight probe this is **fatal**; on a per-resource list it means the product/
       permission is absent → best-effort **skip at Verbose** (the New Relic analogue of
       Datadog's 403/404 skip).
     - `TIMEOUT` / `INTERNAL_SERVER_ERROR` / `SERVER_ERROR` (and HTTP 5xx / network) →
       enumeration may be silently incomplete → **Warn + count** (tell a systemic failure
       apart from an empty account).
     - Rate limiting (NerdGraph enforces a per-request timeout and a query-cost/rate limit;
       surfaces as an errorClass or HTTP 429) → back off and retry, bounded.
  3. **The `account: null` subtlety.** An authenticated key that simply *cannot see* the
     requested account returns `{"data":{"actor":{"account":null}}}` with **no** `errors`
     entry. Preflight must treat a null `account` as fatal ("key valid but no access to
     account <id>"), not as an empty result.
- **Preflight**: `terraform` present + `NEW_RELIC_API_KEY` set + `NEW_RELIC_ACCOUNT_ID` set
  and parseable as an int + region resolves. Then **two probes in one query** (NerdGraph
  lets you select both at once, saving a round-trip):
  `{ actor { user { email } account(id: $acct) { name } } }`.
  - `actor.user.email` non-empty → the key is a valid User key.
  - `actor.account.name` non-null → the key can see the account; capture it as the container
    name. `account == null` → fatal (see the subtlety above).
- **Connect**: no real resolution — the account id simply *is* the scope. Confirm the probe,
  set the single flat container (id = `NEW_RELIC_ACCOUNT_ID`, name = the resolved account
  name).
- **Query hygiene / cost.** NerdGraph rate-limits and times out expensive queries, so batch:
  fetch `obfuscationRules` + `obfuscationExpressions` in one `logConfigurations` selection;
  fetch the user-email + account-name probe in one query. Pass the entitySearch query string,
  the account id, and the cursor as **GraphQL variables** (`$query: String!`, `$acct: Int!`,
  `$cursor: String`) — never string-concatenate the account id or a name into the query text
  (injection + quoting hygiene, and it keeps the query static/cacheable/loggable).

## Query-source + discriminator + import-key determination — the CRITICAL classification

This is New Relic's analogue of Datadog's "which API version / which envelope / which pager."
Because *every* resource arrives through a handful of shared NerdGraph queries rather than a
dedicated endpoint, the load-bearing per-resource facts are different — and getting any of
the three wrong silently corrupts the inventory:

1. **Query source — entitySearch vs a dedicated query (and WHERE its cursor is).** Two
   families:
   - **entitySearch** — `actor { entitySearch(query: $q) { results { entities { guid name
     entityType ... } nextCursor } } }`. Cursor is `results.nextCursor`. Surfaces everything
     that is a first-class *entity*: dashboards, all synthetics monitors, workloads, key
     transactions, service-level-bearing entities, private locations. Filtered by an NRQL-ish
     `query` string that MUST pin `accountId = <acct>` (else it searches every account the
     key can see).
   - **dedicated queries** — alert policies (`alerts.policiesSearch`), NRQL conditions
     (`alerts.nrqlConditionsSearch`), muting rules (`alerts.mutingRules`), notification
     destinations/channels (`aiNotifications.destinations` / `.channels`), workflows
     (`aiWorkflows.workflows`), drop rules (`nrqlDropRules.list`), obfuscation
     rules/expressions (`logConfigurations`), secure credentials. Each nests differently and
     each **puts its cursor in a different place** (or has *no* cursor — muting rules, drop
     rules, obfuscation, and the `mutingRules` list return everything in one shot). Decode
     the wrong tree path and you get zero results with no error.
2. **Discriminator — several TF resources come out of ONE query and must be split by a
   field.** This is the New Relic-specific hazard with no Datadog analogue:
   - **Synthetics: ONE entitySearch → SIX TF resources.** `domain = 'SYNTH' AND type =
     'MONITOR'` returns every monitor; the `monitorType` field on the
     `SyntheticMonitorEntityOutline` is the ONLY thing that maps it to the right resource:
     `SIMPLE`/`BROWSER` → `newrelic_synthetics_monitor`, `SCRIPT_API`/`SCRIPT_BROWSER` →
     `newrelic_synthetics_script_monitor`, `CERT_CHECK` → `newrelic_synthetics_cert_check_monitor`,
     `BROKEN_LINKS` → `newrelic_synthetics_broken_links_monitor`, `STEP_MONITOR` →
     `newrelic_synthetics_step_monitor`. Get it wrong and six resource types collapse into one
     wrong type → every generated resource diffs on apply.
   - **Dashboards: ONE entitySearch returns dashboards AND their pages.** `type = 'DASHBOARD'`
     yields both top-level dashboards and each of their per-page child entities (pages are
     also `DASHBOARD` entities). Keep only the parents — filter `dashboardParentGuid IS NULL`
     — because `newrelic_one_dashboard` owns the pages as nested blocks; adopting a page as
     its own resource is wrong.
   - **NRQL conditions: the `type` field is PART OF THE IMPORT ID.** `nrqlConditionsSearch`
     returns each condition's `type` (`STATIC`/`BASELINE`); this is not informational — the
     import composite is `<policy_id>:<condition_id>:<static|baseline>` (lower-cased). Miss it
     and the import block is unusable.
   - **Obfuscation: ONE `logConfigurations` query yields rules AND expressions** — two
     distinct TF types (`newrelic_obfuscation_rule`, `newrelic_obfuscation_expression`) split
     out of one response.
3. **Import-key shape — five distinct forms; this is the one thing we cannot get wrong.**
   New Relic mixes bare GUIDs, bare numeric/string ids, and 2- and 3-part composites, and
   the composites are **not consistently ordered**:
   - **bare entity GUID**: `newrelic_one_dashboard`, all five synthetics monitor resources,
     `newrelic_synthetics_private_location`, `newrelic_key_transaction`,
     `newrelic_entity_tags`.
   - **bare id (numeric or string, no account)**: `newrelic_workflow`,
     `newrelic_notification_destination`, `newrelic_notification_channel`,
     `newrelic_obfuscation_rule`, `newrelic_obfuscation_expression`.
   - **`<account_id>:<id>` (account FIRST)**: `newrelic_alert_muting_rule`,
     `newrelic_nrql_drop_rule`.
   - **`<id>:<account_id>` (account SECOND — the odd one out)**: `newrelic_alert_policy`. This
     is the *reverse* order of the muting-rule/drop-rule composites — the single easiest
     import to get backwards.
   - **3-part composite**: `newrelic_nrql_alert_condition` = `<policy_id>:<condition_id>:<type>`;
     `newrelic_service_level` = `<account_id>:<sli_id>:<guid>`; `newrelic_workload` =
     `<account_id>:<workload_id>:<guid>`. The last two need an id that entitySearch does NOT
     hand back in the entity outline (the numeric `workloadId` / the `sli_id`) — see quirks.
   - **secret key name**: `newrelic_synthetics_secure_credential` = the credential `key` (the
     *value* is write-only → excluded).

## Enumeration spine

Flat account scope; **no parent fan-out** (contrast Fastly's per-service loop). Every
resource is one best-effort account-level NerdGraph query (Verbose skip on
`UNAUTHORIZED`/`FORBIDDEN` = product absent; Warn+count on transient), each tagged with its
query source + cursor location + discriminator per the catalog. All entitySearch queries pin
`accountId = <acct>`.

- **entitySearch** (cursor = `results.nextCursor`; loop until null):
  - `type = 'DASHBOARD'` → **keep only `dashboardParentGuid IS NULL`** (drop page children)
    → `newrelic_one_dashboard`.
  - `domain = 'SYNTH' AND type = 'MONITOR'` → split by `monitorType` into the five monitor
    resources (see CRITICAL §2).
  - `type = 'WORKLOAD'` → `newrelic_workload` (then resolve `workloadId` per entity, below).
  - `type = 'KEY_TRANSACTION'` → `newrelic_key_transaction`.
  - service-level-bearing entities → `newrelic_service_level` (SLIs are reached through the
    owning entity's `serviceLevel { indicators { id guid } }` — see quirks; hardest to
    enumerate, later increment).
- **dedicated queries** (cursor location varies — noted per row):
  - `actor { account(id:$acct) { alerts { policiesSearch(cursor:$c) { policies { id name
    incidentPreference } nextCursor } } } }` → `newrelic_alert_policy`. Cursor under
    `policiesSearch`.
  - `... alerts { nrqlConditionsSearch(cursor:$c) { nrqlConditions { id name policyId type }
    nextCursor } }` → `newrelic_nrql_alert_condition`. Cursor under `nrqlConditionsSearch`;
    `type` → import discriminator.
  - `... alerts { mutingRules { id name } }` → `newrelic_alert_muting_rule`. **No cursor**
    (full list).
  - `... aiNotifications { destinations(cursor:$c) { entities { id name type } nextCursor } }`
    → `newrelic_notification_destination`. Cursor under `destinations`.
  - `... aiNotifications { channels(cursor:$c) { entities { id name type destinationId }
    nextCursor } }` → `newrelic_notification_channel`. Cursor under `channels`.
  - `... aiWorkflows { workflows(cursor:$c) { entities { id name } nextCursor } }` →
    `newrelic_workflow`. Cursor under `workflows` (note the extra `aiWorkflows` nesting).
  - `... logConfigurations { obfuscationRules { id name } obfuscationExpressions { id name }
    }` → `newrelic_obfuscation_rule` + `newrelic_obfuscation_expression` in ONE call. **No
    cursor**.
  - `... nrqlDropRules { list { rules { id description action nrql } } } }` →
    `newrelic_nrql_drop_rule`. **No cursor** (deprecated — see out of scope).
  - secure credentials (keys/metadata only; **value never returned**) →
    `newrelic_synthetics_secure_credential` — **EXCLUDED** (secret); enumerate at most to
    surface, never to adopt the value.
  - private locations → a dedicated synthetics NerdGraph query (verify the exact field path
    against the live schema — private locations are not `entitySearch` entities); import by
    GUID. Later increment.

If nothing was found AND lists failed with real (non-`UNAUTHORIZED`/`FORBIDDEN`) errors,
surface a systemic failure rather than shipping an empty inventory (same guard as
`enumerate.go`).

## Resource catalog

Import IDs verified against the current `newrelic/newrelic` registry docs
(`website/docs/r/*.html.markdown`, quoted). "source" = the NerdGraph query family; "cursor"
= where the pagination cursor lives (— = unpaged single list).

| native key | TF type | source (NerdGraph) | cursor | import ID |
|---|---|---|---|---|
| newrelic:dashboard | newrelic_one_dashboard | entitySearch `type='DASHBOARD'` (parent only) | `results.nextCursor` | `<guid>` |
| newrelic:alert_policy | newrelic_alert_policy | `alerts.policiesSearch` | `policiesSearch.nextCursor` | `<policy_id>:<account_id>` **(account SECOND)** |
| newrelic:nrql_alert_condition | newrelic_nrql_alert_condition | `alerts.nrqlConditionsSearch` | `nrqlConditionsSearch.nextCursor` | `<policy_id>:<condition_id>:<static\|baseline>` |
| newrelic:alert_muting_rule | newrelic_alert_muting_rule | `alerts.mutingRules` | — | `<account_id>:<muting_rule_id>` |
| newrelic:notification_destination | newrelic_notification_destination | `aiNotifications.destinations` | `destinations.nextCursor` | `<destination_id>` (org scope: `<id>:ORGANIZATION:<org_id>`) |
| newrelic:notification_channel | newrelic_notification_channel | `aiNotifications.channels` | `channels.nextCursor` | `<channel_id>` |
| newrelic:workflow | newrelic_workflow | `aiWorkflows.workflows` | `workflows.nextCursor` | `<workflow_id>` |
| newrelic:synthetics_monitor | newrelic_synthetics_monitor | entitySearch `domain='SYNTH' type='MONITOR'`, `monitorType∈{SIMPLE,BROWSER}` | `results.nextCursor` | `<guid>` |
| newrelic:synthetics_script_monitor | newrelic_synthetics_script_monitor | same search, `monitorType∈{SCRIPT_API,SCRIPT_BROWSER}` | `results.nextCursor` | `<guid>` |
| newrelic:synthetics_cert_check_monitor | newrelic_synthetics_cert_check_monitor | same search, `monitorType=CERT_CHECK` | `results.nextCursor` | `<guid>` |
| newrelic:synthetics_broken_links_monitor | newrelic_synthetics_broken_links_monitor | same search, `monitorType=BROKEN_LINKS` | `results.nextCursor` | `<guid>` |
| newrelic:synthetics_step_monitor | newrelic_synthetics_step_monitor | same search, `monitorType=STEP_MONITOR` | `results.nextCursor` | `<guid>` |
| newrelic:synthetics_private_location | newrelic_synthetics_private_location | dedicated synthetics query (verify path) | verify | `<guid>` |
| newrelic:synthetics_secure_credential | newrelic_synthetics_secure_credential | dedicated (keys only) | — | `<key>` **(value WRITE-ONLY → EXCLUDE)** |
| newrelic:workload | newrelic_workload | entitySearch `type='WORKLOAD'` + per-entity `workloadId` | `results.nextCursor` | `<account_id>:<workload_id>:<guid>` |
| newrelic:service_level | newrelic_service_level | entitySearch + owner's `serviceLevel.indicators` | `results.nextCursor` | `<account_id>:<sli_id>:<guid>` |
| newrelic:nrql_drop_rule | newrelic_nrql_drop_rule | `nrqlDropRules.list` | — | `<account_id>:<rule_id>` **(DEPRECATED, removal 2026-06-30)** |
| newrelic:entity_tags | newrelic_entity_tags | entities' `tags` (companion) | — | `<guid>` |
| newrelic:key_transaction | newrelic_key_transaction | entitySearch `type='KEY_TRANSACTION'` | `results.nextCursor` | `<guid>` |
| newrelic:obfuscation_rule | newrelic_obfuscation_rule | `logConfigurations.obfuscationRules` | — | `<rule_id>` (bare numeric) |
| newrelic:obfuscation_expression | newrelic_obfuscation_expression | `logConfigurations.obfuscationExpressions` | — | `<expression_id>` (bare numeric) |

### Import-format quirks (§ do not get wrong)
1. **Composite ordering is INCONSISTENT — this is the #1 hazard.** `newrelic_alert_policy`
   puts the account **second** (`<policy_id>:<account_id>`, verified example
   `23423556:4593020`), whereas `newrelic_alert_muting_rule` (`<account_id>:<muting_rule_id>`,
   `538291:6789035`), `newrelic_nrql_drop_rule` (`<account_id>:<rule_id>`), `newrelic_service_level`
   (`<account_id>:<sli_id>:<guid>`) and `newrelic_workload` (`<account_id>:<workload_id>:<guid>`)
   all put the account **first**. Do not assume a uniform order; encode alert_policy's reversed
   form explicitly.
2. **`newrelic_nrql_alert_condition` embeds the condition TYPE in the id.**
   `<policy_id>:<condition_id>:<conditionType>` where conditionType is the lower-cased
   `type` from `nrqlConditionsSearch` — `static` or `baseline` (verified examples
   `538291:6789035:baseline` / `538291:6789035:static`). The type is *not* a display field;
   it is load-bearing for import.
3. **`newrelic_workload` is a 3-part `<account_id>:<workload_id>:<guid>`, NOT a bare GUID**
   (verified example `12345678:1456:MjUy…`). entitySearch yields the `guid` but **not** the
   numeric `workloadId`; resolve it per entity via `actor { entity(guid:$g) { ... on
   WorkloadEntity { workloadId } } }` (or the workloads collection query) before building the
   id.
4. **`newrelic_service_level` is `<account_id>:<sli_id>:<guid>`** where `guid` is the entity
   the SLI *relates to* (the APM service / workload), not the SLI's own guid. The `sli_id`
   comes from that owning entity's `serviceLevel { indicators { id guid } }`. Hardest id to
   assemble — a per-entity follow-up query is required; defer to a later increment.
5. **Everything else is a BARE token** — a GUID (dashboards, all synthetics monitors,
   private_location, key_transaction, entity_tags) or a bare id (workflow,
   notification_destination, notification_channel, obfuscation_rule/_expression). GUIDs are
   opaque base64-ish strings (e.g. `MjUyMDUyOHxBUE18QVBQTElDQVRJT058Mg`); pass them through
   verbatim, they are not numeric.
6. **`newrelic_notification_channel` import id is the channel id, not the destination id.**
   The registry doc's Import block is a copy-paste glitch (it shows
   `newrelic_notification_destination.foo <destination_id>`); the real id is the channel `id`
   from the `aiNotifications.channels` query, which is *not* surfaced in the UI. Emit the
   channel id.
7. **`newrelic_notification_destination` has an org-scope import variant**
   (`<id>:ORGANIZATION:<org_id>`) — default/account scope is the bare `<destination_id>`; use
   the bare form (this provider is account-scoped).
8. **Numeric ids** (obfuscation rule/expression, workload_id, sli_id, condition/policy ids)
   arrive as JSON numbers or strings depending on the NerdGraph field — **stringify** before
   composing an id.

## Curation gotchas (Phase B, when live)

Confirmed shapes to verify against real `terraform plan -generate-config-out` on a live
account — prune computed via `hcl.WalkResourceBlocks`; scrub/exclude secrets like the
Datadog/Fastly providers. **`newrelic_one_dashboard` is the heaviest curation surface** (the
New Relic analogue of `datadog_dashboard` / `fastly_service_vcl`).

- **`newrelic_one_dashboard` — the big one.** generate-config-out emits the full
  `page { widget_* { ... } }` tree; each page holds many typed widget blocks
  (`widget_line`, `widget_billboard`, `widget_markdown`, `widget_table`…), each carrying an
  `nrql_query { query }`. Prune computed `guid`/`permalink`/per-page `guid`/per-widget `id`.
  **Template hazard:** NRQL widget queries and titles contain `${...}`-style template
  variables and dashboard variable syntax → the generated HCL must keep these literal
  (verify terraform's writer escapes them, exactly as flagged for the Datadog widget tree).
  Widget-block ordering may churn (tolerate). Phase-B-heavy; Phase A is a breadth scaffold.
- **`newrelic_nrql_alert_condition`.** `nrql { query }` + `critical`/`warning` term blocks
  are the core; `type` (`static`/`baseline`) both drives the schema *and* the import id.
  Prune computed `entity_guid`; defaults over-emit (`aggregation_window`/`aggregation_method`/
  `fill_option`/`violation_time_limit_seconds`). Same NRQL literal-string hazard as
  dashboards.
- **`newrelic_alert_policy`.** Light — `name` + `incident_preference`. Prune computed. Note
  the import composite order quirk (§1).
- **Synthetics (`newrelic_synthetics_*`).** `locations_public` / `locations_private`, `period`,
  `status`, `uri`/`domains`/`script` per type. `newrelic_synthetics_script_monitor` emits the
  raw `script` (may embed secrets referencing secure credentials — keep the `$secure.CRED`
  references, do not inline secrets). Prune computed `guid`/`period_in_minutes`. The
  `monitorType` split (CRITICAL §2) determines which resource each becomes.
- **`newrelic_notification_destination` — nested secret (adopt-with-scrub, not full
  exclude).** The shell (`name`, `type`, `property` blocks) is adoptable, but the `auth_token`
  (token/prefix), `auth_basic` (user/password), `auth_custom_header` (key/value) and
  `secure_url` blocks are **write-only** — the API explicitly does not return them on read
  ("Sensitive data such as destination API keys, service keys, auth object etc. are not
  returned … and may not be set in state when importing"). Scrub these + flag for
  out-of-band re-supply. Also **Slack destinations can only be imported/destroyed, not
  created/updated** → note on adoption.
- **`newrelic_notification_channel` / `newrelic_workflow`.** `type` + `property`/`enrichments`/
  `issues_filter` blocks; channel references a `destination_id`, workflow references channels.
  No secret in the channel/workflow themselves (the secret lives in the destination auth).
  Prune computed ids. Note channels are 1:1 with workflows (see import quirk §6).
- **`newrelic_workload`.** `entity_guids` / `entity_search_query` / `scope_account_ids`
  blocks. Prune computed `guid`/`workload_status`/`composite_entity_search_query`. The
  `workloadId` must be resolved for the import id (§3).
- **`newrelic_service_level`.** `events { valid_events / good_events / bad_events }` +
  `objective` blocks, attached to a target `guid`. Prune computed `sli_guid`. Hardest import
  to assemble (§4).
- **`newrelic_obfuscation_rule` / `_expression`.** NOT secret — they define *what* to
  obfuscate (a regex expression + which log attributes/actions). Adopt freely. Rule
  references an `expression_id`; expression carries a `regex`. Bare-numeric-id imports.
- **`newrelic_nrql_drop_rule`.** `action` (`drop_data`/`drop_attributes`) + `nrql`. Prune
  computed. **Deprecated** — see out of scope; prefer `newrelic_pipeline_cloud_rule`.
- **`newrelic_key_transaction`.** `apdex_target`/`browser_apdex_target`/`application_guid`.
  Light. GUID import.
- **`newrelic_entity_tags` — a companion, double-management hazard.** It manages tags on an
  entity that is ALSO adopted as its own resource; entitySearch already returns each entity's
  `tags`. Adopting both risks two resources fighting over the same tags. Treat as opt-in /
  later increment, not part of the default spine.

## Write-only / secret resources (EXCLUDE)

New Relic's secret material lives in the key-management and destination-auth plane — exclude
these (surface, adopt out-of-band), exactly like Datadog's `datadog_api_key` /
`datadog_application_key`:
- **`newrelic_api_access_key`** — the `key` value is write-only (returned once at creation,
  never on read) and *is* the authentication material (including possibly the very User key
  in use) → exclude entirely; do not enumerate.
- **`newrelic_synthetics_secure_credential`** — the `value` is write-only (the NerdGraph
  query returns the `key`/metadata but never the value) → exclude the value; the shell can be
  imported by `key` but the value must be re-supplied out-of-band. Do not adopt as if
  round-trippable.
- **`newrelic_notification_destination` — nested secret, scrub not exclude.** The auth blocks
  (`auth_token`/`auth_basic`/`auth_custom_header`/`secure_url`) are write-only and not
  returned by the API → adopt the destination shell but scrub the auth + flag for re-supply
  (see curation gotchas). This is the notification-stack analogue of Datadog's
  `datadog_webhook.custom_headers`.
- **Obfuscation (`newrelic_obfuscation_rule` / `_expression`) is NOT secret** — it names what
  to mask, carrying no secret itself → adopt freely.

## Deliberately out of scope
- **Deprecated resources** — `newrelic_alert_channel` (→ notification stack),
  `newrelic_alert_condition` + `newrelic_infra_alert_condition` (→ `newrelic_nrql_alert_condition`;
  note a few infra condition kinds have no NRQL equivalent — revisit only if needed),
  `newrelic_dashboard` (→ `newrelic_one_dashboard`). Terraformer emits the first three; we do
  not.
- **`newrelic_nrql_drop_rule`** — covered above but **deprecated, removal 2026-06-30**;
  superseded by **`newrelic_pipeline_cloud_rule`** (a later increment — the drop-rule
  successor, a `pipelineCloudRules` NerdGraph query). Include drop_rule only for back-compat
  on accounts that still have them.
- **Cloud-integrations plane** (`newrelic_cloud_aws_link_account`/`_azure_link_account`/
  `_gcp_link_account` + the `_*_integrations` resources) — a large separate plane, several
  carrying write-only cloud credentials (Azure `client_secret`, GCP keys) → later increment,
  gated on the credential handling, not core observability config.
- **Org / IAM plane** (`newrelic_group`, `newrelic_user` [authentication-domain user mgmt],
  `newrelic_authentication_domain`) — `Capabilities.IAM=false`; org-scoped (needs org-level
  keys, SCIM/SSO), not account observability. Out of scope.
- **JSON escape-hatch dashboards** — emit the typed `newrelic_one_dashboard`, not a raw-JSON
  variant (same rule as Datadog's `_json` resources).
- **Log-config depth beyond obfuscation** (`newrelic_data_partition_rule`,
  `newrelic_log_parsing_rule`) — a later `logConfigurations` increment.
- **`newrelic_monitor_downtime`** and other synthetics-adjacent scheduling — later increment.
- **`newrelic_entity_tags`** as a default resource — companion/opt-in only (double-management
  hazard, above).
- **Data planes** — NRQL query results, metrics, events, logs, SLI attainment history: the
  DATA behind the config, not config.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD `newrelic_one_dashboard` + `newrelic_alert_policy` + `newrelic_nrql_alert_condition`
(what essentially every New Relic customer manages as IaC, and the set that exercises ALL the
hard machinery at once: dashboard = entitySearch + the `dashboardParentGuid IS NULL` parent
filter + the heaviest curation surface / recursive page-widget-NRQL tree; alert_policy = the
`policiesSearch` dedicated cursor query + the reversed `<policy_id>:<account_id>` composite;
nrql_alert_condition = the `nrqlConditionsSearch` dedicated cursor query + the
`type`→`<…>:<static|baseline>` import discriminator) → INC-1 the synthetics family
`newrelic_synthetics_monitor` / `_script_monitor` / `_cert_check_monitor` /
`_broken_links_monitor` / `_step_monitor` + `_private_location` (the six-way `monitorType`
split out of one entitySearch; all bare-GUID imports; `secure_credential` EXCLUDED) → INC-2
the notification stack `newrelic_notification_destination` + `newrelic_notification_channel` +
`newrelic_workflow` + `newrelic_alert_muting_rule` (the `aiNotifications`/`aiWorkflows`
dedicated cursor queries; the destination-auth scrub; the channel-id vs destination-id import
quirk; muting_rule `<account_id>:<id>`) → INC-3 `newrelic_workload` + `newrelic_service_level`
+ `newrelic_key_transaction` (the 3-part composite imports and the per-entity follow-up
queries to resolve `workloadId` / `sli_id` — the hardest id assembly) → INC-4
`newrelic_obfuscation_rule` + `newrelic_obfuscation_expression` + `newrelic_nrql_drop_rule`
(one `logConfigurations` query for the first two; bare-numeric-id imports; drop_rule flagged
deprecated) → LATER/BLOCKED `newrelic_synthetics_secure_credential` (value write-only),
`newrelic_api_access_key` (secret), `newrelic_pipeline_cloud_rule` (the drop_rule successor),
`newrelic_entity_tags` (companion/double-management), the cloud-integrations plane (write-only
credentials), the org/IAM plane, and the data planes.
