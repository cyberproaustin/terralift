##############################################################################
# network.tf
#
# Resource groups + a hub/spoke-ish network: 2 VNets, 6 subnets (service
# endpoints + one delegation), 2 NSGs (one using inline rules, one using
# standalone azurerm_network_security_rule resources for breadth), a route
# table, bidirectional VNet peering, a public DNS zone + records, a private
# DNS zone + VNet links + a private endpoint, a Basic internal load
# balancer, and public IPs.
##############################################################################

resource "random_string" "suffix" {
  length  = 6
  special = false
  upper   = false
  numeric = true
}

# ---------------------------------------------------------------------------
# Resource groups
# ---------------------------------------------------------------------------

resource "azurerm_resource_group" "core" {
  name     = "${var.name_prefix}-core-${random_string.suffix.result}"
  location = var.location
  tags     = local.common_tags
}

resource "azurerm_resource_group" "apps" {
  name     = "${var.name_prefix}-apps-${random_string.suffix.result}"
  location = var.location
  tags     = local.common_tags
}

# ---------------------------------------------------------------------------
# Hub VNet (shared services: private endpoints, shared subnet)
# ---------------------------------------------------------------------------

resource "azurerm_virtual_network" "hub" {
  name                = "${var.name_prefix}-vnet-hub"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  address_space       = ["10.10.0.0/16"]
  tags                = local.common_tags
}

resource "azurerm_subnet" "hub_shared" {
  name                 = "hub-shared"
  resource_group_name  = azurerm_resource_group.core.name
  virtual_network_name = azurerm_virtual_network.hub.name
  address_prefixes     = ["10.10.1.0/24"]
  service_endpoints    = ["Microsoft.Storage", "Microsoft.KeyVault", "Microsoft.Sql", "Microsoft.EventHub"]
}

resource "azurerm_subnet" "hub_pe" {
  name                 = "hub-private-endpoints"
  resource_group_name  = azurerm_resource_group.core.name
  virtual_network_name = azurerm_virtual_network.hub.name
  address_prefixes     = ["10.10.2.0/24"]

  private_endpoint_network_policies = "Disabled"
}

# ---------------------------------------------------------------------------
# Spoke VNet (workload: VM, load balancer, delegated app subnet)
# ---------------------------------------------------------------------------

resource "azurerm_virtual_network" "spoke" {
  name                = "${var.name_prefix}-vnet-spoke"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  address_space       = ["10.20.0.0/16"]
  tags                = local.common_tags
}

resource "azurerm_subnet" "spoke_app" {
  name                 = "spoke-app"
  resource_group_name  = azurerm_resource_group.core.name
  virtual_network_name = azurerm_virtual_network.spoke.name
  address_prefixes     = ["10.20.1.0/24"]
}

resource "azurerm_subnet" "spoke_data" {
  name                 = "spoke-data"
  resource_group_name  = azurerm_resource_group.core.name
  virtual_network_name = azurerm_virtual_network.spoke.name
  address_prefixes     = ["10.20.2.0/24"]
  service_endpoints    = ["Microsoft.Storage", "Microsoft.Sql"]
}

# Delegated but deliberately left unwired: a common brownfield artifact
# where a subnet was carved out for future App Service regional VNet
# integration and never connected.
resource "azurerm_subnet" "spoke_delegated" {
  name                 = "spoke-delegated-webapp"
  resource_group_name  = azurerm_resource_group.core.name
  virtual_network_name = azurerm_virtual_network.spoke.name
  address_prefixes     = ["10.20.3.0/24"]

  delegation {
    name = "webapp-delegation"

    service_delegation {
      name    = "Microsoft.Web/serverFarms"
      actions = ["Microsoft.Network/virtualNetworks/subnets/action"]
    }
  }
}

resource "azurerm_subnet" "spoke_pe" {
  name                 = "spoke-private-endpoints"
  resource_group_name  = azurerm_resource_group.core.name
  virtual_network_name = azurerm_virtual_network.spoke.name
  address_prefixes     = ["10.20.4.0/24"]

  private_endpoint_network_policies = "Disabled"
}

# ---------------------------------------------------------------------------
# Peering (both directions)
# ---------------------------------------------------------------------------

