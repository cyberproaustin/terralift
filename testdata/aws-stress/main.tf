# TerraLift AWS stress seed — WAVE 1. Broad coverage of FREE / near-free AWS
# resource types (definition-only: no running compute, no data flow). Applied,
# exercised by TerraLift, round-tripped, then destroyed. Nothing here incurs
# meaningful cost for a short-lived test (KMS/Secrets/Route53 are pennies/month
# prorated). NO instances / NAT / RDS / LB / EKS.
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

data "aws_caller_identity" "me" {}

locals {
  tags = { app = "terralift-stress", wave = "1" }
}

############################################
# Networking (VPC + friends) — free
############################################
resource "aws_vpc" "main" {
  cidr_block           = "10.50.0.0/16"
  enable_dns_hostnames = true
  tags                 = merge(local.tags, { Name = "tl-stress-vpc" })
}

resource "aws_subnet" "a" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.50.1.0/24"
  availability_zone = "us-east-1a"
  tags              = merge(local.tags, { Name = "tl-stress-subnet-a" })
}

resource "aws_subnet" "b" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.50.2.0/24"
  availability_zone = "us-east-1b"
  tags              = merge(local.tags, { Name = "tl-stress-subnet-b" })
}

resource "aws_internet_gateway" "igw" {
  vpc_id = aws_vpc.main.id
  tags   = local.tags
}

resource "aws_route_table" "rt" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.igw.id
  }
  tags = local.tags
}

resource "aws_route_table_association" "a" {
  subnet_id      = aws_subnet.a.id
  route_table_id = aws_route_table.rt.id
}

resource "aws_network_acl" "nacl" {
  vpc_id     = aws_vpc.main.id
  subnet_ids = [aws_subnet.b.id]
  tags       = local.tags
}

resource "aws_network_acl_rule" "nacl_in" {
  network_acl_id = aws_network_acl.nacl.id
  rule_number    = 100
  egress         = false
  protocol       = "tcp"
  rule_action    = "allow"
  cidr_block     = "0.0.0.0/0"
  from_port      = 443
  to_port        = 443
}

resource "aws_security_group" "web" {
  name_prefix = "tl-stress-web-"
  description = "stress web sg"
  vpc_id      = aws_vpc.main.id
  tags        = local.tags
}

resource "aws_vpc_security_group_ingress_rule" "https" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_ec2_managed_prefix_list" "pl" {
  name           = "tl-stress-pl"
  address_family = "IPv4"
  max_entries    = 5
  entry {
    cidr        = "10.0.0.0/8"
    description = "internal"
  }
  tags = local.tags
}

resource "aws_vpc_dhcp_options" "dopt" {
  domain_name         = "tl.internal"
  domain_name_servers = ["AmazonProvidedDNS"]
  tags                = local.tags
}

resource "aws_vpc_dhcp_options_association" "dopt" {
  vpc_id          = aws_vpc.main.id
  dhcp_options_id = aws_vpc_dhcp_options.dopt.id
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.us-east-1.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.rt.id]
  tags              = local.tags
}

resource "aws_flow_log" "vpc" {
  log_destination      = aws_cloudwatch_log_group.flow.arn
  log_destination_type = "cloud-watch-logs"
  traffic_type         = "ALL"
  vpc_id               = aws_vpc.main.id
  iam_role_arn         = aws_iam_role.flow.arn
  tags                 = local.tags
}

############################################
# IAM — free
############################################
data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com", "lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "app" {
  name               = "tl-stress-app"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
  tags               = local.tags
}

resource "aws_iam_role_policy" "app_inline" {
  name = "tl-stress-inline"
  role = aws_iam_role.app.id
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = ["s3:GetObject"], Resource = "*" }]
  })
}

resource "aws_iam_policy" "managed" {
  name = "tl-stress-managed"
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = ["logs:PutLogEvents"], Resource = "*" }]
  })
  tags = local.tags
}

resource "aws_iam_role_policy_attachment" "app_managed" {
  role       = aws_iam_role.app.name
  policy_arn = aws_iam_policy.managed.arn
}

resource "aws_iam_instance_profile" "app" {
  name = "tl-stress-app"
  role = aws_iam_role.app.name
  tags = local.tags
}

resource "aws_iam_group" "devs" {
  name = "tl-stress-devs"
}

