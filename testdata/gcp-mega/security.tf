# ---- KMS: CMEK for the locked bucket -----------------------------------------
resource "google_kms_key_ring" "main" {
  name     = "${var.prefix}-keyring"
  location = var.region
}

resource "google_kms_crypto_key" "storage" {
  name            = "${var.prefix}-storage-key"
  key_ring        = google_kms_key_ring.main.id
  rotation_period = "7776000s" # 90 days
}

data "google_storage_project_service_account" "gcs" {}

resource "google_kms_crypto_key_iam_member" "gcs_encrypter" {
  crypto_key_id = google_kms_crypto_key.storage.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = "serviceAccount:${data.google_storage_project_service_account.gcs.email_address}"
}

# ---- Secret Manager: the SECURE half of the key-comparison ------------------
# Cloud Run (app.tf) references this by ID via value_source.secret_key_ref —
# never a literal — the pattern TerraLift should ship as a reference, not a
# captured value.
resource "google_secret_manager_secret" "db_root_password" {
  secret_id = "${var.prefix}-db-root-password"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "db_root_password" {
  secret      = google_secret_manager_secret.db_root_password.id
  secret_data = "R00tS3cr3t-Lab-Only-2026" # data-plane value; TerraLift must capture the resource, never this value
}

resource "google_secret_manager_secret_iam_member" "app_secret_accessor" {
  secret_id = google_secret_manager_secret.db_root_password.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.app.email}"
}
