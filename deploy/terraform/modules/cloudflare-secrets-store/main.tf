# Every Cloudflare Secrets Store secret of the platform, and only this module creates them: the
# rest of the stack and the control Worker see names and ids, never own a secret's lifecycle. A
# secret is readable only through a Worker binding (scope workers); the control Worker passes each
# container exactly its own (spec 4 §5.12). Values are write-only at Cloudflare but kept in the
# Terraform state, which is why the state bucket is the operator's alone.
resource "cloudflare_secrets_store_secret" "this" {
  for_each = toset(nonsensitive(keys(var.secrets)))

  account_id = var.account_id
  store_id   = var.store_id
  name       = "${var.name_prefix}-${each.key}"
  value      = var.secrets[each.key]
  scopes     = ["workers"]
  comment    = var.comment
}
