# The one object storage bucket (AGENTS.md rule 3): factory definitions and uploaded knowledge
# documents as immutable content-addressed blobs. goald reaches it through s3store with an R2 API
# token scoped to this bucket (r2 in the SOPS file). R2 has no object versioning and nothing relies
# on it (ADR 0005 amendment); R2 aborts incomplete multipart uploads after 7 days by default.
# Destroying it would lose every organization's definitions, so it is protected.
resource "cloudflare_r2_bucket" "main" {
  account_id    = local.account_id
  name          = local.project_name_env
  location      = local.settings.r2_location
  storage_class = "Standard"

  lifecycle {
    prevent_destroy = true
  }
}
