##############################################################################
# security.tf
#
# Key Vault using the classic access-policy model (common in brownfield
# estates that predate RBAC-based vaults). Holds the SECURE counterparts of
# the secrets that are deliberately leaked in plaintext elsewhere -- see
# MANIFEST.md for the full insecure-vs-secure map.
##############################################################################

resource "azurerm_key_vault" "main" {
  name                = "${var.name_prefix}-kv-${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  tenant_id           = data.azurerm_client_config.current.tenant_id
  sku_name            = "standard"

  soft_delete_retention_days = 7
  purge_protection_enabled   = false

  tags = local.common_tags
}

# The deploying principal gets full secret management so this Terraform can
# create/update secrets.
resource "azurerm_key_vault_access_policy" "deployer" {
  key_vault_id = azurerm_key_vault.main.id
  tenant_id    = data.azurerm_client_config.current.tenant_id
  object_id    = data.azurerm_client_config.current.object_id

  secret_permissions = ["Get", "List", "Set", "Delete", "Purge", "Recover"]
}

# The user-assigned identity (attached to the Web App + Function App, see
# app.tf / iam.tf) gets read-only access so its Key Vault references resolve.
resource "azurerm_key_vault_access_policy" "workload_identity" {
  key_vault_id = azurerm_key_vault.main.id
  tenant_id    = data.azurerm_client_config.current.tenant_id
  object_id    = azurerm_user_assigned_identity.workload.principal_id

  secret_permissions = ["Get", "List"]
}

resource "random_password" "sendgrid_api_key" {
  length  = 32
  special = false
}

# SECURE counterpart of the SQL admin password that is also (deliberately)
# hardcoded in plaintext in app.tf / data.tf. See MANIFEST.md.
resource "azurerm_key_vault_secret" "sql_admin_password" {
  name         = "sql-admin-password"
  value        = local.insecure_sql_admin_password
  key_vault_id = azurerm_key_vault.main.id

  depends_on = [azurerm_key_vault_access_policy.deployer]
}

resource "azurerm_key_vault_secret" "sendgrid_api_key" {
  name         = "sendgrid-api-key"
  value        = random_password.sendgrid_api_key.result
  key_vault_id = azurerm_key_vault.main.id

  depends_on = [azurerm_key_vault_access_policy.deployer]
}

resource "azurerm_key_vault_secret" "cosmos_primary_key" {
  name         = "cosmos-primary-key"
  value        = azurerm_cosmosdb_account.main.primary_key
  key_vault_id = azurerm_key_vault.main.id

  depends_on = [azurerm_key_vault_access_policy.deployer]
}
