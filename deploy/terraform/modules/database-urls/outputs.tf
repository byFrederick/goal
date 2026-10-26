output "admin_url" {
  description = "goal db init's URL as the administrator (GOAL_DATABASE_ADMIN_URL)."
  value       = "postgres://${urlencode(var.admin.username)}:${urlencode(var.admin.password)}@${local.server}?${local.go_tls}"
  sensitive   = true
}

output "app_url" {
  description = "goald's URL as goal_app (GOAL_DATABASE_URL), its pool capped."
  value       = "postgres://${local.user.app}@${local.server}?${local.go_tls}&pool_max_conns=${var.goald_pool_max_conns}"
  sensitive   = true
}

output "migrate_url" {
  description = "goald's URL as goal_owner, for the migrations it runs at startup (GOAL_MIGRATE_DATABASE_URL)."
  value       = "postgres://${local.user.owner}@${local.server}?${local.go_tls}"
  sensitive   = true
}

output "auth_url" {
  description = "The auth service's URL as goal_auth (GOAL_AUTH_DATABASE_URL), for node-postgres: no sslrootcert."
  value       = "postgres://${local.user.auth}@${local.server}?sslmode=verify-full"
  sensitive   = true
}
