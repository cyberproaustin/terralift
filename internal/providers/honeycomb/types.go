package honeycomb

// tfTypeMap maps a native Honeycomb resource key ("honeycomb:<kind>") to its Terraform type.
// NB: current provider names only — boards are honeycombio_flexible_board (classic
// honeycombio_board is deprecated/removed from main), and recipients are the TYPED resources
// (no generic honeycombio_recipient). Terraformer emits the stale names; we do not copy them.
var tfTypeMap = map[string]string{
	"honeycomb:dataset":                    "honeycombio_dataset",
	"honeycomb:column":                     "honeycombio_column",
	"honeycomb:derived_column":             "honeycombio_derived_column",
	"honeycomb:query_annotation":           "honeycombio_query_annotation",
	"honeycomb:board":                      "honeycombio_flexible_board",
	"honeycomb:trigger":                    "honeycombio_trigger",
	"honeycomb:slo":                        "honeycombio_slo",
	"honeycomb:burn_alert":                 "honeycombio_burn_alert",
	"honeycomb:email_recipient":            "honeycombio_email_recipient",
	"honeycomb:pagerduty_recipient":        "honeycombio_pagerduty_recipient",
	"honeycomb:slack_recipient":            "honeycombio_slack_recipient",
	"honeycomb:webhook_recipient":          "honeycombio_webhook_recipient",
	"honeycomb:msteams_recipient":          "honeycombio_msteams_recipient",
	"honeycomb:msteams_workflow_recipient": "honeycombio_msteams_workflow_recipient",
}

func tfType(native string) string { return tfTypeMap[native] }
