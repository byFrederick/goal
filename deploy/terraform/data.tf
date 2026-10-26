# Account-wide objects this stack uses but does not own.
#
# The zone is the account's, shared with whatever else it serves. The Secrets Store is one per
# account (Cloudflare allows exactly one), so it is read too: create it once if the account has none
# (runbook, bootstrap), and this stack only adds Goal's secrets to it.
data "cloudflare_zone" "main" {
  filter = {
    name = local.settings.zone
  }
}

# Without a store id the data source answers the account's first store, its only one.
data "cloudflare_secrets_store" "main" {
  account_id = local.account_id
  filter = {
    order     = "created"
    direction = "asc"
  }
}
