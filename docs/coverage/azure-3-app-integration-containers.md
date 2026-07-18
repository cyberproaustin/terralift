# Azure coverage: app / integration / containers

All import IDs are relative to `/subscriptions/{subId}/resourceGroups/{rg}/` unless the Notes column says otherwise (composite `|`-joined IDs, data-plane endpoint IDs, or synthetic Terraform-only IDs).

## App Service / Web

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_service_plan | Microsoft.Web/serverfarms | providers/Microsoft.Web/serverfarms/{name} | replaces deprecated azurerm_app_service_plan, removed in provider v4 |
| azurerm_linux_web_app | Microsoft.Web/sites | providers/Microsoft.Web/sites/{name} | shares Microsoft.Web/sites with windows_web_app + both function app types; `kind`=app,linux distinguishes |
| azurerm_windows_web_app | Microsoft.Web/sites | providers/Microsoft.Web/sites/{name} | kind=app |
| azurerm_linux_function_app | Microsoft.Web/sites | providers/Microsoft.Web/sites/{name} | kind=functionapp,linux |
| azurerm_windows_function_app | Microsoft.Web/sites | providers/Microsoft.Web/sites/{name} | kind=functionapp |
| azurerm_linux_web_app_slot | Microsoft.Web/sites/slots | providers/Microsoft.Web/sites/{app}/slots/{slot} | shares ARM type with all 4 slot resources; kind distinguishes |
| azurerm_windows_web_app_slot | Microsoft.Web/sites/slots | providers/Microsoft.Web/sites/{app}/slots/{slot} | |
| azurerm_linux_function_app_slot | Microsoft.Web/sites/slots | providers/Microsoft.Web/sites/{app}/slots/{slot} | |
| azurerm_windows_function_app_slot | Microsoft.Web/sites/slots | providers/Microsoft.Web/sites/{app}/slots/{slot} | |
| azurerm_static_web_app | Microsoft.Web/staticSites | providers/Microsoft.Web/staticSites/{name} | supersedes deprecated azurerm_static_site |
| azurerm_static_web_app_custom_domain | Microsoft.Web/staticSites/customDomains | providers/Microsoft.Web/staticSites/{site}/customDomains/{domain} | sub-resource |
| azurerm_static_web_app_function_app_registration | Microsoft.Web/staticSites/userProvidedFunctionApps | providers/Microsoft.Web/staticSites/{site}/userProvidedFunctionApps/{name} | sub-resource, links to a separate Function App |
| azurerm_app_service_source_control | Microsoft.Web/sites (sourcecontrol config) | providers/Microsoft.Web/sites/{name} | import ID = same ID as the parent site; not a distinct sub-resource ID |
| azurerm_app_service_source_control_slot | Microsoft.Web/sites/slots | providers/Microsoft.Web/sites/{app}/slots/{slot} | import ID = same ID as parent slot |
| azurerm_app_service_custom_hostname_binding | Microsoft.Web/sites/hostNameBindings | providers/Microsoft.Web/sites/{app}/hostNameBindings/{hostname} | |
| azurerm_app_service_slot_custom_hostname_binding | Microsoft.Web/sites/slots/hostNameBindings | providers/Microsoft.Web/sites/{app}/slots/{slot}/hostNameBindings/{hostname} | sub-resource of slot |
| azurerm_app_service_certificate | Microsoft.Web/certificates | providers/Microsoft.Web/certificates/{name} | shares ARM type with app_service_managed_certificate |
| azurerm_app_service_certificate_binding | Microsoft.Web/sites/hostNameBindings + Microsoft.Web/certificates | `{hostNameBindingID}|{certificateID}` | composite `\|`-joined two-resource import ID |
| azurerm_app_service_certificate_order | Microsoft.CertificateRegistration/certificateOrders | providers/Microsoft.CertificateRegistration/certificateOrders/{name} | |
| azurerm_app_service_managed_certificate | Microsoft.Web/certificates | providers/Microsoft.Web/certificates/{name} | shares ARM type with app_service_certificate (free cert, distinguished by issuer) |
| azurerm_app_service_public_certificate | Microsoft.Web/sites/publicCertificates | providers/Microsoft.Web/sites/{app}/publicCertificates/{name} | sub-resource |
| azurerm_app_service_environment_v3 | Microsoft.Web/hostingEnvironments | providers/Microsoft.Web/hostingEnvironments/{name} | ASEv1/v2 resources were removed from the provider; only v3 remains |
| azurerm_app_service_virtual_network_swift_connection | Microsoft.Web/sites/config | providers/Microsoft.Web/sites/{app}/config/virtualNetwork | fixed-name config sub-resource (VNet integration), not a normal named child |
| azurerm_app_service_slot_virtual_network_swift_connection | Microsoft.Web/sites/slots/config | providers/Microsoft.Web/sites/{app}/slots/{slot}/config/virtualNetwork | slot variant of above |
| azurerm_app_service_hybrid_connection | Microsoft.Web/sites/hybridConnectionNamespaces/relays | providers/Microsoft.Web/sites/{app}/hybridConnectionNamespaces/{relayNamespace}/relays/{relayName} | binds app to a Relay hybrid connection, see Relay family |

