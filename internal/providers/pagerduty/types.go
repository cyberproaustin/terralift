package pagerduty

// tfTypeMap maps a native PagerDuty resource key ("pagerduty:<kind>") to its Terraform type.
// NB: Event Orchestration supersedes the legacy per-service event rules; Terraformer's
// deprecated pagerduty_service_event_rule is NOT emitted. Rulesets are kept as legacy.
var tfTypeMap = map[string]string{
	"pagerduty:service":                "pagerduty_service",
	"pagerduty:service_integration":    "pagerduty_service_integration",
	"pagerduty:escalation_policy":      "pagerduty_escalation_policy",
	"pagerduty:schedule":               "pagerduty_schedule",
	"pagerduty:team":                   "pagerduty_team",
	"pagerduty:team_membership":        "pagerduty_team_membership",
	"pagerduty:user":                   "pagerduty_user",
	"pagerduty:user_contact_method":    "pagerduty_user_contact_method",
	"pagerduty:user_notification_rule": "pagerduty_user_notification_rule",
	"pagerduty:business_service":       "pagerduty_business_service",
	"pagerduty:maintenance_window":     "pagerduty_maintenance_window",
	"pagerduty:extension":              "pagerduty_extension",
	"pagerduty:extension_servicenow":   "pagerduty_extension_servicenow",
	"pagerduty:webhook_subscription":   "pagerduty_webhook_subscription",
	"pagerduty:tag":                    "pagerduty_tag",
	"pagerduty:response_play":          "pagerduty_response_play",
	"pagerduty:ruleset":                "pagerduty_ruleset",
	"pagerduty:ruleset_rule":           "pagerduty_ruleset_rule",
}

func tfType(native string) string { return tfTypeMap[native] }
