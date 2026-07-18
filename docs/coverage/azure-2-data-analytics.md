# Azure coverage: data / analytics

| Terraform type | ARM resource type | Import ID format | Notes |
|---|---|---|---|
| azurerm_sql_server / _database / _firewall_rule / _elasticpool / _failover_group / _virtual_network_rule / _active_directory_administrator | Microsoft.Sql/* | n/a | REMOVED in provider v4.0 (Aug 2024); use the `azurerm_mssql_*` equivalents below |
| azurerm_mssql_server | Microsoft.Sql/servers | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Sql/servers/{server}` | |
| azurerm_mssql_database | Microsoft.Sql/servers/databases | `.../servers/{server}/databases/{db}` | sub-resource of server |
| azurerm_mssql_elasticpool | Microsoft.Sql/servers/elasticPools | `.../servers/{server}/elasticPools/{pool}` | sub-resource of server |
| azurerm_mssql_managed_instance | Microsoft.Sql/managedInstances | `.../providers/Microsoft.Sql/managedInstances/{mi}` | |
| azurerm_mssql_managed_database | Microsoft.Sql/managedInstances/databases | `.../managedInstances/{mi}/databases/{db}` | sub-resource of managed instance |
| azurerm_mssql_firewall_rule | Microsoft.Sql/servers/firewallRules | `.../servers/{server}/firewallRules/{rule}` | current name; legacy `azurerm_sql_firewall_rule` removed |
| azurerm_mssql_virtual_network_rule | Microsoft.Sql/servers/virtualNetworkRules | `.../servers/{server}/virtualNetworkRules/{rule}` | |
| azurerm_mssql_failover_group | Microsoft.Sql/servers/failoverGroups | `.../servers/{server}/failoverGroups/{group}` | |
| azurerm_mssql_managed_instance_failover_group | Microsoft.Sql/locations/instanceFailoverGroups | `.../providers/Microsoft.Sql/locations/{location}/instanceFailoverGroups/{group}` | location-scoped path (not server-scoped like the plain failover group) |
| azurerm_mssql_server_security_alert_policy | Microsoft.Sql/servers/securityAlertPolicies | `.../servers/{server}/securityAlertPolicies/Default` | sub-resource of server; name is always literal `Default` |
| azurerm_mysql_flexible_server | Microsoft.DBforMySQL/flexibleServers | `.../providers/Microsoft.DBforMySQL/flexibleServers/{server}` | |
| azurerm_mysql_flexible_database | Microsoft.DBforMySQL/flexibleServers/databases | `.../flexibleServers/{server}/databases/{db}` | sub-resource of server |
| azurerm_mysql_flexible_server_firewall_rule | Microsoft.DBforMySQL/flexibleServers/firewallRules | `.../flexibleServers/{server}/firewallRules/{rule}` | |
| azurerm_mysql_flexible_server_configuration | Microsoft.DBforMySQL/flexibleServers/configurations | `.../flexibleServers/{server}/configurations/{name}` | default/singleton setting provisioned automatically; provider doesn't check for existing before create |
| azurerm_postgresql_flexible_server | Microsoft.DBforPostgreSQL/flexibleServers | `.../providers/Microsoft.DBforPostgreSQL/flexibleServers/{server}` | |
| azurerm_postgresql_flexible_server_database | Microsoft.DBforPostgreSQL/flexibleServers/databases | `.../flexibleServers/{server}/databases/{db}` | sub-resource of server |
| azurerm_postgresql_flexible_server_firewall_rule | Microsoft.DBforPostgreSQL/flexibleServers/firewallRules | `.../flexibleServers/{server}/firewallRules/{rule}` | |
| azurerm_postgresql_flexible_server_configuration | Microsoft.DBforPostgreSQL/flexibleServers/configurations | `.../flexibleServers/{server}/configurations/{name}` | default/singleton setting, same caveat as MySQL configuration |
| azurerm_mariadb_server / _database / _firewall_rule / _configuration / _virtual_network_rule | Microsoft.DBforMariaDB/* | n/a | REMOVED from provider; Azure retiring MariaDB single server (Sept 2025); no replacement resource — migrate to `azurerm_mysql_flexible_server` |
| azurerm_cosmosdb_account | Microsoft.DocumentDB/databaseAccounts | `.../providers/Microsoft.DocumentDB/databaseAccounts/{account}` | keys/connection strings are exported attrs, not imported as separate state |
| azurerm_cosmosdb_sql_database | Microsoft.DocumentDB/databaseAccounts/sqlDatabases | `.../databaseAccounts/{account}/sqlDatabases/{db}` | sub-resource of account |
| azurerm_cosmosdb_sql_container | Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers | `.../sqlDatabases/{db}/containers/{container}` | sub-resource of database |
| azurerm_cosmosdb_mongo_database | Microsoft.DocumentDB/databaseAccounts/mongodbDatabases | `.../databaseAccounts/{account}/mongodbDatabases/{db}` | sub-resource of account |
| azurerm_cosmosdb_mongo_collection | Microsoft.DocumentDB/databaseAccounts/mongodbDatabases/collections | `.../mongodbDatabases/{db}/collections/{coll}` | sub-resource of database |
| azurerm_cosmosdb_cassandra_keyspace | Microsoft.DocumentDB/databaseAccounts/cassandraKeyspaces | `.../databaseAccounts/{account}/cassandraKeyspaces/{ks}` | sub-resource of account |
| azurerm_cosmosdb_cassandra_table | Microsoft.DocumentDB/databaseAccounts/cassandraKeyspaces/tables | `.../cassandraKeyspaces/{ks}/tables/{table}` | sub-resource of keyspace |
| azurerm_cosmosdb_gremlin_database | Microsoft.DocumentDB/databaseAccounts/gremlinDatabases | `.../databaseAccounts/{account}/gremlinDatabases/{db}` | sub-resource of account |
| azurerm_cosmosdb_gremlin_graph | Microsoft.DocumentDB/databaseAccounts/gremlinDatabases/graphs | `.../gremlinDatabases/{db}/graphs/{graph}` | sub-resource of database |
| azurerm_cosmosdb_table | Microsoft.DocumentDB/databaseAccounts/tables | `.../databaseAccounts/{account}/tables/{table}` | Table API; sub-resource of account |
| azurerm_redis_cache | Microsoft.Cache/redis | `.../providers/Microsoft.Cache/redis/{cache}` | |
| azurerm_redis_firewall_rule | Microsoft.Cache/redis/firewallRules | `.../redis/{cache}/firewallRules/{rule}` | sub-resource of cache |
| azurerm_redis_linked_server | Microsoft.Cache/redis/linkedServers | `.../redis/{primaryCache}/linkedServers/{secondaryCache}` | ID keyed by primary cache name; sub-resource, geo-replication link |
| azurerm_synapse_workspace | Microsoft.Synapse/workspaces | `.../providers/Microsoft.Synapse/workspaces/{ws}` | |
| azurerm_synapse_sql_pool | Microsoft.Synapse/workspaces/sqlPools | `.../workspaces/{ws}/sqlPools/{pool}` | sub-resource of workspace (dedicated SQL pool) |
| azurerm_synapse_spark_pool | Microsoft.Synapse/workspaces/bigDataPools | `.../workspaces/{ws}/bigDataPools/{pool}` | sub-resource of workspace |
| azurerm_synapse_integration_runtime_azure | Microsoft.Synapse/workspaces/integrationRuntimes | `.../workspaces/{ws}/integrationRuntimes/{ir}` | shares ARM type/path with self-hosted variant below |
| azurerm_synapse_integration_runtime_self_hosted | Microsoft.Synapse/workspaces/integrationRuntimes | `.../workspaces/{ws}/integrationRuntimes/{ir}` | |
| azurerm_data_factory | Microsoft.DataFactory/factories | `.../providers/Microsoft.DataFactory/factories/{factory}` | |
| azurerm_data_factory_pipeline | Microsoft.DataFactory/factories/pipelines | `.../factories/{factory}/pipelines/{pipeline}` | sub-resource of factory |
| azurerm_data_factory_linked_service_azure_blob_storage | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | 1 of ~22 `linked_service_*`/`linked_custom_service` TF resources sharing this same ARM type/path — only ARM `properties.type` differs per kind |
| azurerm_data_factory_linked_service_azure_databricks | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_azure_file_storage | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_azure_function | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_azure_search | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_azure_sql_database | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_azure_table_storage | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_cosmosdb | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_cosmosdb_mongoapi | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_data_lake_storage_gen2 | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_key_vault | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_kusto | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_mysql | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_odata | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_odbc | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_postgresql | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_sftp | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_snowflake | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_sql_managed_instance | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_sql_server | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_synapse | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_service_web | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | |
| azurerm_data_factory_linked_custom_service | Microsoft.DataFactory/factories/linkedservices | `.../factories/{factory}/linkedservices/{name}` | generic/custom linked-service type |
| azurerm_data_factory_dataset_azure_blob | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | 1 of ~13 `dataset_*`/`custom_dataset` TF resources sharing this same ARM type/path |
| azurerm_data_factory_dataset_azure_sql_table | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_binary | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_cosmosdb_sqlapi | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_delimited_text | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_http | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_json | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_mysql | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_parquet | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_postgresql | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_snowflake | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_dataset_sql_server_table | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | |
| azurerm_data_factory_custom_dataset | Microsoft.DataFactory/factories/datasets | `.../factories/{factory}/datasets/{name}` | generic/custom dataset type |
| azurerm_data_factory_trigger_blob_event | Microsoft.DataFactory/factories/triggers | `.../factories/{factory}/triggers/{name}` | 1 of 4 `trigger_*` TF resources sharing this ARM type/path |
| azurerm_data_factory_trigger_custom_event | Microsoft.DataFactory/factories/triggers | `.../factories/{factory}/triggers/{name}` | |
| azurerm_data_factory_trigger_schedule | Microsoft.DataFactory/factories/triggers | `.../factories/{factory}/triggers/{name}` | |
| azurerm_data_factory_trigger_tumbling_window | Microsoft.DataFactory/factories/triggers | `.../factories/{factory}/triggers/{name}` | |
| azurerm_data_factory_integration_runtime_azure | Microsoft.DataFactory/factories/integrationruntimes | `.../factories/{factory}/integrationruntimes/{name}` | shares ARM type/path with the other 2 IR kinds below |
| azurerm_data_factory_integration_runtime_azure_ssis | Microsoft.DataFactory/factories/integrationruntimes | `.../factories/{factory}/integrationruntimes/{name}` | |
| azurerm_data_factory_integration_runtime_self_hosted | Microsoft.DataFactory/factories/integrationruntimes | `.../factories/{factory}/integrationruntimes/{name}` | |
| azurerm_databricks_workspace | Microsoft.Databricks/workspaces | `.../providers/Microsoft.Databricks/workspaces/{ws}` | |
| azurerm_databricks_access_connector | Microsoft.Databricks/accessConnectors | `.../providers/Microsoft.Databricks/accessConnectors/{name}` | |
| azurerm_eventhub_namespace | Microsoft.EventHub/namespaces | `.../providers/Microsoft.EventHub/namespaces/{ns}` | Kafka protocol enabled via `kafka_enabled` attr on this resource; no separate "kafka" TF resource exists |
| azurerm_eventhub | Microsoft.EventHub/namespaces/eventhubs | `.../namespaces/{ns}/eventhubs/{hub}` | sub-resource of namespace |
| azurerm_eventhub_consumer_group | Microsoft.EventHub/namespaces/eventhubs/consumergroups | `.../eventhubs/{hub}/consumergroups/{group}` | sub-resource of event hub |
| azurerm_eventhub_authorization_rule | Microsoft.EventHub/namespaces/eventhubs/authorizationRules | `.../eventhubs/{hub}/authorizationRules/{rule}` | event-hub-scoped rule; distinct from namespace-scoped rule below |
| azurerm_eventhub_namespace_authorization_rule | Microsoft.EventHub/namespaces/authorizationRules | `.../namespaces/{ns}/authorizationRules/{rule}` | namespace-scoped rule |
| azurerm_stream_analytics_job | Microsoft.StreamAnalytics/streamingjobs | `.../providers/Microsoft.StreamAnalytics/streamingjobs/{job}` | |
| azurerm_stream_analytics_stream_input_blob | Microsoft.StreamAnalytics/streamingjobs/inputs | `.../streamingjobs/{job}/inputs/{name}` | 1 of 6 input TF resources (stream + reference) sharing this ARM type/path |
| azurerm_stream_analytics_stream_input_eventhub | Microsoft.StreamAnalytics/streamingjobs/inputs | `.../streamingjobs/{job}/inputs/{name}` | |
| azurerm_stream_analytics_stream_input_eventhub_v2 | Microsoft.StreamAnalytics/streamingjobs/inputs | `.../streamingjobs/{job}/inputs/{name}` | |
| azurerm_stream_analytics_stream_input_iothub | Microsoft.StreamAnalytics/streamingjobs/inputs | `.../streamingjobs/{job}/inputs/{name}` | |
| azurerm_stream_analytics_reference_input_blob | Microsoft.StreamAnalytics/streamingjobs/inputs | `.../streamingjobs/{job}/inputs/{name}` | |
| azurerm_stream_analytics_reference_input_mssql | Microsoft.StreamAnalytics/streamingjobs/inputs | `.../streamingjobs/{job}/inputs/{name}` | |
| azurerm_stream_analytics_output_blob | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | 1 of 10 output TF resources sharing this ARM type/path |
| azurerm_stream_analytics_output_cosmosdb | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_eventhub | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_function | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_mssql | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_powerbi | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_servicebus_queue | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_servicebus_topic | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_synapse | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_output_table | Microsoft.StreamAnalytics/streamingjobs/outputs | `.../streamingjobs/{job}/outputs/{name}` | |
| azurerm_stream_analytics_function_javascript_uda | Microsoft.StreamAnalytics/streamingjobs/functions | `.../streamingjobs/{job}/functions/{name}` | shares ARM type/path with UDF variant below |
| azurerm_stream_analytics_function_javascript_udf | Microsoft.StreamAnalytics/streamingjobs/functions | `.../streamingjobs/{job}/functions/{name}` | |
| azurerm_hdinsight_hadoop_cluster | Microsoft.HDInsight/clusters | `.../providers/Microsoft.HDInsight/clusters/{cluster}` | all 4 HDInsight cluster-kind TF resources share this same ARM type/path — only the `kind`/config differs |
| azurerm_hdinsight_spark_cluster | Microsoft.HDInsight/clusters | `.../providers/Microsoft.HDInsight/clusters/{cluster}` | |
| azurerm_hdinsight_kafka_cluster | Microsoft.HDInsight/clusters | `.../providers/Microsoft.HDInsight/clusters/{cluster}` | |
| azurerm_hdinsight_interactive_query_cluster | Microsoft.HDInsight/clusters | `.../providers/Microsoft.HDInsight/clusters/{cluster}` | |
| azurerm_data_lake_store | Microsoft.DataLakeStore/accounts | n/a | REMOVED from provider; ADLS Gen1 retired by Azure 29 Feb 2024 |
| azurerm_data_lake_analytics_account | Microsoft.DataLakeAnalytics/accounts | n/a | REMOVED from provider; Data Lake Analytics retired by Azure 29 Feb 2024; migrate to Synapse/HDInsight/Databricks |
| azurerm_kusto_cluster | Microsoft.Kusto/clusters | `.../providers/Microsoft.Kusto/clusters/{cluster}` | |
| azurerm_kusto_database | Microsoft.Kusto/clusters/databases | `.../clusters/{cluster}/databases/{db}` | sub-resource of cluster |
| azurerm_kusto_eventhub_data_connection | Microsoft.Kusto/clusters/databases/dataConnections | `.../databases/{db}/dataConnections/{conn}` | 1 of 4 `*_data_connection` TF resources sharing this ARM type/path |
| azurerm_kusto_iothub_data_connection | Microsoft.Kusto/clusters/databases/dataConnections | `.../databases/{db}/dataConnections/{conn}` | |
| azurerm_kusto_eventgrid_data_connection | Microsoft.Kusto/clusters/databases/dataConnections | `.../databases/{db}/dataConnections/{conn}` | |
| azurerm_kusto_cosmosdb_data_connection | Microsoft.Kusto/clusters/databases/dataConnections | `.../databases/{db}/dataConnections/{conn}` | |
| azurerm_purview_account | Microsoft.Purview/accounts | `.../providers/Microsoft.Purview/accounts/{account}` | |
| azurerm_data_share_account | Microsoft.DataShare/accounts | `.../providers/Microsoft.DataShare/accounts/{account}` | |
| azurerm_data_share | Microsoft.DataShare/accounts/shares | `.../accounts/{account}/shares/{share}` | sub-resource of account |
| azurerm_data_share_dataset_blob_storage | Microsoft.DataShare/accounts/shares/dataSets | `.../shares/{share}/dataSets/{dataset}` | 1 of 4 `dataset_*` TF resources sharing this ARM type/path |
| azurerm_data_share_dataset_data_lake_gen2 | Microsoft.DataShare/accounts/shares/dataSets | `.../shares/{share}/dataSets/{dataset}` | |
| azurerm_data_share_dataset_kusto_cluster | Microsoft.DataShare/accounts/shares/dataSets | `.../shares/{share}/dataSets/{dataset}` | |
| azurerm_data_share_dataset_kusto_database | Microsoft.DataShare/accounts/shares/dataSets | `.../shares/{share}/dataSets/{dataset}` | |
| azurerm_iot_time_series_insights_gen2_environment / _event_source_eventhub / _reference_data_set / _access_policy | Microsoft.TimeSeriesInsights/* | n/a | REMOVED from provider; Azure retired Time Series Insights 31 Mar 2025; migrate to Kusto/Data Explorer |
| azurerm_powerbi_embedded | Microsoft.PowerBIDedicated/capacities | `.../providers/Microsoft.PowerBIDedicated/capacities/{capacity}` | |
