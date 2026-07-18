# ---- APIs (brownfield projects typically manage enablement in TF too) -------
locals {
  required_apis = [
    "compute.googleapis.com",
    "run.googleapis.com",
    "cloudfunctions.googleapis.com",
    "cloudbuild.googleapis.com",
    "artifactregistry.googleapis.com",
    "eventarc.googleapis.com",
    "sqladmin.googleapis.com",
    "bigquery.googleapis.com",
    "pubsub.googleapis.com",
    "cloudscheduler.googleapis.com",
    "cloudtasks.googleapis.com",
    "secretmanager.googleapis.com",
    "cloudkms.googleapis.com",
    "dns.googleapis.com",
    "monitoring.googleapis.com",
    "logging.googleapis.com",
    "iam.googleapis.com",
  ]
}

resource "google_project_service" "apis" {
  for_each = toset(local.required_apis)
  project  = var.project_id
  service  = each.value

  disable_on_destroy = false # don't disable shared APIs on teardown — avoids destroy-order races
}

data "google_project" "current" {}

# ---- Service accounts ---------------------------------------------------------
resource "google_service_account" "app" {
  account_id   = "${var.prefix}-app"
  display_name = "TerraLift mega-seed app runtime (Cloud Run + Functions)"
}

resource "google_service_account" "compute" {
  account_id   = "${var.prefix}-compute"
  display_name = "TerraLift mega-seed VM/MIG runtime"
}

resource "google_service_account" "pipeline" {
  account_id   = "${var.prefix}-pipeline"
  display_name = "TerraLift mega-seed scheduler/tasks/pubsub pipeline"
}

# ---- Project-scoped bindings ---------------------------------------------------
resource "google_project_iam_member" "app_sql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.app.email}"
}

resource "google_project_iam_member" "app_bq_editor" {
  project = var.project_id
  role    = "roles/bigquery.dataEditor"
  member  = "serviceAccount:${google_service_account.app.email}"
}

resource "google_project_iam_member" "compute_logging_writer" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.compute.email}"
}

resource "google_project_iam_member" "compute_ar_reader" {
  project = var.project_id
  role    = "roles/artifactregistry.reader"
  member  = "serviceAccount:${google_service_account.compute.email}"
}

resource "google_project_iam_member" "pipeline_pubsub_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.pipeline.email}"
}

resource "google_project_iam_member" "pipeline_tasks_enqueuer" {
  project = var.project_id
  role    = "roles/cloudtasks.enqueuer"
  member  = "serviceAccount:${google_service_account.pipeline.email}"
}

# Pub/Sub's own service agent needs publish rights on the DLQ topic for the
# dead_letter_policy on events_worker (data.tf) to actually deliver.
resource "google_project_iam_member" "pubsub_sa_dlq_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:service-${data.google_project.current.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

# ---- Project-level custom role (NOT org — TerraLift distinguishes the two) --
resource "google_project_iam_custom_role" "read_only_plus" {
  role_id     = "${replace(var.prefix, "-", "_")}ReadOnlyPlus"
  title       = "TerraLift Mega-seed Read-Only Plus"
  description = "Project-scoped custom role: light viewer permissions plus Secret Manager list (never secret values)"
  permissions = [
    "resourcemanager.projects.get",
    "secretmanager.secrets.list",
    "storage.buckets.list",
    "compute.instances.list",
  ]
}

resource "google_project_iam_member" "pipeline_custom_role" {
  project = var.project_id
  role    = google_project_iam_custom_role.read_only_plus.id
  member  = "serviceAccount:${google_service_account.pipeline.email}"
}

# ---- Resource-scoped binding: sa-app may actAs sa-compute --------------------
resource "google_service_account_iam_member" "app_can_actas_compute" {
  service_account_id = google_service_account.compute.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.app.email}"
}
