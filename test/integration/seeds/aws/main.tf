# Integration-test seed: a small, cheap, self-contained set of AWS resources that
# exercises the pipeline's hot paths — networking cross-references (VPC/subnet/SG),
# IAM roles, and two resources whose role lives in a different stack (Step Functions
# and CodeBuild), which drives the cross-stack aws_iam_role data-source path.
#
# Everything here is free-tier / zero standing cost: no NAT gateway, no build runs,
# no state-machine executions. The integration test destroys it on completion.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

# --- Networking (exercises vpc_id / subnet rewiring) ---------------------------

resource "aws_vpc" "it" {
  cidr_block = "10.199.0.0/16"
  tags       = { Name = "tl-it-vpc" }
}

resource "aws_subnet" "it" {
  vpc_id     = aws_vpc.it.id
  cidr_block = "10.199.1.0/24"
  tags       = { Name = "tl-it-subnet" }
}

resource "aws_security_group" "it" {
  name        = "tl-it-sg"
  description = "terralift integration test"
  vpc_id      = aws_vpc.it.id
  tags        = { Name = "tl-it-sg" }
}

# --- Step Functions (role_arn -> cross-stack data source) ----------------------

resource "aws_iam_role" "sfn" {
  name = "tl-it-sfn-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "states.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_sfn_state_machine" "it" {
  name     = "tl-it-sm"
  role_arn = aws_iam_role.sfn.arn
  definition = jsonencode({
    StartAt = "Pass"
    States  = { Pass = { Type = "Pass", End = true } }
  })
}

# --- CodeBuild (service_role -> cross-stack data source) -----------------------

resource "aws_iam_role" "cb" {
  name = "tl-it-cb-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "codebuild.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_codebuild_project" "it" {
  name         = "tl-it-cb"
  service_role = aws_iam_role.cb.arn

  artifacts {
    type = "NO_ARTIFACTS"
  }

  environment {
    compute_type = "BUILD_GENERAL1_SMALL"
    image        = "aws/codebuild/amazonlinux2-x86_64-standard:5.0"
    type         = "LINUX_CONTAINER"
  }

  source {
    type      = "NO_SOURCE"
    buildspec = "version: 0.2\nphases:\n  build:\n    commands:\n      - echo noop\n"
  }
}
