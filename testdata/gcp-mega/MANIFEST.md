# gcp-mega — MANIFEST

TerraLift GCP MEGA seed: a large, complex brownfield "before" state for
project `terralift-mega-161207246` (region `us-central1`, zone `us-central1-a`,
all names prefixed `tlmega-`). Authored to maximize distinct `google_*`
resource-type breadth (1–2 of each) while staying cheap and fast to
provision — no Cloud VPN, Interconnect, Cloud NAT, GKE, or Filestore.

Provider: `hashicorp/google ~> 7.0` (+ `hashicorp/random ~> 3.6` for
globally-unique name suffixes, `hashicorp/archive ~> 2.4` to zip the Cloud
Function source inline — no external build step).

## Resource types (50 distinct `google_*` types, 65 resource blocks total)

| File | Resource types |
|---|---|
| `network.tf` | `google_compute_network` (2), `google_compute_network_peering` (2, both directions), `google_compute_subnetwork` (3, one with 2 secondary ranges + Private Google Access on all), `google_compute_firewall` (6: 4 ingress across both VPCs — internal, IAP-SSH, LB health check, data-VPC internal — + 2 egress on the `data` VPC — deny-all + allow Google APIs), `google_compute_route`, `google_compute_router` (no NAT attached), `google_compute_address`, `google_compute_global_address`, `google_dns_managed_zone` (public + private), `google_dns_record_set` (2), `google_compute_health_check`, `google_compute_backend_service`, `google_compute_url_map`, `google_compute_target_http_proxy`, `google_compute_global_forwarding_rule` |
| `compute.tf` | `google_compute_disk`, `google_compute_instance` (e2-micro), `google_compute_instance_template` (e2-micro), `google_compute_instance_group_manager` (size 1, LB backend), `google_artifact_registry_repository` |
| `app.tf` | `google_cloud_run_v2_service`, `google_cloudfunctions2_function` (2nd gen), `google_storage_bucket_object` (function source zip) |
| `data.tf` | `google_sql_database_instance` (db-f1-micro Postgres), `google_sql_database`, `google_sql_user`, `google_bigquery_dataset`, `google_bigquery_table`, `google_pubsub_topic` (2: main + DLQ), `google_pubsub_subscription`, `google_pubsub_schema`, `google_cloud_scheduler_job`, `google_cloud_tasks_queue` |
| `storage.tf` | `google_storage_bucket` (3: public, locked/CMEK, function-source), `google_storage_bucket_iam_member` |
| `security.tf` | `google_secret_manager_secret`, `google_secret_manager_secret_version`, `google_secret_manager_secret_iam_member`, `google_kms_key_ring`, `google_kms_crypto_key`, `google_kms_crypto_key_iam_member` |
| `observability.tf` | `google_logging_metric`, `google_logging_project_sink`, `google_storage_bucket_iam_member` (sink writer grant), `google_monitoring_alert_policy`, `google_monitoring_notification_channel` |
| `iam.tf` | `google_service_account` (3), `google_project_iam_member` (8), `google_project_iam_custom_role` (**project-level**, not org), `google_service_account_iam_member`, `google_project_service` (17, `for_each` over required APIs) |

## Insecure vs. secure secret handling (key comparison)

| Posture | File:resource | What it is |
|---|---|---|
| **INSECURE** | `app.tf:google_cloud_run_v2_service.api` (`env["DB_PASSWORD"]`) | Plaintext Cloud SQL app-user password as a literal `env.value` |
| **INSECURE** | `app.tf:google_cloud_run_v2_service.api` (`env["STRIPE_API_KEY"]`) | Plaintext third-party API key literal |
| **INSECURE** | `app.tf:google_cloudfunctions2_function.worker` (`environment_variables.DATABASE_URL`) | Connection string with an embedded DB password |
| **INSECURE** | `app.tf:google_cloudfunctions2_function.worker` (`environment_variables.THIRD_PARTY_API_KEY`) | Plaintext API key literal |
| **INSECURE** | `app.tf:google_cloudfunctions2_function.worker` (`environment_variables.SLACK_WEBHOOK_URL`) | Bearer-token-bearing webhook URL literal |
| **SECURE** | `app.tf:google_cloud_run_v2_service.api` (`env["DB_ROOT_PASSWORD"]`) → `security.tf:google_secret_manager_secret.db_root_password` | Cloud Run env sourced via `value_source.secret_key_ref` — a *reference*, never a literal value; TerraLift should ship the reference, not resolve it |

Both services also carry a rich mix of benign config alongside the secrets
(Cloud Run: 19 env vars total; Cloud Function: 18) so the secrets review has
to distinguish signal from the surrounding noise, not just flag every var.

## Other deliberate signals (consistent with `gcp-seed`/`gcp-stress` precedent)

- `storage.tf:google_storage_bucket.public` + `google_storage_bucket_iam_member.public_read` — `allUsers` `objectViewer` (public-bucket hygiene signal).
- `storage.tf:google_storage_bucket.locked` — `uniform_bucket_level_access` + `public_access_prevention = "enforced"` + CMEK (the "locked" contrast case).
- `data.tf:google_sql_database_instance.main` — `authorized_networks` includes `0.0.0.0/0` (intentional lab-only exposure signal, same pattern as the `0.0.0.0/0` firewall rules in the other seeds).
- `network.tf` — no `0.0.0.0/0` firewall rule this time (SSH is IAP-only); the exposure signal here is carried by the public bucket + open Cloud SQL authorized network instead, for variety.

## Skipped for cost/time (per brief)

Cloud VPN, Interconnect, Cloud NAT, GKE, Memorystore, Filestore. Private
Service Access / `google_service_networking_connection` was also skipped —
Cloud SQL uses a public IP + `authorized_networks` instead, to avoid a PSA
peering range that can be slow/finicky to tear down cleanly.

## Validate

```
terraform init -backend=false
terraform validate
```

Result: **Success! The configuration is valid.** (provider `hashicorp/google
v7.40.0`). Only `.terraform.lock.hcl` is kept in the directory; the provider
plugin cache (`.terraform/`) was removed after validation. Not planned or
applied — author/validate only, per instructions.
