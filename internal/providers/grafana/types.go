package grafana

// tfTypeMap maps a native Grafana resource key ("grafana:<kind>") to its Terraform type.
// NB: emit grafana_contact_point (unified alerting), NOT the deprecated
// grafana_alert_notification; and folder refs by uid, not the legacy numeric id.
var tfTypeMap = map[string]string{
	"grafana:dashboard":           "grafana_dashboard",
	"grafana:folder":              "grafana_folder",
	"grafana:data_source":         "grafana_data_source",
	"grafana:contact_point":       "grafana_contact_point",
	"grafana:notification_policy": "grafana_notification_policy",
	"grafana:message_template":    "grafana_message_template",
	"grafana:mute_timing":         "grafana_mute_timing",
	"grafana:rule_group":          "grafana_rule_group",
	"grafana:team":                "grafana_team",
	"grafana:service_account":     "grafana_service_account",
	"grafana:playlist":            "grafana_playlist",
	"grafana:library_panel":       "grafana_library_panel",
	"grafana:role":                "grafana_role",
	"grafana:report":              "grafana_report",
}

func tfType(native string) string { return tfTypeMap[native] }
