package launchdarkly

// tfTypeMap maps a native LaunchDarkly resource key ("launchdarkly:<kind>") to its Terraform
// type. Phase-A config core; the long tail (access_token, relay_proxy_configuration,
// flag_trigger/approvals/workflows, context_kind, inline project environments) is deferred.
var tfTypeMap = map[string]string{
	"launchdarkly:project":                  "launchdarkly_project",
	"launchdarkly:environment":              "launchdarkly_environment",
	"launchdarkly:feature_flag":             "launchdarkly_feature_flag",
	"launchdarkly:feature_flag_environment": "launchdarkly_feature_flag_environment",
	"launchdarkly:segment":                  "launchdarkly_segment",
	"launchdarkly:destination":              "launchdarkly_destination",
	"launchdarkly:metric":                   "launchdarkly_metric",
	"launchdarkly:webhook":                  "launchdarkly_webhook",
	"launchdarkly:team":                     "launchdarkly_team",
	"launchdarkly:custom_role":              "launchdarkly_custom_role",
}

func tfType(native string) string { return tfTypeMap[native] }
