# modules/cloudflare-account-token refuses a permission group name the API's list does not have
# before any token is made. The mock's list holds no group (a mock cannot fill the data source's
# nested result list), so every name is unknown; the owner's plan checks the real names.

mock_provider "cloudflare" {}

run "an_unknown_group_is_refused" {
  command = plan

  module {
    source = "./modules/cloudflare-account-token"
  }

  variables {
    account_id        = "fedcba9876543210fedcba9876543210"
    name              = "goal-production-test"
    permission_groups = ["Containers Write"]
    resources         = { "com.cloudflare.api.account.fedcba9876543210fedcba9876543210" = "*" }
  }

  expect_failures = [cloudflare_account_token.this]
}
