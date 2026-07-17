# TerraLift Azure stress seed — broad native-type coverage, FREE/near-free +
# cheapest compute SKU (B1s VM), all inside one resource group that is deleted
# entirely at teardown. Data-plane control tests front and center: Key Vault
# secret + key, and storage container/queue/table (must be captured as
# control-plane resources, their CONTENTS/VALUES never captured). NO expensive
# SKUs (no SQL/Cosmos/Redis/premium/AKS).
terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
  }
}

provider "azurerm" {
  features {
    resource_group { prevent_deletion_if_contains_resources = false }
    key_vault { purge_soft_delete_on_destroy = true }
  }
  subscription_id = "81106197-4fec-452c-8cef-69328e602e8a"
}

data "azurerm_client_config" "current" {}

locals {
  suffix = "tlstress81106"
  tags   = { app = "terralift-stress" }
}

resource "azurerm_resource_group" "rg" {
  name     = "tl-stress-rg"
  location = "eastus"
  tags     = local.tags
}

############################################
# Storage — data-plane content control test
############################################
resource "azurerm_storage_account" "sa" {
  name                     = local.suffix
  resource_group_name      = azurerm_resource_group.rg.name
  location                 = azurerm_resource_group.rg.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  tags                     = local.tags
}

resource "azurerm_storage_container" "c" {
  name                  = "tl-stress-data"
  storage_account_id    = azurerm_storage_account.sa.id
  container_access_type = "private"
}

resource "azurerm_storage_queue" "q" {
  name               = "tl-stress-queue"
  storage_account_name = azurerm_storage_account.sa.name
}

resource "azurerm_storage_table" "t" {
  name                 = "tlstresstable"
  storage_account_name = azurerm_storage_account.sa.name
}

resource "azurerm_storage_share" "sh" {
  name               = "tl-stress-share"
  storage_account_id = azurerm_storage_account.sa.id
  quota              = 1
}

############################################
# Key Vault — data-plane secret material control test
############################################
resource "azurerm_key_vault" "kv" {
  name                       = "tl-stress-kv-81106"
  resource_group_name        = azurerm_resource_group.rg.name
  location                   = azurerm_resource_group.rg.location
  tenant_id                  = data.azurerm_client_config.current.tenant_id
  sku_name                   = "standard"
  soft_delete_retention_days = 7
  access_policy {
    tenant_id = data.azurerm_client_config.current.tenant_id
    object_id = data.azurerm_client_config.current.object_id
    secret_permissions      = ["Get", "Set", "List", "Delete", "Purge"]
    key_permissions         = ["Get", "Create", "List", "Delete", "Purge", "GetRotationPolicy"]
    certificate_permissions = ["Get", "Create", "List", "Delete", "Purge"]
  }
  tags = local.tags
}

resource "azurerm_key_vault_secret" "s" {
  name         = "tl-stress-api-key"
  value        = "AZURE-STRESS-SECRET-do-not-capture"
  key_vault_id = azurerm_key_vault.kv.id
}

resource "azurerm_key_vault_key" "k" {
  name         = "tl-stress-key"
  key_vault_id = azurerm_key_vault.kv.id
  key_type     = "RSA"
  key_size     = 2048
  key_opts     = ["encrypt", "decrypt", "sign", "verify"]
}

############################################
# Networking (+ 0.0.0.0/0 exposure)
############################################
resource "azurerm_virtual_network" "vnet" {
  name                = "tl-stress-vnet"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  address_space       = ["10.70.0.0/16"]
  tags                = local.tags
}

resource "azurerm_subnet" "subnet" {
  name                 = "tl-stress-subnet"
  resource_group_name  = azurerm_resource_group.rg.name
  virtual_network_name = azurerm_virtual_network.vnet.name
  address_prefixes     = ["10.70.1.0/24"]
}

resource "azurerm_network_security_group" "nsg" {
  name                = "tl-stress-nsg"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  security_rule {
    name                       = "allow-ssh-any"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = "0.0.0.0/0"
    destination_address_prefix = "*"
  }
  tags = local.tags
}

