# TerraLift GCP stress seed — free/near-free types chosen to exercise the
# path-shaped import-ID derivation (projects/.../locations/.../<kind>/<name>),
# plus a Secret Manager secret WITH a value (control-plane test) and a
# 0.0.0.0/0 firewall (exposure). e2-micro is free-tier. Torn down immediately.
terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
  }
}

variable "project" { type = string }

provider "google" {
  project = var.project
  region  = "us-central1"
  zone    = "us-central1-a"
}

# --- Storage (import: bucket name) ---
resource "google_storage_bucket" "data" {
  name                        = "tl-stress-${var.project}-data"
  location                    = "US"
  force_destroy               = true
  uniform_bucket_level_access = true
  labels                      = { app = "terralift-stress" }
}

# --- Pub/Sub (import: projects/{{project}}/topics|subscriptions/{{name}}) ---
resource "google_pubsub_topic" "events" {
  name   = "tl-stress-events"
  labels = { app = "terralift-stress" }
}

resource "google_pubsub_subscription" "events" {
  name   = "tl-stress-events-sub"
  topic  = google_pubsub_topic.events.id
  labels = { app = "terralift-stress" }
}

resource "google_pubsub_schema" "schema" {
  name       = "tl-stress-schema"
  type       = "AVRO"
  definition = "{\"type\":\"record\",\"name\":\"m\",\"fields\":[{\"name\":\"id\",\"type\":\"string\"}]}"
}

# --- Compute network (import: projects/{{project}}/global|regions/.../...) ---
resource "google_compute_network" "vpc" {
  name                    = "tl-stress-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "subnet" {
  name          = "tl-stress-subnet"
  ip_cidr_range = "10.60.0.0/24"
  region        = "us-central1"
  network       = google_compute_network.vpc.id
}

resource "google_compute_firewall" "ssh" {
  name          = "tl-stress-allow-ssh"
  network       = google_compute_network.vpc.id
  source_ranges = ["0.0.0.0/0"] # exposure signal
  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
}

resource "google_compute_address" "ip" {
  name   = "tl-stress-ip"
  region = "us-central1"
}

resource "google_compute_router" "router" {
  name    = "tl-stress-router"
  region  = "us-central1"
  network = google_compute_network.vpc.id
}

# --- Compute instance (e2-micro, free-tier; import: projects/.../zones/.../instances/...) ---
resource "google_compute_instance" "vm" {
  name         = "tl-stress-vm"
  machine_type = "e2-micro"
  zone         = "us-central1-a"
  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = 10
    }
  }
  network_interface {
    subnetwork = google_compute_subnetwork.subnet.id
  }
  labels = { app = "terralift-stress" }
}

# --- BigQuery (import: projects/{{project}}/datasets/{{id}} , .../tables/{{id}}) ---
resource "google_bigquery_dataset" "ds" {
  dataset_id = "tl_stress_ds"
  location   = "US"
  labels     = { app = "terralift-stress" }
}

resource "google_bigquery_table" "tbl" {
  dataset_id          = google_bigquery_dataset.ds.dataset_id
  table_id            = "tl_stress_tbl"
  deletion_protection = false
  schema              = jsonencode([{ name = "id", type = "STRING", mode = "NULLABLE" }])
}

# --- IAM (import: various; custom role: projects/{{project}}/roles/{{id}}) ---
resource "google_service_account" "sa" {
  account_id   = "tl-stress-sa"
  display_name = "TerraLift stress SA"
}

resource "google_project_iam_custom_role" "role" {
  role_id     = "tlStressRole"
  title       = "TerraLift Stress Role"
  permissions = ["storage.buckets.get"]
}

resource "google_project_iam_member" "sa_viewer" {
  project = var.project
  role    = "roles/storage.objectViewer"
  member  = "serviceAccount:${google_service_account.sa.email}"
}

# --- KMS (import: projects/.../locations/.../keyRings/... ; .../cryptoKeys/...) ---
resource "google_kms_key_ring" "kr" {
  name     = "tl-stress-kr"
  location = "us-central1"
}

resource "google_kms_crypto_key" "key" {
  name     = "tl-stress-key"
  key_ring = google_kms_key_ring.kr.id
}

# --- DNS (managed zone import: projects/{{project}}/managedZones/{{name}};
#     record set import: {{project}}/{{zone}}/{{name}}/{{type}} — tricky) ---
resource "google_dns_managed_zone" "zone" {
  name        = "tl-stress-zone"
  dns_name    = "tl-stress.example.com."
  description = "stress"
  labels      = { app = "terralift-stress" }
}

resource "google_dns_record_set" "a" {
  name         = "app.tl-stress.example.com."
  managed_zone = google_dns_managed_zone.zone.name
  type         = "A"
  ttl          = 300
  rrdatas      = ["10.60.0.10"]
}

# --- Artifact Registry (import: projects/.../locations/.../repositories/...) ---
resource "google_artifact_registry_repository" "repo" {
  location      = "us-central1"
  repository_id = "tl-stress-repo"
  format        = "DOCKER"
  labels        = { app = "terralift-stress" }
}

# --- Secret Manager: secret + version WITH a value (control-plane test) ---
resource "google_secret_manager_secret" "api" {
  secret_id = "tl-stress-api-key"
  labels    = { app = "terralift-stress" }
  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "api" {
  secret      = google_secret_manager_secret.api.id
  secret_data = "GCP-STRESS-SECRET-do-not-capture"
}
