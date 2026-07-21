package cloudflare

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Cloudflare
// ids are hex so injection risk is low, but escaping keeps parity with the other
// providers (hcl.ImportBlock renders with %q, which does not neutralize ${ } / %{ }).
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

// rawImportID builds the un-escaped import ID per the cloudflare/cloudflare v4.52.1
// docs. Formats verified there; the scope-word and prefix quirks are load-bearing:
//   - ruleset / access_rule embed a literal "zone"/"account" scope word.
//   - access_policy has an "account/" prefix; access_application has NONE.
//   - everything else is "<parent_id>/<id>"; zone / zone_settings are the bare zone id.
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "cloudflare_zone", "cloudflare_zone_settings_override":
		return p("zone_id")
	case "cloudflare_record":
		return p("zone_id") + "/" + p("record_id")
	case "cloudflare_page_rule":
		return p("zone_id") + "/" + p("page_rule_id")
	case "cloudflare_ruleset":
		return p("scope") + "/" + p("parent_id") + "/" + p("ruleset_id")
	case "cloudflare_filter":
		return p("zone_id") + "/" + p("filter_id")
	case "cloudflare_firewall_rule":
		return p("zone_id") + "/" + p("firewall_rule_id")
	case "cloudflare_zone_lockdown":
		return p("zone_id") + "/" + p("lockdown_id")
	case "cloudflare_rate_limit":
		return p("zone_id") + "/" + p("rate_limit_id")
	case "cloudflare_access_rule":
		return p("scope") + "/" + p("parent_id") + "/" + p("rule_id")
	case "cloudflare_load_balancer":
		return p("zone_id") + "/" + p("load_balancer_id")
	case "cloudflare_load_balancer_pool":
		return p("account_id") + "/" + p("pool_id")
	case "cloudflare_load_balancer_monitor":
		return p("account_id") + "/" + p("monitor_id")
	case "cloudflare_custom_ssl":
		return p("zone_id") + "/" + p("certificate_id")
	case "cloudflare_access_application":
		return p("account_id") + "/" + p("app_id") // NO scope prefix
	case "cloudflare_access_policy":
		return "account/" + p("account_id") + "/" + p("app_id") + "/" + p("policy_id") // WITH prefix
	default:
		return r.ID
	}
}
