package datadog

// tfTypeMap maps a native Datadog resource key ("datadog:<kind>") to its Terraform type.
// NB: emit datadog_downtime_schedule (v2), NOT the deprecated datadog_downtime; and the
// typed datadog_dashboard/_monitor, NOT the _json escape-hatch variants.
var tfTypeMap = map[string]string{
	"datadog:monitor":                  "datadog_monitor",
	"datadog:dashboard":                "datadog_dashboard",
	"datadog:dashboard_list":           "datadog_dashboard_list",
	"datadog:service_level_objective":  "datadog_service_level_objective",
	"datadog:synthetics_test":          "datadog_synthetics_test",
	"datadog:logs_index":               "datadog_logs_index",
	"datadog:logs_custom_pipeline":     "datadog_logs_custom_pipeline",
	"datadog:logs_metric":              "datadog_logs_metric",
	"datadog:notebook":                 "datadog_notebook",
	"datadog:security_monitoring_rule": "datadog_security_monitoring_rule",
	"datadog:downtime_schedule":        "datadog_downtime_schedule",
	"datadog:role":                     "datadog_role",
	"datadog:user":                     "datadog_user",
}

func tfType(native string) string { return tfTypeMap[native] }
