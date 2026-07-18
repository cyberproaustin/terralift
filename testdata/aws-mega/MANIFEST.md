# aws-mega — TerraLift AWS brownfield test seed

A large, complex, hand-authored "before" environment: real ClickOps-style AWS
infrastructure (never touched by TerraLift) that exercises breadth of
resource-type coverage, LB-type classification (ALB vs NLB), storage hygiene
(public vs locked bucket), and the insecure-vs-secure secrets comparison.

Region: `us-east-1` (var, defaults there). Provider `hashicorp/aws ~> 5.0`.
Every resource name/tag is prefixed `tlmega-<random 8-hex suffix>` (the
suffix comes from `random_id.suffix`, needed for globally-unique names like
S3 buckets). No NAT Gateway / VPN / Transit Gateway / Network Firewall / EKS
— per the cost + provisioning-time constraints.

Authored and validated only: `terraform init -backend=false` +
`terraform validate` pass clean. **Not applied** by this workspace.

## Resource-type inventory

**72 distinct `aws_*` resource types**, 1-2 instances each (110 total `aws_*`
resource blocks), plus `random_id` for suffixing and `archive_file`
(hashicorp/archive) for the Lambda deployment package. Organized breadth-first
across 10 files:

| File | Resource types |
|---|---|
| `providers.tf` | (provider/data/locals only — no resources besides `random_id.suffix`) |
| `network.tf` (19 types) | `aws_vpc`, `aws_subnet`, `aws_internet_gateway`, `aws_vpc_peering_connection`, `aws_route_table`, `aws_route`, `aws_route_table_association`, `aws_vpc_endpoint`, `aws_security_group`, `aws_security_group_rule`, `aws_network_acl`, `aws_network_acl_rule`, `aws_route53_zone`, `aws_route53_record`, `aws_lb`, `aws_lb_target_group`, `aws_lb_target_group_attachment`, `aws_lb_listener`, `aws_network_interface` |
| `compute.tf` (9 types) | `aws_key_pair`, `aws_instance`, `aws_ebs_volume`, `aws_volume_attachment`, `aws_launch_template`, `aws_ecr_repository`, `aws_ecs_cluster`, `aws_ecs_task_definition`, `aws_ecs_service` |
| `app.tf` (11 types) | `aws_lambda_function`, `aws_api_gateway_rest_api`, `aws_api_gateway_resource`, `aws_api_gateway_method`, `aws_api_gateway_integration`, `aws_api_gateway_deployment`, `aws_api_gateway_stage`, `aws_lambda_permission`, `aws_api_gateway_api_key`, `aws_api_gateway_usage_plan`, `aws_api_gateway_usage_plan_key` |
| `data.tf` (10 types) | `aws_dynamodb_table`, `aws_sns_topic`, `aws_sns_topic_policy`, `aws_sqs_queue`, `aws_sqs_queue_policy`, `aws_sns_topic_subscription`, `aws_cloudwatch_event_bus`, `aws_cloudwatch_event_rule`, `aws_cloudwatch_event_target`, `aws_kinesis_stream` |
| `storage.tf` (7 types) | `aws_s3_bucket`, `aws_s3_bucket_ownership_controls`, `aws_s3_bucket_public_access_block`, `aws_s3_bucket_acl`, `aws_s3_bucket_policy`, `aws_s3_bucket_server_side_encryption_configuration`, `aws_s3_bucket_versioning` |
| `security.tf` (5 types) | `aws_kms_key`, `aws_kms_alias`, `aws_secretsmanager_secret`, `aws_secretsmanager_secret_version`, `aws_ssm_parameter` |
| `observability.tf` (3 types) | `aws_cloudwatch_log_group`, `aws_cloudwatch_metric_alarm`, `aws_cloudwatch_dashboard` |
| `iam.tf` (8 types) | `aws_iam_role`, `aws_iam_role_policy_attachment`, `aws_iam_role_policy`, `aws_iam_policy`, `aws_iam_instance_profile`, `aws_iam_group`, `aws_iam_user`, `aws_iam_user_group_membership` |

### Networking specifics (LB classification exercise)

- 2 VPCs (`10.60.0.0/16` main, `10.61.0.0/16` peer) connected by
  `aws_vpc_peering_connection` (`auto_accept = true`, same account/region).
- Main VPC: 2 AZs x 3 tiers = 6 subnets (public/private/isolated), IGW +
  3 route tables. **No NAT** — the private tier is genuinely egress-isolated
  except for the peering route and the S3 gateway endpoint; the ECS Fargate
  service instead runs in the public subnets with `assign_public_ip = true`.
- `aws_vpc_endpoint`: one Gateway (S3, attached to the private + isolated
  route tables) and one Interface (Secrets Manager, in the private subnets).
- `aws_route53_zone`: one private (associated to the main VPC) + one public,
  each with an alias `aws_route53_record` pointing at the ALB.
- **`aws_lb.app`** — `load_balancer_type = "application"`, internal, target
  group `target_type = "instance"` (registers the EC2 instance).
