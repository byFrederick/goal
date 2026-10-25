# modules/cloudflare-secrets-store stores nothing while a value is still an empty placeholder.

mock_provider "cloudflare" {}

variables {
  account_id  = "fedcba9876543210fedcba9876543210"
  store_id    = "5ec7e75000000000000000000000000e"
  name_prefix = "goal-production"
  comment     = "managed by terraform (production)"
  secrets = {
    GOAL_GATEWAY_KEY = "test-gateway-key"
    GOAL_SANDBOX_KEY = "test-sandbox-key"
  }
}

run "secrets_are_workers_scoped_and_prefixed" {
  command = plan

  module {
    source = "./modules/cloudflare-secrets-store"
  }

  assert {
    condition     = cloudflare_secrets_store_secret.this["GOAL_SANDBOX_KEY"].name == "goal-production-GOAL_SANDBOX_KEY"
    error_message = "A secret is named <prefix>-<ENV NAME>."
  }

  assert {
    condition     = cloudflare_secrets_store_secret.this["GOAL_SANDBOX_KEY"].scopes == tolist(["workers"])
    error_message = "Secrets are readable by Worker bindings only."
  }
}

run "an_empty_placeholder_is_refused" {
  command = plan

  module {
    source = "./modules/cloudflare-secrets-store"
  }

  variables {
    secrets = {
      GOAL_GATEWAY_KEY = "test-gateway-key"
      GOAL_SANDBOX_KEY = ""
    }
  }

  expect_failures = [var.secrets]
}

run "a_name_that_is_no_env_name_is_refused" {
  command = plan

  module {
    source = "./modules/cloudflare-secrets-store"
  }

  variables {
    secrets = {
      "gateway-key" = "test-gateway-key"
    }
  }

  expect_failures = [var.secrets]
}
