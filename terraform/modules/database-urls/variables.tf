variable "host" {
  description = "The database's host name, without scheme or port (PlanetScale's access host)."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$", var.host))
    error_message = "host must be a bare host name: no scheme, port or path."
  }
}

variable "database" {
  description = "Goal's own PostgreSQL database on the server (CREATE DATABASE goal OWNER postgres), never the provider's default."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z_][a-z0-9_]*$", var.database)) && var.database != "postgres"
    error_message = "database must be Goal's own database (such as goal), not postgres."
  }
}

variable "branch_id" {
  description = "The branch id every user name carries: <role>.<branch-id>."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9]+$", var.branch_id))
    error_message = "branch_id must be the branch's id (lowercase letters and digits), as the admin's user name ends."
  }
}

variable "admin" {
  description = "The administrator goal db init connects as: its full user name (<role>.<branch-id>) and password."
  type = object({
    username = string
    password = string
  })
  sensitive = true
  nullable  = false
}

variable "passwords" {
  description = "The login passwords goal db init gives goal_owner, goal_app and goal_auth."
  type = object({
    owner = string
    app   = string
    auth  = string
  })
  sensitive = true
  nullable  = false
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
