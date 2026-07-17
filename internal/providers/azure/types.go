package azure

import "strings"

// azureTypeToTF maps an Azure resource type to its azurerm Terraform type.
// Best-effort and incremental (aztfexport does the authoritative mapping during
// export); "" => coverage gap. Keys are lower-cased.
var azureTypeToTF = map[string]string{
	"microsoft.storage/storageaccounts":                  "azurerm_storage_account",
	"microsoft.keyvault/vaults":                          "azurerm_key_vault",
	"microsoft.web/serverfarms":                          "azurerm_service_plan",
	"microsoft.web/sites":                                "azurerm_linux_web_app",
	"microsoft.web/staticsites":                          "azurerm_static_web_app",
	"microsoft.insights/components":                      "azurerm_application_insights",
	"microsoft.insights/actiongroups":                    "azurerm_monitor_action_group",
	"microsoft.operationalinsights/workspaces":           "azurerm_log_analytics_workspace",
	"microsoft.sql/servers":                              "azurerm_mssql_server",
	"microsoft.sql/servers/databases":                    "azurerm_mssql_database",
	"microsoft.cognitiveservices/accounts":               "azurerm_cognitive_account",
	"microsoft.network/dnszones":                         "azurerm_dns_zone",
	"microsoft.network/virtualnetworks":                  "azurerm_virtual_network",
	"microsoft.network/networksecuritygroups":            "azurerm_network_security_group",
	"microsoft.network/publicipaddresses":                "azurerm_public_ip",
	"microsoft.compute/virtualmachines":                  "azurerm_linux_virtual_machine",
	"microsoft.compute/disks":                            "azurerm_managed_disk",
	"microsoft.cdn/profiles":                             "azurerm_cdn_frontdoor_profile",
	"microsoft.logic/workflows":                          "azurerm_logic_app_workflow",
	"microsoft.managedidentity/userassignedidentities":   "azurerm_user_assigned_identity",
	"microsoft.resources/resourcegroups":                 "azurerm_resource_group",
	"microsoft.containerregistry/registries":             "azurerm_container_registry",
	"microsoft.app/managedenvironments":                  "azurerm_container_app_environment",
	"microsoft.app/jobs":                                 "azurerm_container_app_job",
	"microsoft.app/containerapps":                        "azurerm_container_app",
	"microsoft.communication/communicationservices":      "azurerm_communication_service",
	"microsoft.communication/emailservices":              "azurerm_email_communication_service",
	"microsoft.communication/emailservices/domains":      "azurerm_email_communication_service_domain",
	"microsoft.insights/webtests":                        "azurerm_application_insights_web_test",
	"microsoft.insights/metricalerts":                    "azurerm_monitor_metric_alert",
	"microsoft.insights/scheduledqueryrules":             "azurerm_monitor_scheduled_query_rules_alert_v2",
	"microsoft.alertsmanagement/smartdetectoralertrules": "azurerm_monitor_smart_detector_alert_rule",
}

func azureTypeToTFType(azureType string) string {
	return azureTypeToTF[strings.ToLower(azureType)]
}

// roleInfo is a resolved role definition: display name + privilege flag.
type roleInfo struct {
	name       string
	privileged bool
}

// builtinRoles hand-curates the privilege flag for high-impact built-in role
// GUIDs (stable global constants Microsoft does not change). This map WINS over
// az-listed definitions so privilege is never mis-inferred for roles whose names
// lack an obvious verb (e.g. "Key Vault Secrets Officer" can write secrets).
// Source: https://learn.microsoft.com/azure/role-based-access-control/built-in-roles
var builtinRoles = map[string]roleInfo{
	"8e3af657-a8ff-443c-a75c-2fe8c4bcb635": {"Owner", true},
	"b24988ac-6180-42a0-ab88-46d3f8ad76d2": {"Contributor", true},
	"18d7d88d-d35e-4fb5-a5c3-7773c20a72d9": {"User Access Administrator", true},
	"f58310d9-a9f6-439a-9e8d-f62e7b41a168": {"Role Based Access Control Administrator", true},
	"acdd72a7-3385-48ef-bd42-f606fba81ae7": {"Reader", false},
	"ba92f5b4-2d11-453d-a403-e96b0029c9fe": {"Storage Blob Data Contributor", false},
	"00482a5a-887f-4fb3-b363-3b7fe8e74483": {"Key Vault Administrator", true},
	"b86a8fe4-44ce-4948-aee5-eccb2c155cd7": {"Key Vault Secrets Officer", true},
	"14b46e9e-c2b7-41b4-b07b-48a6ebf60603": {"Key Vault Crypto Officer", true},
	"4633458b-17de-408a-b874-0445c86b69e6": {"Key Vault Secrets User", false},
}

// inferPrivileged guesses privilege from a role's display name when it isn't in
// the curated map (write/admin verbs => privileged).
func inferPrivileged(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "owner") ||
		strings.Contains(l, "contributor") ||
		strings.Contains(l, "administrator") ||
		strings.Contains(l, "user access") ||
		l == "admin"
}

// resolveRole returns the role name and privilege for a role-definition id (full
// path or bare guid). Precedence: curated builtinRoles > az-listed defs > name
// heuristic on the raw guid.
func resolveRole(roleDefinitionID string, defs map[string]roleInfo) (string, bool) {
	guid := roleDefinitionID
	if i := strings.LastIndex(guid, "/"); i >= 0 {
		guid = guid[i+1:]
	}
	guid = strings.ToLower(guid)
	if r, ok := builtinRoles[guid]; ok {
		return r.name, r.privileged
	}
	if r, ok := defs[guid]; ok && r.name != "" {
		return r.name, r.privileged
	}
	return guid, inferPrivileged(guid)
}

// containerSet lowercases a container/resource-group filter into a lookup set,
// or nil when the filter is empty (meaning "all containers").
func containerSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[strings.ToLower(n)] = true
	}
	return set
}

// --- small JSON helpers for Resource Graph rows (map[string]any) ---

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func toStringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = str(val)
	}
	return out
}
