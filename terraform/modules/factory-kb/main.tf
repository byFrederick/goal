data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  # The prefix is the UUID, not the name, so a rename never moves documents.
  prefix      = "${var.factory_id}/"
  objects_arn = "${var.bucket_arn}/${local.prefix}*"
  tags = {
    FactoryId   = var.factory_id
    FactoryName = var.factory_name
  }
}

# A README per collection makes each prefix exist and tells humans (and the librarian) what goes there.
resource "aws_s3_object" "collection_readme" {
  for_each = toset(var.collections)

  bucket       = var.bucket_name
  key          = "${local.prefix}${each.key}/README.md"
  content_type = "text/markdown"
  content      = <<-EOT
    ---
    title: ${each.key}
    factory: ${var.factory_name}
    factory_id: ${var.factory_id}
    ---
    # ${each.key}

    Documents in the `${each.key}` collection of the ${var.factory_name} factory. Written by agents through `kb_write`
    or uploaded by hand. One topic per file, Markdown with YAML front matter.
  EOT
}

resource "aws_s3vectors_index" "this" {
  vector_bucket_name = var.vector_bucket_name
  index_name         = "goal-${var.factory_id}"
  data_type          = "float32"
  dimension          = var.embedding_dimensions
  distance_metric    = "cosine"

  # Bedrock stores chunk text and metadata on each vector; keeping them non-filterable avoids
  # the 2 KB filterable-metadata limit.
  metadata_configuration {
    non_filterable_metadata_keys = ["AMAZON_BEDROCK_TEXT", "AMAZON_BEDROCK_METADATA"]
  }
}

# ---- Role Bedrock assumes to read documents, embed them and write vectors -------------------

data "aws_iam_policy_document" "kb_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["bedrock.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }
}

resource "aws_iam_role" "kb" {
  name               = "goal-${var.factory_id}-kb"
  tags               = local.tags
  assume_role_policy = data.aws_iam_policy_document.kb_trust.json
}

data "aws_iam_policy_document" "kb" {
  statement {
    sid       = "Embed"
    actions   = ["bedrock:InvokeModel"]
    resources = [var.embedding_model_arn]
  }
  statement {
    sid       = "ReadFactoryPrefix"
    actions   = ["s3:GetObject"]
    resources = [local.objects_arn]
  }
  statement {
    sid       = "ListFactoryPrefix"
    actions   = ["s3:ListBucket"]
    resources = [var.bucket_arn]
    condition {
      test     = "StringLike"
      variable = "s3:prefix"
      values   = [local.prefix, "${local.prefix}*"]
    }
  }
  statement {
    sid = "Vectors"
    actions = [
      "s3vectors:GetIndex",
      "s3vectors:PutVectors",
      "s3vectors:GetVectors",
      "s3vectors:DeleteVectors",
      "s3vectors:QueryVectors",
    ]
    resources = [aws_s3vectors_index.this.index_arn]
  }
  statement {
    sid       = "Decrypt"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [var.kms_key_arn]
  }
}

resource "aws_iam_role_policy" "kb" {
  role   = aws_iam_role.kb.id
  policy = data.aws_iam_policy_document.kb.json
}

resource "aws_bedrockagent_knowledge_base" "this" {
  name     = "goal-${var.factory_id}"
  role_arn = aws_iam_role.kb.arn
  tags     = local.tags

  knowledge_base_configuration {
    type = "VECTOR"
    vector_knowledge_base_configuration {
      embedding_model_arn = var.embedding_model_arn
    }
  }

  storage_configuration {
    type = "S3_VECTORS"
    s3_vectors_configuration {
      index_arn = aws_s3vectors_index.this.index_arn
    }
  }

  depends_on = [aws_iam_role_policy.kb]
}

resource "aws_bedrockagent_data_source" "this" {
  knowledge_base_id = aws_bedrockagent_knowledge_base.this.id
  name              = "${var.factory_id}-s3"

  data_source_configuration {
    type = "S3"
    s3_configuration {
      bucket_arn         = var.bucket_arn
      inclusion_prefixes = [local.prefix]
    }
  }

  vector_ingestion_configuration {
    chunking_configuration {
      chunking_strategy = "FIXED_SIZE"
      fixed_size_chunking_configuration {
        max_tokens         = 512
        overlap_percentage = 20
      }
    }
  }
}

# ---- Role the Goal knowledge gateway assumes for this factory only ------------------------

data "aws_iam_policy_document" "agent_trust" {
  statement {
    actions = ["sts:AssumeRole", "sts:TagSession"]
    principals {
      type        = "AWS"
      identifiers = var.agent_principal_arns
    }
  }
}

resource "aws_iam_role" "agent" {
  name                 = "goal-${var.factory_id}-agent"
  tags                 = local.tags
  assume_role_policy   = data.aws_iam_policy_document.agent_trust.json
  max_session_duration = 3600
}

data "aws_iam_policy_document" "agent" {
  statement {
    sid       = "ReadWriteOwnPrefix"
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = [local.objects_arn]
  }
  statement {
    sid       = "ListOwnPrefix"
    actions   = ["s3:ListBucket"]
    resources = [var.bucket_arn]
    condition {
      test     = "StringLike"
      variable = "s3:prefix"
      values   = [local.prefix, "${local.prefix}*"]
    }
  }
  statement {
    sid       = "RetrieveAndSync"
    actions   = ["bedrock:Retrieve", "bedrock:StartIngestionJob", "bedrock:GetIngestionJob", "bedrock:ListIngestionJobs"]
    resources = [aws_bedrockagent_knowledge_base.this.arn]
  }
  statement {
    sid       = "EncryptDocuments"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [var.kms_key_arn]
  }
}

resource "aws_iam_role_policy" "agent" {
  role   = aws_iam_role.agent.id
  policy = data.aws_iam_policy_document.agent.json
}

# ---- Optional scheduled re-ingestion (knowledgeBase.ingestion.schedule) ----------------------

data "aws_iam_policy_document" "scheduler_trust" {
  count = var.schedule == null ? 0 : 1
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "scheduler" {
  count              = var.schedule == null ? 0 : 1
  name               = "goal-${var.factory_id}-ingest"
  assume_role_policy = data.aws_iam_policy_document.scheduler_trust[0].json
}

resource "aws_iam_role_policy" "scheduler" {
  count = var.schedule == null ? 0 : 1
  role  = aws_iam_role.scheduler[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "bedrock:StartIngestionJob"
      Resource = aws_bedrockagent_knowledge_base.this.arn
    }]
  })
}

resource "aws_scheduler_schedule" "ingest" {
  count               = var.schedule == null ? 0 : 1
  name                = "goal-${var.factory_id}-ingest"
  schedule_expression = var.schedule

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = "arn:aws:scheduler:::aws-sdk:bedrockagent:startIngestionJob"
    role_arn = aws_iam_role.scheduler[0].arn
    input = jsonencode({
      KnowledgeBaseId = aws_bedrockagent_knowledge_base.this.id
      DataSourceId    = aws_bedrockagent_data_source.this.data_source_id
    })
  }
}
