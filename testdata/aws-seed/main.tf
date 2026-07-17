# Throwaway TerraLift AWS test seed. Cheap/free-tier resources, deleted after
# testing. Covers the import-id override cases (bucket name, vpc/sg ids, iam role
# name, sns ARN, sqs URL, secret ARN), an exposure signal (0.0.0.0/0 SG), and a
# Secrets Manager secret WITH a value (control-plane test: capture the resource,
# never the value).
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

data "aws_caller_identity" "current" {}

locals {
  suffix = data.aws_caller_identity.current.account_id
}

# --- S3 (import id = bucket name) ---
resource "aws_s3_bucket" "private" {
  bucket        = "terralift-seed-${local.suffix}-private"
  force_destroy = true
  tags          = { app = "terralift-seed", tier = "private" }
}

resource "aws_s3_bucket" "logs" {
  bucket        = "terralift-seed-${local.suffix}-logs"
  force_destroy = true
  tags          = { app = "terralift-seed", tier = "logs" }
}

# --- VPC + security group (import id = vpc-…/sg-… ids; 0.0.0.0/0 = exposure) ---
resource "aws_vpc" "main" {
  cidr_block = "10.42.0.0/16"
  tags       = { app = "terralift-seed", Name = "terralift-seed-vpc" }
}

resource "aws_subnet" "main" {
  vpc_id     = aws_vpc.main.id
  cidr_block = "10.42.1.0/24"
  tags       = { app = "terralift-seed", Name = "terralift-seed-subnet" }
}

resource "aws_security_group" "web" {
  name        = "terralift-seed-web"
  description = "TerraLift seed - intentionally open for exposure testing"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "ssh from anywhere (exposure signal)"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { app = "terralift-seed" }
}

# --- IAM role (import id = role name; global container) ---
resource "aws_iam_role" "app" {
  name = "terralift-seed-app"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
  tags = { app = "terralift-seed" }
}

# --- SNS topic (import id = full ARN) ---
resource "aws_sns_topic" "events" {
  name = "terralift-seed-events"
  tags = { app = "terralift-seed" }
}

# --- SQS queue (import id = queue URL) ---
resource "aws_sqs_queue" "jobs" {
  name = "terralift-seed-jobs"
  tags = { app = "terralift-seed" }
}

# --- DynamoDB table (import id = table name) ---
resource "aws_dynamodb_table" "state" {
  name         = "terralift-seed-state"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"
  attribute {
    name = "id"
    type = "S"
  }
  tags = { app = "terralift-seed" }
}

# --- Secrets Manager secret WITH a value (control-plane test) ---
resource "aws_secretsmanager_secret" "api" {
  name                    = "terralift-seed-api-key"
  recovery_window_in_days = 0 # allow immediate delete on teardown
  tags                    = { app = "terralift-seed" }
}

resource "aws_secretsmanager_secret_version" "api" {
  secret_id     = aws_secretsmanager_secret.api.id
  secret_string = "SUPER-SECRET-do-not-capture-0xDEADBEEF"
}

output "bucket" { value = aws_s3_bucket.private.bucket }
output "vpc_id" { value = aws_vpc.main.id }
output "secret_arn" { value = aws_secretsmanager_secret.api.arn }
