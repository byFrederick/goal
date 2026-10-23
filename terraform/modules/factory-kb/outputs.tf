output "prefix" {
  value = local.prefix
}

output "knowledge_base_id" {
  value = aws_bedrockagent_knowledge_base.this.id
}

output "data_source_id" {
  value = aws_bedrockagent_data_source.this.data_source_id
}

output "agent_role_arn" {
  description = "Role the Goal gateway assumes to serve kb_search / kb_write for this factory."
  value       = aws_iam_role.agent.arn
}
