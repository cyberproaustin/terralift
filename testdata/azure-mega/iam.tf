##############################################################################
# iam.tf
#
# A user-assigned identity shared by the Web App + Function App, a custom
# role definition, and role assignments at three different scopes.
##############################################################################

resource "azurerm_user_assigned_identity" "workload" {
  name                = "${var.name_prefix}-uai-workload"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  tags                = local.common_tags
}

resource "azurerm_role_definition" "monitoring_reader" {
  name  = "${var.name_prefix}-custom-monitoring-reader"
  scope = azurerm_resource_group.apps.id

  description = "Custom role: read-only access to logs/metrics for the apps resource group, without broader Reader rights."

  permissions {
    actions = [
      "Microsoft.Insights/components/read",
      "Microsoft.OperationalInsights/workspaces/read",
      "Microsoft.OperationalInsights/workspaces/query/read",
      "Microsoft.Web/sites/read",
    ]
    not_actions = []
  }

  assignable_scopes = [
    azurerm_resource_group.apps.id,
  ]
}

# Scope 1: storage account -- data-plane blob access for the workload identity.
resource "azurerm_role_assignment" "workload_storage_blob" {
  scope                = azurerm_storage_account.locked_down.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_user_assigned_identity.workload.principal_id
}

# Scope 2: core resource group -- built-in Reader, for visibility into
# shared networking/observability resources.
resource "azurerm_role_assignment" "workload_core_reader" {
  scope                = azurerm_resource_group.core.id
  role_definition_name = "Reader"
  principal_id         = azurerm_user_assigned_identity.workload.principal_id
}

# Scope 3: apps resource group -- the custom role defined above.
resource "azurerm_role_assignment" "workload_apps_custom" {
  scope              = azurerm_resource_group.apps.id
  role_definition_id = azurerm_role_definition.monitoring_reader.role_definition_resource_id
  principal_id       = azurerm_user_assigned_identity.workload.principal_id
}
