# TerraLift GCP test SEED — throwaway control-plane resources for exercising the
# enumerator, the per-type import-ID table, the IAM/exposure enrichers, and the
# born-correct round-trip. Apply into the disposable terralift-lab project, then
# `terraform destroy` when done. Everything is force-destroyable.
#
# Deliberate signals: a PUBLIC bucket (allUsers) and a 0.0.0.0/0 firewall rule
# (exposure), an IAM binding (hygiene), and a Secret Manager secret WITH a value
# (control-plane only — TerraLift must capture the secret resource but NOT its value).

terraform {
  required_version = ">= 1.5"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 7.0" }
  }
}

variable "project_id" { type = string }
variable "region" {
  type    = string
  default = "us-central1"
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# ---- Storage: private + public (public = exposure signal) --------------------
resource "google_storage_bucket" "private" {
  name                        = "${var.project_id}-private"
  location                    = "US"
  force_destroy               = true
  uniform_bucket_level_access = true
}

resource "google_storage_bucket" "public" {
  name                        = "${var.project_id}-public"
  location                    = "US"
  force_destroy               = true
  uniform_bucket_level_access = true
  public_access_prevention    = "inherited"
}

resource "google_storage_bucket_iam_member" "public_read" {
  bucket = google_storage_bucket.public.name
  role   = "roles/storage.objectViewer"
  member = "allUsers" # <-- public exposure the enricher should flag
}

# ---- Pub/Sub -----------------------------------------------------------------
resource "google_pubsub_topic" "events" {
  name = "terralift-events"
}

resource "google_pubsub_subscription" "events_sub" {
  name  = "terralift-events-sub"
  topic = google_pubsub_topic.events.id
}

# ---- Networking: a VPC + a wide-open firewall rule (exposure) -----------------
resource "google_compute_network" "lab" {
  name                    = "terralift-lab-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_firewall" "open_ssh" {
  name          = "terralift-allow-ssh-world"
  network       = google_compute_network.lab.id
  source_ranges = ["0.0.0.0/0"] # <-- exposure the enricher should flag
  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
}

# ---- IAM: a service account + a role binding (hygiene) -----------------------
resource "google_service_account" "app" {
  account_id   = "terralift-app"
  display_name = "TerraLift Lab App SA"
}

resource "google_project_iam_member" "app_viewer" {
  project = var.project_id
  role    = "roles/viewer"
  member  = "serviceAccount:${google_service_account.app.email}"
}

# ---- BigQuery ----------------------------------------------------------------
resource "google_bigquery_dataset" "analytics" {
  dataset_id                 = "terralift_analytics"
  location                   = "US"
  delete_contents_on_destroy = true
}

# ---- Secret Manager: secret + version (value must NOT be captured by TerraLift)
resource "google_secret_manager_secret" "api_key" {
  secret_id = "terralift-api-key"
  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "api_key_v1" {
  secret      = google_secret_manager_secret.api_key.id
  secret_data = "SUPER-SECRET-VALUE-should-not-be-captured"
}
