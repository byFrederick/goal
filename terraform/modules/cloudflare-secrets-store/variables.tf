variable "account_id" {
  description = "The Cloudflare account that holds the Secrets Store."
  type        = string
  nullable    = false
}

variable "store_id" {
  description = "The account's Secrets Store (Cloudflare allows one per account; create it once if the account has none)."
  type        = string
  nullable    = false
}

variable "name_prefix" {
  description = "Prefix of every secret's name in the account-wide store, such as goal-production: the store may hold other projects' secrets."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9-]+$", var.name_prefix))
    error_message = "name_prefix may hold lowercase letters, digits and hyphens only."
  }
}

variable "secrets" {
  description = "Every secret, by the environment variable name its consumers read (GOAL_MASTER_KEYS): the value goes to the store as it is."
  type        = map(string)
  sensitive   = true
  nullable    = false

  validation {
    condition     = alltrue([for name in keys(var.secrets) : can(regex("^[A-Z][A-Z0-9_]*$", name))])
    error_message = "Secret names are environment variable names: uppercase letters, digits and underscores."
  }

  # Nothing goes to the store until every value exists: a placeholder would start goald or the
  # auth service with an empty secret.
  validation {
    condition     = alltrue([for value in values(var.secrets) : value != ""])
    error_message = "These secrets are empty in the SOPS file (task secrets:edit): ${join(", ", sort(nonsensitive([for name, value in var.secrets : name if value == ""])))}."
  }
}

variable "comment" {
  description = "Comment on every secret, saying who manages it."
  type        = string
  nullable    = false
}