## Container — Kubernetes (AKS)

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_kubernetes_cluster | Microsoft.ContainerService/managedClusters | providers/Microsoft.ContainerService/managedClusters/{name} | |
| azurerm_kubernetes_cluster_node_pool | Microsoft.ContainerService/managedClusters/agentPools | providers/Microsoft.ContainerService/managedClusters/{cluster}/agentPools/{pool} | the cluster's default/system node pool is managed inline on azurerm_kubernetes_cluster, not via this resource |

## Container — Instances (ACI)

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_container_group | Microsoft.ContainerInstance/containerGroups | providers/Microsoft.ContainerInstance/containerGroups/{name} | |

## Container — Registry (ACR)

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_container_registry | Microsoft.ContainerRegistry/registries | providers/Microsoft.ContainerRegistry/registries/{name} | |
| azurerm_container_registry_token | Microsoft.ContainerRegistry/registries/tokens | providers/Microsoft.ContainerRegistry/registries/{reg}/tokens/{token} | sub-resource |
| azurerm_container_registry_scope_map | Microsoft.ContainerRegistry/registries/scopeMaps | providers/Microsoft.ContainerRegistry/registries/{reg}/scopeMaps/{name} | sub-resource |
| azurerm_container_registry_webhook | Microsoft.ContainerRegistry/registries/webhooks | providers/Microsoft.ContainerRegistry/registries/{reg}/webhooks/{name} | sub-resource |
| azurerm_container_registry_task | Microsoft.ContainerRegistry/registries/tasks | providers/Microsoft.ContainerRegistry/registries/{reg}/tasks/{name} | sub-resource |

## Container — Container Apps

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_container_app | Microsoft.App/containerApps | providers/Microsoft.App/containerApps/{name} | |
| azurerm_container_app_environment | Microsoft.App/managedEnvironments | providers/Microsoft.App/managedEnvironments/{name} | |
| azurerm_container_app_environment_certificate | Microsoft.App/managedEnvironments/certificates | providers/Microsoft.App/managedEnvironments/{env}/certificates/{name} | sub-resource |
| azurerm_container_app_environment_dapr_component | Microsoft.App/managedEnvironments/daprComponents | providers/Microsoft.App/managedEnvironments/{env}/daprComponents/{name} | sub-resource |
| azurerm_container_app_environment_storage | Microsoft.App/managedEnvironments/storages | providers/Microsoft.App/managedEnvironments/{env}/storages/{name} | sub-resource |
| azurerm_container_app_job | Microsoft.App/jobs | providers/Microsoft.App/jobs/{name} | |

## Container — Service Fabric

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_service_fabric_cluster | Microsoft.ServiceFabric/clusters | providers/Microsoft.ServiceFabric/clusters/{name} | "classic" unmanaged cluster; distinct type from managed_cluster |
| azurerm_service_fabric_managed_cluster | Microsoft.ServiceFabric/managedClusters | providers/Microsoft.ServiceFabric/managedClusters/{name} | node types are managed inline via a `node_type` block, not a standalone azurerm_service_fabric_managed_cluster_node_type resource (no such resource exists) |

