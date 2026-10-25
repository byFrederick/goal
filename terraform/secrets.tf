# Platform secrets -> Cloudflare Secrets Store (spec 4 §5.12, §11.1).
#
# Every value comes from the one place that makes it, and nobody copies one by hand:
# - Terraform makes what Goal generates for itself (random_password in database.tf), the
#   PlanetScale administrator (database.tf), the database URLs (modules/database-urls) and the
#   Cloudflare tokens (tokens.tf).
# - secrets/<workspace>.sops.yaml, age-encrypted (.sops.yaml), holds only what comes from outside
#   and the master keys: the Google OAuth client (made in Google's console), the mail provider's
#   SMTP URL, and GOAL_MASTER_KEYS. The master keys stay in SOPS on purpose: they must have an
#   escrowed copy that no state loss or damage can take (a lost key loses every organization's
#   credentials), they rotate as a list (k2:<new>,k1:<old>, then goal db rekey), and a generated
#   one would be replaced, unrecoverably, by any -replace or recreation of its resource.
# The state holds every value below in plain text too, the master keys included (data sources and
# store secret values are saved there): the state bucket is as sensitive as the master key.
# Provider tokens are never in either (providers.tf). Organization credentials are never platform
# secrets: they are connections, sealed in PostgreSQL under GOAL_MASTER_KEYS.
# goal db init's administrator and the role passwords are not store secrets: nothing on Cloudflare
# runs goal db init, and any Worker of the account could bind one. They are sensitive outputs, read
# by task db:production only (outputs.tf).
data "sops_file" "secrets" {
  source_file = "${path.module}/secrets/${local.environment}.sops.yaml"
}

locals {
  sops = yamldecode(data.sops_file.secrets.raw)

  # Every key becomes one store secret, named by the environment variable its consumers read.
  secret_values = {
    GOAL_MASTER_KEYS          = local.sops.master_key.GOAL_MASTER_KEYS
    GOOGLE_CLIENT_ID          = local.sops.google.GOOGLE_CLIENT_ID
    GOOGLE_CLIENT_SECRET      = local.sops.google.GOOGLE_CLIENT_SECRET
    GOAL_SMTP_URL             = local.sops.smtp.GOAL_SMTP_URL
    GOAL_GATEWAY_KEY          = random_password.generated["gateway_key"].result
    GOAL_SANDBOX_KEY          = random_password.generated["sandbox_key"].result
    GOAL_AUTH_PROXY_SECRET    = random_password.generated["auth_proxy_secret"].result
    BETTER_AUTH_SECRET        = random_password.generated["better_auth_secret"].result
    GOAL_S3_ACCESS_KEY        = module.r2_token.id
    GOAL_S3_SECRET_KEY        = sha256(module.r2_token.value)
    GOAL_EMBED_KEY            = module.workers_ai_token.value
    GOAL_DATABASE_URL         = module.database_urls.app_url
    GOAL_MIGRATE_DATABASE_URL = module.database_urls.migrate_url
    GOAL_AUTH_DATABASE_URL    = module.database_urls.auth_url
  }

  # Who reads which secret, as environment variables: the control Worker passes each container
  # exactly its own (spec 4 §11.1) and uses GOAL_SANDBOX_KEY itself to check goald's sandbox calls.
  # goald never gets the auth service's secrets, and no box gets any.
  secret_consumers = {
    goald = [
      "GOAL_MASTER_KEYS", "GOAL_GATEWAY_KEY", "GOAL_SANDBOX_KEY", "GOAL_AUTH_PROXY_SECRET",
      "GOAL_DATABASE_URL", "GOAL_MIGRATE_DATABASE_URL", "GOAL_S3_ACCESS_KEY", "GOAL_S3_SECRET_KEY",
      "GOAL_EMBED_KEY",
    ]
    auth = [
      "BETTER_AUTH_SECRET", "GOAL_AUTH_PROXY_SECRET", "GOAL_AUTH_DATABASE_URL",
      "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOAL_SMTP_URL",
    ]
    worker = ["GOAL_SANDBOX_KEY"]
  }

  secret_names   = toset(nonsensitive(keys(local.secret_values)))
  consumed_names = toset(flatten(values(local.secret_consumers)))
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
    error_message = "Consumers name secrets that do not exist (add them to local.secret_values): ${join(", ", sort(setsubtract(local.consumed_names, local.secret_names)))}."
  }
}
