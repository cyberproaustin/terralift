##############################################################################
# observability.tf
#
# Log Analytics + Application Insights, plus Service Bus and Event Hub as
# messaging/eventing surfaces.
#
# DEVIATION FROM BRIEF: Service Bus Topics are not supported on the Basic
# SKU (an Azure platform constraint, not a cost choice) -- Standard is the
# minimum SKU that supports topics/subscriptions. Namespace below uses
# Standard (list price is still low, ~$10/mo base, and provisions in
# seconds). Event Hub stays on Basic as originally specified since it has
# no such constraint.
##############################################################################

resource "azurerm_log_analytics_workspace" "main" {
  name                = "${var.name_prefix}-log"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  sku                 = "PerGB2018"
  retention_in_days   = 30
  tags                = local.common_tags
}

resource "azurerm_application_insights" "main" {
  name                = "${var.name_prefix}-appi"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  application_type    = "web"
  workspace_id        = azurerm_log_analytics_workspace.main.id
  tags                = local.common_tags
}

resource "azurerm_servicebus_namespace" "main" {
  name                = "${var.name_prefix}-sb-${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  sku                 = "Standard"
  tags                = local.common_tags
}

resource "azurerm_servicebus_queue" "orders" {
  name         = "orders-queue"
  namespace_id = azurerm_servicebus_namespace.main.id
}

resource "azurerm_servicebus_topic" "events" {
  name         = "events-topic"
  namespace_id = azurerm_servicebus_namespace.main.id
}

resource "azurerm_eventhub_namespace" "main" {
  name                = "${var.name_prefix}-eh-${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  sku                 = "Basic"
  capacity            = 1
  tags                = local.common_tags
}

resource "azurerm_eventhub" "orders" {
  name                = "orders-eh"
  resource_group_name = azurerm_resource_group.core.name
  namespace_name      = azurerm_eventhub_namespace.main.name
  partition_count     = 2
  message_retention   = 1
}
