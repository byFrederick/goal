# One account-owned API token with one permission group on the given resources. The group is
# looked up by its name, as the dashboard shows it, so no opaque id is written down here.
data "cloudflare_account_api_token_permission_groups_list" "all" {
  account_id = var.account_id
}

locals {
  permission_group_id = one([
    for group in data.cloudflare_account_api_token_permission_groups_list.all.result : group.id
    if group.name == var.permission_group
  ])
}

resource "cloudflare_account_token" "this" {
  account_id = var.account_id
  name       = var.name

  policies = [{
    effect            = "allow"
    permission_groups = [{ id = local.permission_group_id }]
    resources         = jsonencode(var.resources)
  }]

  lifecycle {
    precondition {
      condition     = local.permission_group_id != null
      error_message = "No account token permission group is named ${var.permission_group}."
    }
  }
}
