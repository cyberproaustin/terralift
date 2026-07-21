package cloudflare

// tfTypeMap maps a native Cloudflare resource key (our "cloudflare:<kind>" scheme)
// to its Terraform type in the cloudflare/cloudflare v4 provider.
var tfTypeMap = map[string]string{
	"cloudflare:zone":                  "cloudflare_zone",
	"cloudflare:record":                "cloudflare_record",
	"cloudflare:zone_settings":         "cloudflare_zone_settings_override",
	"cloudflare:page_rule":             "cloudflare_page_rule",
	"cloudflare:ruleset":               "cloudflare_ruleset",
	"cloudflare:filter":                "cloudflare_filter",
	"cloudflare:firewall_rule":         "cloudflare_firewall_rule",
	"cloudflare:zone_lockdown":         "cloudflare_zone_lockdown",
	"cloudflare:rate_limit":            "cloudflare_rate_limit",
	"cloudflare:access_rule":           "cloudflare_access_rule",
	"cloudflare:load_balancer":         "cloudflare_load_balancer",
	"cloudflare:load_balancer_pool":    "cloudflare_load_balancer_pool",
	"cloudflare:load_balancer_monitor": "cloudflare_load_balancer_monitor",
	"cloudflare:custom_ssl":            "cloudflare_custom_ssl",
	"cloudflare:access_application":    "cloudflare_access_application",
	"cloudflare:access_policy":         "cloudflare_access_policy",
}

func tfType(native string) string { return tfTypeMap[native] }
