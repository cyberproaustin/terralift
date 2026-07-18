# ---- Cloud Run v2: rich env block mixing INSECURE literals + a SECURE ref ---
resource "google_cloud_run_v2_service" "api" {
  name     = "${var.prefix}-api"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    service_account = google_service_account.app.email

    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello"

      env {
        name  = "ENVIRONMENT"
        value = "production"
      }
      env {
        name  = "LOG_LEVEL"
        value = "info"
      }
      env {
        name  = "REGION"
        value = var.region
      }
      env {
        name  = "SERVICE_NAME"
        value = "${var.prefix}-api"
      }
      env {
        name  = "APP_VERSION"
        value = "1.4.2"
      }
      env {
        name  = "MAX_CONNECTIONS"
        value = "100"
      }
      env {
        name  = "REQUEST_TIMEOUT_SECONDS"
        value = "30"
      }
      env {
        name  = "FEATURE_FLAG_NEW_UI"
        value = "true"
      }
      env {
        name  = "CACHE_TTL_SECONDS"
        value = "300"
      }
      env {
        name  = "DB_HOST"
        value = google_sql_database_instance.main.public_ip_address
      }
      env {
        name  = "DB_PORT"
        value = "5432"
      }
      env {
        name  = "DB_NAME"
        value = google_sql_database.app.name
      }
      env {
        name  = "DB_USER"
        value = google_sql_user.app.name
      }
      env {
        # INSECURE: plaintext DB password literal — TerraLift's secrets review
        # should flag this and refuse to ship it as-is.
        name  = "DB_PASSWORD"
        value = "S3cur3P@ssw0rd!2026"
      }
      env {
        # INSECURE: plaintext API key literal. (Value is a scrubbed placeholder — a
        # realistic sk_live_ literal trips GitHub push protection; TerraLift flags this
        # by the STRIPE_API_KEY key name regardless of the value.)
        name  = "STRIPE_API_KEY"
        value = "PLACEHOLDER_stripe_live_key_do_not_use"
      }
      env {
        name  = "ALLOWED_ORIGINS"
        value = "https://app.tlmega-lab.example.com"
      }
      env {
        name  = "ENABLE_TRACING"
        value = "true"
      }
      env {
        name  = "METRICS_NAMESPACE"
        value = "tlmega/prod"
      }
      env {
        name  = "SUPPORT_EMAIL"
        value = "support@tlmega-lab.example.com"
      }
      env {
        # SECURE: Secret Manager reference, not a literal value — the pattern
        # TerraLift should ship as-is (a reference, never the resolved value).
        name = "DB_ROOT_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.db_root_password.secret_id
            version = "latest"
          }
        }
      }

      ports {
        container_port = 8080
      }
    }
  }

  depends_on = [google_secret_manager_secret_iam_member.app_secret_accessor]
}

# ---- Cloud Functions (2nd gen): rich env, all INSECURE by design ------------
data "archive_file" "function_source" {
  type        = "zip"
  output_path = "${path.module}/build/${var.prefix}-worker-source.zip"

  source {
    filename = "index.js"
    content  = <<-EOT
      exports.handler = (req, res) => {
        res.status(200).send('tlmega-worker-ok');
      };
    EOT
  }

  source {
    filename = "package.json"
    content  = <<-EOT
      {
        "name": "tlmega-worker",
        "version": "1.0.0",
        "main": "index.js"
      }
    EOT
  }
}

resource "google_storage_bucket_object" "function_source" {
  name   = "functions/${var.prefix}-worker-${random_id.suffix.hex}.zip"
  bucket = google_storage_bucket.function_source.name
  source = data.archive_file.function_source.output_path
}

resource "google_cloudfunctions2_function" "worker" {
  name     = "${var.prefix}-worker"
  location = var.region

  build_config {
    runtime     = "nodejs20"
    entry_point = "handler"
    source {
      storage_source {
        bucket = google_storage_bucket.function_source.name
        object = google_storage_bucket_object.function_source.name
      }
    }
  }

  service_config {
    available_memory      = "256M"
    timeout_seconds       = 60
    max_instance_count    = 2
    min_instance_count    = 0
    service_account_email = google_service_account.app.email

    environment_variables = {
      ENVIRONMENT        = "production"
      LOG_LEVEL          = "info"
      FUNCTION_REGION    = var.region
      RUNTIME            = "nodejs20"
      TASK_QUEUE_NAME    = google_cloud_tasks_queue.worker.name
      TOPIC_NAME         = google_pubsub_topic.events.name
      BUCKET_NAME        = google_storage_bucket.function_source.name
      MAX_RETRIES        = "3"
      TIMEOUT_MS         = "30000"
      FEATURE_FLAG_BATCH = "true"
      NOTIFY_EMAIL       = "alerts@tlmega-lab.example.com"
      CACHE_ENABLED      = "true"
      # INSECURE: connection string with an embedded password.
      DATABASE_URL = "postgresql://${google_sql_user.app.name}:S3cur3P@ssw0rd!2026@${google_sql_database_instance.main.public_ip_address}:5432/${google_sql_database.app.name}"
      # INSECURE: literal API key. (Scrubbed placeholder — a realistic AIza… literal
      # trips GitHub push protection; flagged by the THIRD_PARTY_API_KEY key name.)
      THIRD_PARTY_API_KEY = "PLACEHOLDER_google_api_key_do_not_use"
      SLACK_WEBHOOK_URL   = "https://hooks.slack.com/services/T00000/B00000/XXXXXXXXXXXXXXXXXXXXXXXX"
      ALERT_THRESHOLD     = "0.95"
      BATCH_SIZE          = "50"
      ENABLE_DEBUG        = "false"
    }
  }
}
