variable "region" {
  description = "AWS region for the knowledge layer. Bedrock Knowledge Bases and S3 Vectors must both be available there."
  type        = string
  default     = "us-east-1"
}

variable "factories_dir" {
  description = "Folder holding <name>/factory.yaml files. Every factory found gets its own knowledge base."
  type        = string
  default     = "../factories"
}

variable "embedding_model_id" {
  description = "Bedrock embedding model used by every knowledge base."
  type        = string
  default     = "amazon.titan-embed-text-v2:0"
}

variable "embedding_dimensions" {
  description = "Vector size produced by the embedding model."
  type        = number
  default     = 1024
}

variable "agent_principal_arns" {
  description = "IAM principals allowed to assume the per-factory agent roles (your SSO role or the Goal host user)."
  type        = list(string)
}

variable "noncurrent_version_days" {
  description = "Days to keep overwritten knowledge documents before they expire."
  type        = number
  default     = 90
}
