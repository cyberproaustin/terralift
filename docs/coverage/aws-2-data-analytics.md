# AWS coverage: databases / analytics

| Terraform type | Resource Explorer resourceType | Import ID format | Notes |
|---|---|---|---|
| aws_db_instance | rds:db | `db-instance-id` | |
| aws_rds_cluster | rds:cluster | `cluster-id` | |
| aws_rds_cluster_instance | rds:db | `instance-id` | sub-resource of cluster; shares `rds:db` with plain instances |
| aws_db_subnet_group | rds:subgrp | `name` | |
| aws_db_parameter_group | rds:pg | `name` | |
| aws_rds_cluster_parameter_group | rds:cluster-pg | `name` | |
| aws_db_option_group | rds:og | `name` | name must be lowercase |
| aws_db_proxy | rds:db-proxy | `name` | |
| aws_db_snapshot | rds:snapshot | `snapshot-id` | |
| aws_db_cluster_snapshot | rds:cluster-snapshot | `cluster-snapshot-id` | Aurora/cluster equivalent of db_snapshot |
| aws_db_event_subscription | rds:es | `name` | |
| aws_rds_global_cluster | rds:global-cluster | `global-cluster-id` | `source_db_cluster_identifier` not readable post-import (no API); needs `ignore_changes` |
| aws_dynamodb_table | dynamodb:table | `table-name` | |
| aws_dynamodb_global_table | dynamodb:table | `name` | deprecated V1 (2017) API; sub-resource, shares table's RE type |
| aws_dynamodb_kinesis_streaming_destination | dynamodb:table | `table_name,stream_arn` | comma-joined; sub-resource |
| aws_dynamodb_contributor_insights | dynamodb:table | `name:table_name/index:index_name/account_id` | literal `name:`/`index:` prefixed composite; sub-resource |
| aws_dynamodb_table_replica | dynamodb:table | `table-name:main-region` | colon-joined; region = main table's region, not replica's; sub-resource |
| aws_elasticache_cluster | elasticache:cluster | `cluster_id` | |
| aws_elasticache_replication_group | elasticache:replicationgroup | `replication_group_id` | |
| aws_elasticache_subnet_group | elasticache:subnetgroup | `name` | |
| aws_elasticache_parameter_group | elasticache:parametergroup | `name` | |
| aws_elasticache_user | elasticache:user | `user_id` | |
| aws_elasticache_user_group | elasticache:usergroup | `user_group_id` | |
| aws_elasticache_serverless_cache | ? | `name` | not RE-enumerable (absent from RE supported-types list) |
| aws_memorydb_cluster | memorydb:cluster | `name` | |
| aws_memorydb_acl | memorydb:acl | `name` | |
| aws_memorydb_user | memorydb:user | `user_name` | password not recoverable/importable |
| aws_memorydb_parameter_group | memorydb:parametergroup | `name` | |
| aws_memorydb_subnet_group | memorydb:subnetgroup | `name` | |
| aws_redshift_cluster | redshift:cluster | `cluster_identifier` | |
| aws_redshift_subnet_group | redshift:subnetgroup | `name` | |
| aws_redshift_parameter_group | redshift:parametergroup | `name` | |
| aws_redshift_cluster_snapshot | redshift:snapshot | `snapshot_identifier` | manual snapshots only |
| aws_redshiftserverless_namespace | ? | `namespace_name` | not RE-enumerable (no "Redshift Serverless" type in RE) |
| aws_redshiftserverless_workgroup | ? | `workgroup_name` | not RE-enumerable |
| aws_neptune_cluster | rds:cluster | `cluster-id` | RE reports `neptune:dbcluster` as `rds:cluster` |
| aws_neptune_cluster_instance | rds:db | `instance-id` | sub-resource; shares `rds:db` |
| aws_neptune_subnet_group | rds:subgrp | `name` | shares `rds:subgrp` |
| aws_neptune_parameter_group | rds:pg | `name` | shares `rds:pg` |
| aws_neptune_cluster_parameter_group | rds:cluster-pg | `name` | shares `rds:cluster-pg` |
| aws_neptune_event_subscription | rds:es | `name` | shares `rds:es` |
| aws_neptune_global_cluster | ? | `global-cluster-id` | UNSURE: `neptune:globalcluster` absent from AWS's `rds:global-cluster` ARN-sharing exception table (only docdb+rds listed) — RE coverage unconfirmed; same `source_db_cluster_identifier` caveat as rds_global_cluster |
| aws_docdb_cluster | rds:cluster | `cluster-id` | shares `rds:cluster`; `master_password` not imported into state |
| aws_docdb_cluster_instance | rds:db | `instance-id` | sub-resource; shares `rds:db` |
| aws_timestreamwrite_database | ? | `database_name` | not RE-enumerable (no Timestream type in RE) |
| aws_timestreamwrite_table | ? | `table_name:database_name` | colon-joined; not RE-enumerable |
| aws_keyspaces_keyspace | ? | `name` | not RE-enumerable (no Keyspaces type in RE) |
| aws_keyspaces_table | ? | `keyspace_name/table_name` | slash-joined; not RE-enumerable |
| aws_athena_workgroup | athena:workgroup | `name` | |
| aws_athena_database | ? | `database-name` | actually a Glue catalog database under the hood, no dedicated Athena DB API; not RE-enumerable as distinct type; `bucket`/`encryption_configuration` need `ignore_changes` post-import |
| aws_athena_data_catalog | athena:datacatalog | `name` | |
| aws_athena_named_query | ? | `query-id` | not RE-enumerable |
| aws_glue_catalog_database | glue:database | `catalog-id:name` | |
| aws_glue_catalog_table | glue:table | `catalog-id:database-name:table-name` | |
| aws_glue_crawler | glue:crawler | `name` | |
| aws_glue_job | glue:job | `name` | |
| aws_glue_trigger | glue:trigger | `name` | |
| aws_glue_connection | ? | `catalog-id:name` | not RE-enumerable |
| aws_glue_registry | glue:registry | `arn` | full-ARN import |
| aws_glue_schema | ? | `arn` | not RE-enumerable (not nested under `glue:registry` either); full-ARN import |
| aws_glue_security_configuration | ? | `name` | not RE-enumerable |
| aws_glue_partition | ? | `catalog-id:database-name:table-name:val1#val2` | sub-resource of table; `#`-joined partition values; not RE-enumerable |
| aws_kinesis_stream | kinesis:stream | `name` | |
| aws_kinesis_firehose_delivery_stream | firehose:deliverystream | `arn` | import unsupported for `s3` destination type; use `extended_s3` |
| aws_kinesisanalyticsv2_application | kinesisanalytics:application | `arn` | RE type = "Managed Service for Apache Flink" (renamed Kinesis Data Analytics v2) |
| aws_kinesis_video_stream | kinesisvideo:stream | `arn` | |
| aws_opensearch_domain | es:domain | `domain_name` | |
| aws_elasticsearch_domain | es:domain | `domain_name` | legacy alias resource; same RE type |
| aws_emr_cluster | elasticmapreduce:cluster | `j-XXXXXXXXXXXXX` | |
| aws_emr_security_configuration | ? | `name` | not RE-enumerable |
| aws_emr_studio | ? | `es-XXXXXXXXXXXXX` | not RE-enumerable |
| aws_lakeformation_resource | ? | no import | not RE-enumerable; no Import section in provider docs |
| aws_lakeformation_permissions | ? | no import | not RE-enumerable; no Import section in provider docs |
| aws_lakeformation_data_lake_settings | ? | no import | not RE-enumerable; singleton account-level settings |
| aws_lakeformation_lf_tag | ? | `catalog_id:key` | not RE-enumerable; colon-joined, catalog_id defaults to account ID |
| aws_lakeformation_resource_lf_tags | ? | no import | not RE-enumerable; no Import section in provider docs |
| aws_quicksight_data_source | quicksight:datasource | `account-id/data-source-id` | slash-joined |
| aws_quicksight_data_set | quicksight:dataset | `account-id,data-set-id` | comma-joined |
| aws_quicksight_template | quicksight:template | `account-id,template-id` | comma-joined |
| aws_quicksight_theme | quicksight:theme | `account-id,theme-id` | comma-joined |
| aws_quicksight_analysis | ? | `account-id,analysis-id` | not RE-enumerable; comma-joined |
| aws_quicksight_dashboard | ? | `account-id,dashboard-id` | not RE-enumerable; comma-joined |
| aws_quicksight_folder | ? | `account-id,folder-id` | not RE-enumerable; comma-joined |
| aws_quicksight_user | ? | no import | not RE-enumerable; provider docs state "you cannot import this resource" |
| aws_quicksight_group | ? | `account-id/namespace/group-name` | not RE-enumerable; slash-joined |
| aws_quicksight_vpc_connection | ? | `account-id,vpc-connection-id` | not RE-enumerable; comma-joined |
| aws_quicksight_namespace | ? | `account-id,namespace` | not RE-enumerable; comma-joined |
| aws_datazone_domain | ? | `domain-id` | not RE-enumerable |
| aws_datazone_project | ? | `domain-id:project-id` | not RE-enumerable; colon-joined |
| aws_datazone_environment | ? | `domain_identifier,id` | not RE-enumerable; comma-joined |
| aws_datazone_environment_profile | ? | `id,domain_identifier` | not RE-enumerable; comma-joined, order REVERSED vs aws_datazone_environment |
| aws_datazone_glossary | ? | `domain_id,glossary_id,owning_project_id` | not RE-enumerable; 3-part comma-joined |
| aws_datazone_glossary_term | ? | `domain_identifier,id,glossary_identifier` | not RE-enumerable; 3-part comma-joined |
| aws_datazone_asset_type | ? | `domain_identifier,name` | not RE-enumerable; comma-joined, keyed by name not id |
| aws_datazone_form_type | ? | `domain_identifier,name,revision` | not RE-enumerable; 3-part comma-joined |
| aws_datapipeline_pipeline | datapipeline:pipeline | `pipeline-id` | |
| aws_datapipeline_pipeline_definition | ? | `pipeline-id` | sub-resource, 1:1 with parent pipeline; not separately RE-enumerable |