resource "azurerm_virtual_network_peering" "hub_to_spoke" {
  name                      = "hub-to-spoke"
  resource_group_name       = azurerm_resource_group.core.name
  virtual_network_name      = azurerm_virtual_network.hub.name
  remote_virtual_network_id = azurerm_virtual_network.spoke.id

  allow_virtual_network_access = true
  allow_forwarded_traffic      = true
  allow_gateway_transit        = false
  use_remote_gateways          = false
}

resource "azurerm_virtual_network_peering" "spoke_to_hub" {
  name                      = "spoke-to-hub"
  resource_group_name       = azurerm_resource_group.core.name
  virtual_network_name      = azurerm_virtual_network.spoke.name
  remote_virtual_network_id = azurerm_virtual_network.hub.id

  allow_virtual_network_access = true
  allow_forwarded_traffic      = true
  allow_gateway_transit        = false
  use_remote_gateways          = false
}

# ---------------------------------------------------------------------------
# NSGs -- nsg-app uses inline security_rule blocks, nsg-data uses standalone
# azurerm_network_security_rule resources (both patterns show up in real
# brownfield estates).
# ---------------------------------------------------------------------------

resource "azurerm_network_security_group" "app" {
  name                = "${var.name_prefix}-nsg-app"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  tags                = local.common_tags

  security_rule {
    name                       = "allow-ssh-from-vnet"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = "VirtualNetwork"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "allow-http-from-vnet"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = "VirtualNetwork"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "allow-lb-health-probes"
    priority                   = 120
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "*"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = "AzureLoadBalancer"
    destination_address_prefix = "*"
  }
}

resource "azurerm_network_security_group" "data" {
  name                = "${var.name_prefix}-nsg-data"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  tags                = local.common_tags
}

resource "azurerm_network_security_rule" "data_allow_sql_from_vnet" {
  name                        = "allow-sql-from-vnet"
  priority                    = 100
  direction                   = "Inbound"
  access                      = "Allow"
  protocol                    = "Tcp"
  source_port_range           = "*"
  destination_port_range      = "1433"
  source_address_prefix       = "VirtualNetwork"
  destination_address_prefix  = "*"
  resource_group_name         = azurerm_resource_group.core.name
  network_security_group_name = azurerm_network_security_group.data.name
}

resource "azurerm_network_security_rule" "data_deny_internet_inbound" {
  name                        = "deny-internet-inbound"
  priority                    = 4096
  direction                   = "Inbound"
  access                      = "Deny"
  protocol                    = "*"
  source_port_range           = "*"
  destination_port_range      = "*"
  source_address_prefix       = "Internet"
  destination_address_prefix  = "*"
  resource_group_name         = azurerm_resource_group.core.name
  network_security_group_name = azurerm_network_security_group.data.name
}

resource "azurerm_subnet_network_security_group_association" "app" {
  subnet_id                 = azurerm_subnet.spoke_app.id
  network_security_group_id = azurerm_network_security_group.app.id
}

resource "azurerm_subnet_network_security_group_association" "data" {
  subnet_id                 = azurerm_subnet.spoke_data.id
  network_security_group_id = azurerm_network_security_group.data.id
}

# ---------------------------------------------------------------------------
# Route table -- explicit summarized route back to the hub, harmless
# VnetLocal next-hop (no NVA / gateway required, keeps this fast + cheap).
# ---------------------------------------------------------------------------

resource "azurerm_route_table" "spoke" {
  name                = "${var.name_prefix}-rt-spoke"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  tags                = local.common_tags

  route {
    name           = "to-hub"
    address_prefix = "10.10.0.0/16"
    next_hop_type  = "VnetLocal"
  }
}

resource "azurerm_subnet_route_table_association" "app" {
  subnet_id      = azurerm_subnet.spoke_app.id
  route_table_id = azurerm_route_table.spoke.id
}

# ---------------------------------------------------------------------------
# Public DNS zone + records
# ---------------------------------------------------------------------------

resource "azurerm_dns_zone" "public" {
  name                = "eutaxia-mega-${random_string.suffix.result}.example.com"
  resource_group_name = azurerm_resource_group.core.name
  tags                = local.common_tags
}

