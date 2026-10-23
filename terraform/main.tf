data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  account_id          = data.aws_caller_identity.current.account_id
  embedding_model_arn = "arn:${data.aws_partition.current.partition}:bedrock:${var.region}::foundation-model/${var.embedding_model_id}"
}

# One key encrypts documents and vectors for every factory; isolation comes from IAM prefixes.
resource "aws_kms_key" "goal" {
  description             = "Goal knowledge base encryption"
  enable_key_rotation     = true
  deletion_window_in_days = 30
}

resource "aws_kms_alias" "goal" {
  name          = "alias/goal"
  target_key_id = aws_kms_key.goal.key_id
}

# s3://goal-kb-<account>-<region>/<factory>/<collection>/<doc>.md
resource "aws_s3_bucket" "knowledge" {
  bucket = "goal-kb-${local.account_id}-${var.region}"
}

resource "aws_s3_bucket_ownership_controls" "knowledge" {
  bucket = aws_s3_bucket.knowledge.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_public_access_block" "knowledge" {
  bucket                  = aws_s3_bucket.knowledge.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_versioning" "knowledge" {
  bucket = aws_s3_bucket.knowledge.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "knowledge" {
  bucket = aws_s3_bucket.knowledge.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.goal.arn
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "knowledge" {
  bucket = aws_s3_bucket.knowledge.id
  rule {
    id     = "expire-old-versions"
    status = "Enabled"
    filter {}
    noncurrent_version_expiration {
      noncurrent_days = var.noncurrent_version_days
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

data "aws_iam_policy_document" "knowledge_bucket" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.knowledge.arn, "${aws_s3_bucket.knowledge.arn}/*"]
    principals {
      type        = "*"
      identifiers = ["*"]
    }
    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "knowledge" {
  bucket = aws_s3_bucket.knowledge.id
  policy = data.aws_iam_policy_document.knowledge_bucket.json
}

# S3 Vectors keeps the vector store cheap for a personal platform (pay per GB and query,
# no always-on OpenSearch Serverless collection). One index per factory, created in the module.
resource "aws_s3vectors_vector_bucket" "goal" {
  vector_bucket_name = "goal-vectors-${local.account_id}"

  encryption_configuration {
    sse_type    = "aws:kms"
    kms_key_arn = aws_kms_key.goal.arn
  }
}
