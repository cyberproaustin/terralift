# Integration-test seed for GCP: free-tier networking + a bucket, exercising the
# pipeline's cross-reference rewiring (subnetwork/firewall -> network). No standing
# cost; the integration test destroys it on completion.

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0"
    }
  }
}

variable "project" {
  type = string
}

variable "region" {
  type    = string
  default = "us-central1"
}

provider "google" {
  project = var.project
  region  = var.region
}

resource "google_compute_network" "it" {
  name                    = "tl-it-net"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "it" {
  name          = "tl-it-subnet"
  ip_cidr_range = "10.199.0.0/24"
  region        = var.region
  network       = google_compute_network.it.id
}

resource "google_compute_firewall" "it" {
  name    = "tl-it-fw"
  network = google_compute_network.it.id

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["10.199.0.0/24"]
}

resource "google_storage_bucket" "it" {
  name          = "tl-it-${var.project}"
  location      = "US"
  force_destroy = true
}
