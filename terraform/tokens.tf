# The Cloudflare API tokens goald and the deploy use, each narrowed to what it needs, made here so
# no one copies a token by hand. Account-owned, so they outlive any person's user. Rotating one is
# `task tf:apply -- -replace='module.<name>.cloudflare_account_token.this'`, then a goald restart
# (or, for the deploy token, `task deploy:token`).

# s3store's keys for the bucket, objects only: R2's S3 access key id is the token's id and its
# secret access key the SHA-256 of the token's value.
module "r2_token" {
  source = "./modules/cloudflare-account-token"

  account_id        = local.account_id
  name              = "${local.project_name_env}-r2"
  permission_groups = ["Workers R2 Storage Bucket Item Write"]
  resources = {
    "com.cloudflare.edge.r2.bucket.${local.account_id}_default_${cloudflare_r2_bucket.main.name}" = "*"
  }
}

# The embedder's bearer key for Workers AI (GOAL_EMBED_URL .../accounts/<id>/ai/v1, @cf/baai/bge-m3).
module "workers_ai_token" {
  source = "./modules/cloudflare-account-token"

  account_id        = local.account_id
  name              = "${local.project_name_env}-workers-ai"
  permission_groups = ["Workers AI Read"]
  resources = {
    "com.cloudflare.api.account.${local.account_id}" = "*"
  }
}

# What `wrangler deploy` of deploy/cloudflare needs and nothing more, by the names in the API's
# permission-group list (the dashboard calls Write "Edit"):
# - Workers Scripts Write: upload the control Worker, its vars and its Durable Object migrations.
# - Workers Containers Write: create and update the goald, auth and goal-agent container
#   applications and push their images to Cloudflare's registry (the only registry login).
# - Account Settings Read: wrangler reads the account it deploys to.
# - Secrets Store Write: binding a Secrets Store secret to a Worker counts as a write against the
#   secret, and a token with Read fails at deploy time. It also lets the token edit or delete
#   every secret in the account's store, so it lives only in the production environment (main).
locals {
  deploy_token_permission_groups = toset([
    "Workers Scripts Write",
    "Workers Containers Write",
    "Account Settings Read",
    "Secrets Store Write",
  ])
}

# The deploy's token (GitHub environment secret GOAL_DEPLOY_API_TOKEN, set by task deploy:token):
# never Terraform's CLOUDFLARE_API_TOKEN, and handed to no container.
module "deploy_token" {
  source = "./modules/cloudflare-account-token"

  account_id        = local.account_id
  name              = "${local.project_name_env}-deploy"
  permission_groups = local.deploy_token_permission_groups
  resources = {
    "com.cloudflare.api.account.${local.account_id}" = "*"
  }
}
