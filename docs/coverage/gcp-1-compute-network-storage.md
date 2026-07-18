# GCP coverage: compute / network / storage

| Terraform type | CAI asset type | Import ID format | Notes |
|---|---|---|---|
| google_compute_instance | compute.googleapis.com/Instance | `projects/{{project}}/zones/{{zone}}/instances/{{name}}` | |
| google_compute_instance_template | compute.googleapis.com/InstanceTemplate | `projects/{{project}}/global/instanceTemplates/{{name}}` | |
| google_compute_instance_group | compute.googleapis.com/InstanceGroup | `projects/{{project}}/zones/{{zone}}/instanceGroups/{{name}}` | unmanaged IGs only — the IG backing a MIG is owned by google_compute_instance_group_manager, don't also import separately |
| google_compute_instance_group_manager | compute.googleapis.com/InstanceGroupManager | `projects/{{project}}/zones/{{zone}}/instanceGroupManagers/{{name}}` | |
| google_compute_region_instance_group_manager | compute.googleapis.com/InstanceGroupManager | `{{name}}` | provider docs list only bare `{{name}}` (region/project resolved from provider config), unlike the zonal resource |
| google_compute_disk | compute.googleapis.com/Disk | `projects/{{project}}/zones/{{zone}}/disks/{{name}}` | |
| google_compute_region_disk | compute.googleapis.com/Disk | `projects/{{project}}/regions/{{region}}/disks/{{name}}` | |
| google_compute_image | compute.googleapis.com/Image | `projects/{{project}}/global/images/{{name}}` | |
| google_compute_snapshot | compute.googleapis.com/Snapshot | `projects/{{project}}/global/snapshots/{{name}}` | |
| google_compute_address | compute.googleapis.com/Address | `projects/{{project}}/regions/{{region}}/addresses/{{name}}` | |
| google_compute_global_address | compute.googleapis.com/GlobalAddress ? | `projects/{{project}}/global/addresses/{{name}}` | unconfirmed whether CAI unifies this with regional Address instead of a distinct type |
| google_compute_network | compute.googleapis.com/Network | `projects/{{project}}/global/networks/{{name}}` | |
| google_compute_subnetwork | compute.googleapis.com/Subnetwork | `projects/{{project}}/regions/{{region}}/subnetworks/{{name}}` | |
| google_compute_firewall | compute.googleapis.com/Firewall | `projects/{{project}}/global/firewalls/{{name}}` | |
| google_compute_route | compute.googleapis.com/Route | `projects/{{project}}/global/routes/{{name}}` | GCP-auto-created default/subnet routes (`default-route-*`) shouldn't be imported/managed |
| google_compute_router | compute.googleapis.com/Router | `projects/{{project}}/regions/{{region}}/routers/{{name}}` | |
| google_compute_router_nat | ? | `projects/{{project}}/regions/{{region}}/routers/{{router}}/{{name}}` | not CAI-enumerable — NAT config is a nested field on the parent Router asset, no separate asset type; sub-resource of google_compute_router |
| google_compute_vpn_gateway | compute.googleapis.com/TargetVpnGateway | `projects/{{project}}/regions/{{region}}/targetVpnGateways/{{name}}` | classic VPN, not HA VPN |
| google_compute_vpn_tunnel | compute.googleapis.com/VpnTunnel | `projects/{{project}}/regions/{{region}}/vpnTunnels/{{name}}` | |
| google_compute_ha_vpn_gateway | compute.googleapis.com/VpnGateway | `projects/{{project}}/regions/{{region}}/vpnGateways/{{name}}` | |
| google_compute_external_vpn_gateway | compute.googleapis.com/ExternalVpnGateway | `projects/{{project}}/global/externalVpnGateways/{{name}}` | |
| google_compute_network_peering | ? | `{{project_id}}/{{network_id}}/{{peering_id}}` | not CAI-enumerable — peering entries live in the parent Network asset's `peerings[]` field, no separate asset type |
| google_compute_network_peering_routes_config | ? | `projects/{{project}}/global/networks/{{network}}/networkPeerings/{{peering}}` | not CAI-enumerable; config sub-resource of a peering |
| google_compute_forwarding_rule | compute.googleapis.com/ForwardingRule | `projects/{{project}}/regions/{{region}}/forwardingRules/{{name}}` | |
| google_compute_global_forwarding_rule | compute.googleapis.com/GlobalForwardingRule | `projects/{{project}}/global/forwardingRules/{{name}}` | |
| google_compute_target_pool | compute.googleapis.com/TargetPool | `projects/{{project}}/regions/{{region}}/targetPools/{{name}}` | |
| google_compute_target_http_proxy | compute.googleapis.com/TargetHttpProxy | `projects/{{project}}/global/targetHttpProxies/{{name}}` | |
| google_compute_target_https_proxy | compute.googleapis.com/TargetHttpsProxy | `projects/{{project}}/global/targetHttpsProxies/{{name}}` | |
| google_compute_target_tcp_proxy | compute.googleapis.com/TargetTcpProxy | `projects/{{project}}/global/targetTcpProxies/{{name}}` | |
| google_compute_target_ssl_proxy | compute.googleapis.com/TargetSslProxy | `projects/{{project}}/global/targetSslProxies/{{name}}` | |
| google_compute_target_instance | compute.googleapis.com/TargetInstance | `projects/{{project}}/zones/{{zone}}/targetInstances/{{name}}` | |
| google_compute_target_grpc_proxy | compute.googleapis.com/TargetGrpcProxy | `projects/{{project}}/global/targetGrpcProxies/{{name}}` | |
| google_compute_backend_service | compute.googleapis.com/BackendService | `projects/{{project}}/global/backendServices/{{name}}` | |
| google_compute_region_backend_service | compute.googleapis.com/BackendService | `projects/{{project}}/regions/{{region}}/backendServices/{{name}}` | same CAI type as global variant; region field differentiates |
| google_compute_backend_bucket | compute.googleapis.com/BackendBucket | `projects/{{project}}/global/backendBuckets/{{name}}` | |
| google_compute_health_check | compute.googleapis.com/HealthCheck | `projects/{{project}}/global/healthChecks/{{name}}` | |
| google_compute_http_health_check | compute.googleapis.com/HttpHealthCheck | `projects/{{project}}/global/httpHealthChecks/{{name}}` | legacy health check type |
| google_compute_url_map | compute.googleapis.com/UrlMap | `projects/{{project}}/global/urlMaps/{{name}}` | |
| google_compute_ssl_certificate | compute.googleapis.com/SslCertificate | `projects/{{project}}/global/sslCertificates/{{name}}` | self-managed certs; same CAI type & import ID as google_compute_managed_ssl_certificate — must pick correct TF resource by the cert's `type` field |
| google_compute_managed_ssl_certificate | compute.googleapis.com/SslCertificate | `projects/{{project}}/global/sslCertificates/{{name}}` | Google-managed certs; see google_compute_ssl_certificate note |
| google_compute_ssl_policy | compute.googleapis.com/SslPolicy | `projects/{{project}}/global/sslPolicies/{{name}}` | |
| google_compute_network_endpoint_group | compute.googleapis.com/NetworkEndpointGroup | `projects/{{project}}/zones/{{zone}}/networkEndpointGroups/{{name}}` | container only; individual endpoints aren't imported |
| google_compute_region_network_endpoint_group | compute.googleapis.com/NetworkEndpointGroup ? | `projects/{{project}}/regions/{{region}}/networkEndpointGroups/{{name}}` | unconfirmed whether CAI unifies this with zonal/global NEG type |
| google_compute_global_network_endpoint_group | compute.googleapis.com/NetworkEndpointGroup ? | `projects/{{project}}/global/networkEndpointGroups/{{name}}` | unconfirmed CAI type unification |
| google_compute_security_policy | compute.googleapis.com/SecurityPolicy | `projects/{{project}}/global/securityPolicies/{{name}}` | Cloud Armor |
| google_compute_region_security_policy | compute.googleapis.com/SecurityPolicy ? | `projects/{{project}}/regions/{{region}}/securityPolicies/{{name}}` | unconfirmed CAI type unification with global |
| google_compute_packet_mirroring | compute.googleapis.com/PacketMirroring | `projects/{{project}}/regions/{{region}}/packetMirrorings/{{name}}` | |
| google_compute_reservation | compute.googleapis.com/Reservation | `projects/{{project}}/zones/{{zone}}/reservations/{{name}}` | |
| google_compute_node_group | compute.googleapis.com/NodeGroup | `projects/{{project}}/zones/{{zone}}/nodeGroups/{{name}}` | |
| google_compute_node_template | compute.googleapis.com/NodeTemplate | `projects/{{project}}/regions/{{region}}/nodeTemplates/{{name}}` | |
| google_compute_resource_policy | compute.googleapis.com/ResourcePolicy | `projects/{{project}}/regions/{{region}}/resourcePolicies/{{name}}` | |
| google_compute_service_attachment | compute.googleapis.com/ServiceAttachment | `projects/{{project}}/regions/{{region}}/serviceAttachments/{{name}}` | |
| google_storage_bucket | storage.googleapis.com/Bucket | `{{project_id}}/{{bucket}}` | also accepts bare `{{bucket}}` |
| google_storage_bucket_iam_policy / _binding / _member | storage.googleapis.com/Bucket | `b/{{bucket}}` | 3 distinct TF resources sharing one import format — policy (authoritative), binding (per-role), member (per-role+principal); IAM policy is attached to the Bucket asset, not its own CAI asset type |
| google_storage_bucket_object | not CAI-enumerable | no import | provider docs: "This resource does not support import"; objects aren't tracked as CAI assets |
| google_storage_bucket_acl | not CAI-enumerable | no import | provider docs: "This resource does not support import"; ACL is a sub-field of the Bucket asset |
| google_storage_default_object_acl | not CAI-enumerable | no import | provider docs: "This resource does not support import" |
| google_storage_hmac_key | ? | `projects/{{project}}/hmacKeys/{{access_id}}` | not confirmed as its own CAI asset type |
| google_storage_notification | ? | `{{bucket_name}}/notificationConfigs/{{id}}` | not confirmed as its own CAI asset type; sub-resource of Bucket |
| google_filestore_instance | file.googleapis.com/Instance | `projects/{{project}}/locations/{{location}}/instances/{{name}}` | CAI/API service prefix is `file.googleapis.com`, not `filestore.googleapis.com` |
| google_filestore_backup | file.googleapis.com/Backup | `projects/{{project}}/locations/{{location}}/backups/{{name}}` | |
| google_filestore_snapshot | file.googleapis.com/Snapshot | `projects/{{project}}/locations/{{location}}/instances/{{instance}}/snapshots/{{name}}` | basic-tier instances only; sub-resource of an instance |
| google_netapp_storage_pool | netapp.googleapis.com/StoragePool | `projects/{{project}}/locations/{{location}}/storagePools/{{name}}` | |
| google_netapp_volume | netapp.googleapis.com/Volume | `projects/{{project}}/locations/{{location}}/volumes/{{name}}` | |
| google_netapp_backup_vault | netapp.googleapis.com/BackupVault | `projects/{{project}}/locations/{{location}}/backupVaults/{{name}}` | |
| google_netapp_backup | netapp.googleapis.com/Backup | `projects/{{project}}/locations/{{location}}/backupVaults/{{vault_name}}/backups/{{name}}` | sub-resource of backup vault |
| google_netapp_active_directory | netapp.googleapis.com/ActiveDirectory | `projects/{{project}}/locations/{{location}}/activeDirectories/{{name}}` | |
| google_netapp_kmsconfig | netapp.googleapis.com/KmsConfig | `projects/{{project}}/locations/{{location}}/kmsConfigs/{{name}}` | |
| google_netapp_backup_policy | ? | `projects/{{project}}/locations/{{location}}/backupPolicies/{{name}}` | CAI enumerability unconfirmed |
| google_netapp_host_group | ? | `projects/{{project}}/locations/{{location}}/hostGroups/{{name}}` | newer resource; CAI enumerability unconfirmed |
| google_netapp_volume_quota_rule | ? | `projects/{{project}}/locations/{{location}}/volumes/{{volume_name}}/quotaRules/{{name}}` | sub-resource of Volume; likely not separately CAI-enumerable |
| google_netapp_volume_replication | ? | `projects/{{project}}/locations/{{location}}/volumes/{{volume_name}}/replications/{{name}}` | sub-resource of Volume; CAI enumerability unconfirmed |
| google_netapp_volume_snapshot | netapp.googleapis.com/Snapshot ? | `projects/{{project}}/locations/{{location}}/volumes/{{volume_name}}/snapshots/{{name}}` | sub-resource of Volume; CAI enumerability unconfirmed |
| google_vmwareengine_private_cloud | vmwareengine.googleapis.com/PrivateCloud | `projects/{{project}}/locations/{{location}}/privateClouds/{{name}}` | |
| google_vmwareengine_cluster | vmwareengine.googleapis.com/Cluster | `{{parent}}/clusters/{{name}}` | `{{parent}}` is the full private cloud resource name; sub-resource of private cloud |
| google_vmwareengine_network | vmwareengine.googleapis.com/VmwareEngineNetwork | `projects/{{project}}/locations/{{location}}/vmwareEngineNetworks/{{name}}` | |
| google_vmwareengine_network_peering | vmwareengine.googleapis.com/NetworkPeering | `projects/{{project}}/locations/global/networkPeerings/{{name}}` | |
| google_vmwareengine_network_policy | vmwareengine.googleapis.com/NetworkPolicy | `projects/{{project}}/locations/{{location}}/networkPolicies/{{name}}` | |
| google_vmwareengine_external_address | vmwareengine.googleapis.com/ExternalAddress | `{{parent}}/externalAddresses/{{name}}` | `{{parent}}` is the full private cloud resource name; sub-resource of private cloud |
| google_vmwareengine_external_access_rule | vmwareengine.googleapis.com/ExternalAccessRule | `{{parent}}/externalAccessRules/{{name}}` | `{{parent}}` is the full private cloud resource name; sub-resource of private cloud |
| google_vmwareengine_subnet | ? | `{{parent}}/subnets/{{name}}` | CAI enumerability unconfirmed; `{{parent}}` is the full private cloud resource name; sub-resource of private cloud |
