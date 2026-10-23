variable "factory_id" {
  description = "Immutable factory UUID (metadata.id). Names every resource and the S3 prefix."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.factory_id))
    error_message = "factory_id must be a lowercase UUID."
  }
}

variable "factory_name" {
  description = "Display name (metadata.name). Used only in tags and README text, so a rename is an in-place update."
  type        = string
}

variable "collections" {
  description = "Top-level folders under the prefix (runbooks, incidents, …)."
  type        = list(string)
}

variable "schedule" {
  description = "EventBridge Scheduler expression for re-ingestion, or null for on-write ingestion only."
  type        = string
  default     = null
}

variable "bucket_name" {
  type = string
}

variable "bucket_arn" {
  type = string
}

variable "kms_key_arn" {
  type = string
}

variable "vector_bucket_name" {
  type = string
}

variable "embedding_model_arn" {
  type = string
}

variable "embedding_dimensions" {
  type = number
}

variable "agent_principal_arns" {
  type = list(string)
}
