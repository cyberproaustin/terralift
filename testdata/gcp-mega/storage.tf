# ---- Storage posture mix: one PUBLIC bucket, one LOCKED (CMEK) bucket -------
resource "google_storage_bucket" "public" {
  name                        = "${var.prefix}-public-${random_id.suffix.hex}"
  location                    = "US"
  force_destroy               = true
  uniform_bucket_level_access = true
  # Intentionally no public_access_prevention override — left open so the
  # allUsers binding below actually grants public read.
}

resource "google_storage_bucket_iam_member" "public_read" {
  bucket = google_storage_bucket.public.name
  role   = "roles/storage.objectViewer"
  member = "allUsers" # <-- public exposure signal for the hygiene report
}

resource "google_storage_bucket" "locked" {
  name                        = "${var.prefix}-locked-${random_id.suffix.hex}"
  location                    = "us-central1" # regional, to match the regional CMEK key (us-central1)
  force_destroy               = true
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  encryption {
    default_kms_key_name = google_kms_crypto_key.storage.id
  }

  # Must exist before the bucket so GCS can use the key on first write.
  depends_on = [google_kms_crypto_key_iam_member.gcs_encrypter]
}

# ---- Dedicated (plain, Google-managed encryption) bucket for function source
# Kept separate from the CMEK "locked" bucket so Cloud Build's own service
# agent never needs KMS decrypt permission just to fetch the deploy zip.
resource "google_storage_bucket" "function_source" {
  name                        = "${var.prefix}-fn-src-${random_id.suffix.hex}"
  location                    = "US"
  force_destroy               = true
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
}
