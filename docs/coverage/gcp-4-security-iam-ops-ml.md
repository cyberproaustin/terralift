# GCP coverage: security / iam / ops / ml

| Terraform type | CAI asset type | Import ID format | Notes |
|---|---|---|---|
| google_service_account | iam.googleapis.com/ServiceAccount | `projects/{{project_id}}/serviceAccounts/{{email}}` | |
| google_service_account_key | iam.googleapis.com/ServiceAccountKey | n/a | no import — "This resource does not support import"; CAI-enumerable but provider blocks import of key material |
| google_project_iam_custom_role | iam.googleapis.com/Role | `projects/{{project}}/roles/{{role_id}}` | also accepts `{{project}}/{{role_id}}`, `{{role_id}}` |
| google_organization_iam_custom_role | iam.googleapis.com/Role | `organizations/{{org_id}}/roles/{{role_id}}` | same CAI type as project custom role; no folder-level equivalent exists — `google_folder_iam_custom_role` doesn't exist because GCP custom roles can only be defined at org or project level |
| google_iam_workload_identity_pool | iam.googleapis.com/WorkloadIdentityPool | `projects/{{project}}/locations/global/workloadIdentityPools/{{workload_identity_pool_id}}` | also accepts `{{project}}/{{workload_identity_pool_id}}`, `{{workload_identity_pool_id}}` |
| google_iam_workload_identity_pool_provider | iam.googleapis.com/WorkloadIdentityPoolProvider | `projects/{{project}}/locations/global/workloadIdentityPools/{{workload_identity_pool_id}}/providers/{{workload_identity_pool_provider_id}}` | also accepts shorter forms; sub-resource of pool |
| google_iam_workforce_pool | iam.googleapis.com/WorkforcePool | `locations/{{location}}/workforcePools/{{workforce_pool_id}}` | also accepts `{{location}}/{{workforce_pool_id}}`; org-level resource, no project in the ID |
| google_iam_workforce_pool_provider | iam.googleapis.com/WorkforcePoolProvider | `locations/{{location}}/workforcePools/{{workforce_pool_id}}/providers/{{provider_id}}` | also accepts shorter form; sub-resource of workforce pool |
| google_project_iam_member / _binding / _policy | ? | member/binding: `"{{project_id}} roles/{{role}} {{member}}"` (space-delimited); policy: `{{project_id}}` | iam member/binding/policy triad (+ `_audit_config`); not a distinct CAI asset type, attaches to the Project asset; `_policy` is authoritative and incompatible with member/binding on the same project |
| google_folder_iam_member / _binding / _policy | ? | member/binding: `"folders/{{folder_id}} roles/{{role}} {{member}}"`; policy: `folders/{{folder_id}}` | iam triad, same pattern as project; attaches to Folder asset |
| google_organization_iam_member / _binding / _policy | ? | member/binding: `"{{org_id}} roles/{{role}} {{member}}"`; policy: `{{org_id}}` | iam triad, same pattern as project; attaches to Organization asset |
| google_service_account_iam_member / _binding / _policy | ? | member/binding: `"projects/{{project_id}}/serviceAccounts/{{email}} roles/{{role}} {{member}}"`; policy: same minus role/member | iam triad; attaches to ServiceAccount asset |
| google_project | cloudresourcemanager.googleapis.com/Project | `{{project_id}}` | |
| google_folder | cloudresourcemanager.googleapis.com/Folder | `folders/{{folder_id}}` | also accepts bare `{{folder_id}}` |
| google_org_policy_policy | orgpolicy.googleapis.com/Policy | `{{parent}}/policies/{{name}}` | unified resource for project/folder/org — `parent` = `projects/x`, `folders/x`, or `organizations/x`; supersedes the removed legacy `google_project_organization_policy` / `google_folder_organization_policy` / `google_organization_policy` (404 in current provider) |
| google_org_policy_custom_constraint | orgpolicy.googleapis.com/CustomConstraint | `{{parent}}/customConstraints/{{name}}` | |
| google_project_service | serviceusage.googleapis.com/Service | `{{project_id}}/{{service}}` | `terraform apply` is idempotent even without import — enabling an already-enabled service succeeds |
| google_resource_manager_lien | cloudresourcemanager.googleapis.com/Lien | `{{parent}}/{{name}}` | CAI docs flag Lien as "Not available in the analysis, export, list, and monitor APIs" — effectively not CAI-enumerable despite having a nominal asset type |
| google_tags_tag_key | cloudresourcemanager.googleapis.com/TagKey | `tagKeys/{{name}}` | also accepts bare `{{name}}` (numeric tag key id) |
| google_tags_tag_value | cloudresourcemanager.googleapis.com/TagValue | `tagValues/{{name}}` | also accepts bare `{{name}}` |
| google_tags_tag_binding | cloudresourcemanager.googleapis.com/TagBinding | `tagBindings/{{name}}` | also accepts bare `{{name}}`; `name` embeds parent resource + tag value |
| google_kms_key_ring | cloudkms.googleapis.com/KeyRing | `projects/{{project}}/locations/{{location}}/keyRings/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}`; key rings can't be destroyed via API (no-op on `terraform destroy`) |
| google_kms_crypto_key | cloudkms.googleapis.com/CryptoKey | `{{key_ring}}/cryptoKeys/{{name}}` | also accepts `{{key_ring}}/{{name}}`; `key_ring` is itself a full resource path |
| google_kms_crypto_key_version | cloudkms.googleapis.com/CryptoKeyVersion | `{{name}}` | `name` is the full resource path; mainly used for imported/external raw key material versions |
| google_kms_key_ring_iam_member / _binding / _policy | ? | `"{{project_id}}/{{location}}/{{key_ring_name}} roles/{{role}} {{member}}"` (member/binding; policy drops role+member) | iam triad; attaches to KeyRing asset |
| google_kms_crypto_key_iam_member / _binding / _policy | ? | `"{{project_id}}/{{location}}/{{key_ring_name}}/{{crypto_key_name}} roles/{{role}} {{member}}"` (member/binding; policy drops role+member) | iam triad; attaches to CryptoKey asset |
| google_kms_ekm_connection | cloudkms.googleapis.com/EkmConnection | `projects/{{project}}/locations/{{location}}/ekmConnections/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_secret_manager_secret | secretmanager.googleapis.com/Secret | `projects/{{project}}/secrets/{{secret_id}}` | also accepts `{{project}}/{{secret_id}}`, `{{secret_id}}` |
| google_secret_manager_secret_version | secretmanager.googleapis.com/SecretVersion | `projects/{{project}}/secrets/{{secret_id}}/versions/{{version}}` | **holds the data-plane secret VALUE** — imported state includes plaintext `secret_data`; redact/exclude from generated HCL and treat state as sensitive |
| google_secret_manager_secret_iam_member / _binding / _policy | ? | `"projects/{{project}}/secrets/{{secret_id}} roles/{{role}} {{member}}"` (member/binding; policy drops role+member) | iam triad; attaches to Secret asset |
| google_certificate_manager_certificate | certificatemanager.googleapis.com/Certificate | `projects/{{project}}/locations/{{location}}/certificates/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_certificate_manager_certificate_map | certificatemanager.googleapis.com/CertificateMap | `projects/{{project}}/locations/global/certificateMaps/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_certificate_manager_certificate_map_entry | certificatemanager.googleapis.com/CertificateMapEntry | `projects/{{project}}/locations/global/certificateMaps/{{map}}/certificateMapEntries/{{name}}` | also accepts `{{project}}/{{map}}/{{name}}`, `{{map}}/{{name}}`; sub-resource of map |
| google_certificate_manager_dns_authorization | certificatemanager.googleapis.com/DnsAuthorization | `projects/{{project}}/locations/{{location}}/dnsAuthorizations/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_certificate_manager_trust_config | certificatemanager.googleapis.com/TrustConfig | `projects/{{project}}/locations/{{location}}/trustConfigs/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_dns_managed_zone | dns.googleapis.com/ManagedZone | `projects/{{project}}/managedZones/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_dns_record_set | dns.googleapis.com/ResourceRecordSet | `projects/{{project}}/managedZones/{{zone}}/rrsets/{{name}}/{{type}}` | also accepts `{{project}}/{{zone}}/{{name}}/{{type}}`, `{{zone}}/{{name}}/{{type}}`; record `name` must include the trailing dot |
| google_dns_policy | dns.googleapis.com/Policy | `projects/{{project}}/policies/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_dns_response_policy | dns.googleapis.com/ResponsePolicy | `projects/{{project}}/responsePolicies/{{response_policy_name}}` | also accepts `{{project}}/{{response_policy_name}}`, `{{response_policy_name}}` |
| google_dns_response_policy_rule | dns.googleapis.com/ResponsePolicyRule | `projects/{{project}}/responsePolicies/{{response_policy}}/rules/{{rule_name}}` | also accepts shorter forms; sub-resource of response policy |
| google_logging_metric | logging.googleapis.com/LogMetric | `{{project}} {{name}}` | space-delimited id needs quoting; also accepts bare `{{name}}` |
| google_logging_project_sink | logging.googleapis.com/LogSink | `projects/{{project_id}}/sinks/{{name}}` | single `LogSink` CAI type covers project/folder/org/billing-account sinks |
| google_logging_folder_sink | logging.googleapis.com/LogSink | `folders/{{folder_id}}/sinks/{{name}}` | same CAI type as project sink |
| google_logging_organization_sink | logging.googleapis.com/LogSink | `organizations/{{organization_id}}/sinks/{{sink_id}}` | same CAI type as project sink |
| google_logging_billing_account_sink | logging.googleapis.com/LogSink | `billingAccounts/{{billing_account_id}}/sinks/{{sink_id}}` | same CAI type as project sink |
| google_logging_log_view | logging.googleapis.com/LogView | `{{parent}}/locations/{{location}}/buckets/{{bucket}}/views/{{name}}` | sub-resource of log bucket |
| google_logging_project_bucket_config | logging.googleapis.com/LogBucket | `projects/{{project}}/locations/{{location}}/buckets/{{bucket_id}}` | |
| google_logging_folder_bucket_config | logging.googleapis.com/LogBucket | `folders/{{folder}}/locations/{{location}}/buckets/{{bucket_id}}` | same CAI type as project bucket config |
| google_logging_organization_bucket_config | logging.googleapis.com/LogBucket | `organizations/{{organization}}/locations/{{location}}/buckets/{{bucket_id}}` | same CAI type as project bucket config |
| google_logging_billing_account_bucket_config | logging.googleapis.com/LogBucket | `billingAccounts/{{billingAccount}}/locations/{{location}}/buckets/{{bucket_id}}` | same CAI type as project bucket config |
| google_monitoring_alert_policy | monitoring.googleapis.com/AlertPolicy | `projects/{{project}}/alertPolicies/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_monitoring_notification_channel | monitoring.googleapis.com/NotificationChannel | `{{name}}` | `name` embeds full resource path |
| google_monitoring_uptime_check_config | monitoring.googleapis.com/UptimeCheckConfig | `{{project}}/{{name}}` | also accepts `"{{project}} {{name}}"`, `{{name}}` |
| google_monitoring_dashboard | monitoring.googleapis.com/Dashboard | `projects/{{project}}/dashboards/{{dashboard_id}}` | also accepts bare `{{dashboard_id}}` |
| google_monitoring_service | ? | `projects/{{project}}/services/{{service_id}}` | not CAI-enumerable — no `monitoring.googleapis.com/Service` type found; underlying TF schema type is "GenericService" |
| google_monitoring_slo | ? | `{{project}}/{{name}}` | not CAI-enumerable; also accepts `"{{project}} {{name}}"`, `{{name}}`; sub-resource of a monitoring service |
| google_monitoring_monitored_project | ? | `v1/locations/global/metricsScopes/{{name}}` | not CAI-enumerable; also accepts bare `{{name}}` |
| google_monitoring_group | ? | `{{project}}/{{name}}` | not CAI-enumerable; also accepts `"{{project}} {{name}}"`, `{{name}}` |
| n/a (Cloud Trace) | n/a | n/a | Cloud Trace has no CAI asset type and no hashicorp/google Terraform resource at all — trace spans are write/query-only via API, nothing to import |
| n/a (Error Reporting) | n/a | n/a | Error Reporting has no CAI asset type and no hashicorp/google Terraform resource — errors are auto-aggregated from logs, no config object exists |
| google_vertex_ai_dataset | aiplatform.googleapis.com/Dataset | n/a | no import — "This resource does not support import" despite being CAI-enumerable |
| google_vertex_ai_endpoint | aiplatform.googleapis.com/Endpoint | `projects/{{project}}/locations/{{location}}/endpoints/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_vertex_ai_featurestore | aiplatform.googleapis.com/Featurestore | `projects/{{project}}/locations/{{region}}/featurestores/{{name}}` | also accepts shorter forms incl. bare `{{name}}` |
| google_vertex_ai_featurestore_entitytype | ? | `{{featurestore}}/entityTypes/{{name}}` | not CAI-enumerable as a distinct type; sub-resource of featurestore |
| google_vertex_ai_index | aiplatform.googleapis.com/Index | `projects/{{project}}/locations/{{region}}/indexes/{{name}}` | also accepts shorter forms incl. bare `{{name}}` |
| google_vertex_ai_index_endpoint | aiplatform.googleapis.com/IndexEndpoint | `projects/{{project}}/locations/{{region}}/indexEndpoints/{{name}}` | also accepts shorter forms incl. bare `{{name}}` |
| google_vertex_ai_tensorboard | aiplatform.googleapis.com/Tensorboard | `projects/{{project}}/locations/{{region}}/tensorboards/{{name}}` | also accepts shorter forms incl. bare `{{name}}` |
| google_vertex_ai_metadata_store | aiplatform.googleapis.com/MetadataStore | `projects/{{project}}/locations/{{region}}/metadataStores/{{name}}` | also accepts shorter forms incl. bare `{{name}}` |
| google_vertex_ai_feature_group | aiplatform.googleapis.com/FeatureGroup | `projects/{{project}}/locations/{{region}}/featureGroups/{{name}}` | also accepts shorter forms incl. bare `{{name}}` |
| google_vertex_ai_feature_online_store | aiplatform.googleapis.com/FeatureOnlineStore | `projects/{{project}}/locations/{{region}}/featureOnlineStores/{{name}}` | also accepts shorter forms incl. bare `{{name}}` |
| n/a (Vertex AI Model) | aiplatform.googleapis.com/Model | n/a | CAI-enumerable, but no `google_vertex_ai_model` (or equivalent) Terraform resource exists — models come from training jobs/imports, not directly creatable/importable via this provider |
| google_notebooks_instance | notebooks.googleapis.com/Instance | `projects/{{project}}/locations/{{location}}/instances/{{name}}` | also accepts shorter forms; legacy Notebooks API — shares the same CAI type as Workbench |
| google_notebooks_runtime | ? | `projects/{{project}}/locations/{{location}}/runtimes/{{name}}` | not CAI-enumerable — no `notebooks.googleapis.com/Runtime` type found (only `Instance`) |
| google_workbench_instance | notebooks.googleapis.com/Instance | `projects/{{project}}/locations/{{location}}/instances/{{name}}` | shares the identical CAI asset type with `google_notebooks_instance` (same underlying API, newer resource schema) — de-dupe carefully when mapping CAI asset → TF type |
| google_dialogflow_agent | ? | `{{project}}` | not CAI-enumerable — ES doesn't get its own asset type; only Dialogflow CX has `dialogflow.googleapis.com/Agent` in supported-asset-types |
| google_dialogflow_intent | ? | `{{name}}` | not CAI-enumerable — no Intent asset type for ES or CX |
| google_dialogflow_entity_type | ? | `{{name}}` | not CAI-enumerable — no EntityType asset type for ES or CX |
| google_dialogflow_cx_agent | dialogflow.googleapis.com/Agent | `projects/{{project}}/locations/{{location}}/agents/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_dialogflow_cx_flow | ? | `{{parent}}/flows/{{name}}` | not CAI-enumerable; also accepts `{{parent}}/{{name}}`; sub-resource of CX agent |
| google_dialogflow_cx_intent | ? | `{{parent}}/intents/{{name}}` | not CAI-enumerable; also accepts `{{parent}}/{{name}}`; sub-resource of CX agent |
| google_dialogflow_cx_entity_type | ? | `{{parent}}/entityTypes/{{name}}` | not CAI-enumerable; also accepts `{{parent}}/{{name}}`; sub-resource of CX agent |
| google_document_ai_processor | documentai.googleapis.com/Processor | `projects/{{project}}/locations/{{location}}/processors/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_document_ai_processor_default_version | ? | `{{processor}}` | not CAI-enumerable; sub-resource — sets a default-version pointer, not a separate object |
| google_healthcare_dataset | healthcare.googleapis.com/Dataset | `projects/{{project}}/locations/{{location}}/datasets/{{name}}` | also accepts `{{project}}/{{location}}/{{name}}`, `{{location}}/{{name}}` |
| google_healthcare_fhir_store | healthcare.googleapis.com/FhirStore | `{{dataset}}/fhirStores/{{name}}` | also accepts `{{dataset}}/{{name}}`; sub-resource of dataset |
| google_healthcare_hl7_v2_store | healthcare.googleapis.com/Hl7V2Store | `{{dataset}}/hl7V2Stores/{{name}}` | also accepts `{{dataset}}/{{name}}`; sub-resource of dataset |
| google_healthcare_dicom_store | healthcare.googleapis.com/DicomStore | `{{dataset}}/dicomStores/{{name}}` | also accepts `{{dataset}}/{{name}}`; sub-resource of dataset |
| google_healthcare_consent_store | healthcare.googleapis.com/ConsentStore | `{{dataset}}/consentStores/{{name}}` | sub-resource of dataset |
| google_recaptcha_enterprise_key | recaptchaenterprise.googleapis.com/Key | `projects/{{project}}/keys/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_binary_authorization_policy | binaryauthorization.googleapis.com/Policy | `projects/{{project}}` | also accepts bare `{{project}}`; singleton per project |
| google_binary_authorization_attestor | binaryauthorization.googleapis.com/Attestor | `projects/{{project}}/attestors/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_access_context_manager_access_policy | identity.accesscontextmanager.googleapis.com/AccessPolicy | `{{name}}` | CAI asset type uses the unusual `identity.accesscontextmanager.googleapis.com` host prefix, not plain `accesscontextmanager.googleapis.com` (that prefix is used only by the unrelated `AuthorizedOrgsDesc` type) |
| google_access_context_manager_access_level | identity.accesscontextmanager.googleapis.com/AccessLevel | `{{name}}` | `name` embeds full path `accessPolicies/{{policy}}/accessLevels/{{level}}`; same unusual CAI host prefix as AccessPolicy |
| google_access_context_manager_service_perimeter | identity.accesscontextmanager.googleapis.com/ServicePerimeter | `{{name}}` | this is the VPC Service Controls perimeter object; same unusual CAI host prefix as AccessPolicy |
| google_identity_platform_config | identitytoolkit.googleapis.com/Config | `projects/{{project}}/config` | also accepts `projects/{{project}}`, `{{project}}`; project-level singleton |
| google_identity_platform_default_supported_idp_config | identitytoolkit.googleapis.com/DefaultSupportedIdpConfig | `projects/{{project}}/defaultSupportedIdpConfigs/{{idp_id}}` | also accepts `{{project}}/{{idp_id}}`, `{{idp_id}}` |
| google_identity_platform_tenant | identitytoolkit.googleapis.com/Tenant | `projects/{{project}}/tenants/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_identity_platform_tenant_default_supported_idp_config | ? | `projects/{{project}}/tenants/{{tenant}}/defaultSupportedIdpConfigs/{{idp_id}}` | not CAI-enumerable as a distinct type (only the project-level DefaultSupportedIdpConfig type was found); sub-resource of tenant |
| google_identity_platform_inbound_saml_config | identitytoolkit.googleapis.com/InboundSamlConfig | `projects/{{project}}/inboundSamlConfigs/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_identity_platform_oauth_idp_config | identitytoolkit.googleapis.com/OauthIdpConfig | `projects/{{project}}/oauthIdpConfigs/{{name}}` | also accepts `{{project}}/{{name}}`, `{{name}}` |
| google_identity_platform_tenant_inbound_saml_config | ? | `projects/{{project}}/tenants/{{tenant}}/inboundSamlConfigs/{{name}}` | not CAI-enumerable as a distinct type; sub-resource of tenant |
| google_identity_platform_tenant_oauth_idp_config | ? | `projects/{{project}}/tenants/{{tenant}}/oauthIdpConfigs/{{name}}` | not CAI-enumerable as a distinct type; sub-resource of tenant |
| google_cloud_identity_group | ? | `{{name}}` | not CAI-enumerable — no `cloudidentity.googleapis.com/*` asset type appears anywhere in the supported-asset-types doc |
| google_cloud_identity_group_membership | ? | `{{name}}` | not CAI-enumerable, same reason as group |
| google_assured_workloads_workload | assuredworkloads.googleapis.com/Workload | `organizations/{{organization}}/locations/{{location}}/workloads/{{name}}` | also accepts `{{organization}}/{{location}}/{{name}}` |
| google_essential_contacts_contact | essentialcontacts.googleapis.com/Contact | `{{name}}` | `name` embeds the full parent path (project/folder/org) |
