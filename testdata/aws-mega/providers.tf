# TerraLift AWS "mega" brownfield seed. This is the BEFORE state: real
# ClickOps-style infrastructure that TerraLift will later enumerate, export,
# and reconcile into its own Terraform. Authored/validated only — not applied
# by this workspace. See MANIFEST.md for the full resource-type inventory and
# the insecure-vs-secure secrets map.
terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.4"
    }
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = "terralift-mega"
      Environment = "brownfield-test"
      ManagedBy   = "clickops-pre-terraform"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

data "aws_availability_zones" "available" {
  state = "available"
}

# Global uniqueness (S3 bucket names, etc.) without hardcoding an account id.
resource "random_id" "suffix" {
  byte_length = 4
}

locals {
  suffix = random_id.suffix.hex
  name   = "${var.prefix}-${local.suffix}"

  az_a = data.aws_availability_zones.available.names[0]
  az_b = data.aws_availability_zones.available.names[1]
}
