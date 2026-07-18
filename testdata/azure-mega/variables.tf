##############################################################################
# variables.tf
##############################################################################

variable "subscription_id" {
  description = "Eutaxia test subscription for TerraLift brownfield seeding."
  type        = string
  default     = "81106197-4fec-452c-8cef-69328e602e8a"
}

variable "location" {
  description = "Primary Azure region. Kept single-region to keep this cheap and fast to provision."
  type        = string
  default     = "eastus"
}

variable "name_prefix" {
  description = "Prefix applied to every resource name so teardown is trivially scoped."
  type        = string
  default     = "tlmega"
}

variable "owner" {
  description = "Tag value identifying who owns this throwaway environment."
  type        = string
  default     = "terralift-seed"
}

# ---------------------------------------------------------------------------
# Local values
# ---------------------------------------------------------------------------

locals {
  common_tags = {
    project     = "terralift"
    purpose     = "brownfield-seed"
    owner       = var.owner
    environment = "sandbox"
    teardown    = "safe-to-destroy"
  }

  # INTENTIONALLY INSECURE: hardcoded plaintext credential, reused verbatim
  # below both as the real SQL admin password AND baked into a web app
  # connection-string app setting, so a secrets scanner finds the same
  # literal in two places. This is the "before" anti-pattern TerraLift's
  # secrets review is meant to flag. See MANIFEST.md.
  insecure_sql_admin_password = "P@ssw0rdIns3cure!2024"
  insecure_sql_admin_login    = "tlmegasqladmin"

  # INTENTIONALLY INSECURE: a fake third-party API key literal, embedded
  # directly in the Function App's app_settings. (Scrubbed placeholder — a realistic
  # sk_live_ literal trips GitHub push protection; flagged by the key name regardless.)
  insecure_third_party_api_key = "PLACEHOLDER_third_party_api_key_do_not_use"
}
