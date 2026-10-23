# The factory YAML files are the single source of truth: adding factories/<name>/factory.yaml
# and running `terraform apply` provisions that factory's knowledge base. No HCL edits.
# Everything keys on metadata.id (a UUID), so renaming a factory or its folder never replaces resources.
locals {
  factory_docs = [
    for path in fileset(var.factories_dir, "*/factory.yaml") : yamldecode(file("${var.factories_dir}/${path}"))
  ]

  factories = { for f in local.factory_docs : f.metadata.id => f }
}

module "factory_kb" {
  source   = "./modules/factory-kb"
  for_each = local.factories

  factory_id   = each.key
  factory_name = each.value.metadata.name
  collections  = each.value.spec.knowledgeBase.collections
  schedule     = try(each.value.spec.knowledgeBase.ingestion.schedule, null)

  bucket_name          = aws_s3_bucket.knowledge.bucket
  bucket_arn           = aws_s3_bucket.knowledge.arn
  kms_key_arn          = aws_kms_key.goal.arn
  vector_bucket_name   = aws_s3vectors_vector_bucket.goal.vector_bucket_name
  embedding_model_arn  = local.embedding_model_arn
  embedding_dimensions = var.embedding_dimensions
  agent_principal_arns = var.agent_principal_arns
}
