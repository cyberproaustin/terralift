# TerraLift v2 Provider Playbook & Tracker

The **execution playbook and cross-session tracker** for building out breadth to
match Terraformer's 44 providers. This is the operational companion to
[V2-ROADMAP.md](V2-ROADMAP.md) (which holds the strategy and cost analysis). If you
are picking this up in a new session, read this file first, then the tracker table
at the bottom for where we are.

---

## The goal

Match Terraformer's provider breadth while keeping TerraLift's depth (plan-clean,
curated, verified output). We have four **golden-image** providers to mirror:
**AWS, GCP, Azure, GitHub**. Forty remain.

## The two-phase model (important — read this)

We are deliberately splitting breadth from depth, because setting up live
credentials for 40 platforms up front is not realistic.

- **Phase A — Breadth (now).** Build a compiling, review-passed **scaffold** for
  each provider: enumeration, import-ID derivation, type map, export wiring,
  provider registration, unit tests. This is committed and pushed. **A Phase-A
  provider is NOT plan-clean.** Curation — the rules that make generated HCL
  plan-clean — is derived from *real* `generate-config-out` diffs, which require
  live credentials. No live run means no curation.
- **Phase B — Depth (later, one platform at a time as credentials become
  available).** Stand up a live account, run the round-trip, read the actual plan
  drift, add curation until plan-clean, tear down. This is where a scaffold earns
  the quality bar.

**Never describe a Phase-A provider as "done" or "validated."** Commit messages say
*scaffold*. The tracker marks live validation separately.

---

## The per-provider loop

Repeat this for each provider, one at a time:

1. **Research.** Read Terraformer's provider code at
   `../terraformer/providers/<name>/` (what resources it enumerates, which native
   API endpoints, how it pages) plus the corresponding `terraform-provider-<name>`
   registry docs (resource types and their **import ID formats** — the one thing we
   cannot get wrong). Produce a build spec: native-key → TF-type map, enumeration
   endpoints + scope, import-ID format per type, and known curation gotchas
   (write-only/sensitive attrs, over-emitted computed attrs, dropped-required
   attrs). Cover the **config layer**; exclude code/data resources (serverless code
   deploys, object/blob data, media) — TerraLift adopts configuration, not code or
   data.
2. **Build.** Mirror the golden pattern (see below). Add the provider package,
   register it in `cmd/terralift/main.go`, and write unit tests for import IDs and
   any curation logic.
3. **Review.** Run a code review and a security review (parallel subagents) over
   the new package. Remediate real findings.
4. **Push.** Commit to the v2 branch with an honest `feat(<name>): ... (scaffold)`
   message. Push.
5. **Track & advance.** Update the tracker table, then move to the next provider.

## The golden-image pattern to mirror

Every provider implements `internal/provider.CloudProvider`
(`Name/CheckDependencies/Connect/Enumerate/Export/Templates/Capabilities`) and
registers via `init()` + a blank import in `cmd/terralift/main.go`. The GitHub
provider (`internal/providers/github/`) is the reference for a flat, HTTP-API,
non-hyperscaler provider — the shape most of the 40 take. Its file layout:

| File | Responsibility |
|------|----------------|
| `<name>.go` | Provider struct, `init()` registration, `Capabilities`, `Templates` |
| `<name>cli.go` / `<name>api.go` | The API/CLI wrapper: one substitutable call var (for tests), list+paginate helpers |
| `preflight.go` | `CheckDependencies` (tools/creds) + `Connect` (resolve & **validate** scope, publish auth env) |
| `enumerate.go` | Native API calls → `model.Inventory`; per-scope sub-resources |
| `types.go` | native-key (`<name>:<kind>`) → Terraform type map |
| `importid.go` | `deriveImportID` per type — **must** wrap the raw id in `util.EscapeHCLTemplate` |
| `export.go` | import blocks + `providers.tf` → `tf.GenerateConfig` → `hcl.SplitByGenerated` → curate; `excludedReason` seam |
| `curate.go` | Prune over-emitted attrs, author dropped-required attrs, redact secrets |
| `<name>_test.go` | Import-id + curation unit tests (fake the API call var) |

