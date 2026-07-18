##############################################################################
# compute.tf
#
# One small Linux VM (NIC + OS disk + attached data disk, wired into the
# internal LB backend pool), a Basic Container Registry, and a small
# Container Instance running a public image (decoupled from the ACR --
# a common brownfield pattern where the registry was provisioned but
# nothing has been pushed to it yet).
##############################################################################

resource "tls_private_key" "vm_ssh" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "azurerm_network_interface" "vm" {
  name                = "${var.name_prefix}-nic-vm"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  tags                = local.common_tags

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.spoke_app.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.vm.id
  }
}

resource "azurerm_network_interface_backend_address_pool_association" "vm" {
  network_interface_id    = azurerm_network_interface.vm.id
  ip_configuration_name   = "internal"
  backend_address_pool_id = azurerm_lb_backend_address_pool.app.id
}

resource "azurerm_linux_virtual_machine" "app" {
  name                = "${var.name_prefix}-vm-app"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  size                = "Standard_B1s"
  admin_username      = "tlmegaadmin"
  network_interface_ids = [
    azurerm_network_interface.vm.id,
  ]
  tags = local.common_tags

  admin_ssh_key {
    username   = "tlmegaadmin"
    public_key = tls_private_key.vm_ssh.public_key_openssh
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts-gen2"
    version   = "latest"
  }
}

resource "azurerm_managed_disk" "vm_data" {
  name                 = "${var.name_prefix}-disk-vm-data"
  resource_group_name  = azurerm_resource_group.apps.name
  location             = azurerm_resource_group.apps.location
  storage_account_type = "Standard_LRS"
  create_option        = "Empty"
  disk_size_gb         = 4
  tags                 = local.common_tags
}

resource "azurerm_virtual_machine_data_disk_attachment" "vm_data" {
  managed_disk_id    = azurerm_managed_disk.vm_data.id
  virtual_machine_id = azurerm_linux_virtual_machine.app.id
  lun                = 0
  caching            = "ReadWrite"
}

# ---------------------------------------------------------------------------
# Container Registry (Basic) -- provisioned, not yet wired to any pipeline.
# ---------------------------------------------------------------------------

resource "azurerm_container_registry" "main" {
  name                = "${var.name_prefix}acr${random_string.suffix.result}"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  sku                 = "Basic"
  admin_enabled       = false
  tags                = local.common_tags
}

# ---------------------------------------------------------------------------
# Container Instance -- small, public sample image, independent of the ACR.
# ---------------------------------------------------------------------------

resource "azurerm_container_group" "worker" {
  name                = "${var.name_prefix}-aci-worker"
  resource_group_name = azurerm_resource_group.apps.name
  location            = azurerm_resource_group.apps.location
  os_type             = "Linux"
  restart_policy      = "OnFailure"
  tags                = local.common_tags

  container {
    name   = "worker"
    image  = "mcr.microsoft.com/azuredocs/aci-helloworld:latest"
    cpu    = "0.5"
    memory = "0.5"

    ports {
      port     = 80
      protocol = "TCP"
    }
  }
}
