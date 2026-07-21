package okta

// tfTypeMap maps a native Okta resource key ("okta:<kind>") to its Terraform type. Phase-A
// config core; the long tail (schema properties, brand/theme/email, factors, app-assignment
// composites, behaviors, social IdPs, the other policy families) is deferred.
var tfTypeMap = map[string]string{
	"okta:user":                      "okta_user",
	"okta:group":                     "okta_group",
	"okta:group_rule":                "okta_group_rule",
	"okta:user_type":                 "okta_user_type",
	"okta:app_oauth":                 "okta_app_oauth",
	"okta:app_saml":                  "okta_app_saml",
	"okta:app_auto_login":            "okta_app_auto_login",
	"okta:app_bookmark":              "okta_app_bookmark",
	"okta:app_basic_auth":            "okta_app_basic_auth",
	"okta:app_swa":                   "okta_app_swa",
	"okta:app_three_field":           "okta_app_three_field",
	"okta:app_secure_password_store": "okta_app_secure_password_store",
	"okta:trusted_origin":            "okta_trusted_origin",
	"okta:network_zone":              "okta_network_zone",
	"okta:auth_server":               "okta_auth_server",
	"okta:auth_server_scope":         "okta_auth_server_scope",
	"okta:auth_server_claim":         "okta_auth_server_claim",
	"okta:auth_server_policy":        "okta_auth_server_policy",
	"okta:auth_server_policy_rule":   "okta_auth_server_policy_rule",
	"okta:policy_signon":             "okta_policy_signon",
	"okta:policy_password":           "okta_policy_password",
	"okta:policy_mfa":                "okta_policy_mfa",
	"okta:policy_rule_signon":        "okta_policy_rule_signon",
	"okta:policy_rule_password":      "okta_policy_rule_password",
	"okta:policy_rule_mfa":           "okta_policy_rule_mfa",
	"okta:inline_hook":               "okta_inline_hook",
	"okta:event_hook":                "okta_event_hook",
	"okta:idp_oidc":                  "okta_idp_oidc",
	"okta:idp_saml":                  "okta_idp_saml",
}

func tfType(native string) string { return tfTypeMap[native] }
