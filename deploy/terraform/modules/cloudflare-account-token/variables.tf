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

variable "permission_group" {
  description = "The one permission group the token holds, by name (such as Workers AI Read)."
  type        = string
  nullable    = false
}

variable "resources" {
  description = "The policy's resources, such as { \"com.cloudflare.api.account.<id>\" = \"*\" }."
  type        = map(string)
  nullable    = false
}
