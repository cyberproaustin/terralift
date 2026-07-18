##############################################################################
# data.tf
#
# Cosmos DB (SQL API, free tier) and Azure SQL (Basic tier, cheapest/fastest
# DTU model).
#
# NOTE: enable_free_tier assumes no other free-tier Cosmos account already
# exists in this subscription (Azure allows exactly one per subscription).
# If apply fails on that constraint, flip it to false.
##############################################################################

resource "azurerm_cosmosdb_account" "main" {
  name                = "${var.name_prefix}-cosmos-${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location

  offer_type        = "Standard"
  kind              = "GlobalDocumentDB"
  free_tier_enabled = true

  consistency_policy {
    consistency_level = "Session"
  }

  geo_location {
    location          = azurerm_resource_group.apps.location
    failover_priority = 0
  }

  tags = local.common_tags
}

resource "azurerm_cosmosdb_sql_database" "main" {
  name                = "tlmega-catalog"
  resource_group_name = azurerm_resource_group.apps.name
  account_name        = azurerm_cosmosdb_account.main.name
  throughput          = 400
}

resource "azurerm_cosmosdb_sql_container" "items" {
  name                = "items"
  resource_group_name = azurerm_resource_group.apps.name
  account_name        = azurerm_cosmosdb_account.main.name
  database_name       = azurerm_cosmosdb_sql_database.main.name
  partition_key_paths = ["/id"]
}

# ---------------------------------------------------------------------------
# Azure SQL -- logical server + Basic database.
# ---------------------------------------------------------------------------

resource "azurerm_mssql_server" "main" {
  name                = "${var.name_prefix}-sql-${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location

  version                      = "12.0"
  administrator_login          = local.insecure_sql_admin_login
  administrator_login_password = local.insecure_sql_admin_password

  tags = local.common_tags
}

resource "azurerm_mssql_database" "main" {
  name        = "tlmega-db"
  server_id   = azurerm_mssql_server.main.id
  sku_name    = "Basic"
  max_size_gb = 2

  tags = local.common_tags
}

resource "azurerm_mssql_firewall_rule" "allow_azure_services" {
  name             = "allow-azure-services"
  server_id        = azurerm_mssql_server.main.id
  start_ip_address = "0.0.0.0"
  end_ip_address   = "0.0.0.0"
}
