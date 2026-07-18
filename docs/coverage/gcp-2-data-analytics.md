# GCP coverage: data / analytics

| Terraform type | CAI asset type | Import ID format | Notes |
|---|---|---|---|
| google_bigquery_dataset | bigquery.googleapis.com/Dataset | `projects/{{project}}/datasets/{{dataset_id}}` | also accepts `{{project}}/{{dataset_id}}`, `{{dataset_id}}` |
| google_bigquery_table | bigquery.googleapis.com/Table | `projects/{{project}}/datasets/{{dataset_id}}/tables/{{table_id}}` | views are this same resource with a `view` block set — no separate TF type for "view" |
| google_bigquery_job | ? | `projects/{{project}}/jobs/{{job_id}}/location/{{location}}` | not CAI-enumerable — BigQuery's CAI types are only Dataset/Model/Routine/RowAccessPolicy/Table, no Job (jobs are transient); also accepts `{{project}}/{{job_id}}/{{location}}`, `{{job_id}}/{{location}}`, `{{project}}/{{job_id}}`, `{{job_id}}` |
| google_bigquery_routine | bigquery.googleapis.com/Routine | `projects/{{project}}/datasets/{{dataset_id}}/routines/{{routine_id}}` | sub-resource of dataset |
| google_bigquery_data_transfer_config | bigquerydatatransfer.googleapis.com/TransferConfig | `{{project}}/{{name}}` | `name` already embeds location + config id; also accepts bare `{{name}}` or `"{{project}} {{name}}"` |
| google_bigquery_reservation | bigqueryreservation.googleapis.com/Reservation | `projects/{{project}}/locations/{{location}}/reservations/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_bigquery_capacity_commitment | bigqueryreservation.googleapis.com/CapacityCommitment | `projects/{{project}}/locations/{{location}}/capacityCommitments/{{capacity_commitment_id}}` | |
| google_bigquery_connection | ? | `projects/{{project}}/locations/{{location}}/connections/{{connection_id}}` | CAI asset type unconfirmed — `bigqueryconnection.googleapis.com` not found in the CAI supported-asset-types doc |
| google_bigquery_dataset_iam_policy / _binding / _member | ? | policy: `projects/{{project}}/datasets/{{dataset_id}}`; binding/member: same + `{{role}}` (+ `{{member}}`) | iam policy/binding/member trio; not a distinct CAI asset type, attaches to the Dataset asset; incompatible with `google_bigquery_dataset_access`/`access` block on the dataset |
| google_bigquery_analytics_hub_data_exchange | analyticshub.googleapis.com/DataExchange | `projects/{{project}}/locations/{{location}}/dataExchanges/{{data_exchange_id}}` | also accepts `{{project}}/{{location}}/{{data_exchange_id}}`, `{{location}}/{{data_exchange_id}}`, `{{data_exchange_id}}` |
| google_bigquery_analytics_hub_listing | analyticshub.googleapis.com/Listing | `projects/{{project}}/locations/{{location}}/dataExchanges/{{data_exchange_id}}/listings/{{listing_id}}` | sub-resource of data exchange |
| google_bigtable_instance | bigtableadmin.googleapis.com/Instance | `projects/{{project}}/instances/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_bigtable_table | bigtableadmin.googleapis.com/Table | `projects/{{project}}/instances/{{instance_name}}/tables/{{name}}` | `split_keys` not readable post-import (config diff if set) |
| google_bigtable_app_profile | bigtableadmin.googleapis.com/AppProfile | `projects/{{project}}/instances/{{instance}}/appProfiles/{{app_profile_id}}` | |
| google_bigtable_gc_policy | ? | n/a | no import — "This resource does not support import"; not a standalone CAI asset (GC rule is config on the table's column family) |
| google_spanner_instance | spanner.googleapis.com/Instance | `projects/{{project}}/instances/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_spanner_database | spanner.googleapis.com/Database | `projects/{{project}}/instances/{{instance}}/databases/{{name}}` | also accepts `instances/{{instance}}/databases/{{name}}`, `{{project}}/{{instance}}/{{name}}`, `{{instance}}/{{name}}` |
| google_spanner_backup_schedule | ? | `projects/{{project}}/instances/{{instance}}/databases/{{database}}/backupSchedules/{{name}}` | no dedicated "backup" TF resource or CAI asset type exists — this only manages the backup *schedule* config, not backup objects themselves; not CAI-enumerable (`spanner.googleapis.com/Backup` not found in supported-asset-types) |
| google_sql_database_instance | sqladmin.googleapis.com/Instance | `projects/{{project}}/instances/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_sql_database | ? | `projects/{{project}}/instances/{{instance}}/databases/{{name}}` | not CAI-enumerable — sqladmin only has Instance/Backup/BackupRun asset types, no Database; sub-resource of instance; also accepts `instances/{{instance}}/databases/{{name}}`, `{{project}}/{{instance}}/{{name}}`, `{{instance}}/{{name}}` |
| google_sql_user | ? | MySQL: `{{project_id}}/{{instance}}/{{host}}/{{name}}`; Postgres: `{{project_id}}/{{instance}}/{{name}}` | not CAI-enumerable; two different import ID formats depending on database engine |
| google_sql_ssl_cert | ? | n/a | no import — cert contents inaccessible after creation, per provider docs; not CAI-enumerable |
| google_firestore_database | firestore.googleapis.com/Database | `projects/{{project}}/databases/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}`; covers both Native mode and Datastore mode DBs |
| google_firestore_index | ? | `{{name}}` (full form `projects/{{project}}/databases/{{database}}/collectionGroups/{{collection}}/indexes/{{index}}`) | not CAI-enumerable — Firestore's only CAI types are Backup/Database; sub-resource of database |
| google_firestore_field | ? | `{{name}}` (full form `projects/{{project}}/databases/{{database}}/collectionGroups/{{collection}}/fields/{{field}}`) | not CAI-enumerable; sub-resource, singleton per field path (import/delete resets to default, doesn't remove) |
| google_firestore_document | ? | `{{name}}` | not CAI-enumerable; manages document *data*, unusual to manage via IaC |
| google_datastore_index | n/a | n/a | resource **removed** from provider in v6.0.0 (Aug 2024); use `google_firestore_index` instead (`database_id = "(default)"` for Datastore mode) — no separate CAI asset type ever existed for it either |
| google_pubsub_topic | pubsub.googleapis.com/Topic | `projects/{{project}}/topics/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_pubsub_subscription | pubsub.googleapis.com/Subscription | `projects/{{project}}/subscriptions/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_pubsub_schema | ? | `projects/{{project}}/schemas/{{name}}` | CAI asset type unconfirmed — not found in supported-asset-types doc search |
| google_pubsub_lite_topic | ? | `projects/{{project}}/locations/{{zone}}/topics/{{name}}` | not CAI-enumerable — `pubsublite.googleapis.com` not found in supported-asset-types doc |
| google_pubsub_lite_subscription | ? | `projects/{{project}}/locations/{{zone}}/subscriptions/{{name}}` | not CAI-enumerable, same reason as topic |
| google_pubsub_lite_reservation | ? | `projects/{{project}}/locations/{{region}}/reservations/{{name}}` | not CAI-enumerable, same reason as topic |
| google_dataflow_job | dataflow.googleapis.com/Job | `{{id}}` | job id only — no project/location components in the import ID |
| google_dataflow_flex_template_job | dataflow.googleapis.com/Job | n/a | no import — "This resource does not support import"; same CAI type as regular job |
| google_dataproc_cluster | dataproc.googleapis.com/Cluster | n/a | no import — "This resource does not support import", despite being CAI-enumerable |
| google_dataproc_job | dataproc.googleapis.com/Job | n/a | no import — "This resource does not support import" |
| google_dataproc_workflow_template | dataproc.googleapis.com/WorkflowTemplate | `projects/{{project}}/locations/{{location}}/workflowTemplates/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_dataproc_autoscaling_policy | dataproc.googleapis.com/AutoscalingPolicy | `projects/{{project}}/locations/{{location}}/autoscalingPolicies/{{policy_id}}` | |
| google_dataproc_metastore_service | metastore.googleapis.com/Service | `projects/{{project}}/locations/{{location}}/services/{{service_id}}` | |
| google_composer_environment | composer.googleapis.com/Environment | `projects/{{project}}/locations/{{region}}/environments/{{name}}` | also accepts `{{project}}/{{region}}/{{name}}`, `{{name}}` |
| google_datastream_connection_profile | datastream.googleapis.com/ConnectionProfile | `projects/{{project}}/locations/{{location}}/connectionProfiles/{{connection_profile_id}}` | |
| google_datastream_stream | datastream.googleapis.com/Stream | `projects/{{project}}/locations/{{location}}/streams/{{stream_id}}` | also accepts `{{project}}/{{location}}/{{stream_id}}`, `{{stream_id}}` |
| google_datastream_private_connection | datastream.googleapis.com/PrivateConnection | `projects/{{project}}/locations/{{location}}/privateConnections/{{private_connection_id}}` | |
| google_data_fusion_instance | datafusion.googleapis.com/Instance | `projects/{{project}}/locations/{{region}}/instances/{{name}}` | also accepts `{{project}}/{{region}}/{{name}}`, `{{region}}/{{name}}`, `{{name}}` |
| google_dataplex_lake | dataplex.googleapis.com/Lake | `projects/{{project}}/locations/{{location}}/lakes/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_dataplex_zone | dataplex.googleapis.com/Zone | `projects/{{project}}/locations/{{location}}/lakes/{{lake}}/zones/{{name}}` | sub-resource of lake |
| google_dataplex_asset | dataplex.googleapis.com/Asset | `projects/{{project}}/locations/{{location}}/lakes/{{lake}}/zones/{{dataplex_zone}}/assets/{{name}}` | sub-resource of zone |
| google_dataplex_task | dataplex.googleapis.com/Task | `projects/{{project}}/locations/{{location}}/lakes/{{lake}}/tasks/{{task_id}}` | sub-resource of lake |
| google_data_catalog_entry_group | ? | `{{name}}` (full form `projects/{{project}}/locations/{{location}}/entryGroups/{{entry_group_id}}`) | not CAI-enumerable — legacy `datacatalog.googleapis.com` asset types absent from current CAI supported-asset-types doc; catalog metadata now surfaces there as `dataplex.googleapis.com/EntryGroup` etc. instead |
| google_data_catalog_entry | ? | `{{name}}` | not CAI-enumerable, same reason; sub-resource of entry group |
| google_data_catalog_tag_template | ? | `{{name}}` | not CAI-enumerable, same reason |
| google_data_catalog_taxonomy | ? | `{{name}}` | not CAI-enumerable, same reason |
| google_data_catalog_policy_tag | ? | `{{name}}` | not CAI-enumerable, same reason; sub-resource of taxonomy |
| google_redis_instance | redis.googleapis.com/Instance | `projects/{{project}}/locations/{{region}}/instances/{{name}}` | also accepts `{{project}}/{{region}}/{{name}}`, `{{region}}/{{name}}`, `{{name}}` |
| google_memcache_instance | memcache.googleapis.com/Instance | `projects/{{project}}/locations/{{region}}/instances/{{name}}` | also accepts `{{project}}/{{region}}/{{name}}`, `{{region}}/{{name}}`, `{{name}}` |
| google_redis_cluster | redis.googleapis.com/Cluster | `projects/{{project}}/locations/{{region}}/clusters/{{name}}` | also accepts `{{project}}/{{region}}/{{name}}`, `{{region}}/{{name}}`, `{{name}}` |
| google_looker_instance | looker.googleapis.com/Instance | `projects/{{project}}/locations/{{region}}/instances/{{name}}` | also accepts `{{project}}/{{region}}/{{name}}`, `{{region}}/{{name}}`, `{{name}}` |