resource "azurerm_public_ip" "pip" {
  name                = "tl-stress-pip"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = local.tags
}

resource "azurerm_route_table" "rt" {
  name                = "tl-stress-rt"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  tags                = local.tags
}

resource "azurerm_private_dns_zone" "pdns" {
  name                = "tl-stress.internal"
  resource_group_name = azurerm_resource_group.rg.name
  tags                = local.tags
}

resource "azurerm_network_interface" "nic" {
  name                = "tl-stress-nic"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.subnet.id
    private_ip_address_allocation = "Dynamic"
  }
  tags = local.tags
}

############################################
# Compute — cheapest SKU (B1s), torn down immediately
############################################
resource "azurerm_managed_disk" "disk" {
  name                 = "tl-stress-disk"
  resource_group_name  = azurerm_resource_group.rg.name
  location             = azurerm_resource_group.rg.location
  storage_account_type = "Standard_LRS"
  create_option        = "Empty"
  disk_size_gb         = 4
  tags                 = local.tags
}

resource "azurerm_linux_virtual_machine" "vm" {
  name                            = "tl-stress-vm"
  resource_group_name             = azurerm_resource_group.rg.name
  location                        = azurerm_resource_group.rg.location
  size                            = "Standard_B1s"
  admin_username                  = "azureuser"
  admin_password                  = "TlStress!Pass2026x"
  disable_password_authentication = false
  network_interface_ids           = [azurerm_network_interface.nic.id]
  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }
  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts"
    version   = "latest"
  }
  tags = local.tags
}

############################################
# Identity
############################################
resource "azurerm_user_assigned_identity" "id" {
  name                = "tl-stress-id"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  tags                = local.tags
}

############################################
# App / web
############################################
resource "azurerm_service_plan" "plan" {
  name                = "tl-stress-plan"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  os_type             = "Linux"
  sku_name            = "B1"
  tags                = local.tags
}

resource "azurerm_linux_web_app" "web" {
  name                = "tl-stress-web-81106"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_service_plan.plan.location
  service_plan_id     = azurerm_service_plan.plan.id
  site_config {}
  tags = local.tags
}

############################################
# Observability
############################################
resource "azurerm_log_analytics_workspace" "law" {
  name                = "tl-stress-law"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku                 = "PerGB2018"
  retention_in_days   = 30
  tags                = local.tags
}

resource "azurerm_application_insights" "ai" {
  name                = "tl-stress-ai"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  application_type    = "web"
  workspace_id        = azurerm_log_analytics_workspace.law.id
  tags                = local.tags
}

resource "azurerm_monitor_action_group" "ag" {
  name                = "tl-stress-ag"
  resource_group_name = azurerm_resource_group.rg.name
  short_name          = "tlstress"
  tags                = local.tags
}

############################################
# Messaging / integration
############################################
resource "azurerm_servicebus_namespace" "sb" {
  name                = "tl-stress-sb-81106"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku                 = "Basic"
  tags                = local.tags
}

resource "azurerm_servicebus_queue" "sbq" {
  name         = "tl-stress-q"
  namespace_id = azurerm_servicebus_namespace.sb.id
}

resource "azurerm_eventgrid_topic" "egt" {
  name                = "tl-stress-egt"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  tags                = local.tags
}

resource "azurerm_eventhub_namespace" "ehn" {
  name                = "tl-stress-ehn-81106"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku                 = "Basic"
  capacity            = 1
  tags                = local.tags
}

resource "azurerm_eventhub" "eh" {
  name              = "tl-stress-eh"
  namespace_id      = azurerm_eventhub_namespace.ehn.id
  partition_count   = 1
  message_retention = 1
}

############################################
# Containers / config / automation
############################################
resource "azurerm_container_registry" "acr" {
  name                = "tlstressacr81106"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku                 = "Basic"
  tags                = local.tags
}

resource "azurerm_automation_account" "aa" {
  name                = "tl-stress-aa"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku_name            = "Basic"
  tags                = local.tags
}
