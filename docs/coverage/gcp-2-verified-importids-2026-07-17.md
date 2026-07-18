# GCP Data & Analytics — Verified Import IDs (research pass 2026-07-17)

Verification of the data-analytics batch against **hashicorp/google v7.40.0** (raw
provider markdown) + the Cloud Asset Inventory supported-asset-types doc. This
records the authoritative import-ID formats and CAI-enumerability for each type so
`deriveImportID` / `assetTypeToTF(Extra)` / `excludedReason` stay honest.

Legend: **CAI type** = the `assetType` Cloud Asset Inventory reports (or `none` =
confirmed absent from CAI, therefore not enumerable by TerraLift's CAI floor).

## Verified rows

| TF resource | CAI type | Import ID (canonical first) | Notes |
|---|---|---|---|
| google_data_fusion_instance | datafusion.googleapis.com/Instance | `projects/{project}/locations/{region}/instances/{name}` | path uses region value (attr is `region`, not `location`) |
| google_dataplex_lake | dataplex.googleapis.com/Lake | `projects/{project}/locations/{location}/lakes/{name}` | |
| google_dataplex_zone | dataplex.googleapis.com/Zone | `projects/{project}/locations/{location}/lakes/{lake}/zones/{name}` | sub-resource of lake |
| google_dataplex_asset | dataplex.googleapis.com/Asset | `.../lakes/{lake}/zones/{zone}/assets/{name}` | sub-resource of zone |
| google_dataplex_task | dataplex.googleapis.com/Task | `.../lakes/{lake}/tasks/{task_id}` | id uses `task_id` |
| google_data_catalog_* (entry_group, entry, tag_template, policy_tag, taxonomy) | none | `{name}` (full resource name only) | Data Catalog NOT in CAI (deprecated for Dataplex Universal Catalog) — correctly excluded |
| google_redis_instance | redis.googleapis.com/Instance | `projects/{project}/locations/{region}/instances/{name}` | |
| google_memcache_instance | memcache.googleapis.com/Instance | `projects/{project}/locations/{region}/instances/{name}` | |
| google_redis_cluster | redis.googleapis.com/Cluster | `projects/{project}/locations/{region}/clusters/{name}` | |
| google_looker_instance | looker.googleapis.com/Instance | `projects/{project}/locations/{region}/instances/{name}` | CAI also has looker.../Backup (no TF resource) |
| google_bigquery_dataset | bigquery.googleapis.com/Dataset | `projects/{project}/datasets/{dataset_id}` | IAM policy rides on this asset (IAM_POLICY content type) |
| google_bigquery_table | bigquery.googleapis.com/Table | `projects/{project}/datasets/{dataset_id}/tables/{table_id}` | BigQuery **views** reuse this resource+CAI type (view{} block); no separate view resource |
| google_bigquery_job | none | `projects/{project}/jobs/{job_id}/location/{location}` (+5 forms) | import supported, but NO bigquery.../Job CAI type — ephemeral, not enumerable |
| google_bigquery_routine | bigquery.googleapis.com/Routine | `projects/{project}/datasets/{dataset_id}/routines/{routine_id}` | sub-resource of dataset |
| google_bigquery_data_transfer_config | bigquerydatatransfer.googleapis.com/TransferConfig | `{project}/{name}` (also space-delimited `{project} {name}`) | separate CAI service |
| google_bigquery_reservation | bigqueryreservation.googleapis.com/Reservation | `projects/{project}/locations/{location}/reservations/{name}` | separate CAI service |
| google_bigquery_capacity_commitment | bigqueryreservation.googleapis.com/CapacityCommitment | `projects/{project}/locations/{location}/capacityCommitments/{id}` | separate CAI service |
| google_bigquery_connection | none | `projects/{project}/locations/{location}/connections/{connection_id}` | bigqueryconnection.../Connection NOT in CAI |
| google_bigquery_analytics_hub_data_exchange | analyticshub.googleapis.com/DataExchange | `projects/{project}/locations/{location}/dataExchanges/{id}` | CAI prefix is **analyticshub** (not bigqueryanalyticshub) |
| google_bigquery_analytics_hub_listing | analyticshub.googleapis.com/Listing | `.../dataExchanges/{id}/listings/{listing_id}` | sub-resource; CAI prefix analyticshub |
| google_bigtable_instance | bigtableadmin.googleapis.com/Instance | `projects/{project}/instances/{name}` | |
| google_bigtable_table | bigtableadmin.googleapis.com/Table | `projects/{project}/instances/{instance}/tables/{name}` | sub-resource of instance |
| google_bigtable_app_profile | bigtableadmin.googleapis.com/AppProfile | `projects/{project}/instances/{instance}/appProfiles/{id}` | sub-resource of instance |
| google_bigtable_gc_policy | none | no import | "does not support import"; part of a table |
| google_spanner_instance | spanner.googleapis.com/Instance | `projects/{project}/instances/{name}` | |
| google_spanner_database | spanner.googleapis.com/Database | `projects/{project}/instances/{instance}/databases/{name}` | sub-resource of instance |
| (google_spanner_backup) | spanner.googleapis.com/Backup | — | **no TF resource exists** (closest: google_spanner_backup_schedule) — correctly absent from our maps |
| google_sql_database_instance | sqladmin.googleapis.com/Instance | `projects/{project}/instances/{name}` | |
| google_sql_database | none | `projects/{project}/instances/{instance}/databases/{name}` | CAI enumerates only the Instance, not databases |
| google_sql_user | none | `{project}/{instance}/{host}/{name}` (MySQL) / `{project}/{instance}/{name}` (PG) | MySQL form includes host; not CAI-enumerable |
| google_sql_ssl_cert | none | no import | private key only at creation — cannot import |
| google_dataflow_job | dataflow.googleapis.com/Job | `{id}` (opaque job id) | short-lived jobs make import flaky (GH #15032) |
| google_dataflow_flex_template_job | dataflow.googleapis.com/Job | no import | same CAI type as classic job |
| google_dataproc_cluster | dataproc.googleapis.com/Cluster | no import | CAI-enumerable but no TF import support |
| google_dataproc_job | dataproc.googleapis.com/Job | no import | ephemeral |
| google_dataproc_workflow_template | dataproc.googleapis.com/WorkflowTemplate | `projects/{project}/locations/{location}/workflowTemplates/{name}` | |
| google_dataproc_autoscaling_policy | dataproc.googleapis.com/AutoscalingPolicy | `projects/{project}/locations/{location}/autoscalingPolicies/{id}` | |
| google_dataproc_metastore_service | metastore.googleapis.com/Service | `projects/{project}/locations/{location}/services/{id}` | under **metastore.**, not dataproc.* |
| google_dataproc_metastore_federation | metastore.googleapis.com/Federation | `projects/{project}/locations/{location}/federations/{id}` | under metastore.* |
| google_composer_environment | composer.googleapis.com/Environment | `projects/{project}/locations/{region}/environments/{name}` | path uses region value |
| google_datastream_connection_profile | datastream.googleapis.com/ConnectionProfile | `projects/{project}/locations/{location}/connectionProfiles/{id}` | |
| google_datastream_stream | datastream.googleapis.com/Stream | `projects/{project}/locations/{location}/streams/{id}` | |
| google_datastream_private_connection | datastream.googleapis.com/PrivateConnection | `projects/{project}/locations/{location}/privateConnections/{id}` | |
| google_firestore_database | firestore.googleapis.com/Database | `projects/{project}/databases/{name}` | CAI enumerates Database (+ Backup) only |
| google_firestore_index / _field / _document | none | `{name}` (full-path) | not CAI-enumerable; note: **_document DOES support import** (data record) |
| (google_datastore_index) | none | n/a | **REMOVED in provider v6.0** — replaced by google_firestore_index (Datastore-mode); correctly absent from our maps |
| google_pubsub_topic | pubsub.googleapis.com/Topic | `projects/{project}/topics/{name}` | |
| google_pubsub_subscription | pubsub.googleapis.com/Subscription | `projects/{project}/subscriptions/{name}` | |
| google_pubsub_schema | none | `projects/{project}/schemas/{name}` | not CAI-enumerable (only Topic + Subscription) |
| google_pubsub_lite_topic / _subscription | none | zonal path (`locations/{zone}`) | Pub/Sub Lite not in CAI |
| google_pubsub_lite_reservation | none | regional path (`locations/{region}`) | Pub/Sub Lite not in CAI; note region not zone |

## Reconciliation against current code (verified 2026-07-17)

- All CAI-enumerable types above are already present in `internal/providers/gcp/types.go`
  (base map) or `coverage.go` (`assetTypeToTFExtra`). No missing enumerable types found.
- The removed/nonexistent resources the research flagged — `google_datastore_index`
  (removed v6.0) and `google_spanner_backup` (never existed) — are **not** referenced
  anywhere in our maps. Confirmed via grep.
- Data Catalog, Pub/Sub Lite, `google_bigquery_connection`, and the SQL sub-resources
  (database/user/ssl_cert) are correctly **not** enumerated (absent from CAI).
