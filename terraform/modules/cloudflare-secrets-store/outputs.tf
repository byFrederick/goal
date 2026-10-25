output "names" {
  description = "Each secret's name in the store, by its environment variable name: what a wrangler secrets_store_secrets binding names."
  value       = { for env, secret in cloudflare_secrets_store_secret.this : env => secret.name }
}

output "ids" {
  description = "Each secret's id in the store, by its environment variable name."
  value       = { for env, secret in cloudflare_secrets_store_secret.this : env => secret.id }
}
