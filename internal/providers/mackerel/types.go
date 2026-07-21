package mackerel

// tfTypeMap maps a native Mackerel resource key ("mackerel:<kind>") to its Terraform type.
// The provider is mackerelio-labs/mackerel (pre-1.0). The 7 polymorphic monitor kinds (host/
// connectivity/service/external/expression/anomalyDetection/query) all collapse to the single
// mackerel_monitor type and import by bare id — the type discriminator does NOT change the TF type.
// Deferred: hosts (agent-registered, no TF resource), users (invite-only), *_metadata, and the
// default_notification_group singleton.
var tfTypeMap = map[string]string{
	"mackerel:service":             "mackerel_service",
	"mackerel:role":                "mackerel_role",
	"mackerel:monitor":             "mackerel_monitor",
	"mackerel:channel":             "mackerel_channel",
	"mackerel:notification_group":  "mackerel_notification_group",
	"mackerel:dashboard":           "mackerel_dashboard",
	"mackerel:aws_integration":     "mackerel_aws_integration",
	"mackerel:downtime":            "mackerel_downtime",
	"mackerel:alert_group_setting": "mackerel_alert_group_setting",
}

func tfType(native string) string { return tfTypeMap[native] }