## API Management

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_api_management | Microsoft.ApiManagement/service | providers/Microsoft.ApiManagement/service/{name} | |
| azurerm_api_management_api | Microsoft.ApiManagement/service/apis | providers/Microsoft.ApiManagement/service/{svc}/apis/{apiId} | apiId may embed `;rev={n}` for a specific revision |
| azurerm_api_management_api_operation | Microsoft.ApiManagement/service/apis/operations | providers/Microsoft.ApiManagement/service/{svc}/apis/{api}/operations/{opId} | |
| azurerm_api_management_product | Microsoft.ApiManagement/service/products | providers/Microsoft.ApiManagement/service/{svc}/products/{productId} | |
| azurerm_api_management_subscription | Microsoft.ApiManagement/service/subscriptions | providers/Microsoft.ApiManagement/service/{svc}/subscriptions/{subId} | |
| azurerm_api_management_backend | Microsoft.ApiManagement/service/backends | providers/Microsoft.ApiManagement/service/{svc}/backends/{name} | |
| azurerm_api_management_named_value | Microsoft.ApiManagement/service/namedValues | providers/Microsoft.ApiManagement/service/{svc}/namedValues/{name} | |
| azurerm_api_management_policy | Microsoft.ApiManagement/service/policies | providers/Microsoft.ApiManagement/service/{svc} | global policy is a singleton; import ID = parent service ID, not `.../policies/policy` |
| azurerm_api_management_api_policy | Microsoft.ApiManagement/service/apis/policies | providers/Microsoft.ApiManagement/service/{svc}/apis/{api} | singleton per API; import ID = parent API ID |
| azurerm_api_management_product_policy | Microsoft.ApiManagement/service/products/policies | providers/Microsoft.ApiManagement/service/{svc}/products/{product} | singleton per product; import ID = parent product ID |
| azurerm_api_management_api_operation_policy | Microsoft.ApiManagement/service/apis/operations/policies | providers/Microsoft.ApiManagement/service/{svc}/apis/{api}/operations/{op} | singleton per operation; import ID = parent operation ID |
| azurerm_api_management_gateway | Microsoft.ApiManagement/service/gateways | providers/Microsoft.ApiManagement/service/{svc}/gateways/{name} | self-hosted gateway entity/registration only, not the runtime container |
| azurerm_api_management_logger | Microsoft.ApiManagement/service/loggers | providers/Microsoft.ApiManagement/service/{svc}/loggers/{name} | |
| azurerm_api_management_diagnostic | Microsoft.ApiManagement/service/diagnostics | providers/Microsoft.ApiManagement/service/{svc}/diagnostics/{name} | service-level diagnostic; distinct resource from api_management_api_diagnostic |
| azurerm_api_management_api_diagnostic | Microsoft.ApiManagement/service/apis/diagnostics | providers/Microsoft.ApiManagement/service/{svc}/apis/{api}/diagnostics/{name} | API-level diagnostic |
| azurerm_api_management_certificate | Microsoft.ApiManagement/service/certificates | providers/Microsoft.ApiManagement/service/{svc}/certificates/{name} | |

## Integration — Service Bus

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_servicebus_namespace | Microsoft.ServiceBus/namespaces | providers/Microsoft.ServiceBus/namespaces/{name} | |
| azurerm_servicebus_queue | Microsoft.ServiceBus/namespaces/queues | providers/Microsoft.ServiceBus/namespaces/{ns}/queues/{name} | |
| azurerm_servicebus_topic | Microsoft.ServiceBus/namespaces/topics | providers/Microsoft.ServiceBus/namespaces/{ns}/topics/{name} | |
| azurerm_servicebus_subscription | Microsoft.ServiceBus/namespaces/topics/subscriptions | providers/Microsoft.ServiceBus/namespaces/{ns}/topics/{topic}/subscriptions/{name} | |
| azurerm_servicebus_subscription_rule | Microsoft.ServiceBus/.../subscriptions/rules | providers/Microsoft.ServiceBus/namespaces/{ns}/topics/{topic}/subscriptions/{sub}/rules/{name} | |
| azurerm_servicebus_namespace_authorization_rule | Microsoft.ServiceBus/namespaces/authorizationRules | providers/Microsoft.ServiceBus/namespaces/{ns}/authorizationRules/{name} | |
| azurerm_servicebus_queue_authorization_rule | Microsoft.ServiceBus/namespaces/queues/authorizationRules | providers/Microsoft.ServiceBus/namespaces/{ns}/queues/{q}/authorizationRules/{name} | |
| azurerm_servicebus_topic_authorization_rule | Microsoft.ServiceBus/namespaces/topics/authorizationRules | providers/Microsoft.ServiceBus/namespaces/{ns}/topics/{t}/authorizationRules/{name} | |

