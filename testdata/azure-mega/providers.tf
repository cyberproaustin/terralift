##############################################################################
# providers.tf
#
# Brownfield "before" environment for TerraLift onboarding tests. This
# Terraform CREATES real infrastructure directly against the Eutaxia
# subscription -- it is intentionally NOT organized the way TerraLift would
# organize it. See MANIFEST.md for the full resource inventory and the
# insecure-vs-secure secret comparison this environment is designed to
# exercise.
##############################################################################

terraform {
  required_version = ">= 1.5"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
  }
}

provider "azurerm" {
  subscription_id = var.subscription_id

  features {
    key_vault {
      purge_soft_delete_on_destroy    = true
      recover_soft_deleted_key_vaults = true
    }
    resource_group {
      prevent_deletion_if_contains_resources = false
    }
  }
}

data "azurerm_client_config" "current" {}
