# Cloudflare provider — build spec

Research artifact for the `cloudflare` provider (Phase A scaffold). Sources:
Terraformer's `providers/cloudflare/`, the `cloudflare/cloudflare` v4.52.1 registry
docs (import formats), and the Cloudflare REST API v4. Build mirrors the GitHub
provider (`internal/providers/github/`).

## Version pin (load-bearing)

Pin `cloudflare/cloudflare ~> 4.52` (last v4 line). v5 renamed core resources
(`cloudflare_record` → `cloudflare_dns_record`, access → zero_trust_*) and removed
several legacy ones; all import IDs below are verified against **v4.52.1**, which
also matches Terraformer's resource names. The REST API v4 endpoints are
provider-version-independent.

## Shape

- Auth: `CLOUDFLARE_API_TOKEN` env var, `Authorization: Bearer <token>`. No CLI — a
  direct `net/http` client to `https://api.cloudflare.com/client/v4`. The TF
  provider reads the same env var.
- Scope: a Cloudflare **account id** (`model.ScopeTenant`, one flat container).
- `Capabilities{IAM:false, Exposure:false, Hierarchy:false}`.
- Envelope (every endpoint): `{result, result_info:{page,per_page,total_pages,...},
  success, errors:[{code,message}]}`. On `!success` → error from `errors[0]`.
  `result_info` is absent on non-paginated endpoints → treat `total_pages==0` as
  single page.
- Preflight: `terraform` present + `CLOUDFLARE_API_TOKEN` set + `GET
  /user/tokens/verify` → `result.status=="active"`.
- Connect: `GET /accounts` → resolve/validate the account scope (default if exactly
  one; error if the passed id isn't visible).

## Resource catalog

Import IDs verified against v4.52.1 docs — the two things not to get wrong. All
enumeration is best-effort per sub-resource (Verbose + continue on 403/404).

| native key | TF type | scope | endpoint | paged | import ID |
|---|---|---|---|---|---|
| cloudflare:zone | cloudflare_zone | acct | `GET /zones?account.id={a}` | yes | `<zone_id>` |
| cloudflare:record | cloudflare_record | zone | `GET /zones/{z}/dns_records` | yes | `<zone_id>/<record_id>` |
| cloudflare:zone_settings | cloudflare_zone_settings_override | zone | singleton per zone | — | `<zone_id>` |
| cloudflare:page_rule | cloudflare_page_rule | zone | `GET /zones/{z}/pagerules` | no | `<zone_id>/<id>` |
| cloudflare:ruleset | cloudflare_ruleset | zone+acct | `GET /zones/{z}/rulesets`, `GET /accounts/{a}/rulesets` | no | `{zone\|account}/<parent_id>/<id>` |
| cloudflare:filter | cloudflare_filter | zone | `GET /zones/{z}/filters` | yes | `<zone_id>/<id>` |
| cloudflare:firewall_rule | cloudflare_firewall_rule | zone | `GET /zones/{z}/firewall/rules` | yes | `<zone_id>/<id>` |
| cloudflare:zone_lockdown | cloudflare_zone_lockdown | zone | `GET /zones/{z}/firewall/lockdowns` | yes | `<zone_id>/<id>` |
| cloudflare:rate_limit | cloudflare_rate_limit | zone | `GET /zones/{z}/rate_limits` | yes | `<zone_id>/<id>` |
| cloudflare:access_rule | cloudflare_access_rule | zone+acct | `GET /{zones/{z}\|accounts/{a}}/firewall/access_rules/rules` | yes | `{account\|zone}/<parent_id>/<id>` |
| cloudflare:load_balancer | cloudflare_load_balancer | zone | `GET /zones/{z}/load_balancers` | no | `<zone_id>/<id>` |
| cloudflare:load_balancer_pool | cloudflare_load_balancer_pool | acct | `GET /accounts/{a}/load_balancers/pools` | no | `<account_id>/<id>` |
| cloudflare:load_balancer_monitor | cloudflare_load_balancer_monitor | acct | `GET /accounts/{a}/load_balancers/monitors` | no | `<account_id>/<id>` |
| cloudflare:custom_ssl | cloudflare_custom_ssl | zone | `GET /zones/{z}/custom_certificates` | yes | `<zone_id>/<id>` **(EXCLUDE)** |
| cloudflare:access_application | cloudflare_access_application | acct | `GET /accounts/{a}/access/apps` | yes | `<account_id>/<id>` (NO prefix) |
| cloudflare:access_policy | cloudflare_access_policy | acct/app | `GET /accounts/{a}/access/apps/{app}/policies` | no | `account/<account_id>/<app_id>/<id>` (WITH prefix) |

### Import-format quirks (§ do not get wrong)
1. `cloudflare_ruleset` and `cloudflare_access_rule` embed a literal `zone`/`account`
   scope word before the parent id.
2. `cloudflare_access_policy` has an `account/` prefix; `cloudflare_access_application`
   does **not**. Inconsistent by design in v4.
3. Everything else is `<parent_id>/<id>` (single slash), except `cloudflare_zone` and
   `cloudflare_zone_settings_override` which are the bare `<zone_id>`.

## Curation gotchas (Phase B, when live)
- **`cloudflare_record` value/data mutual-exclusion**: generate-config-out emits both
  `value` and an (empty) `data {}` block → ConflictsWith break. Drop whichever is
  empty (Terraformer's PostConvertHook). Prune computed: `metadata`, `proxiable`,
  `created_on`, `modified_on`, `hostname`. (v4 uses `value`, not v5's `content`.)
- **`cloudflare_zone`**: prune computed `meta`, `status`, `name_servers`,
  `vanity_name_servers`, `verification_key`, `cname_suffix`, `plan`. `account_id`
  required — author from property if dropped.
- **`cloudflare_zone_settings_override`**: emits a huge `settings {}` with plan-gated
  (Enterprise-only) settings → perpetual diffs. Needs a curated allow-list of
  commonly-writable settings, not a block-list. Most curation-heavy resource.
- **`cloudflare_ruleset`**: skip `kind=="managed"` (read-only Cloudflare-owned); only
  enumerate `custom`/`zone`/`root`.
- **`cloudflare_firewall_rule`**: drop `priority = 0` (default). `filter_id` literal is
  fine (no need to wire the reference).
- **`cloudflare_access_rule`**: skip `scope.type=="organization"` (inherited).
- **`cloudflare_custom_ssl`**: `custom_ssl_options.private_key` is write-only →
  `excludedReason` (surface, don't adopt), like GitHub actions_secret.
- **LB pool/monitor**: `account_id` required — author if dropped; prune
  `created_on`/`modified_on`.

## Deliberately out of scope
- `cloudflare_account_member` — the IAM plane (`Capabilities.IAM=false`).
- Code/data: workers scripts, pages, R2, stream, KV.

## Build order (Phase B increments; Phase A builds all at once)
BEACHHEAD zone+record → INC-1 zone_settings+page_rule → INC-2 ruleset+legacy
firewall+rate_limit → INC-3 access_rule+LB+custom_ssl → INC-4 access apps+policies.
