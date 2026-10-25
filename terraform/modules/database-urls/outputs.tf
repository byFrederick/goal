output "app_url" {
  description = "goald's URL as goal_app (GOAL_DATABASE_URL), its pool capped."
  value       = "postgres://goal_app.${local.admin.branch}:${var.passwords.app}@${local.base}?${local.go_tls}&pool_max_conns=${var.goald_pool_max_conns}"
  sensitive   = true
}

output "migrate_url" {
  description = "goald's URL as goal_owner, for the migrations it runs at startup (GOAL_MIGRATE_DATABASE_URL)."
  value       = "postgres://goal_owner.${local.admin.branch}:${var.passwords.owner}@${local.base}?${local.go_tls}"
  sensitive   = true
}

output "auth_url" {
  description = "The auth service's URL as goal_auth (GOAL_AUTH_DATABASE_URL), for node-postgres: no sslrootcert."
  value       = "postgres://goal_auth.${local.admin.branch}:${var.passwords.auth}@${local.base}?sslmode=verify-full"
  sensitive   = true
}
