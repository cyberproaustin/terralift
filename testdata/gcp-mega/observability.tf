# ---- Logging: metric + audit export sink to the locked bucket ---------------
resource "google_logging_metric" "errors" {
  name   = "${var.prefix}-error-count"
  filter = "resource.type=\"cloud_run_revision\" AND severity>=ERROR"

  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
  }
}

resource "google_logging_project_sink" "audit_export" {
  name                   = "${var.prefix}-audit-export"
  destination            = "storage.googleapis.com/${google_storage_bucket.locked.name}"
  filter                 = "logName:\"cloudaudit.googleapis.com\""
  unique_writer_identity = true
}

resource "google_storage_bucket_iam_member" "sink_writer" {
  bucket = google_storage_bucket.locked.name
  role   = "roles/storage.objectCreator"
  member = google_logging_project_sink.audit_export.writer_identity
}

# ---- Monitoring: notification channel + alert policy -------------------------
resource "google_monitoring_notification_channel" "email" {
  display_name = "TerraLift mega-seed alerts"
  type         = "email"

  labels = {
    email_address = "alerts@tlmega-lab.example.com"
  }
}

resource "google_monitoring_alert_policy" "vm_cpu_high" {
  display_name = "${var.prefix}-vm-cpu-high"
  combiner     = "OR"

  conditions {
    display_name = "VM CPU > 90%"

    condition_threshold {
      filter          = "resource.type = \"gce_instance\" AND metric.type = \"compute.googleapis.com/instance/cpu/utilization\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0.9
      duration        = "300s"

      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_MEAN"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]
}
