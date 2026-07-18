# Storage posture mix: one bucket intentionally PUBLIC (exercises the
# hygiene report) and one locked down + encrypted + versioned.

# --- INSECURE: public bucket -----------------------------------------------

resource "aws_s3_bucket" "public_assets" {
  bucket        = "${local.name}-public-assets"
  force_destroy = true
  tags          = { Name = "${local.name}-public-assets" }
}

resource "aws_s3_bucket_ownership_controls" "public_assets" {
  bucket = aws_s3_bucket.public_assets.id
  rule {
    object_ownership = "BucketOwnerPreferred"
  }
}

resource "aws_s3_bucket_public_access_block" "public_assets" {
  bucket = aws_s3_bucket.public_assets.id

  block_public_acls       = false
  block_public_policy     = false
  ignore_public_acls      = false
  restrict_public_buckets = false
}

resource "aws_s3_bucket_acl" "public_assets" {
  bucket = aws_s3_bucket.public_assets.id
  acl    = "public-read"

  depends_on = [
    aws_s3_bucket_ownership_controls.public_assets,
    aws_s3_bucket_public_access_block.public_assets,
  ]
}

resource "aws_s3_bucket_policy" "public_assets" {
  bucket = aws_s3_bucket.public_assets.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "PublicRead"
      Effect    = "Allow"
      Principal = "*"
      Action    = "s3:GetObject"
      Resource  = "${aws_s3_bucket.public_assets.arn}/*"
    }]
  })

  depends_on = [aws_s3_bucket_public_access_block.public_assets]
}

# --- SECURE: locked-down bucket --------------------------------------------

resource "aws_s3_bucket" "private_data" {
  bucket        = "${local.name}-private-data"
  force_destroy = true
  tags          = { Name = "${local.name}-private-data" }
}

resource "aws_s3_bucket_public_access_block" "private_data" {
  bucket = aws_s3_bucket.private_data.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "private_data" {
  bucket = aws_s3_bucket.private_data.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.main.arn
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_versioning" "private_data" {
  bucket = aws_s3_bucket.private_data.id
  versioning_configuration {
    status = "Enabled"
  }
}
