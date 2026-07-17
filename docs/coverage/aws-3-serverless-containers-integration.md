# AWS coverage: serverless / containers / integration

| Terraform type | Resource Explorer resourceType | Import ID format | Notes |
|---|---|---|---|
| aws_lambda_function | lambda:function | `function_name` | |
| aws_lambda_alias | - | `function_name/alias` | sub-resource of function; not RE-enumerable |
| aws_lambda_layer_version | lambda:layer/version | `arn` (e.g. `arn:aws:lambda:region:account:layer:name:version`) | full-ARN import; version is embedded in ARN |
| aws_lambda_permission | - | `function_name/statement_id` (or `function_name:qualifier/statement_id`) | sub-resource of function; not RE-enumerable; composite id |
| aws_lambda_event_source_mapping | lambda:event-source-mapping | `UUID` | |
| aws_lambda_function_url | - | `function_name` or `function_name/qualifier` | sub-resource of function; not RE-enumerable |
| aws_lambda_provisioned_concurrency_config | - | `function_name,qualifier` | sub-resource; not RE-enumerable; comma-delimited (not slash) |
| aws_lambda_code_signing_config | lambda:code-signing-config | `arn` | |
| aws_ecs_cluster | ecs:cluster | `cluster name` | |
| aws_ecs_service | ecs:service | `cluster-name/service-name` | |
| aws_ecs_task_definition | ecs:task-definition | `arn` (family:revision, e.g. `arn:aws:ecs:region:account:task-definition/family:revision`) | full-ARN import; each revision is a distinct resource |
| aws_ecs_capacity_provider | ecs:capacity-provider | `arn` | full-ARN import, not name |
| aws_eks_cluster | eks:cluster | `cluster name` | |
| aws_eks_node_group | - | `cluster_name:node_group_name` | sub-resource of cluster; not RE-enumerable |
| aws_eks_fargate_profile | - | `cluster_name:fargate_profile_name` | sub-resource of cluster; not RE-enumerable |
| aws_eks_addon | - | `cluster_name:addon_name` | sub-resource of cluster; not RE-enumerable |
| aws_eks_identity_provider_config | - | `cluster_name:config_name` | sub-resource of cluster; not RE-enumerable |
| aws_eks_access_entry | - | `cluster_name:principal_arn` | sub-resource of cluster; not RE-enumerable |
| aws_ecr_repository | ecr:repository | `repository name` | |
| aws_ecr_lifecycle_policy | - | `repository name` | sub-resource of repository; not RE-enumerable |
| aws_ecr_repository_policy | - | `repository name` | sub-resource of repository; not RE-enumerable |
| aws_ecr_registry_policy | - | `registry id` (AWS account id) | account-level singleton; not RE-enumerable |
| aws_ecr_pull_through_cache_rule | - | `ecr_repository_prefix` | not RE-enumerable |
| aws_ecrpublic_repository | ecr-public:repository | `repository name` | us-east-1 only |
| aws_ecrpublic_repository_policy | - | `repository name` | sub-resource of repository; not RE-enumerable; us-east-1 only |
| aws_apprunner_service | apprunner:service | `arn` | |
| aws_apprunner_connection | apprunner:connection | `connection_name` | |
| aws_apprunner_auto_scaling_configuration_version | apprunner:autoscalingconfiguration | `arn` | ARN includes revision number |
| aws_apprunner_vpc_connector | apprunner:vpcconnector | `arn` | |
| aws_apprunner_observability_configuration | - | `arn` | not RE-enumerable (absent from RE App Runner list) |
| aws_batch_compute_environment | batch:compute-environment | `name` | |
| aws_batch_job_queue | batch:job-queue | `arn` | |
| aws_batch_job_definition | batch:job-definition | `arn` | ARN includes revision; each revision is a distinct resource |
| aws_batch_scheduling_policy | batch:scheduling-policy | `arn` | |
| aws_appmesh_mesh | appmesh:mesh | `mesh name` | |
| aws_appmesh_virtual_service | appmesh:mesh/virtualService | `mesh_name/virtual_service_name` | |
| aws_appmesh_virtual_node | appmesh:mesh/virtualNode | `mesh_name/virtual_node_name` | |
| aws_appmesh_virtual_router | appmesh:mesh/virtualRouter | `mesh_name/virtual_router_name` | |
| aws_appmesh_route | appmesh:mesh/virtualRouter/route | `mesh_name/virtual_router_name/route_name` | composite id |
| aws_appmesh_virtual_gateway | appmesh:mesh/virtualGateway | `mesh_name/virtual_gateway_name` | |
| aws_appmesh_gateway_route | appmesh:mesh/virtualGateway/gatewayRoute | `mesh_name/virtual_gateway_name/gateway_route_name` | composite id |
| aws_sns_topic | sns:topic | `arn` | |
| aws_sns_topic_subscription | - | `subscription arn` | not RE-enumerable (RE lists only sns:topic) |
| aws_sns_topic_policy | - | `topic arn` | sub-resource of topic; not RE-enumerable |
| aws_sns_platform_application | - | `arn` | not RE-enumerable |
| aws_sqs_queue | sqs:queue | `queue URL` | import id is the queue URL, not the ARN — common gotcha |
| aws_sqs_queue_policy | - | `queue URL` | sub-resource of queue; not RE-enumerable |
| aws_cloudwatch_event_bus | events:event-bus | `name` | |
| aws_cloudwatch_event_rule | events:rule | `event_bus_name/rule_name` | for default bus, bus name is still part of documented id form |
| aws_cloudwatch_event_target | - | `event_bus_name/rule-name/target-id` (bus name omittable for default bus) | sub-resource of rule; not RE-enumerable |
| aws_cloudwatch_event_api_destination | events:api-destination | `name` | |
| aws_cloudwatch_event_connection | events:connection | `name` | |
| aws_cloudwatch_event_archive | events:archive | `name` | |
| aws_pipes_pipe | pipes:pipe | `name` | |
| aws_scheduler_schedule | - | `group_name/name` | not RE-enumerable (RE lists only scheduler:schedule-group, not individual schedules) |
| aws_scheduler_schedule_group | scheduler:schedule-group | `name` | |
| aws_sfn_state_machine | states:stateMachine | `arn` | |
| aws_sfn_activity | states:activity | `arn` | |
| aws_api_gateway_rest_api | apigateway:restapis | `rest-api-id` | |
| aws_api_gateway_resource | apigateway:restapis/resources | `rest-api-id/resource-id` | |
| aws_api_gateway_method | apigateway:restapis/resources/methods | `rest-api-id/resource-id/http-method` | |
| aws_api_gateway_integration | - | `rest-api-id/resource-id/http-method` | sub-resource of method; not RE-enumerable |
| aws_api_gateway_deployment | apigateway:restapis/deployments | `rest-api-id/deployment-id` | |
| aws_api_gateway_stage | apigateway:restapis/stages | `rest-api-id/stage-name` | |
| aws_api_gateway_api_key | - | `api-key-id` | not RE-enumerable |
| aws_api_gateway_usage_plan | - | `usage-plan-id` | not RE-enumerable |
| aws_api_gateway_authorizer | - | `rest-api-id/authorizer-id` | sub-resource; not RE-enumerable |
| aws_api_gateway_domain_name | - | `domain-name` (or `domain-name/domain_name_id` for private) | not RE-enumerable |
| aws_api_gateway_vpc_link | apigateway:vpclinks | `vpc-link-id` | |
| aws_api_gateway_model | - | `rest-api-id/model-name` | sub-resource; not RE-enumerable |
| aws_api_gateway_gateway_response | - | `rest-api-id/response-type` | sub-resource; not RE-enumerable |
| aws_apigatewayv2_api | apigateway:apis | `api-id` | |
| aws_apigatewayv2_route | apigateway:apis/routes | `api-id/route-id` | |
| aws_apigatewayv2_integration | apigateway:apis/integrations | `api-id/integration-id` | |
| aws_apigatewayv2_stage | apigateway:apis/stages | `api-id/stage-name` | |
| aws_apigatewayv2_deployment | - | `api-id/deployment-id` | not RE-enumerable; `triggers` attribute not importable |
| aws_apigatewayv2_domain_name | - | `domain-name` | not RE-enumerable |
| aws_apigatewayv2_authorizer | - | `api-id/authorizer-id` | sub-resource; not RE-enumerable |
| aws_appsync_graphql_api | appsync:apis | `api-id` | |
| aws_appsync_datasource | - | `api_id-name` | sub-resource; not RE-enumerable; hyphen-joined id, not slash |
| aws_appsync_resolver | - | `api_id-type-field` | sub-resource; not RE-enumerable; hyphen-joined id, not slash |
| aws_appsync_function | - | `api_id-function_id` | sub-resource; not RE-enumerable; hyphen-joined id, not slash |
| aws_mq_broker | mq:broker | `broker id` | |
| aws_mq_configuration | mq:configuration | `configuration id` | |
| aws_msk_cluster | kafka:cluster | `arn` | |
| aws_msk_configuration | kafka:configuration | `arn` | |
| aws_msk_serverless_cluster | kafka:cluster ? | `arn` | RE resourceType not separately confirmed; likely reported under shared kafka:cluster type |
| aws_mwaa_environment | airflow:environment | `environment name` | |
| aws_swf_domain | - | `domain name` | not RE-enumerable — SWF has no resourceType in RE's supported list at all |
