output "id" {
  description = "The token's id (for R2's S3 API, the access key id)."
  value       = cloudflare_account_token.this.id
}

output "value" {
  description = "The token's value (a bearer token; for R2's S3 API, its SHA-256 is the secret access key)."
  value       = cloudflare_account_token.this.value
  sensitive   = true
}
