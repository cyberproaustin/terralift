# Integration-test seed for Azure: a dedicated resource group holding a small,
# cheap set of resources (vnet/subnet/nsg/public-ip/storage). The whole RG is
# created and destroyed by the test, so nothing outside it is ever touched.

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 3.0"
    }
  }
}

variable "subscription_id" {
  type = string
}

variable "rg_name" {
  type = string
}

variable "sa_name" {
  # Storage account: 3-24 chars, lowercase alphanumeric, globally unique.
  type = string
}

variable "location" {
  type    = string
  default = "eastus2"
}

provider "azurerm" {
  features {}
  subscription_id = var.subscription_id
}

resource "azurerm_resource_group" "it" {
  name     = var.rg_name
  location = var.location
}

resource "azurerm_virtual_network" "it" {
  name                = "tl-it-vnet"
  location            = azurerm_resource_group.it.location
  resource_group_name = azurerm_resource_group.it.name
  address_space       = ["10.199.0.0/16"]
}

resource "azurerm_subnet" "it" {
  name                 = "tl-it-subnet"
  resource_group_name  = azurerm_resource_group.it.name
  virtual_network_name = azurerm_virtual_network.it.name
  address_prefixes     = ["10.199.1.0/24"]
}

resource "azurerm_network_security_group" "it" {
  name                = "tl-it-nsg"
  location            = azurerm_resource_group.it.location
  resource_group_name = azurerm_resource_group.it.name
}

resource "azurerm_public_ip" "it" {
  name                = "tl-it-pip"
  location            = azurerm_resource_group.it.location
  resource_group_name = azurerm_resource_group.it.name
  allocation_method   = "Static"
  sku                 = "Standard"
}

resource "azurerm_storage_account" "it" {
  name                     = var.sa_name
  resource_group_name      = azurerm_resource_group.it.name
  location                 = azurerm_resource_group.it.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}