Export flow (shared, identical to AWS/GCP/GitHub): derive per-type import IDs →
write born-correct `import.tf` + a keyless `providers.tf` (auth via env, **never
inline a token**) → `terraform plan -generate-config-out` → `hcl.SplitByGenerated`
keeps only import blocks whose config generated (rest are honest gaps) → provider
curation. Capability defaults for a flat SaaS provider:
`Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.

Three reusable curation moves (all proven on GitHub):
- **Prune** computed noise the generator over-emits (`hcl.Prune` with regex rules).
- **Author** back settable attributes the generator wrongly drops or nulls as
  sensitive, from live enumeration data (e.g. GitHub webhook URLs) — keeps them
  managed instead of abandoning them via `ignore_changes`.
- **Exclude** (via `excludedReason` → `ExcludedIDs`) resources that cannot be
  adopted plan-clean — e.g. a write-only secret value, where adopting with a
  placeholder would overwrite the real value on apply. Surface it, don't adopt it.

## Standards / definition of done (Phase A, per provider)

- [ ] `go build ./...`, `go vet ./...`, `gofmt -l` clean; full `go test ./...` green.
- [ ] Unit tests cover every import-ID format and any curation logic.
- [ ] Import IDs escaped with `util.EscapeHCLTemplate` (template-injection guard).
- [ ] Auth via env var only; no token/secret ever written to config, state, or logs.
- [ ] Scope resolved **and validated** (reject a scope that would silently target
      the wrong account — the GitHub `user/repos` lesson).
- [ ] `Capabilities` set honestly; write-only/un-adoptable resources excluded with a
      reason, not left as misleading gaps.
- [ ] Code review + security review passed and remediated.
- [ ] Registered in `cmd/terralift/main.go`, committed, pushed; commit labeled
      *scaffold*; gotchas/deferred items noted in the commit and this tracker.

## Conventions & hard-won lessons

- **Branch:** all v2 providers accumulate on **`feat/v2-breadth`** (based off the
  GitHub work). `main` stays at v1.2.1 until we choose to release v2.
- **Native-key scheme:** `"<provider>:<kind>"` (e.g. `cloudflare:record`).
- **Escaping:** import IDs embed free text (names, patterns) that can contain
  `${ }`; `hcl.ImportBlock` uses `%q` which does not neutralize templates. Always
  `util.EscapeHCLTemplate`. (GitHub HIGH finding.)
- **Case sensitivity:** key the inventory by the raw id when the platform's ids are
  case-sensitive (don't blindly `strings.ToLower`). (GitHub finding.)
- **Shared scaffolding:** as the HTTP-API + token + `generate-config-out` pattern
  repeats across the SaaS providers, extract common helpers (a shared HTTP-JSON
  paginator, a generate-config-out export skeleton) so later providers get thinner.
  Do this opportunistically once ~2-3 confirm the shape; do not pre-abstract.
- **Kubernetes is the odd one** — in-cluster resources fight the import/scope model;
  build it last and expect it to need a different approach.

---

## Tracker

Status legend: `todo` · `research` · `built` (compiles + tests) · `reviewed`
(code+sec review remediated) · `pushed` · `LIVE` (Phase-B plan-clean validated).

### Golden images (reference — already shipped)

| Provider | Status | Notes |
|----------|--------|-------|
| AWS | LIVE (v1.x) | Resource Explorer + generate-config-out |
| GCP | LIVE (v1.x) | Cloud Asset Inventory + generate-config-out |
| Azure | LIVE (v1.x) | Resource Graph + aztfexport |
| GitHub | LIVE | First non-hyperscaler; `feat/github-provider` (8 commits), plan-clean on user + org scope |

### Batch 1 — GitHub-like (HTTP API + token + generate-config-out)

| # | Provider | Status | Notes |
|---|----------|--------|-------|
| 1 | Cloudflare | pushed | 16 config resources; spec at docs/v2-specs/cloudflare.md; reviewed (2 MED + LOWs remediated); curation is Phase B |
| 2 | DigitalOcean | pushed | 22 resources; spec at docs/v2-specs/digitalocean.md; per-endpoint nesting key + links.pages.next paging; reviewed + remediated (next-url host check, DB-default gating, systemic-failure guard) |
| 3 | Fastly | pushed | 11 standalone resources (service-centric: config nests in fastly_service_vcl); spec at docs/v2-specs/fastly.md; two response families (bare arrays + JSON:API); reviewed + remediated (401-fatal, decode-error wrap) |
| 4 | NS1 | pushed | 10 resources (DNS: zones + per-zone records, monitoring/datasource/datafeed/notifylist/team/user; apikey+tsigkey excluded); bare arrays, no pagination, X-NSONE-Key header; reviewed clean |
| 5 | Linode | pushed | 18 resources (IaaS: instances/DNS/firewall/nodebalancer+config+node/volume/vpc+subnet/lke/image/rdns/stackscript/object-storage/db); Bearer + data-envelope numeric paging + X-Filter; 2-level fan-out; reviewed + remediated |
| 6 | Vultr | pushed | 16 resources (IaaS: instance/bare-metal/DNS/firewall+rules/block/LB/vpc+vpc2/ssh/reserved-ip/startup/k8s/db/object-storage); Bearer + per-key envelope + CURSOR paging; node_pools deferred (double-mgmt); reviewed + remediated. Spec commit d4ba0a4 |

### Batch 2 — SaaS / observability / identity

| # | Provider | Status | Notes |
|---|----------|--------|-------|
| 7 | Datadog | pushed | 13 config resources (monitors/dashboards/dashboard_lists/SLOs/synthetics/logs index+pipeline+metric/notebooks/security rules/downtimes/roles/users); spec at docs/v2-specs/datadog.md (commit 677066a). TWO auth headers (DD-API-KEY + DD-APPLICATION-KEY); site-configurable base (DD_HOST, forced https); THREE response families (v1 bare / v1 keyed / v2 JSON:API) + FOUR pagers; flex ddID (numeric notebook ids); flat-object attr fallback (security rules); redirect-refusing client. Reviewed (2 agents): HIGH notebook-id decode + MED flat isDefault + MED redirect-leak + LOW http-scheme all remediated. Integration plane + api/app keys excluded by non-enumeration |
| 8 | New Relic | pushed | 16 config resources (one_dashboard/alert_policy/nrql_alert_condition/muting_rule/notification destination+channel/workflow/5 synthetics monitor types/workload/key_transaction/obfuscation rule+expression); spec at docs/v2-specs/newrelic.md (commit 1ab96ce). FIRST GraphQL provider — NerdGraph single-endpoint POST client, nextCursor pagination, 200-with-errors=failure, bounded 429/5xx backoff. Import-ID composites verified: alert_policy `<policy_id>:<account_id>` (account SECOND, reversed!), nrql_condition `<policy>:<cond>:<static|baseline>`, workload/muting_rule account-FIRST. Synthetics 6-way monitorType split; dashboard parent filter; workload workloadId per-entity follow-up; nrID flex string/number decode. Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED 429-backoff + LOW entityFilter-guard remediated. service_level/private_location/entity_tags/drop_rule deferred; api_access_key/secure_credential-value excluded |
| 9 | Grafana | pushed | 14 config resources (dashboard/folder/data_source/unified-alerting provisioning: contact_point+notification_policy(singleton)+message_template+mute_timing+rule_group/team/service_account/playlist/library_panel/role+report(Enterprise best-effort)); spec at docs/v2-specs/grafana.md (commit ce977d7). FIRST user-supplied host (GRAFANA_URL, validated); dual Bearer/Basic auth (GRAFANA_AUTH); X-Grafana-Org-Id; 4 response families + 3 pagers (perPage vs perpage casing). Org-scoped COMPOSITE import IDs built at export from Container: orgID:token / orgID:name / 3-part orgID:folderUID:title (rule_group) / orgID:policy (singleton). Contact-point name-dedup; rule-group synthesis by (folderUID,ruleGroup); General-folder + fixed-role skips. Reviewed (2 agents): both APPROVE, no CRIT/HIGH/MED; LOW fixes (auth TrimSpace, http+Basic warn, org guard-case) remediated. permissions/annotation/organization deferred; SA-token/datasource-secure-fields/Cloud-stack excluded |
| 10 | Honeycomb | pushed | 14 config resources (dataset/column/derived_column/query_annotation/flexible_board/trigger/slo/burn_alert + 6 typed recipients); spec at docs/v2-specs/honeycomb.md (commit e626c51). Fastly-style per-dataset FAN-OUT (/1/datasets → per-dataset sub-lists) + second-level per-SLO burn-alert fan-out + synthetic __all__ pass; X-Honeycomb-Team auth, US/EU base (https-forced), bare JSON arrays no pagination. Composite import IDs: <dataset>/<token> dataset-scoped, BARE for team-wide AND for __all__ env-wide variants (the subtle fork); column by key_name, derived_column by alias. Recipient type-split; classic-board skip. Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED https-enforce + LOW (minor-pin, PathEscape ds, 401 early-out) remediated. query/dataset_definition/marker/board_view/v2-mgmt deferred; api_key + recipient secrets excluded/scrubbed |
| 11 | PagerDuty | pushed | 18 config resources (service+integration/escalation_policy/schedule/team+membership/user+contact_method+notification_rule/business_service/maintenance_window/extension+servicenow/webhook_subscription/tag/response_play/ruleset+rule); spec at docs/v2-specs/pagerduty.md (commit 3ed91fc). Distinctive `Authorization: Token token=` header + vnd.pagerduty+json;version=2; US/EU region (https-forced); keyed offset/limit/`more` pager; From-header gating for response_plays. Import IDs: DOT (service_integration/ruleset_rule, parent-first) vs COLON (team_membership/user_contact_method/user_notification_rule, USER-first) vs bare. 5 fan-outs (service→integrations via include, team→members, user→contact/notif, ruleset→rules); extension-schema discriminator. Reviewed (2 agents): both APPROVE, no CRIT/HIGH; LOW fixes (PathEscape fan-out ids, mw filter future+ongoing, drop email from label, 401 short-circuit) remediated. Event Orchestration/automation_actions/slack_connection deferred; integration_key/webhook/extension secrets Phase-B scrub |
| 12 | Opsgenie | pushed | 16 config resources (team+routing_rule/user+contact+notification_rule/schedule+rotation/escalation/service+incident_rule/api+email integration/alert+notification policy/maintenance/heartbeat); spec at docs/v2-specs/opsgenie.md (commit a7c52f3). `Authorization: GenieKey` header; US/EU region (https-forced); data/paging.next SERVER-URL cursor — HOST-VALIDATED before re-sending the key (isOpsgenieURL, the Fastly next-link lesson). All SLASH composites; per-user fan-outs use DIFFERENT parents (user_contact by USERNAME, notification_rule by user_id); alert_policy flips bare(global)/team-slash; heartbeat by-name from nested data.heartbeats. Integration type-discriminator (API/Email only). Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED /v2/account→/v2/users fallback + LOW global-policy overwrite guard remediated. integration_action/custom_role (no import) + vendor integrations deferred; api_key Phase-B scrub. NOTE: 2nd review pair (session-limit re-run) |
| 13 | Okta | pushed | ~29 config-core TF types (user/group/group_rule/user_type + 8 signOnMode app types + trusted_origin/network_zone + auth_server+scope/claim/policy/policy_rule + signon/password/mfa policies+rules + inline/event hooks + oidc/saml idps); spec at docs/v2-specs/okta.md (commit fac4d16). Big provider (100+ resources) — scoped to config core, long tail deferred. FIRST Link-header (RFC5988) pagination — next-URL in the `Link` header, host-validated before re-sending the SSWS token (probed vs 12 bypass forms); `SSWS` auth; CONSTRUCTED base (OKTA_ORG_NAME+OKTA_BASE_URL). Discriminators: apps by signOnMode (+BROWSER_PLUGIN name split, skip Okta-own), policies by required ?type=, idps by type. Composite DEPTH: bare / 2-part / 3-part (auth_server_policy_rule). Bounded 429 retry (Retry-After) added. Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED 429-backoff + LOW bracket-aware-Link-parse remediated. schema/brand/factors/assignments/social-idp deferred; app/idp/hook secrets Phase-B scrub |
| 14 | Auth0 | pushed | 15 config-core TF types (client/resource_server/connection/role/action/organization/client_grant/log_stream/email_template + 6 settings singletons); spec at docs/v2-specs/auth0.md (commit d6fb303). FIRST OAuth2 client-credentials token EXCHANGE (POST /oauth/token → short-lived Bearer; AUTH0_API_TOKEN static bypass); tenant-domain base (validated hostname shape); page/per_page+include_totals keyed pager (+bare-array log-streams, name-fanout email-templates, singleton GETs). Phase A is ::-composite-FREE (bare id / name / singleton sentinel); client imports by client_id NOT id; system-object skips (global client, is_system RS). Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED pager-keeps-pages-on-error + LOW (domain @-validation, ensureToken short-circuit, connect self-validate) remediated. user-plane/::-relationship-plane/rules-hooks deferred; all secrets Phase-B scrub |
| 15 | LaunchDarkly | pushed | 10 config types (project/environment/feature_flag/feature_flag_environment/segment/destination/metric/webhook/team/custom_role); spec at docs/v2-specs/launchdarkly.md (commit 0dbe99d). RAW-token Authorization header (NO scheme prefix — the inverse trap); app.launchdarkly.com base (+LAUNCHDARKLY_API_HOST, validated hostname shape); project→env fan-out + two-level project×env (segments/destinations) + flag×env derivation from the embedded environments map. HATEOAS _links.next pager — host-validated after resolving relative hrefs (security pass verified vs 9 bypass vectors, same-parser no-differential). Composites bare/2-part/3-part; feature_flag_environment env-in-the-MIDDLE (<proj>/<env>/<flag>, NOT flag_id+env). Bounded 429 Retry-After backoff. Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED 429-backoff + LOW (importid explicit-bare default, enumerate host guard) remediated. SDK-key/webhook-secret/destination-config never decoded; access_token/relay-proxy deferred |
| 16 | Keycloak | pushed | 11 config-core TF types (realm + openid/saml client + role(realm&client, one type) + group + openid_client_scope + auth_flow + oidc/saml idp + ldap_user_federation + required_action); spec at docs/v2-specs/keycloak.md (commit c6ea6ea). FORM-encoded OAuth2 token exchange (client_credentials OR password grant), short-lived tokens refreshed on mid-run 401; user-supplied KEYCLOAK_URL+base_path (http allowed for local w/ warn, userinfo-splice rejected); realm fan-out + two-level realm×client (client roles) + first/max bare-array pager. Discriminators: clients/scopes by protocol, idps/components by providerId. Import IDs: bare realm-name / 2-part <realm>/<leaf> (NO 3-part — role is 2-part, client leaf is UUID not clientId). Built-in client/flow + master-realm skips; group-tree flatten. Reviewed (2 agents): both APPROVE, no CRIT/HIGH; LOW (dead itoa, kcBase path-fold, kcHost splice-safe, connect http-warn) remediated. users/mappers/assignments deferred; all secrets never-decoded Phase-B scrub |
| 17 | Logz.io | pushed | 10 config-core TF types (alert_v2 + endpoint + drop_filter + subaccount + user + log_shipping_token + s3_fetcher + archive_logs + metrics_account + authentication_groups singleton); spec at docs/v2-specs/logzio.md (commit a4c975f). SIMPLEST shape — flat account container, no fan-out. Custom `X-API-TOKEN` header auth (not a scheme); region base URL LOGZIO_REGION→api[-<region>].logz.io (charset-guarded before host interpolation) or LOGZIO_BASE_URL override (https-forced); redirect-refusing client. MIXED enumerate shapes: GET bare-list, POST …/retrieve with a pagination BODY (lzSearch, {key:[...]} envelope-then-bare-array), and a singleton GET. Tolerant lzGet/decodeList absorbs bare-array vs named-array-wrapper vs single-object VERIFY variance; flexible lzID (number-or-string) + lzObj (id/alertId/accountId × name/title/accountName/username) decode NO secret field. All-bare-token imports (numeric or string hash; auth-groups sentinel). NB `logzio_subaccount` (one word — spec catalog typo said sub_account) + `logzio_alert_v2` (not deprecated logzio_alert). Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED (lzGet object-with-named-array tolerance) + LOW-2 (region charset guard) remediated. grafana_* embedded-Grafana plane + kibana data-view deferred; endpoint creds / token values / AWS+storage keys never-decoded, Phase-B scrub |
| 18 | Mackerel | pushed | 9 config-core TF types (service + role + monitor + channel + notification_group + dashboard + aws_integration + downtime + alert_group_setting); spec at docs/v2-specs/mackerel.md. Provider is `mackerelio-labs/mackerel` (pre-1.0, pin ~> 0.9); SDK is mackerelio/mackerel-client-go. Custom `X-Api-Key` header auth (env MACKEREL_APIKEY / MACKEREL_API_KEY alias); base https://api.mackerelio.com (override MACKEREL_API_BASE/APIURL, https-forced); redirect-refusing client. All list endpoints are unpaginated GET `{"<key>":[...]}` named-array envelopes (decodeEnvelope, bare-array fallback) — keys NOT uniform: notificationGroups + alertGroupSettings camelCase, aws_integrations snake (verified vs SDK json tags). SERVICE→role fan-out (subList; roles/services identify by name, no id); Mackerel ids are opaque STRINGS (no numeric juggling). Import IDs: bare name (service) / bare opaque id (most) / COLON composite `<service>:<role>` (role). 7 polymorphic monitor kinds collapse to one mackerel_monitor (import by id, nameless-monitor safe). Preflight/connect probe GET /api/v0/services (403=restricted-but-valid, NOT auth-fail); org name best-effort via /api/v0/org with base-host fallback. Reviewed (2 agents): both APPROVE, no CRIT/HIGH; MED (probe /services not /org so a restricted key doesn't abort) + LOW (unused Type field, decode comment) remediated. hosts/users/*_metadata/default_notification_group singleton deferred; channel url + aws_integration secret_key + external-monitor headers never-decoded, Phase-B scrub. Dashboard import id + service_metadata `:` vs `/` are Phase-B VERIFY |
| 19 | Vault | pushed | 11 config-core TF types (mount + auth_backend + policy + audit + namespace + pki/database/aws_secret_backend_role + jwt/approle/token_auth_backend_role); spec at docs/v2-specs/vault.md. Provider hashicorp/vault (~> 5.0). THE security-sensitive one: adopts CONFIG only, NEVER enumerates/reads secret DATA (KV contents, dynamic creds, root/unseal keys HARD-EXCLUDED — enumeration only touches sys/* + `.../roles` name LISTs, never a data path; reviewer grep-verified the entire endpoint set is clean). `X-Vault-Token` header auth (+ optional `X-Vault-Namespace`); base VAULT_ADDR (default https://127.0.0.1:8200; scheme RESPECTED not forced — dev is http; userinfo-splice rejected; http-non-loopback cleartext warn); redirect-refusing client (Vault 307 standby→active); TLS ALWAYS verified — NO InsecureSkipVerify (VAULT_SKIP_VERIFY/CACERT deliberately ignored in scaffold). THREE decode shapes: map-keyed-by-path (sys/mounts/auth/audit → `{"data":{"<path>/":{"type":..}}}`, NEW shape, trailing-/ filter drops envelope keys); LIST-keys (`{"data":{"keys":[...]}}` + top-level fallback); tolerant data-wrapper. sys/* backbone + per-mount role fan-out discriminated on mount TYPE (pki/database/aws secret roles; jwt/oidc→jwt + approle + token auth roles). SIX path-based import shapes: bare path trailing-/ stripped (mount/auth/audit) / bare name (policy/namespace) / `<backend>/roles/<name>` / `auth/<backend>/role/<name>` / odd `auth/token/roles/<name>`; precomputed + EscapeHCLTemplate'd, stored under Properties["importID"] NOT "token" (token=VAULT_TOKEN footgun). Error taxonomy: 401 fatal, 404 skip (empty LIST dir 404s), 403 skip on soft lists but COUNTED on core mounts+auth backbone (blind token → systemic, not silent-empty). Preflight probe auth/token/lookup-self (any valid token self-looks-up); cluster_name from sys/health best-effort. Skips system/identity/cubbyhole mounts by type + built-in token/ auth (roles still fan out) + root/default policies. Reviewed (2 agents): both APPROVE, no CRIT/HIGH/MED; LOW (vList top-level-keys fallback + Properties importID rename) remediated. ldap/github/identity type-specific auth config + KV metadata deferred; ldap bindpass/db conn pw/aws secret_key/oidc client_secret never-decoded, Phase-B scrub (must land WITH those resources) |

### Batch 3 — VCS / dev / platform

| # | Provider | Status | Notes |
|---|----------|--------|-------|
| 20 | GitLab | pushed | 16 config-core TF types (group + project + group/project_variable + group/project_label + group/project_hook + deploy_key + branch_protection + tag_protection + group/project_membership + project_milestone + project_share_group + group_ldap_link); spec at docs/v2-specs/gitlab.md. Provider gitlabhq/gitlab (~> 17.0). TWO-ROOT fan-out (groups + projects the token can manage via membership=true&min_access_level=40; subgroups come back FLAT, no recursion), then durable config children. `PRIVATE-TOKEN` header auth (env GITLAB_TOKEN); base GITLAB_BASE_URL default https://gitlab.com/api/v4 — ALREADY carries /api/v4 (unlike Vault /v1/; bare host appends once, existing kept, userinfo-splice rejected, http-non-loopback cleartext warn); redirect-refusing client. Pagination: offset ?page=&per_page=100 following the X-Next-Page response header (next==page guard). FOUR composite import shapes (the defining hazard, all verified vs real docs): bare numeric id (group/project) / 2-part `<parent>:<leaf>` (leaf=numeric id for hook/label/deploy_key/membership/milestone/share_group, NAME for branch/tag protection) / 3-part `<parent>:<key>:<env_scope>` (variables, default scope *) / 4-part `<group>:<provider>:<cn>:<filter>` (ldap, cn XOR filter one-empty). Precomputed + EscapeHCLTemplate'd, stored Properties["importID"] NOT "token" (GitLab full of real tokens). project_share_group derived from the project object's shared_with_groups (no list endpoint). PARAMOUNT secret: the CI/CD variable VALUE is returned on read → glVariable decodes ONLY key+scope, never value (grep-verified); hook token/custom_headers/url_variables + project runners_token never decoded; access-token resources (PAT/project/group/deploy tokens) HARD-EXCLUDED, never enumerated. Reviewed (2 agents): both APPROVE, no CRIT/HIGH/MED-blocking; remediated http-cleartext preflight warn + ldap inventory-key separator; group_label id-vs-name is a Phase-B VERIFY (canonical doc uses name, we emit id). admin planes (GET /users, system hooks) + CI-job-token /version fallback + archived-project filter deferred; variable value/hook token/runners_token Phase-B scrub |
| 21 | Azure DevOps | pushed | 9 config-core TF types (project + git_repository + build_definition + variable_group + agent_queue + team + environment + agent_pool + group); spec at docs/v2-specs/azuredevops.md. Provider microsoft/azuredevops (~> 1.0). Single-root org→project fan-out + 2 org-level roots (agent_pool; group on the SEPARATE vssps.dev.azure.com graph host). NEW mechanics: auth is a PAT over HTTP Basic `base64(":"+PAT)` (empty username; env AZDO_PERSONAL_ACCESS_TOKEN; org url AZDO_ORG_SERVICE_URL); every request REQUIRES `?api-version=` (7.1 GA; graph groups 7.1-preview.1; variablegroups 7.1-preview.2; queues 7.1-preview.1); list responses are the VSTS `{"count":N,"value":[...]}` envelope; pagination via the `x-ms-continuationtoken` RESPONSE header fed back as &continuationToken=. BAD-PAT GOTCHA: a bad/under-scoped PAT returns 203 Non-Authoritative + text/html sign-in page (NOT 401) → azDo normalizes status==203 OR Content-Type text/html to a 401 (httptest-validated). FOUR import shapes: bare GUID (project) / bare int (agent_pool) / bare descriptor vssgp./aadgp./ungrp. (group) / 2-part `<projectGUID>/<leaf>` (leaf=UUID for git_repository/team, INT for build_definition/variable_group/agent_queue/environment); precomputed + EscapeHCLTemplate'd, Properties["importID"] NOT "token" (PAT). redirect-refusing client; userinfo-splice reject; graph host derived only for exact dev.azure.com (else groups skipped); http-non-loopback cleartext warn; NO InsecureSkipVerify. Hosted agent pools skipped. SECRET discipline: service-endpoint/service-connection family (authorization=live creds) NEVER enumerated; azVarGroup decodes ONLY id+name (variables map with secret values/KeyVault refs never pulled); env only, PAT never inlined. Reviewed (2 agents): both APPROVE, no CRIT/HIGH/MED; remediated http-cleartext warn + queues api-version preview.1 + spec environments-route typo (code was right: distributedtask not pipelines — sec reviewer erred, correctness confirmed vs MS Learn). policy plane + service hooks + serviceendpoint family deferred; variable_group secret values + is_secret pipeline vars Phase-B scrub |
| 22 | Entra ID (azuread) | todo | good dogfood candidate (live tenant) |
| 23 | Heroku | todo | |
| 24 | Octopus Deploy | todo | |
| 25 | commercetools | todo | |
| 26 | Opal | todo | |

### Batch 4 — Other clouds (CLI/SDK-heavy)

| # | Provider | Status | Notes |
|---|----------|--------|-------|
| 27 | AliCloud | todo | |
| 28 | IBM Cloud | todo | |
| 29 | IONOS Cloud | todo | |
| 30 | Tencent Cloud | todo | |
| 31 | Yandex Cloud | todo | |
| 32 | OpenStack | todo | |
| 33 | Equinix Metal | todo | |

### Batch 5 — Niche / special (Kubernetes last)

| # | Provider | Status | Notes |
|---|----------|--------|-------|
| 34 | gmailfilter | todo | |
| 35 | MikroTik (RouterOS) | todo | |
| 36 | Myra Security | todo | |
| 37 | PAN-OS (Palo Alto) | todo | |
| 38 | RabbitMQ | todo | |
| 39 | Xen Orchestra | todo | |
| 40 | Kubernetes | todo | build last; may need a different approach |

---

## Current status / session handoff

- **Done & pushed:** GitHub (`feat/github-provider`, reviewed, plan-clean). BATCH 1
  complete on `feat/v2-breadth`: Cloudflare (#1, 16), DigitalOcean (#2, 22),
  Fastly (#3, 11), NS1 (#4, 10), Linode (#5, 18), Vultr (#6, 16) — reviewed
  scaffolds; specs at docs/v2-specs/. BATCH 2 in progress: Datadog (#7, 13),
  New Relic (#8, 16), Grafana (#9, 14), Honeycomb (#10, 14), PagerDuty (#11, 18) —
  reviewed scaffolds pushed. New Relic is the first GraphQL (NerdGraph) provider;
  Grafana is the first user-supplied-host provider (GRAFANA_URL); Honeycomb is a
  Fastly-style per-dataset fan-out; PagerDuty adds the `Token token=` auth + mixed
  dot/colon composites; Opsgenie (#12, 16) adds the `GenieKey` auth + the
  host-validated server-supplied `paging.next` cursor — all reusable shapes for later
  providers. Phase-B (curation → plan-clean) pending live creds per provider.
- **Deferred house-pattern cleanup (from PagerDuty review):** the shared `list()`
  error helper (copied across all providers from Fastly) lets a fatal 401 fall through
  to `hardFails++`+Warn before the `fatal` short-circuit. Harmless (fatal wins before
  the guard is read) but noisy. PagerDuty's `list()`/`subList()` now `return` after
  setting fatal + early-out when fatal is set; retrofit the earlier providers
  (cloudflare/DO/fastly/ns1/linode/vultr/datadog/newrelic/grafana/honeycomb) in a
  Phase-B sweep.
- **Cross-provider note (from Datadog security review):** all HTTP providers use
  `http.DefaultClient`, which auto-follows redirects and does NOT strip custom auth
  headers on a cross-host 3xx. Datadog now uses a redirect-refusing client; the other
  six (cloudflare/digitalocean/fastly/linode/ns1/vultr) should get the same hardening
  in a Phase-B sweep (their auth headers are single, lower blast radius, but same class).
- **Cross-cutting Phase-B gate (from every review):** all scaffolds run
  `generate-config-out` and write `generated.tf` with the provider's read-back secrets
  UN-scrubbed (`scrubGeneratedHCL` is a no-op until Phase B). This is the documented
  two-phase posture (a Phase-A export is NOT plan-clean and must not be applied to
  production), backstopped by the pipeline's repo-wide secret scan. Phase-B: gate
  `GenerateConfig` behind the per-provider scrub (or a `--allow-unscrubbed` flag) before
  any live production export. Tracked once here, not per-provider.
- **429 backoff:** New Relic + Okta now do bounded `Retry-After` backoff (large/
  aggressively-rate-limited lists). The other REST providers treat 429 as a transient
  Warn (house pattern); revisit per-provider in Phase B if live runs brush the limit.
- **Review cadence:** complex/novel-client providers get 2 parallel reviewers
  (correctness + security); simple ones (bare arrays, established pattern) get 1
  combined reviewer — saves context without losing coverage.
- **Next up:** Batch 3 — Entra ID/azuread (#22), then Heroku, Octopus Deploy, commercetools, Opal. (Azure DevOps #21 pushed. 21/40 built.)
- **Constructed-host validation (from Auth0 review):** Auth0 now validates AUTH0_DOMAIN to a
  bare-hostname shape (rejects `@`/userinfo/path so the token can't go to a foreign host on
  the first request, before the redirect-refuser engages). Okta's cleanHostPart
  (org+"."+base_url) is the same class — retrofit the same shape check in a Phase-B sweep.