resource "azurerm_dns_a_record" "www" {
  name                = "www"
  zone_name           = azurerm_dns_zone.public.name
  resource_group_name = azurerm_resource_group.core.name
  ttl                 = 300
  records             = [azurerm_public_ip.vm.ip_address]
}

resource "azurerm_dns_cname_record" "app" {
  name                = "app"
  zone_name           = azurerm_dns_zone.public.name
  resource_group_name = azurerm_resource_group.core.name
  ttl                 = 300
  record              = azurerm_linux_web_app.web.default_hostname
}

# ---------------------------------------------------------------------------
# Private DNS zones + VNet links + a private endpoint onto the locked-down
# storage account (see storage.tf).
# ---------------------------------------------------------------------------

resource "azurerm_private_dns_zone" "blob" {
  name                = "privatelink.blob.core.windows.net"
  resource_group_name = azurerm_resource_group.core.name
  tags                = local.common_tags
}

resource "azurerm_private_dns_zone" "vault" {
  name                = "privatelink.vaultcore.azure.net"
  resource_group_name = azurerm_resource_group.core.name
  tags                = local.common_tags
}

resource "azurerm_private_dns_zone_virtual_network_link" "blob_hub" {
  name                  = "blob-hub-link"
  resource_group_name   = azurerm_resource_group.core.name
  private_dns_zone_name = azurerm_private_dns_zone.blob.name
  virtual_network_id    = azurerm_virtual_network.hub.id
}

resource "azurerm_private_dns_zone_virtual_network_link" "vault_hub" {
  name                  = "vault-hub-link"
  resource_group_name   = azurerm_resource_group.core.name
  private_dns_zone_name = azurerm_private_dns_zone.vault.name
  virtual_network_id    = azurerm_virtual_network.hub.id
}

resource "azurerm_private_endpoint" "storage_locked_down" {
  name                = "${var.name_prefix}-pe-storage-locked"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  subnet_id           = azurerm_subnet.hub_pe.id
  tags                = local.common_tags

  private_service_connection {
    name                           = "storage-locked-psc"
    private_connection_resource_id = azurerm_storage_account.locked_down.id
    subresource_names              = ["blob"]
    is_manual_connection           = false
  }

  private_dns_zone_group {
    name                 = "blob-dns-zone-group"
    private_dns_zone_ids = [azurerm_private_dns_zone.blob.id]
  }
}

# ---------------------------------------------------------------------------
# Public IPs
# ---------------------------------------------------------------------------

resource "azurerm_public_ip" "vm" {
  name                = "${var.name_prefix}-pip-vm"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = local.common_tags
}

resource "azurerm_public_ip" "spare" {
  name                = "${var.name_prefix}-pip-spare"
  resource_group_name = azurerm_resource_group.core.name
  location            = azurerm_resource_group.core.location
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = local.common_tags
}

# ---------------------------------------------------------------------------
# Basic internal load balancer in front of the VM
# ---------------------------------------------------------------------------

resource "azurerm_lb" "internal" {
  name                = "${var.name_prefix}-ilb"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  sku                 = "Basic"
  tags                = local.common_tags

  frontend_ip_configuration {
    name                          = "internal-frontend"
    subnet_id                     = azurerm_subnet.spoke_app.id
    private_ip_address_allocation = "Static"
    private_ip_address            = "10.20.1.100"
  }
}

resource "azurerm_lb_backend_address_pool" "app" {
  name            = "app-pool"
  loadbalancer_id = azurerm_lb.internal.id
}

resource "azurerm_lb_probe" "http" {
  name            = "http-probe"
  loadbalancer_id = azurerm_lb.internal.id
  protocol        = "Tcp"
  port            = 80
}

resource "azurerm_lb_rule" "http" {
  name                           = "http-rule"
  loadbalancer_id                = azurerm_lb.internal.id
  protocol                       = "Tcp"
  frontend_port                  = 80
  backend_port                   = 80
  frontend_ip_configuration_name = "internal-frontend"
  backend_address_pool_ids       = [azurerm_lb_backend_address_pool.app.id]
  probe_id                       = azurerm_lb_probe.http.id
}