## Integration — Event Grid

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_eventgrid_topic | Microsoft.EventGrid/topics | providers/Microsoft.EventGrid/topics/{name} | |
| azurerm_eventgrid_domain | Microsoft.EventGrid/domains | providers/Microsoft.EventGrid/domains/{name} | |
| azurerm_eventgrid_domain_topic | Microsoft.EventGrid/domains/topics | providers/Microsoft.EventGrid/domains/{domain}/topics/{name} | |
| azurerm_eventgrid_event_subscription | Microsoft.EventGrid/eventSubscriptions | {scopeResourceID}/providers/Microsoft.EventGrid/eventSubscriptions/{name} | scope can be a subscription, RG, or any resource (topic, VNet, storage account, etc) — ID nests under whatever resource is subscribed, not just a topic |
| azurerm_eventgrid_system_topic | Microsoft.EventGrid/systemTopics | providers/Microsoft.EventGrid/systemTopics/{name} | |
| azurerm_eventgrid_system_topic_event_subscription | Microsoft.EventGrid/systemTopics/eventSubscriptions | providers/Microsoft.EventGrid/systemTopics/{topic}/eventSubscriptions/{name} | separate resource type from generic eventgrid_event_subscription |
| azurerm_eventgrid_namespace | Microsoft.EventGrid/namespaces | providers/Microsoft.EventGrid/namespaces/{name} | newer MQTT/pull-delivery namespace model |
| azurerm_eventgrid_namespace_topic | Microsoft.EventGrid/namespaces/topics | providers/Microsoft.EventGrid/namespaces/{ns}/topics/{name} | sub-resource of eventgrid_namespace, unrelated to azurerm_eventgrid_topic |

## Integration — Logic Apps

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_logic_app_workflow | Microsoft.Logic/workflows | providers/Microsoft.Logic/workflows/{name} | Consumption-plan Logic App |
| azurerm_logic_app_standard | Microsoft.Web/sites | providers/Microsoft.Web/sites/{name} | Standard-plan Logic App is actually a Function App under the hood; shares Microsoft.Web/sites with web/function apps, kind includes `workflowapp` |
| azurerm_logic_app_trigger_custom | Microsoft.Logic/workflows (definition JSON) | providers/Microsoft.Logic/workflows/{wf}/triggers/{name} | synthetic Terraform-only ID (workflow ID + `/triggers/{name}`); trigger lives inside the workflow's `definition` JSON, not a real separate ARM entity |
| azurerm_logic_app_trigger_recurrence | Microsoft.Logic/workflows (definition JSON) | providers/Microsoft.Logic/workflows/{wf}/triggers/{name} | same synthetic-ID caveat |
| azurerm_logic_app_trigger_http_request | Microsoft.Logic/workflows (definition JSON) | providers/Microsoft.Logic/workflows/{wf}/triggers/{name} | same synthetic-ID caveat |
| azurerm_logic_app_action_custom | Microsoft.Logic/workflows (definition JSON) | providers/Microsoft.Logic/workflows/{wf}/actions/{name} | synthetic Terraform-only ID, doc explicitly notes it "doesn't directly match to any other resource" |
| azurerm_logic_app_action_http | Microsoft.Logic/workflows (definition JSON) | providers/Microsoft.Logic/workflows/{wf}/actions/{name} | same synthetic-ID caveat |
| azurerm_logic_app_integration_account | Microsoft.Logic/integrationAccounts | providers/Microsoft.Logic/integrationAccounts/{name} | |
| azurerm_logic_app_integration_account_map | Microsoft.Logic/integrationAccounts/maps | providers/Microsoft.Logic/integrationAccounts/{acct}/maps/{name} | sub-resource |
| azurerm_logic_app_integration_account_schema | Microsoft.Logic/integrationAccounts/schemas | providers/Microsoft.Logic/integrationAccounts/{acct}/schemas/{name} | sub-resource |
| azurerm_logic_app_integration_account_certificate | Microsoft.Logic/integrationAccounts/certificates | providers/Microsoft.Logic/integrationAccounts/{acct}/certificates/{name} | sub-resource |
| azurerm_logic_app_integration_account_partner | Microsoft.Logic/integrationAccounts/partners | providers/Microsoft.Logic/integrationAccounts/{acct}/partners/{name} | sub-resource |
| azurerm_logic_app_integration_account_session | Microsoft.Logic/integrationAccounts/sessions | providers/Microsoft.Logic/integrationAccounts/{acct}/sessions/{name} | sub-resource |
| azurerm_api_connection | Microsoft.Web/connections | providers/Microsoft.Web/connections/{name} | managed connector instance commonly referenced by Consumption Logic App workflows |

