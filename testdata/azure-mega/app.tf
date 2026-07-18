##############################################################################
# app.tf
#
# A Linux Web App (B1 plan) and a Linux Function App (Y1 consumption plan),
# each with a rich app_settings block that deliberately mixes benign config
# with INSECURE plaintext secrets and SECURE Key Vault references to the
# *same* underlying credentials where applicable. See MANIFEST.md for the
# full insecure-vs-secure map.
##############################################################################

resource "azurerm_service_plan" "web" {
  name                = "${var.name_prefix}-asp-web"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  os_type             = "Linux"
  sku_name            = "B1"
  tags                = local.common_tags
}

resource "azurerm_service_plan" "func" {
  name                = "${var.name_prefix}-asp-func"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  os_type             = "Linux"
  sku_name            = "Y1"
  tags                = local.common_tags
}

resource "azurerm_linux_web_app" "web" {
  name                = "${var.name_prefix}-web-${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  service_plan_id     = azurerm_service_plan.web.id
  tags                = local.common_tags

  site_config {
    always_on = true

    application_stack {
      node_version = "18-lts"
    }
  }

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.workload.id]
  }

  connection_string {
    name = "PrimarySqlDb"
    type = "SQLAzure"
    # INSECURE: plaintext password baked into a connection string literal.
    value = "Server=tcp:${azurerm_mssql_server.main.fully_qualified_domain_name},1433;Database=${azurerm_mssql_database.main.name};User ID=${local.insecure_sql_admin_login};Password=${local.insecure_sql_admin_password};Encrypt=true;"
  }

  app_settings = {
    "ENVIRONMENT"            = "production"
    "APP_NAME"               = "tlmega-web"
    "LOG_LEVEL"              = "Information"
    "API_BASE_URL"           = "https://api.internal.eutaxia-mega.example.com"
    "FEATURE_FLAG_NEW_UI"    = "true"
    "MAX_UPLOAD_SIZE_MB"     = "25"
    "CACHE_TTL_SECONDS"      = "300"
    "SUPPORT_EMAIL"          = "support@eutaxia-mega.example.com"
    "DEFAULT_LOCALE"         = "en-US"
    "WEBSITE_TIME_ZONE"      = "Eastern Standard Time"
    "ASPNETCORE_ENVIRONMENT" = "Production"
    "COSMOS_DB_ENDPOINT"     = azurerm_cosmosdb_account.main.endpoint

    "APPINSIGHTS_INSTRUMENTATIONKEY"        = azurerm_application_insights.main.instrumentation_key
    "APPLICATIONINSIGHTS_CONNECTION_STRING" = azurerm_application_insights.main.connection_string

    # INSECURE: plaintext DB password embedded in a connection string.
    "DB_CONNECTION_STRING" = "Server=tcp:${azurerm_mssql_server.main.fully_qualified_domain_name},1433;Database=${azurerm_mssql_database.main.name};User ID=${local.insecure_sql_admin_login};Password=${local.insecure_sql_admin_password};"

    # INSECURE: storage connection string with AccountKey= embedded.
    "STORAGE_CONNECTION_STRING" = azurerm_storage_account.pub.primary_connection_string

    # SECURE: same SQL admin password as above, but resolved from Key Vault
    # at runtime instead of being stored in plaintext.
    "KEYVAULT_DB_PASSWORD_REF" = "@Microsoft.KeyVault(SecretUri=${azurerm_key_vault_secret.sql_admin_password.versionless_id})"
  }
}

resource "azurerm_linux_function_app" "func" {
  name                       = "${var.name_prefix}-func-${random_string.suffix.result}"
  resource_group_name        = azurerm_resource_group.apps.name
  location                   = azurerm_resource_group.apps.location
  service_plan_id            = azurerm_service_plan.func.id
  storage_account_name       = azurerm_storage_account.func.name
  storage_account_access_key = azurerm_storage_account.func.primary_access_key
  tags                       = local.common_tags

  site_config {
    application_stack {
      node_version = "18"
    }
  }

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.workload.id]
  }

  app_settings = {
    "FUNCTIONS_WORKER_RUNTIME"       = "node"
    "WEBSITE_NODE_DEFAULT_VERSION"   = "~18"
    "ENVIRONMENT"                    = "production"
    "LOG_LEVEL"                      = "Information"
    "MAX_RETRY_COUNT"                = "3"
    "TIMEOUT_SECONDS"                = "30"
    "FEATURE_ASYNC_PROCESSING"       = "true"
    "WEBSITE_RUN_FROM_PACKAGE"       = "1"
    "AzureWebJobsFeatureFlags"       = "EnableWorkerIndexing"
    "EVENT_HUB_NAMESPACE"            = azurerm_eventhub_namespace.main.name
    "SERVICE_BUS_QUEUE_NAME"         = azurerm_servicebus_queue.orders.name
    "APPINSIGHTS_INSTRUMENTATIONKEY" = azurerm_application_insights.main.instrumentation_key

    # INSECURE: literal third-party API key.
    "THIRD_PARTY_API_KEY" = local.insecure_third_party_api_key

    # SECURE: Key Vault references instead of plaintext values.
    "SENDGRID_API_KEY_REF" = "@Microsoft.KeyVault(SecretUri=${azurerm_key_vault_secret.sendgrid_api_key.versionless_id})"
    "COSMOS_DB_KEY_REF"    = "@Microsoft.KeyVault(SecretUri=${azurerm_key_vault_secret.cosmos_primary_key.versionless_id})"
  }
}
