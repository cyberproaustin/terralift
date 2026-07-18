##############################################################################
# storage.tf
#
# Three storage accounts with deliberately different postures:
#   - "pub"          : blob public access ON, container is public   -- INSECURE
#   - "locked_down"   : public access OFF, network-restricted, fronted
#                       by the private endpoint in network.tf        -- SECURE
#   - "func"          : plain private account backing the Function App runtime
##############################################################################

resource "azurerm_storage_account" "pub" {
  name                = "${var.name_prefix}pub${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location

  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  allow_nested_items_to_be_public = true
  min_tls_version                 = "TLS1_2"

  tags = merge(local.common_tags, { posture = "public-insecure" })
}

resource "azurerm_storage_container" "pub_assets" {
  name                  = "public-assets"
  storage_account_name  = azurerm_storage_account.pub.name
  container_access_type = "container"
}

resource "azurerm_storage_account" "locked_down" {
  name                = "${var.name_prefix}sec${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location

  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  allow_nested_items_to_be_public = false
  min_tls_version                 = "TLS1_2"

  network_rules {
    default_action = "Deny"
    bypass         = ["AzureServices"]
  }

  tags = merge(local.common_tags, { posture = "locked-down-secure" })
}

resource "azurerm_storage_container" "locked_private" {
  name                  = "private-data"
  storage_account_name  = azurerm_storage_account.locked_down.name
  container_access_type = "private"
}

resource "azurerm_storage_queue" "orders" {
  name                 = "orders"
  storage_account_name = azurerm_storage_account.locked_down.name
}

resource "azurerm_storage_table" "sessions" {
  name                 = "sessions"
  storage_account_name = azurerm_storage_account.locked_down.name
}

# ---------------------------------------------------------------------------
# Dedicated storage account backing the Function App runtime (required by
# the Azure Functions host, kept separate from the app-data accounts above).
# ---------------------------------------------------------------------------

resource "azurerm_storage_account" "func" {
  name                = "${var.name_prefix}func${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location

  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  allow_nested_items_to_be_public = false
  min_tls_version                 = "TLS1_2"

  tags = local.common_tags
}
