# ---- Cloud SQL (db-f1-micro Postgres — cheapest tier) ------------------------
resource "google_sql_database_instance" "main" {
  name             = "${var.prefix}-pg-${random_id.suffix.hex}"
  database_version = "POSTGRES_15"
  region           = var.region

  settings {
    tier              = "db-f1-micro"
    availability_type = "ZONAL"
    disk_size         = 10
    disk_type         = "PD_SSD"

    backup_configuration {
      enabled = false
    }

    ip_configuration {
      ipv4_enabled = true
      authorized_networks {
        name  = "${var.prefix}-lab-wide-open"
        value = "0.0.0.0/0" # <-- lab-only exposure signal, intentionally broad
      }
    }
  }

  deletion_protection = false
}

resource "google_sql_database" "app" {
  name     = "${var.prefix}_appdb"
  instance = google_sql_database_instance.main.name
}

resource "google_sql_user" "app" {
  name     = "${var.prefix}_app_user"
  instance = google_sql_database_instance.main.name
  password = "S3cur3P@ssw0rd!2026" # matches the intentionally-insecure literal reused in app.tf
}

# ---- BigQuery (on-demand — no reserved slots) --------------------------------
resource "google_bigquery_dataset" "analytics" {
  dataset_id                 = "${var.prefix}_analytics"
  location                   = "US"
  delete_contents_on_destroy = true
}

resource "google_bigquery_table" "events" {
  dataset_id          = google_bigquery_dataset.analytics.dataset_id
  table_id            = "${var.prefix}_events"
  deletion_protection = false

  schema = jsonencode([
    { name = "id", type = "STRING", mode = "REQUIRED" },
    { name = "event_type", type = "STRING", mode = "NULLABLE" },
    { name = "occurred_at", type = "TIMESTAMP", mode = "NULLABLE" },
    { name = "payload_json", type = "STRING", mode = "NULLABLE" },
  ])
}

# ---- Pub/Sub: schema-validated topic + DLQ + subscription -------------------
resource "google_pubsub_schema" "events" {
  name = "${var.prefix}-events-schema"
  type = "AVRO"
  definition = jsonencode({
    type = "record"
    name = "TlmegaEvent"
    fields = [
      { name = "id", type = "string" },
      { name = "event_type", type = "string" },
    ]
  })
}

resource "google_pubsub_topic" "events" {
  name = "${var.prefix}-events"

  schema_settings {
    schema   = google_pubsub_schema.events.id
    encoding = "JSON"
  }
}

resource "google_pubsub_topic" "events_dlq" {
  name = "${var.prefix}-events-dlq"
}

resource "google_pubsub_subscription" "events_worker" {
  name  = "${var.prefix}-events-worker-sub"
  topic = google_pubsub_topic.events.id

  ack_deadline_seconds = 20

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.events_dlq.id
    max_delivery_attempts = 5
  }
}

# ---- Cloud Tasks + Cloud Scheduler --------------------------------------------
resource "google_cloud_tasks_queue" "worker" {
  name     = "${var.prefix}-worker-queue"
  location = var.region

  rate_limits {
    max_concurrent_dispatches = 5
    max_dispatches_per_second = 5
  }

  retry_config {
    max_attempts = 3
  }
}

resource "google_cloud_scheduler_job" "nightly_sync" {
  name      = "${var.prefix}-nightly-sync"
  region    = var.region
  schedule  = "0 3 * * *"
  time_zone = "Etc/UTC"

  pubsub_target {
    topic_name = google_pubsub_topic.events.id
    data       = base64encode(jsonencode({ job = "nightly_sync" }))
  }
}