- **`aws_lb.svc`** — `load_balancer_type = "network"`, internal, target group
  `target_type = "ip"` (registered dynamically by the ECS service's
  `load_balancer {}` block — Fargate awsvpc mode requires `ip` targets).
  This ALB/NLB pair is specifically to exercise TerraLift's LB-type
  classification against the `elasticloadbalancing:loadbalancer/app` vs
  `/net` ARN-path distinction.

### Skipped for cost / provisioning time

- **NAT Gateway, VPN Gateway, Transit Gateway, Network Firewall, EKS** — all
  explicitly excluded per the brief (hourly cost and/or slow to provision).
- **RDS** — skipped entirely in favor of DynamoDB (on-demand, instant
  provisioning); the brief flagged RDS as optional and "if it slows things,
  prefer DynamoDB."
- **`aws_iam_access_key`** — skipped. It would mint a real, live AWS secret
  access key (unlike the other insecure examples below, which are inert
  strings/params), which is a materially different risk to leave lying
  around in a test fixture and wasn't required by the brief.
- **`aws_vpc_dhcp_options`** and **Classic ELB** — skipped, no incremental
  test value over what's already covered.

## Insecure vs. secure secrets — key comparison

The whole point of this fixture: TerraLift's redactor (`internal/hcl/redact.go`)
removes **unambiguous single secrets** before export; its secrets-review
scanner (`internal/reconcile/secrets_review.go`) **flags** shipped app config
that looks like a secret instead of blanking it (config ships — that's the
value of onboarding to IaC). Both are exercised here:

| # | Insecure (plaintext — redacted or flagged) | File:Resource | Secure (reference — ships clean) | File:Resource |
|---|---|---|---|---|
| 1 | `DB_PASSWORD` literal in Lambda `environment{}` | `app.tf:aws_lambda_function.api` | Secrets Manager secret backing it | `security.tf:aws_secretsmanager_secret.app_db` + `aws_secretsmanager_secret_version.app_db` |
| 2 | `LEGACY_DB_CONNECTION_STRING` with embedded `Password=` in Lambda `environment{}` | `app.tf:aws_lambda_function.api` | — (illustrates the value-pattern heuristic, not key-name) | — |
| 3 | `DB_PASSWORD` literal in ECS `container_definitions[].environment[]` | `compute.tf:aws_ecs_task_definition.app` | `APP_DB_CONN` in the same task's `container_definitions[].secrets[]`, `valueFrom` = Secrets Manager ARN (no literal value) | `compute.tf:aws_ecs_task_definition.app` (`secrets[]`) referencing `security.tf:aws_secretsmanager_secret.app_db` |
| 4 | SSM `String` parameter holding a secret-looking value (NOT SecureString — not redacted by the unambiguous-secret rule, since `type = "String"` isn't in that class) | `security.tf:aws_ssm_parameter.legacy_db_password` | SSM `SecureString` parameter | `security.tf:aws_ssm_parameter.app_api_secret` |
| 5 | `aws_api_gateway_api_key.value` — AWS auto-generates a real, live secret value on create | `app.tf:aws_api_gateway_api_key.client` | — (no "secure" analog; TerraLift must redact this attribute wholesale on export) | — |

Redaction-vs-flag split, matching the tool's split responsibility:
- **Redacted (removed) by the exporter, not just flagged**: `aws_ssm_parameter.app_api_secret.value` (SecureString), `aws_secretsmanager_secret_version.app_db.secret_string`, `aws_api_gateway_api_key.client.value`.
- **Flagged by secrets-review, shipped as config**: the Lambda `environment{}` block (#1, #2) and the ECS `container_definitions[].environment[]` entry (#3) — these are optional app-config maps, not unambiguous single-secret attributes, so per `internal/hcl/redact.go`'s ADR-001 policy they ship and get flagged instead of blanked.
- **`aws_ssm_parameter.legacy_db_password`** (#4) is the deliberate near-miss: same secret-looking value as its SecureString sibling, but `type = "String"` — this is exactly the class of finding the secrets-review backstop exists to catch, since the redactor's SecureString-only rule won't touch it.

## Storage posture mix

- **Public** (`storage.tf:aws_s3_bucket.public_assets`): `aws_s3_bucket_public_access_block` all four flags `false`, `aws_s3_bucket_ownership_controls` set to `BucketOwnerPreferred` (required for the ACL to apply), `aws_s3_bucket_acl` = `public-read`, and an explicit `aws_s3_bucket_policy` granting anonymous `s3:GetObject`.
- **Locked** (`storage.tf:aws_s3_bucket.private_data`): `aws_s3_bucket_public_access_block` all four flags `true`, KMS (`aws_kms_key.main`) server-side encryption via `aws_s3_bucket_server_side_encryption_configuration`, and `aws_s3_bucket_versioning` enabled.

## Validation

```
$ terraform init -backend=false     # succeeded — aws ~>5.0, random ~>3.6, archive ~>2.4
$ terraform validate
Success! The configuration is valid.
```

`.terraform/` (provider plugin cache) has been removed after validation;
`.terraform.lock.hcl` is kept so `terraform init` reproduces the same
provider versions. Re-run `terraform init -backend=false` before validating
or applying again.

No credentials were used or required for authoring/validation — `validate`
never contacts AWS. Applying this configuration for real requires standard
AWS credentials (not hardcoded anywhere in this repo) and will create real,
billable resources; destroy when finished.
