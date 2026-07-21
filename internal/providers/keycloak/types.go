package keycloak

// tfTypeMap maps a native Keycloak resource key ("keycloak:<kind>") to its Terraform type.
// NB: keycloak_role is ONE type for both realm AND client roles (a client role just carries a
// client_id attribute). Phase-A config core; the long tail (users, protocol mappers,
// default-scope/group assignments, auth sub-flow/execution depth, LDAP mappers, SAML client
// scopes, social IdPs) is deferred.
var tfTypeMap = map[string]string{
	"keycloak:realm":                  "keycloak_realm",
	"keycloak:openid_client":          "keycloak_openid_client",
	"keycloak:saml_client":            "keycloak_saml_client",
	"keycloak:role":                   "keycloak_role",
	"keycloak:group":                  "keycloak_group",
	"keycloak:openid_client_scope":    "keycloak_openid_client_scope",
	"keycloak:authentication_flow":    "keycloak_authentication_flow",
	"keycloak:oidc_identity_provider": "keycloak_oidc_identity_provider",
	"keycloak:saml_identity_provider": "keycloak_saml_identity_provider",
	"keycloak:ldap_user_federation":   "keycloak_ldap_user_federation",
	"keycloak:required_action":        "keycloak_required_action",
}

func tfType(native string) string { return tfTypeMap[native] }
