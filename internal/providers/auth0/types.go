package auth0

// tfTypeMap maps a native Auth0 resource key ("auth0:<kind>") to its Terraform type. Phase-A
// config core; the long tail (the user plane, the :: relationship/membership resources, the
// deprecated rules/hooks, custom domains, singleton sub-settings, Forms/Flows) is deferred.
// NB: current provider names — auth0_email_provider (not the retired auth0_email), auth0_action
// (not the deprecated auth0_rule/_hook).
var tfTypeMap = map[string]string{
	"auth0:client":            "auth0_client",
	"auth0:resource_server":   "auth0_resource_server",
	"auth0:connection":        "auth0_connection",
	"auth0:role":              "auth0_role",
	"auth0:action":            "auth0_action",
	"auth0:organization":      "auth0_organization",
	"auth0:client_grant":      "auth0_client_grant",
	"auth0:log_stream":        "auth0_log_stream",
	"auth0:email_template":    "auth0_email_template",
	"auth0:tenant":            "auth0_tenant",
	"auth0:branding":          "auth0_branding",
	"auth0:attack_protection": "auth0_attack_protection",
	"auth0:prompt":            "auth0_prompt",
	"auth0:guardian":          "auth0_guardian",
	"auth0:email_provider":    "auth0_email_provider",
}

func tfType(native string) string { return tfTypeMap[native] }
