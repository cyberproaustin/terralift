package azuread

// tfTypeMap maps a native Entra ID resource key ("azuread:<kind>") to its Terraform type. The
// provider is hashicorp/azuread (v3.x). Phase A adopts CONFIGURATION over the Microsoft Graph API —
// groups, application registrations, service principals, named locations, conditional-access
// policies, administrative units, directory-role assignments, and the two relationship resources
// (group members, app-role assignments). Application/SP secret credentials (passwordCredentials/
// keyCredentials) are never decoded (see curate.go); users (PII/scale) and the credential resources
// are deferred.
var tfTypeMap = map[string]string{
	"azuread:group":                     "azuread_group",
	"azuread:application":               "azuread_application_registration",
	"azuread:service_principal":         "azuread_service_principal",
	"azuread:named_location":            "azuread_named_location",
	"azuread:conditional_access_policy": "azuread_conditional_access_policy",
	"azuread:administrative_unit":       "azuread_administrative_unit",
	"azuread:directory_role_assignment": "azuread_directory_role_assignment",
	"azuread:group_member":              "azuread_group_member",
	"azuread:app_role_assignment":       "azuread_app_role_assignment",
}

func tfType(native string) string { return tfTypeMap[native] }
