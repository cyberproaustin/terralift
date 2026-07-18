# azure-mega -- brownfield seed manifest

Purpose: a "before" Terraform environment, applied directly against the
Eutaxia subscription (`81106197-4fec-452c-8cef-69328e602e8a`), that TerraLift
will later onboard into its own Terraform. Everything lives in two resource
groups so teardown is trivially scoped:

- `tlmega-core-<suffix>` -- networking, Key Vault, observability, IAM
- `tlmega-apps-<suffix>` -- compute, app hosting, data, storage

`<suffix>` is a random 6-character string generated once (`random_string.suffix`)
and reused for every globally-unique name (storage accounts, Key Vault, ACR,
Cosmos DB, SQL server, Web App, Function App, Service Bus/Event Hub
namespaces, public DNS zone).

## Resource type inventory (53 distinct `azurerm_*` types)

| # | Resource type | File | Count |
|---|---|---|---|
| 1 | azurerm_resource_group | network.tf | 2 |
| 2 | azurerm_virtual_network | network.tf | 2 |
| 3 | azurerm_subnet | network.tf | 6 |
| 4 | azurerm_network_security_group | network.tf | 2 |
| 5 | azurerm_network_security_rule | network.tf | 2 |
| 6 | azurerm_subnet_network_security_group_association | network.tf | 2 |
| 7 | azurerm_route_table | network.tf | 1 |
| 8 | azurerm_subnet_route_table_association | network.tf | 1 |
| 9 | azurerm_virtual_network_peering | network.tf | 2 |
| 10 | azurerm_dns_zone | network.tf | 1 |
| 11 | azurerm_dns_a_record | network.tf | 1 |
| 12 | azurerm_dns_cname_record | network.tf | 1 |
| 13 | azurerm_private_dns_zone | network.tf | 2 |
| 14 | azurerm_private_dns_zone_virtual_network_link | network.tf | 2 |
| 15 | azurerm_private_endpoint | network.tf | 1 |
| 16 | azurerm_public_ip | network.tf | 2 |
| 17 | azurerm_lb | network.tf | 1 |
| 18 | azurerm_lb_backend_address_pool | network.tf | 1 |
| 19 | azurerm_lb_probe | network.tf | 1 |
| 20 | azurerm_lb_rule | network.tf | 1 |
| 21 | azurerm_network_interface | compute.tf | 1 |
| 22 | azurerm_network_interface_backend_address_pool_association | compute.tf | 1 |
| 23 | azurerm_linux_virtual_machine | compute.tf | 1 |
| 24 | azurerm_managed_disk | compute.tf | 1 |
| 25 | azurerm_virtual_machine_data_disk_attachment | compute.tf | 1 |
| 26 | azurerm_container_registry | compute.tf | 1 |
| 27 | azurerm_container_group | compute.tf | 1 |
| 28 | azurerm_service_plan | app.tf | 2 |
| 29 | azurerm_linux_web_app | app.tf | 1 |
| 30 | azurerm_linux_function_app | app.tf | 1 |
| 31 | azurerm_cosmosdb_account | data.tf | 1 |
| 32 | azurerm_cosmosdb_sql_database | data.tf | 1 |
| 33 | azurerm_cosmosdb_sql_container | data.tf | 1 |
| 34 | azurerm_mssql_server | data.tf | 1 |
| 35 | azurerm_mssql_database | data.tf | 1 |
| 36 | azurerm_mssql_firewall_rule | data.tf | 1 |
| 37 | azurerm_storage_account | storage.tf | 3 |
| 38 | azurerm_storage_container | storage.tf | 2 |
| 39 | azurerm_storage_queue | storage.tf | 1 |
| 40 | azurerm_storage_table | storage.tf | 1 |
| 41 | azurerm_key_vault | security.tf | 1 |
| 42 | azurerm_key_vault_access_policy | security.tf | 2 |
| 43 | azurerm_key_vault_secret | security.tf | 3 |
| 44 | azurerm_log_analytics_workspace | observability.tf | 1 |
| 45 | azurerm_application_insights | observability.tf | 1 |
| 46 | azurerm_servicebus_namespace | observability.tf | 1 |
| 47 | azurerm_servicebus_queue | observability.tf | 1 |
| 48 | azurerm_servicebus_topic | observability.tf | 1 |
| 49 | azurerm_eventhub_namespace | observability.tf | 1 |
| 50 | azurerm_eventhub | observability.tf | 1 |
| 51 | azurerm_user_assigned_identity | iam.tf | 1 |
| 52 | azurerm_role_definition | iam.tf | 1 |
| 53 | azurerm_role_assignment | iam.tf | 3 |

