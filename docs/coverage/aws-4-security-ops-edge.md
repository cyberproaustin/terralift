# AWS coverage: security / identity / ops / edge

| Terraform type | Resource Explorer resourceType | Import ID format | Notes |
|---|---|---|---|
| aws_iam_role | iam:role | `role name` | |
| aws_iam_policy | iam:policy | `arn` | full-ARN import |
| aws_iam_user | iam:user | `user name` | |
| aws_iam_group | iam:group | `group name` | |
| aws_iam_instance_profile | iam:instance-profile | `instance profile name` | |
| aws_iam_role_policy | ? | `role_name:role_policy_name` | inline policy; sub-resource of role; not RE-enumerable |
| aws_iam_user_policy | ? | `user_name:policy_name` | inline policy; sub-resource of user; not RE-enumerable |
| aws_iam_group_policy | ? | `group_name:policy_name` | inline policy; sub-resource of group; not RE-enumerable |
| aws_iam_role_policy_attachment | ? | `role_name/policy_arn` | sub-resource of role; not RE-enumerable |
| aws_iam_user_policy_attachment | ? | `user_name/policy_arn` | sub-resource of user; not RE-enumerable |
| aws_iam_group_policy_attachment | ? | `group_name/policy_arn` | sub-resource of group; not RE-enumerable |
| aws_iam_openid_connect_provider | iam:oidc-provider | `arn` | full-ARN import |
| aws_iam_saml_provider | iam:saml-provider | `arn` | full-ARN import |
| aws_iam_account_password_policy | ? | `iam-account-password-policy` | fixed literal string; cloud-managed singleton; not RE-enumerable |
| aws_iam_service_linked_role | iam:role | `arn` | full-ARN import; still reported as `iam:role` by RE |
| aws_iam_access_key | ? | `user_name:access_key_id` | not RE-enumerable |
| aws_iam_signing_certificate | ? | `certificate_id:user_name` | not RE-enumerable |
| aws_iam_virtual_mfa_device | iam:mfa | `arn` | full-ARN import |
| aws_organizations_organization | ? | `organization id` | cloud-managed singleton (one per account); not RE-enumerable (no `organizations:` RE type exists) |
| aws_organizations_account | ? | `account id` | not RE-enumerable |
| aws_organizations_organizational_unit | ? | `ou id` | not RE-enumerable |
| aws_organizations_policy | ? | `policy id` | not RE-enumerable |
| aws_organizations_policy_attachment | ? | `target_id:policy_id` | composite id; not RE-enumerable |
| aws_kms_key | kms:key | `key id` | |
| aws_kms_alias | ? | `alias/name` | sub-resource of key; not RE-enumerable |
| aws_kms_grant | ? | `key_id:grant_id` | sub-resource of key; not RE-enumerable |
| aws_kms_replica_key | kms:key | `key id` | shares `kms:key` RE type with plain keys |
| aws_kms_external_key | kms:key | `key id` | shares `kms:key` RE type with plain keys |
| aws_secretsmanager_secret | secretsmanager:secret | `arn` | full-ARN import |
| aws_secretsmanager_secret_policy | ? | `secret id (arn or name)` | sub-resource of secret; not RE-enumerable |
| aws_secretsmanager_secret_rotation | ? | `secret id (arn or name)` | sub-resource of secret; not RE-enumerable |
| aws_acm_certificate | acm:certificate | `arn` | full-ARN import |
| aws_acmpca_certificate_authority | acm-pca:certificate-authority | `arn` | full-ARN import |
| aws_acmpca_certificate | ? | `certificate_authority_arn,certificate_arn` | sub-resource; not RE-enumerable |
| aws_acmpca_permission | ? | `ca_arn,principal,source_account` | composite id; sub-resource; no update, create/delete only in some versions; not RE-enumerable |
| aws_wafv2_web_acl | wafv2:webacl | `ID/Name/Scope` | composite, slash-joined |
| aws_wafv2_ip_set | wafv2:ipset | `ID/Name/Scope` | composite, slash-joined |
| aws_wafv2_regex_pattern_set | wafv2:regexpatternset | `ID/Name/Scope` | composite, slash-joined |
| aws_wafv2_rule_group | wafv2:rulegroup | `ID/Name/Scope` | composite, slash-joined |
| aws_waf_web_acl | ? | `id` | WAF Classic; not RE-enumerable (only WAFV2 is in RE) |
| aws_waf_ip_set | ? | `id` | WAF Classic; not RE-enumerable |
| aws_waf_rule | ? | `id` | WAF Classic; not RE-enumerable |
| aws_wafregional_web_acl | ? | `id` | WAF Classic Regional; not RE-enumerable |
| aws_wafregional_ip_set | ? | `id` | WAF Classic Regional; not RE-enumerable |
| aws_wafregional_rule | ? | `id` | WAF Classic Regional; not RE-enumerable |
| aws_shield_protection | shield:protection | `id` | |
| aws_shield_protection_group | shield:protection-group | `id` | |
| aws_guardduty_detector | guardduty:detector | `detector id` | |
| aws_guardduty_filter | guardduty:detector/filter | `detector_id:filter_name` | |
| aws_guardduty_ipset | guardduty:detector/ipset | `detector_id:ipset_id` | |
| aws_guardduty_threatintelset | guardduty:detector/threatintelset | `detector_id:threat_intel_set_id` | |
| aws_guardduty_publishing_destination | guardduty:detector/publishingDestination | `detector_id:publishing_destination_id` | |
| aws_guardduty_malware_protection_plan | guardduty:malware-protection-plan | `id` | |
| aws_guardduty_organization_admin_account | ? | `admin_account_id` | account-level; not RE-enumerable |
| aws_guardduty_member | ? | `detector_id:account_id` | not RE-enumerable |
| aws_securityhub_account | ? | `account_id` | cloud-managed singleton (per account/region); not RE-enumerable (SecurityHub absent from RE) |
| aws_securityhub_standards_subscription | ? | `arn` | full-ARN import; not RE-enumerable |
| aws_securityhub_member | ? | `account_id` | not RE-enumerable |
| aws_securityhub_organization_admin_account | ? | `admin_account_id` | not RE-enumerable |
| aws_inspector2_filter | inspector2:filter | `arn` | full-ARN import |
| aws_inspector2_enabler | ? | none | account/resource-type enabler, singleton-ish; not RE-enumerable; import not supported |
| aws_inspector2_organization_configuration | ? | `account_id` | singleton; not RE-enumerable |
| aws_macie2_account | ? | `id` | cloud-managed singleton (per account/region); not RE-enumerable |
| aws_macie2_allow_list | macie2:allow-list | `id` | |
| aws_macie2_custom_data_identifier | macie2:custom-data-identifier | `id` | |
| aws_macie2_findings_filter | macie2:findings-filter | `id` | |
| aws_macie2_member | macie2:member | `account_id` | |
| aws_macie2_organization_admin_account | ? | `id` | not RE-enumerable |
| aws_detective_graph | detective:graph | `arn` | full-ARN import |
| aws_detective_member | ? | `graph_arn/account_id` | composite; not RE-enumerable |
| aws_detective_invitation_accepter | ? | `graph_arn` | not RE-enumerable |
| aws_detective_organization_admin_account | ? | `account_id` | not RE-enumerable |
| aws_accessanalyzer_analyzer | access-analyzer:analyzer | `analyzer name` | |
| aws_accessanalyzer_archive_rule | ? | `analyzer_name/rule_name` | sub-resource; not RE-enumerable |
| aws_cloudfront_distribution | cloudfront:distribution | `id` | |
| aws_cloudfront_cache_policy | cloudfront:cache-policy | `id` | |
| aws_cloudfront_origin_request_policy | cloudfront:origin-request-policy | `id` | |
| aws_cloudfront_response_headers_policy | cloudfront:response-headers-policy | `id` | |
| aws_cloudfront_function | cloudfront:function | `name` | |
| aws_cloudfront_key_group | ? | `id` | not RE-enumerable |
| aws_cloudfront_public_key | ? | `id` | not RE-enumerable |
| aws_cloudfront_origin_access_control | cloudfront:origin-access-control | `id` | |
| aws_cloudfront_origin_access_identity | cloudfront:origin-access-identity | `id` | |
| aws_cloudfront_field_level_encryption_config | cloudfront:field-level-encryption-config | `id` | |
| aws_cloudfront_field_level_encryption_profile | cloudfront:field-level-encryption-profile | `id` | |
| aws_route53_zone | route53:hostedzone | `zone id` | |
| aws_route53_record | ? | `zone_id_recordname_type` | underscore-joined composite; sub-resource of zone; not RE-enumerable |
| aws_route53_health_check | route53:healthcheck | `id` | |
| aws_route53_resolver_endpoint | route53resolver:resolver-endpoint | `id` | |
| aws_route53_resolver_rule | route53resolver:resolver-rule | `id` | |
| aws_route53_resolver_rule_association | ? | `id` | sub-resource; not RE-enumerable |
| aws_route53_resolver_query_log_config | route53resolver:resolver-query-log-config | `id` | |
| aws_route53_resolver_query_log_config_association | ? | `id` | sub-resource; not RE-enumerable |
| aws_route53_delegation_set | ? | `id` | not RE-enumerable |
| aws_route53_traffic_policy | ? | `id/version` | composite; not RE-enumerable |
| aws_route53domains_registered_domain | route53:domain | `domain name` | |
| aws_globalaccelerator_accelerator | globalaccelerator:accelerator | `arn` | full-ARN import |
| aws_globalaccelerator_listener | globalaccelerator:accelerator/listener | `arn` | full-ARN import |
| aws_globalaccelerator_endpoint_group | globalaccelerator:accelerator/listener/endpoint-group | `arn` | full-ARN import |
| aws_cloudwatch_metric_alarm | cloudwatch:alarm | `alarm name` | shares `cloudwatch:alarm` RE type with composite alarms |
| aws_cloudwatch_composite_alarm | cloudwatch:alarm | `alarm name` | shares `cloudwatch:alarm` RE type with metric alarms |
| aws_cloudwatch_dashboard | cloudwatch:dashboard | `dashboard name` | |
| aws_cloudwatch_log_group | logs:log-group | `log group name` | |
| aws_cloudwatch_log_stream | ? | `log_group_name:log_stream_name` | sub-resource; not RE-enumerable |
| aws_cloudwatch_log_metric_filter | ? | `log_group_name:filter_name` | sub-resource; not RE-enumerable |
| aws_cloudwatch_log_subscription_filter | ? | `log_group_name|name` | pipe-joined composite; sub-resource; not RE-enumerable |
| aws_cloudwatch_log_resource_policy | ? | `policy name` | account-level; not RE-enumerable |
| aws_ssm_parameter | ssm:parameter | `/param/path` | |
| aws_ssm_document | ssm:document | `name` | |
| aws_ssm_association | ssm:association | `association id` | |
| aws_ssm_maintenance_window | ssm:maintenancewindow | `id` | |
| aws_ssm_patch_baseline | ? | `id` | RE support for `ssm:patchbaseline` was removed 2024-07-09; not RE-enumerable |
| aws_ssm_resource_data_sync | ssm:resource-data-sync | `name` | |
| aws_cloudtrail | cloudtrail:trail | `arn` | |
| aws_cloudtrail_event_data_store | cloudtrail:eventdatastore | `arn` | |
| aws_config_config_rule | config:config-rule | `name` | |
| aws_config_configuration_recorder | ? | `name` | not RE-enumerable |
| aws_config_delivery_channel | ? | `name` | not RE-enumerable |
| aws_config_configuration_aggregator | ? | `name` | not RE-enumerable |
| aws_config_conformance_pack | ? | `name` | not RE-enumerable |
| aws_cognito_user_pool | cognito-idp:userpool | `id` | |
| aws_cognito_user_pool_client | ? | `user_pool_id/client_id` | sub-resource; not RE-enumerable |
| aws_cognito_identity_pool | cognito-identity:identitypool | `id` | |
| aws_cognito_user_group | ? | `user_pool_id/group_name` | sub-resource; not RE-enumerable |
| aws_cognito_resource_server | ? | `user_pool_id|identifier` | pipe-joined composite; sub-resource; not RE-enumerable |
| aws_cognito_user_pool_domain | ? | `domain` | sub-resource; not RE-enumerable |
| aws_ses_domain_identity | ses:identity | `domain` | |
| aws_sesv2_email_identity | ses:identity | `email_identity name` | shares `ses:identity` RE type with v1 domain identity |
| aws_ses_configuration_set | ses:configuration-set | `confset name` | |
| aws_sesv2_configuration_set | ses:configuration-set | `configuration_set_name` | shares `ses:configuration-set` RE type with v1 |
| aws_ses_receipt_rule | ? | `rule_set_name:rule_name` | sub-resource of receipt rule set (also not RE-enumerable); not RE-enumerable |
| aws_sns_platform_application | ? | `arn` | not RE-enumerable (RE only covers `sns:topic`) |
| aws_xray_sampling_rule | xray:sampling-rule | `rule name` | AWS provisions a built-in cloud-managed "Default" rule in every account/region — appears in enumeration but is not user-created |
| aws_xray_group | ? | `arn` | not RE-enumerable (RE only covers `xray:sampling-rule`) |
| aws_servicecatalogappregistry_application | servicecatalog:applications | `id` | AppRegistry, not classic Service Catalog |
| aws_servicecatalogappregistry_attribute_group | servicecatalog:attribute-groups | `id` | AppRegistry, not classic Service Catalog |
| aws_servicecatalog_portfolio | ? | `id` | classic Service Catalog; not RE-enumerable (RE's `servicecatalog:*` types are AppRegistry only) |
| aws_servicecatalog_product | ? | `product id` | classic Service Catalog; not RE-enumerable |
| aws_servicecatalog_constraint | ? | `id` | classic Service Catalog; not RE-enumerable |
| aws_resourcegroups_group | resource-groups:group | `name` | |
| aws_resourceexplorer2_index | resource-explorer-2:index | `arn` | full-ARN import |
| aws_resourceexplorer2_view | resource-explorer-2:view | `arn` | full-ARN import |
