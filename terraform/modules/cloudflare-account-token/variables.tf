variable "account_id" {
  description = "The Cloudflare account that owns the token."
  type        = string
  nullable    = false
}

variable "name" {
  description = "The token's name in the dashboard."
  type        = string
  nullable    = false
}

variable "permission_groups" {
  description = "The permission groups the token holds, by their names in the API's list (such as Workers AI Read)."
  type        = set(string)
  nullable    = false

  validation {
    condition     = length(var.permission_groups) > 0
    error_message = "A token holds at least one permission group."
  }
}

variable "resources" {
  description = "The policy's resources, such as { \"com.cloudflare.api.account.<id>\" = \"*\" }."
  type        = map(string)
  nullable    = false
}
