output "knowledge_bucket" {
  description = "Bucket holding every factory's documents under its own prefix."
  value       = aws_s3_bucket.knowledge.bucket
}

output "factories" {
  description = "Per-factory knowledge base wiring, consumed by goald (goal.lock.json)."
  value = {
    for id, kb in module.factory_kb : id => {
      name              = local.factories[id].metadata.name
      prefix            = kb.prefix
      knowledge_base_id = kb.knowledge_base_id
      data_source_id    = kb.data_source_id
      agent_role_arn    = kb.agent_role_arn
    }
  }
}