Plus non-`azurerm` helper resources: `random_string.suffix`,
`random_password.sendgrid_api_key`, `tls_private_key.vm_ssh`, and the
`azurerm_client_config` data source.

## Insecure vs. secure secret map

| Posture | Location (file:resource / setting) | Value |
|---|---|---|
| INSECURE | `app.tf:azurerm_linux_web_app.web` app_settings `DB_CONNECTION_STRING` | SQL admin password embedded as plaintext in a connection string |
| INSECURE | `app.tf:azurerm_linux_web_app.web` `connection_string["PrimarySqlDb"]` | Same plaintext SQL password, in a native connection_string block |
| INSECURE | `app.tf:azurerm_linux_web_app.web` app_settings `STORAGE_CONNECTION_STRING` | `azurerm_storage_account.pub.primary_connection_string` -- contains `AccountKey=` |
| INSECURE | `app.tf:azurerm_linux_function_app.func` app_settings `THIRD_PARTY_API_KEY` | Literal fake API key string |
| INSECURE | `data.tf:azurerm_mssql_server.main` `administrator_login_password` | `local.insecure_sql_admin_password`, hardcoded in `variables.tf` |
| SECURE | `app.tf:azurerm_linux_web_app.web` app_settings `KEYVAULT_DB_PASSWORD_REF` | `@Microsoft.KeyVault(SecretUri=...)` -> `security.tf:azurerm_key_vault_secret.sql_admin_password` (same underlying password as the insecure spots above, for direct comparison) |
| SECURE | `app.tf:azurerm_linux_function_app.func` app_settings `SENDGRID_API_KEY_REF` | KV reference -> `security.tf:azurerm_key_vault_secret.sendgrid_api_key` (value generated by `random_password`, never hardcoded) |
| SECURE | `app.tf:azurerm_linux_function_app.func` app_settings `COSMOS_DB_KEY_REF` | KV reference -> `security.tf:azurerm_key_vault_secret.cosmos_primary_key` (pulled from `azurerm_cosmosdb_account.main.primary_key`, never hardcoded) |

## Storage posture mix

- `azurerm_storage_account.pub` -- `allow_nested_items_to_be_public = true`,
  container `public-assets` has `container_access_type = "container"`
  (public). Also the source of the leaked `AccountKey=` connection string
  above. -- INSECURE, intentional.
- `azurerm_storage_account.locked_down` -- public access disabled, network
  rules default-deny, fronted by a private endpoint + private DNS zone.
  Holds the queue/table. -- SECURE.
- `azurerm_storage_account.func` -- private, dedicated to the Function App
  runtime.

## Deviations from the brief (documented, not silent)

- **Service Bus SKU bumped from Basic to Standard.** Azure does not support
  Topics/Subscriptions on the Basic SKU -- it's a platform constraint, not a
  cost choice. Standard is still cheap (~$10/mo base) and provisions in
  seconds. Event Hub stays on Basic as specified since it has no such
  constraint.
- **Cosmos DB free tier** assumes no other free-tier Cosmos account already
  exists in this subscription (Azure allows exactly one per subscription).
  If apply fails on that constraint, flip `enable_free_tier` to `false` in
  `data.tf`.

## Deliberately skipped

- AKS -- too slow to provision for a "few minutes" cost-conscious seed.
- VPN Gateway / ExpressRoute / Azure Firewall / Application Gateway /
  Bastion / NAT Gateway / dedicated hosts -- all excluded per brief
  (hourly-expensive and/or slow to provision).
- Regional VNet integration was NOT wired up between the Web App/Function
  App and `spoke_delegated` -- the delegated subnet exists as an unused
  brownfield artifact (a realistic "we provisioned this and never finished
  the job" scenario) rather than adding cross-resource plumbing that isn't
  needed to exercise TerraLift's onboarding logic.
- ACR is provisioned but not wired to the Container Instance, which instead
  pulls a public `mcr.microsoft.com` sample image -- avoids an image
  push/build step this seed doesn't need.

## Validation

`terraform init -backend=false && terraform validate` -- see session output.
Only `validate` was run; this Terraform was authored but **not** applied.
