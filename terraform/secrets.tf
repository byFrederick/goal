# Platform secrets: SOPS -> Cloudflare Secrets Store (spec 4 §5.12, §11.1).
#
# secrets/<workspace>.sops.yaml is committed, encrypted with age (.sops.yaml). Its top-level keys
# group values by what they belong to; the nested keys are the environment variable names their
# consumers read. Provider tokens are never in it (providers.tf). Organization credentials are
# never platform secrets: they are connections, sealed in PostgreSQL under GOAL_MASTER_KEYS.
#
# The Secrets Store holds only what a container needs at run time. The database section is the
# operator's: goal db init's admin URL and the three role passwords, from which the database URLs
# are built (database-urls); the admin URL and the passwords themselves never leave the SOPS file.
data "sops_file" "secrets" {
  source_file = "${path.module}/secrets/${local.environment}.sops.yaml"
}

locals {
  sops = yamldecode(data.sops_file.secrets.raw)

  # The one place that knows the SOPS file's shape. Every key here becomes a store secret.
  secret_values = {
    GOAL_MASTER_KEYS          = local.sops.master_key.GOAL_MASTER_KEYS
    GOAL_GATEWAY_KEY          = local.sops.internal.GOAL_GATEWAY_KEY
    GOAL_SANDBOX_KEY          = local.sops.internal.GOAL_SANDBOX_KEY
    GOAL_AUTH_PROXY_SECRET    = local.sops.internal.GOAL_AUTH_PROXY_SECRET
    BETTER_AUTH_SECRETS       = local.sops.better_auth.BETTER_AUTH_SECRETS
    GOOGLE_CLIENT_ID          = local.sops.google.GOOGLE_CLIENT_ID
    GOOGLE_CLIENT_SECRET      = local.sops.google.GOOGLE_CLIENT_SECRET
    GOAL_SMTP_URL             = local.sops.smtp.GOAL_SMTP_URL
    GOAL_S3_ACCESS_KEY        = local.sops.r2.GOAL_S3_ACCESS_KEY
    GOAL_S3_SECRET_KEY        = local.sops.r2.GOAL_S3_SECRET_KEY
    GOAL_EMBED_KEY            = local.sops.embedding.GOAL_EMBED_KEY
    GOAL_DATABASE_URL         = module.database_urls.app_url
    GOAL_MIGRATE_DATABASE_URL = module.database_urls.migrate_url
    GOAL_AUTH_DATABASE_URL    = module.database_urls.auth_url
  }

  # Who reads which secret, as environment variables: the control Worker passes each container
  # exactly its own (spec 4 §11.1) and uses GOAL_SANDBOX_KEY itself to check goald's sandbox calls.
  # goald never gets the auth service's secrets, and no box gets any of them.
  secret_consumers = {
    goald = [
      "GOAL_MASTER_KEYS", "GOAL_GATEWAY_KEY", "GOAL_SANDBOX_KEY", "GOAL_AUTH_PROXY_SECRET",
      "GOAL_DATABASE_URL", "GOAL_MIGRATE_DATABASE_URL", "GOAL_S3_ACCESS_KEY", "GOAL_S3_SECRET_KEY",
      "GOAL_EMBED_KEY",
    ]
    auth = [
      "BETTER_AUTH_SECRETS", "GOAL_AUTH_PROXY_SECRET", "GOAL_AUTH_DATABASE_URL",
      "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOAL_SMTP_URL",
    ]
    worker = ["GOAL_SANDBOX_KEY"]
  }

  secret_names   = toset(nonsensitive(keys(local.secret_values)))
  consumed_names = toset(flatten(values(local.secret_consumers)))
}

module "database_urls" {
  source = "./modules/database-urls"

  admin_url = local.sops.database.GOAL_DATABASE_ADMIN_URL
  passwords = {
    owner = local.sops.database.GOAL_PG_OWNER_PASSWORD
    app   = local.sops.database.GOAL_PG_APP_PASSWORD
    auth  = local.sops.database.GOAL_PG_AUTH_PASSWORD
  }
  goald_pool_max_conns = local.settings.goald_pool_max_conns
}

module "secrets" {
  source = "./modules/cloudflare-secrets-store"

  account_id  = local.account_id
  store_id    = data.cloudflare_secrets_store.main.id
  name_prefix = local.project_name_env
  secrets     = local.secret_values
  comment     = local.managed_by
}

check "every_secret_has_a_consumer" {
  assert {
    condition     = length(setsubtract(local.secret_names, local.consumed_names)) == 0
    error_message = "Secrets no consumer reads (drop them or add them to local.secret_consumers): ${join(", ", sort(setsubtract(local.secret_names, local.consumed_names)))}."
  }
}

check "every_consumer_has_its_secrets" {
  assert {
    condition     = length(setsubtract(local.consumed_names, local.secret_names)) == 0
    error_message = "Consumers name secrets that do not exist (add them to local.secret_values and the SOPS file): ${join(", ", sort(setsubtract(local.consumed_names, local.secret_names)))}."
  }
}
