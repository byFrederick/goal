# The managed PostgreSQL (PlanetScale Postgres, spec 4 §5.12) and every credential Goal's
# processes connect with.
#
# The database and its production branch already exist (made in the Cloudflare dashboard, billed
# through Cloudflare): the branch is adopted by the import below and protected, never recreated.
# The administrator is a Terraform-managed role inheriting `postgres` (CREATEROLE, CREATEDB,
# BYPASSRLS, no superuser); goal db init runs as it. Its password exists only in the state and the
# Secrets Store. goal_owner, goal_app and goal_auth are PostgreSQL roles goal db init creates, with
# the passwords generated here, and every URL is built from them by modules/database-urls.
import {
  # Adopts the existing branch; delete this block after the apply that imported it.
  to = planetscale_postgres_branch.main
  id = jsonencode({
    organization = local.settings.planetscale.organization
    database     = local.settings.planetscale.database
    id           = local.settings.planetscale.branch
  })
}

resource "planetscale_postgres_branch" "main" {
  organization = local.settings.planetscale.organization
  database     = local.settings.planetscale.database
  name         = local.settings.planetscale.branch

  lifecycle {
    prevent_destroy = true
  }
}

# goal db init's administrator. Destroying it hands what it owns to postgres first.
resource "planetscale_postgres_branch_role" "admin" {
  organization    = local.settings.planetscale.organization
  database        = local.settings.planetscale.database
  branch          = planetscale_postgres_branch.main.name
  name            = "goal_db_admin"
  inherited_roles = ["postgres"]
  successor       = "postgres"
}

# Everything Goal generates for itself. Rotating one is
# `task tf:apply -- -replace='random_password.generated["<name>"]'` (runbook).
resource "random_password" "generated" {
  for_each = toset([
    "gateway_key", "sandbox_key", "auth_proxy_secret", "better_auth_secret",
    "goal_owner", "goal_app", "goal_auth",
  ])

  length  = 48
  special = false
}

module "database_urls" {
  source = "./modules/database-urls"

  host     = regex("^(?:[a-z]+://)?([^/:?]+)", planetscale_postgres_branch_role.admin.access_host_url)[0]
  database = local.settings.postgres_database
  admin = {
    username = planetscale_postgres_branch_role.admin.username
    password = planetscale_postgres_branch_role.admin.password
  }
  # The username is <role>.<branch-id>: every other role logs in with the same branch id.
  branch_id = trimprefix(planetscale_postgres_branch_role.admin.username, "${planetscale_postgres_branch_role.admin.base_username}.")
  passwords = {
    owner = random_password.generated["goal_owner"].result
    app   = random_password.generated["goal_app"].result
    auth  = random_password.generated["goal_auth"].result
  }
  goald_pool_max_conns = local.settings.goald_pool_max_conns
}