## Integration — Relay

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_relay_namespace | Microsoft.Relay/namespaces | providers/Microsoft.Relay/namespaces/{name} | |
| azurerm_relay_hybrid_connection | Microsoft.Relay/namespaces/hybridConnections | providers/Microsoft.Relay/namespaces/{ns}/hybridConnections/{name} | |
| azurerm_relay_namespace_authorization_rule | Microsoft.Relay/namespaces/authorizationRules | providers/Microsoft.Relay/namespaces/{ns}/authorizationRules/{name} | |
| azurerm_relay_hybrid_connection_authorization_rule | Microsoft.Relay/namespaces/hybridConnections/authorizationRules | providers/Microsoft.Relay/namespaces/{ns}/hybridConnections/{hc}/authorizationRules/{name} | |

## Integration — Notification Hubs

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_notification_hub_namespace | Microsoft.NotificationHubs/namespaces | providers/Microsoft.NotificationHubs/namespaces/{name} | |
| azurerm_notification_hub | Microsoft.NotificationHubs/namespaces/notificationHubs | providers/Microsoft.NotificationHubs/namespaces/{ns}/notificationHubs/{name} | |
| azurerm_notification_hub_authorization_rule | Microsoft.NotificationHubs/.../authorizationRules | providers/Microsoft.NotificationHubs/namespaces/{ns}/notificationHubs/{hub}/authorizationRules/{name} | |

## Integration — SignalR

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_signalr_service | Microsoft.SignalRService/signalR | providers/Microsoft.SignalRService/signalR/{name} | |
| azurerm_signalr_service_network_acl | Microsoft.SignalRService/signalR | providers/Microsoft.SignalRService/signalR/{name} | 1:1 config sub-resource; import ID = same as parent SignalR service ID |
| azurerm_signalr_shared_private_link_resource | Microsoft.SignalRService/signalR/sharedPrivateLinkResources | providers/Microsoft.SignalRService/signalR/{svc}/sharedPrivateLinkResources/{name} | sub-resource |

## Integration — Web PubSub

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_web_pubsub | Microsoft.SignalRService/webPubSub | providers/Microsoft.SignalRService/webPubSub/{name} | shares the SignalRService RP namespace, not a separate "WebPubSub" provider |
| azurerm_web_pubsub_hub | Microsoft.SignalRService/webPubSub/hubs | providers/Microsoft.SignalRService/webPubSub/{svc}/hubs/{name} | sub-resource |
| azurerm_web_pubsub_network_acl | Microsoft.SignalRService/webPubSub | providers/Microsoft.SignalRService/webPubSub/{name} | 1:1 config sub-resource; import ID = same as parent Web PubSub ID |

## Messaging / App Configuration

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_app_configuration | Microsoft.AppConfiguration/configurationStores | providers/Microsoft.AppConfiguration/configurationStores/{name} | |
| azurerm_app_configuration_key | Microsoft.AppConfiguration/configurationStores (data-plane key) | `https://{store}.azconfig.io/kv/{key}?label={label}` | data-plane, endpoint-based ID (not an ARM resource ID); leave `label=` blank for no label |
| azurerm_app_configuration_feature | Microsoft.AppConfiguration/configurationStores (data-plane key) | `https://{store}.azconfig.io/kv/.appconfig.featureflag%2F{feature}?label={label}` | data-plane, endpoint-based ID; feature flags are stored as specially-named keys |

## Communication Services

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_communication_service | Microsoft.Communication/communicationServices | providers/Microsoft.Communication/communicationServices/{name} | |
| azurerm_communication_service_email_domain_association | Microsoft.Communication/communicationServices + Microsoft.Communication/emailServices/domains | `{communicationServiceID}|{emailDomainID}` | composite `\|`-joined two-resource import ID |
| azurerm_email_communication_service | Microsoft.Communication/emailServices | providers/Microsoft.Communication/emailServices/{name} | |
| azurerm_email_communication_service_domain | Microsoft.Communication/emailServices/domains | providers/Microsoft.Communication/emailServices/{svc}/domains/{name} | sub-resource |
| azurerm_email_communication_service_domain_sender_username | Microsoft.Communication/emailServices/domains/senderUsernames | providers/Microsoft.Communication/emailServices/{svc}/domains/{domain}/senderUsernames/{name} | sub-resource |

