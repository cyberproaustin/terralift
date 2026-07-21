package opsgenie

// tfTypeMap maps a native Opsgenie resource key ("opsgenie:<kind>") to its Terraform type.
// NB: team membership has NO standalone resource (it is an inline member block on
// opsgenie_team); integration_action and custom_role have no documented import and are
// deferred (not enumerated).
var tfTypeMap = map[string]string{
	"opsgenie:team":                  "opsgenie_team",
	"opsgenie:team_routing_rule":     "opsgenie_team_routing_rule",
	"opsgenie:user":                  "opsgenie_user",
	"opsgenie:user_contact":          "opsgenie_user_contact",
	"opsgenie:notification_rule":     "opsgenie_notification_rule",
	"opsgenie:schedule":              "opsgenie_schedule",
	"opsgenie:schedule_rotation":     "opsgenie_schedule_rotation",
	"opsgenie:escalation":            "opsgenie_escalation",
	"opsgenie:service":               "opsgenie_service",
	"opsgenie:service_incident_rule": "opsgenie_service_incident_rule",
	"opsgenie:api_integration":       "opsgenie_api_integration",
	"opsgenie:email_integration":     "opsgenie_email_integration",
	"opsgenie:alert_policy":          "opsgenie_alert_policy",
	"opsgenie:notification_policy":   "opsgenie_notification_policy",
	"opsgenie:maintenance":           "opsgenie_maintenance",
	"opsgenie:heartbeat":             "opsgenie_heartbeat",
}

func tfType(native string) string { return tfTypeMap[native] }
