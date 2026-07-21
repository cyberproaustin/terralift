package newrelic

// tfTypeMap maps a native New Relic resource key ("newrelic:<kind>") to its Terraform type.
// NB: current provider names only — emit newrelic_one_dashboard (NOT the deprecated
// newrelic_dashboard), newrelic_nrql_alert_condition (NOT the legacy _alert_condition /
// _infra_alert_condition), and the notification stack (NOT the deprecated
// newrelic_alert_channel). Terraformer emits the deprecated set; we do not copy it.
var tfTypeMap = map[string]string{
	"newrelic:dashboard":                       "newrelic_one_dashboard",
	"newrelic:alert_policy":                    "newrelic_alert_policy",
	"newrelic:nrql_alert_condition":            "newrelic_nrql_alert_condition",
	"newrelic:alert_muting_rule":               "newrelic_alert_muting_rule",
	"newrelic:notification_destination":        "newrelic_notification_destination",
	"newrelic:notification_channel":            "newrelic_notification_channel",
	"newrelic:workflow":                        "newrelic_workflow",
	"newrelic:synthetics_monitor":              "newrelic_synthetics_monitor",
	"newrelic:synthetics_script_monitor":       "newrelic_synthetics_script_monitor",
	"newrelic:synthetics_cert_check_monitor":   "newrelic_synthetics_cert_check_monitor",
	"newrelic:synthetics_broken_links_monitor": "newrelic_synthetics_broken_links_monitor",
	"newrelic:synthetics_step_monitor":         "newrelic_synthetics_step_monitor",
	"newrelic:workload":                        "newrelic_workload",
	"newrelic:key_transaction":                 "newrelic_key_transaction",
	"newrelic:obfuscation_rule":                "newrelic_obfuscation_rule",
	"newrelic:obfuscation_expression":          "newrelic_obfuscation_expression",
}

func tfType(native string) string { return tfTypeMap[native] }