## Batch

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_batch_account | Microsoft.Batch/batchAccounts | providers/Microsoft.Batch/batchAccounts/{name} | |
| azurerm_batch_pool | Microsoft.Batch/batchAccounts/pools | providers/Microsoft.Batch/batchAccounts/{acct}/pools/{name} | |
| azurerm_batch_job | Microsoft.Batch/batchAccounts/pools/jobs | providers/Microsoft.Batch/batchAccounts/{acct}/pools/{pool}/jobs/{name} | |
| azurerm_batch_application | Microsoft.Batch/batchAccounts/applications | providers/Microsoft.Batch/batchAccounts/{acct}/applications/{name} | package versions are referenced via `default_version`/`allow_updates`, not a separate azurerm_batch_application_package resource (no such resource exists) |
| azurerm_batch_certificate | Microsoft.Batch/batchAccounts/certificates | providers/Microsoft.Batch/batchAccounts/{acct}/certificates/{name} | deprecated — Azure is retiring Batch Account Certificates; resource will be removed in azurerm v5.0 |

## Search

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_search_service | Microsoft.Search/searchServices | providers/Microsoft.Search/searchServices/{name} | |
| azurerm_search_shared_private_link_service | Microsoft.Search/searchServices/sharedPrivateLinkResources | providers/Microsoft.Search/searchServices/{svc}/sharedPrivateLinkResources/{name} | sub-resource |

## Maps

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_maps_account | Microsoft.Maps/accounts | providers/Microsoft.Maps/accounts/{name} | |

## Spring Cloud / Azure Spring Apps

| azurerm type | ARM resource type | import ID | notes |
|---|---|---|---|
| azurerm_spring_cloud_service | Microsoft.AppPlatform/Spring | providers/Microsoft.AppPlatform/Spring/{name} | Terraform resource/import ID still use "spring_cloud" naming despite Azure's "Azure Spring Apps" rebrand |
| azurerm_spring_cloud_app | Microsoft.AppPlatform/Spring/apps | providers/Microsoft.AppPlatform/Spring/{svc}/apps/{name} | |
| azurerm_spring_cloud_java_deployment | Microsoft.AppPlatform/Spring/apps/deployments | providers/Microsoft.AppPlatform/Spring/{svc}/apps/{app}/deployments/{name} | |
| azurerm_spring_cloud_active_deployment | Microsoft.AppPlatform/Spring/apps | providers/Microsoft.AppPlatform/Spring/{svc}/apps/{app} | pointer/config sub-resource; import ID = same as parent app ID |
| azurerm_spring_cloud_certificate | Microsoft.AppPlatform/Spring/certificates | providers/Microsoft.AppPlatform/Spring/{svc}/certificates/{name} | |
| azurerm_spring_cloud_custom_domain | Microsoft.AppPlatform/Spring/apps/domains | providers/Microsoft.AppPlatform/Spring/{svc}/apps/{app}/domains/{name} | |
| azurerm_spring_cloud_config_server | Microsoft.AppPlatform/Spring/configServers | providers/Microsoft.AppPlatform/Spring/{svc} | singleton config sub-resource; import ID = parent service ID |
| azurerm_spring_cloud_gateway | Microsoft.AppPlatform/Spring/gateways | providers/Microsoft.AppPlatform/Spring/{svc}/gateways/{name} | Enterprise tier only |
| azurerm_spring_cloud_api_portal | Microsoft.AppPlatform/Spring/apiPortals | providers/Microsoft.AppPlatform/Spring/{svc}/apiPortals/{name} | Enterprise tier only |
| azurerm_spring_cloud_service_registry | Microsoft.AppPlatform/Spring/serviceRegistries | providers/Microsoft.AppPlatform/Spring/{svc}/serviceRegistries/{name} | Enterprise tier only |
| azurerm_spring_cloud_builder | Microsoft.AppPlatform/Spring/buildServices/builders | providers/Microsoft.AppPlatform/Spring/{svc}/buildServices/{build}/builders/{name} | Enterprise tier only, nested under buildServices |
| azurerm_spring_cloud_build_deployment | Microsoft.AppPlatform/Spring/apps/deployments | providers/Microsoft.AppPlatform/Spring/{svc}/apps/{app}/deployments/{name} | Enterprise tier equivalent of spring_cloud_java_deployment |
