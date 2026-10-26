# The Cloudflare API tokens goald uses, each narrowed to the one thing it needs, made here so no one
# copies a token by hand. Account-owned, so they outlive any person's user. Rotating one is
# `task tf:apply -- -replace='module.<name>.cloudflare_account_token.this'` and a goald restart.

# s3store's keys for the bucket, objects only: R2's S3 access key id is the token's id and its
# secret access key the SHA-256 of the token's value.
module "r2_token" {
  source = "./modules/cloudflare-account-token"

  account_id       = local.account_id
  name             = "${local.project_name_env}-r2"
  permission_group = "Workers R2 Storage Bucket Item Write"
  resources = {
    "com.cloudflare.edge.r2.bucket.${local.account_id}_default_${cloudflare_r2_bucket.main.name}" = "*"
  }
}

# The embedder's bearer key for Workers AI (GOAL_EMBED_URL .../accounts/<id>/ai/v1, @cf/baai/bge-m3).
module "workers_ai_token" {
  source = "./modules/cloudflare-account-token"

  account_id       = local.account_id
  name             = "${local.project_name_env}-workers-ai"
  permission_group = "Workers AI Read"
  resources = {
    "com.cloudflare.api.account.${local.account_id}" = "*"
  }
}
