# TerraLift GCP MEGA seed — large, brownfield "before" state used to exercise
# TerraLift's enumerator across the widest practical breadth of GCP resource
# types (50+ distinct google_* types) plus its secrets-review (insecure
# plaintext vs. Secret Manager reference) and storage-hygiene (public vs.
# locked bucket) signals. See MANIFEST.md for the full inventory.
#
# Apply into the disposable terralift-mega-161207246 project; `terraform destroy`
# when done. Everything here is force-destroyable / deletion_protection = false.
# No Cloud VPN, Interconnect, Cloud NAT, GKE, or Filestore — all types here
# provision in a few minutes.

terraform {
  required_version = ">= 1.5"
  required_providers {
    google  = { source = "hashicorp/google", version = "~> 7.0" }
    random  = { source = "hashicorp/random", version = "~> 3.6" }
    archive = { source = "hashicorp/archive", version = "~> 2.4" }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

# Suffix for globally-unique names (buckets, Cloud SQL instance).
resource "random_id" "suffix" {
  byte_length = 3
}
