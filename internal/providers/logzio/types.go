package logzio

// tfTypeMap maps a native Logz.io resource key ("logzio:<kind>") to its Terraform type.
// NB: emit logzio_alert_v2 (the current v2 alerts API), NOT the deprecated logzio_alert that
// Terraformer emits. The grafana_* embedded-Grafana plane and the Kibana data-view are deferred.
var tfTypeMap = map[string]string{
	"logzio:alert_v2":              "logzio_alert_v2",
	"logzio:endpoint":              "logzio_endpoint",
	"logzio:drop_filter":           "logzio_drop_filter",
	"logzio:sub_account":           "logzio_subaccount",
	"logzio:user":                  "logzio_user",
	"logzio:log_shipping_token":    "logzio_log_shipping_token",
	"logzio:s3_fetcher":            "logzio_s3_fetcher",
	"logzio:archive_logs":          "logzio_archive_logs",
	"logzio:metrics_account":       "logzio_metrics_account",
	"logzio:authentication_groups": "logzio_authentication_groups",
}

func tfType(native string) string { return tfTypeMap[native] }