resource "aws_iam_role" "flow" {
  name = "tl-stress-flow"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Principal = { Service = "vpc-flow-logs.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
  tags = local.tags
}

############################################
# S3 — free (empty buckets)
############################################
resource "aws_s3_bucket" "data" {
  bucket        = "tl-stress-${local.acct}-data"
  force_destroy = true
  tags          = local.tags
}

resource "aws_s3_bucket_versioning" "data" {
  bucket = aws_s3_bucket.data.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_public_access_block" "data" {
  bucket                  = aws_s3_bucket.data.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "data" {
  bucket = aws_s3_bucket.data.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "data" {
  bucket = aws_s3_bucket.data.id
  rule {
    id     = "expire"
    status = "Enabled"
    filter { prefix = "tmp/" }
    expiration { days = 30 }
  }
}

resource "aws_s3_bucket_ownership_controls" "data" {
  bucket = aws_s3_bucket.data.id
  rule { object_ownership = "BucketOwnerEnforced" }
}

############################################
# Serverless / compute-definition — free (no invocations)
############################################
data "archive_file" "lambda" {
  type                    = "zip"
  output_path             = "${path.module}/lambda.zip"
  source_content          = "exports.handler = async () => 'ok';"
  source_content_filename = "index.js"
}

resource "aws_lambda_function" "fn" {
  function_name    = "tl-stress-fn"
  role             = aws_iam_role.app.arn
  runtime          = "nodejs20.x"
  handler          = "index.handler"
  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256
  # env vars commonly hold secrets — exercises the Lambda-env redaction path
  environment {
    variables = {
      LOG_LEVEL   = "info"
      API_TOKEN   = "LAMBDA-ENV-SECRET-do-not-capture"
      DB_PASSWORD = "LAMBDA-ENV-PASSWORD-do-not-capture"
    }
  }
  tags = local.tags
}

resource "aws_lambda_alias" "fn" {
  name             = "live"
  function_name    = aws_lambda_function.fn.function_name
  function_version = "$LATEST"
}

############################################
# Data / messaging — free
############################################
resource "aws_dynamodb_table" "state" {
  name         = "tl-stress-state"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  range_key    = "sk"
  attribute {
    name = "pk"
    type = "S"
  }
  attribute {
    name = "sk"
    type = "S"
  }
  attribute {
    name = "gsi1"
    type = "S"
  }
  global_secondary_index {
    name            = "gsi1"
    hash_key        = "gsi1"
    projection_type = "ALL"
  }
  ttl {
    attribute_name = "expires"
    enabled        = true
  }
  tags = local.tags
}

resource "aws_sns_topic" "events" {
  name = "tl-stress-events"
  tags = local.tags
}

resource "aws_sqs_queue" "dlq" {
  name = "tl-stress-dlq"
  tags = local.tags
}

resource "aws_sqs_queue" "jobs" {
  name                       = "tl-stress-jobs"
  visibility_timeout_seconds = 60
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 5
  })
  tags = local.tags
}

resource "aws_sns_topic_subscription" "to_sqs" {
  topic_arn = aws_sns_topic.events.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.jobs.arn
}

############################################
# Observability — free / cheap
############################################
resource "aws_cloudwatch_log_group" "app" {
  name              = "/tl-stress/app"
  retention_in_days = 7
  tags              = local.tags
}

resource "aws_cloudwatch_log_group" "flow" {
  name              = "/tl-stress/flow"
  retention_in_days = 1
  tags              = local.tags
}

resource "aws_cloudwatch_metric_alarm" "errors" {
  alarm_name          = "tl-stress-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  tags                = local.tags
}

resource "aws_cloudwatch_event_rule" "schedule" {
  name                = "tl-stress-schedule"
  schedule_expression = "rate(1 day)"
  tags                = local.tags
}

resource "aws_cloudwatch_event_target" "to_lambda" {
  rule = aws_cloudwatch_event_rule.schedule.name
  arn  = aws_lambda_function.fn.arn
}

############################################
# Security / config — free / pennies
############################################
resource "aws_kms_key" "main" {
  description             = "tl-stress key"
  deletion_window_in_days = 7
  tags                    = local.tags
}

resource "aws_kms_alias" "main" {
  name          = "alias/tl-stress"
  target_key_id = aws_kms_key.main.key_id
}

resource "aws_secretsmanager_secret" "api" {
  name                    = "tl-stress-api-key"
  recovery_window_in_days = 0
  tags                    = local.tags
}

resource "aws_secretsmanager_secret_version" "api" {
  secret_id     = aws_secretsmanager_secret.api.id
  secret_string = "STRESS-SECRET-do-not-capture"
}

resource "aws_ssm_parameter" "config" {
  name  = "/tl-stress/config"
  type  = "String"
  value = "plain-config-value"
  tags  = local.tags
}

resource "aws_ssm_parameter" "secure" {
  name  = "/tl-stress/secure"
  type  = "SecureString"
  value = "SECURE-do-not-capture"
  tags  = local.tags
}

############################################
# Registries / containers (definition only) — free
############################################
resource "aws_ecr_repository" "app" {
  name         = "tl-stress-app"
  force_delete = true
  tags         = local.tags
}

resource "aws_ecs_cluster" "main" {
  name = "tl-stress-cluster"
  tags = local.tags
}

############################################
# Compute — CHEAPEST SKUs, torn down immediately. t3.micro is free-tier on a new
# account; a VM alive for minutes is pennies. Tests instance/volume/AMI imports.
############################################
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }
}

resource "aws_launch_template" "app" {
  name_prefix   = "tl-stress-"
  image_id      = data.aws_ami.al2023.id
  instance_type = "t3.micro"
  tags          = local.tags
}

resource "aws_instance" "app" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.a.id
  vpc_security_group_ids = [aws_security_group.web.id]
  tags                   = merge(local.tags, { Name = "tl-stress-vm" })
}

resource "aws_ebs_volume" "data" {
  availability_zone = "us-east-1a"
  size              = 1
  type              = "gp3"
  tags              = local.tags
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.app.id
}

############################################
# Route53 — pennies/month prorated
############################################
resource "aws_route53_zone" "internal" {
  name    = "tl-stress.internal"
  comment = "stress private zone"
  vpc {
    vpc_id = aws_vpc.main.id
  }
  tags = local.tags
}

resource "aws_route53_record" "a" {
  zone_id = aws_route53_zone.internal.zone_id
  name    = "app.tl-stress.internal"
  type    = "A"
  ttl     = 300
  records = ["10.50.1.10"]
}

locals {
  acct = data.aws_caller_identity.me.account_id
}
