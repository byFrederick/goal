# One account-owned API token with the given permission groups on the given resources. The groups
# are looked up by their names, as the API's permission-group list names them, so no opaque id is
# written down here.
data "cloudflare_account_api_token_permission_groups_list" "all" {
  account_id = var.account_id
}

locals {
  permission_group_ids = {
    for name in var.permission_groups : name => one([
      for group in data.cloudflare_account_api_token_permission_groups_list.all.result : group.id
      if group.name == name
    ])
  }
  unknown_permission_groups = sort([for name, id in local.permission_group_ids : name if id == null])
}

resource "cloudflare_account_token" "this" {
  account_id = var.account_id
  name       = var.name

  policies = [{
    effect            = "allow"
    permission_groups = [for name in sort(var.permission_groups) : { id = local.permission_group_ids[name] }]
    resources         = jsonencode(var.resources)
  }]

  lifecycle {
    precondition {
      condition     = length(local.unknown_permission_groups) == 0
      error_message = "No account token permission group is named ${join(", ", local.unknown_permission_groups)}."
    }
  }
}
