# What the control Worker's wrangler config (deploy/cloudflare) and the operator need: names and
# ids, plus the sensitive values task db:production hands goal db (shown only by
# `terraform output -raw <name>`, never by plan or apply).

# Cloudflare: account
output "account_id" {
  description = "The Cloudflare account that holds the zone, the bucket, the Secrets Store and the Worker."
  value       = local.account_id
}

# Cloudflare: routing
output "public_url" {
  description = "The one origin people open Goal at (GOAL_PUBLIC_URL of goald and the auth service)."
  value       = local.public_url
}

output "worker_name" {
  description = "The Worker the route sends every public path to: the name in deploy/cloudflare's wrangler config."
  value       = local.settings.worker
}

output "worker_route" {
  description = "The Workers route this stack owns; the wrangler config declares none."
  value       = cloudflare_workers_route.goal.pattern
}

# Cloudflare: storage
output "bucket" {
  description = "The R2 bucket goald uses through s3store (GOAL_S3_BUCKET)."
  value       = cloudflare_r2_bucket.main.name
}

output "s3_endpoint" {
  description = "The bucket's S3 API endpoint (GOAL_S3_ENDPOINT); R2's region is auto."
  value       = "https://${local.account_id}.r2.cloudflarestorage.com"
}

# Cloudflare: secrets
output "secrets_store_id" {
  description = "The account's Secrets Store: the store_id of every wrangler secrets_store_secrets binding."
  value       = data.cloudflare_secrets_store.main.id
}

output "secret_names" {
  description = "Each platform secret's name in the store, by the environment variable its consumers read."
  value       = module.secrets.names
}

output "secret_consumers" {
  description = "Which environment variables each consumer (goald, auth, worker) gets from the store."
  value       = local.secret_consumers
}

# PlanetScale
output "database_admin_role" {
  description = "goal db init's administrator role on the branch (its base user name, without the branch suffix)."
  value       = planetscale_postgres_branch_role.admin.base_username
}

# Operator: what task db:production hands goal db (sensitive; terraform output -raw <name>)
output "goal_database_admin_url" {
  description = "GOAL_DATABASE_ADMIN_URL: the administrator on Goal's database."
  value       = module.database_urls.admin_url
  sensitive   = true
}

output "goal_migrate_database_url" {
  description = "GOAL_MIGRATE_DATABASE_URL: goal_owner on Goal's database."
  value       = module.database_urls.migrate_url
  sensitive   = true
}

output "goal_pg_owner_password" {
  description = "GOAL_PG_OWNER_PASSWORD, which goal db init gives goal_owner."
  value       = random_password.generated["goal_owner"].result
  sensitive   = true
}

output "goal_pg_app_password" {
  description = "GOAL_PG_APP_PASSWORD, which goal db init gives goal_app."
  value       = random_password.generated["goal_app"].result
  sensitive   = true
}

output "goal_pg_auth_password" {
  description = "GOAL_PG_AUTH_PASSWORD, which goal db init gives goal_auth."
  value       = random_password.generated["goal_auth"].result
  sensitive   = true
}

# GitHub Actions: the deploy's credential (task deploy:token puts it into the production environment)
output "deploy_api_token" {
  description = "GOAL_DEPLOY_API_TOKEN: the account token wrangler deploy uses, nothing more (tokens.tf)."
  value       = module.deploy_token.value
  sensitive   = true
}

# Google: GitHub Actions (ci.tf); variables of the GitHub environments, not secrets
output "ci_workload_identity_provider" {
  description = "workload_identity_provider of google-github-actions/auth in both GitHub environments."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "ci_plan_service_account" {
  description = "service_account of google-github-actions/auth in the production-plan GitHub environment."
  value       = google_service_account.terraform_plan.email
}

output "ci_apply_service_account" {
  description = "service_account of google-github-actions/auth in the production GitHub environment."
  value       = google_service_account.terraform_apply.email
}

# Google: sign-in
output "google_project_id" {
  description = "The Google Cloud project of the sign-in OAuth client (console steps in the runbook)."
  value       = google_project.sign_in.project_id
}

output "google_redirect_uri" {
  description = "The authorized redirect URI of the OAuth web client."
  value       = "${local.public_url}/api/auth/callback/google"
}
