variable "admin_url" {
  description = <<-EOT
    The database administrator's URL, as goal db init uses it (GOAL_DATABASE_ADMIN_URL): the
    managed PostgreSQL's credential, on Goal's own database. On PlanetScale Postgres its user is
    <role>.<branch-id>, and the other roles' users take the same branch id.
  EOT
  type        = string
  sensitive   = true
  nullable    = false

  validation {
    condition     = can(regex("^postgres(ql)?://[^.:@/]+\\.[^:@/]+:[^@]+@", var.admin_url))
    error_message = "The admin URL needs a user <role>.<branch-id> and its password: postgres://<role>.<branch-id>:<password>@<host>:5432/goal?sslmode=verify-full."
  }

  validation {
    condition     = can(regex("@[^:/?]+:5432/", var.admin_url))
    error_message = "The admin URL must name port 5432, the direct connection: PgBouncer's 6432 drops LISTEN notifications and session advisory locks (spec 4 §5.12)."
  }

  validation {
    condition     = can(regex(":5432/goal(\\?|$)", var.admin_url))
    error_message = "The admin URL must name the database goal, Goal's own, created as CREATE DATABASE goal OWNER postgres; never the provider's default database (spec 4 §5.12)."
  }

  validation {
    condition     = can(regex("[?&]sslmode=verify-full(&|$)", var.admin_url))
    error_message = "The admin URL must verify the server: sslmode=verify-full."
  }
}

variable "passwords" {
  description = "The login passwords goal db init gives goal_owner, goal_app and goal_auth (GOAL_PG_OWNER_PASSWORD, GOAL_PG_APP_PASSWORD, GOAL_PG_AUTH_PASSWORD)."
  type = object({
    owner = string
    app   = string
    auth  = string
  })
  sensitive = true
  nullable  = false

  validation {
    condition     = alltrue([for p in [var.passwords.owner, var.passwords.app, var.passwords.auth] : can(regex("^[A-Za-z0-9_-]{32,}$", p))])
    error_message = "Every role password must be at least 32 URL-safe characters (letters, digits, _ and -), such as openssl rand -hex 24, so it goes into a URL unescaped."
  }
}

variable "goald_pool_max_conns" {
  description = "The largest pool goald's connection as goal_app opens (pgxpool's pool_max_conns); its default, the CPU count, can exhaust a small cluster."
  type        = number
  nullable    = false

  validation {
    condition     = var.goald_pool_max_conns >= 2 && floor(var.goald_pool_max_conns) == var.goald_pool_max_conns
    error_message = "goald_pool_max_conns must be a whole number of at least 2."
  }
}
