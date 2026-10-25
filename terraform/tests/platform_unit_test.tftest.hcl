# The production stack planned against mock providers: no Cloudflare, Google or age key needed
# (task tf:test runs it in the production workspace). The SOPS file is replaced by test values.

mock_provider "cloudflare" {
  override_data {
    target = data.cloudflare_zone.main
    values = {
      id      = "0123456789abcdef0123456789abcdef"
      account = { id = "fedcba9876543210fedcba9876543210", name = "acme" }
    }
  }

  override_data {
    target = data.cloudflare_secrets_store.main
    values = { id = "5ec7e75000000000000000000000000e", name = "default_secrets_store" }
  }
}

mock_provider "google" {}

mock_provider "sops" {
  override_data {
    target = data.sops_file.secrets
    values = {
      raw = <<-EOT
        master_key:
          GOAL_MASTER_KEYS: k1:test-master-key
        internal:
          GOAL_GATEWAY_KEY: test-gateway-key
          GOAL_SANDBOX_KEY: test-sandbox-key
          GOAL_AUTH_PROXY_SECRET: test-proxy-secret
        better_auth:
          BETTER_AUTH_SECRETS: 1:test-better-auth-secret
        google:
          GOOGLE_CLIENT_ID: test-client.apps.googleusercontent.com
          GOOGLE_CLIENT_SECRET: test-client-secret
        smtp:
          GOAL_SMTP_URL: smtps://goal:test-smtp@smtp.example.com:465
        database:
          GOAL_DATABASE_ADMIN_URL: postgres://pscale_api_test.br4nch:test-admin@db.example.com:5432/goal?sslmode=verify-full
          GOAL_PG_OWNER_PASSWORD: owner0000000000000000000000000000000
          GOAL_PG_APP_PASSWORD: app00000000000000000000000000000000
          GOAL_PG_AUTH_PASSWORD: auth0000000000000000000000000000000
        r2:
          GOAL_S3_ACCESS_KEY: test-access-key
          GOAL_S3_SECRET_KEY: test-secret-key
        embedding:
          GOAL_EMBED_KEY: test-embed-key
      EOT
    }
  }
}

run "every_secret_reaches_the_store_under_its_env_name" {
  command = plan

  assert {
    condition     = length(module.secrets.names) == 14
    error_message = "Want 14 store secrets, got ${length(module.secrets.names)}."
  }

  assert {
    condition     = module.secrets.names["GOAL_MASTER_KEYS"] == "goal-production-GOAL_MASTER_KEYS"
    error_message = "Store secrets are named goal-production-<ENV NAME>."
  }

  assert {
    condition     = output.secrets_store_id == "5ec7e75000000000000000000000000e"
    error_message = "The account's one store holds the secrets."
  }

  # Neither the admin URL nor the bare passwords leave the SOPS file.
  assert {
    condition = length(setintersection(keys(module.secrets.names), [
      "GOAL_DATABASE_ADMIN_URL", "GOAL_PG_OWNER_PASSWORD", "GOAL_PG_APP_PASSWORD", "GOAL_PG_AUTH_PASSWORD",
    ])) == 0
    error_message = "The operator's database values must not become store secrets."
  }
}

run "each_container_gets_only_its_own_secrets" {
  command = plan

  assert {
    condition     = !contains(output.secret_consumers.goald, "BETTER_AUTH_SECRETS") && !contains(output.secret_consumers.goald, "GOOGLE_CLIENT_SECRET")
    error_message = "goald must never hold the auth service's secrets."
  }

  assert {
    condition     = !contains(output.secret_consumers.auth, "GOAL_MASTER_KEYS") && !contains(output.secret_consumers.auth, "GOAL_DATABASE_URL")
    error_message = "The auth service must never hold goald's secrets."
  }

  assert {
    condition     = output.secret_consumers.worker == ["GOAL_SANDBOX_KEY"]
    error_message = "The control Worker itself reads only the sandbox key."
  }
}

run "the_database_urls_follow_the_planetscale_rules" {
  command = plan

  assert {
    condition     = local.secret_values.GOAL_DATABASE_URL == "postgres://goal_app.br4nch:app00000000000000000000000000000000@db.example.com:5432/goal?sslmode=verify-full&sslrootcert=system&pool_max_conns=8"
    error_message = "goald's URL: goal_app.<branch>, port 5432, database goal, verify-full against the system CAs, pool capped at 8."
  }

  assert {
    condition     = local.secret_values.GOAL_MIGRATE_DATABASE_URL == "postgres://goal_owner.br4nch:owner0000000000000000000000000000000@db.example.com:5432/goal?sslmode=verify-full&sslrootcert=system"
    error_message = "The migrations' URL: goal_owner.<branch>, verify-full against the system CAs."
  }

  assert {
    condition     = local.secret_values.GOAL_AUTH_DATABASE_URL == "postgres://goal_auth.br4nch:auth0000000000000000000000000000000@db.example.com:5432/goal?sslmode=verify-full"
    error_message = "The auth service's URL: goal_auth.<branch>, verify-full without sslrootcert (node-postgres reads it as a file)."
  }
}

run "the_host_routes_to_the_worker" {
  command = plan

  assert {
    condition     = cloudflare_workers_route.goal.pattern == "goal.byfrederick.work/*" && cloudflare_workers_route.goal.script == "goal"
    error_message = "Every path of goal.byfrederick.work goes to the goal Worker."
  }

  assert {
    condition     = cloudflare_dns_record.goal.type == "AAAA" && cloudflare_dns_record.goal.content == "100::" && cloudflare_dns_record.goal.proxied
    error_message = "The host is a proxied AAAA to 100::, the placeholder for a Worker-only host."
  }

  assert {
    condition     = output.google_redirect_uri == "https://goal.byfrederick.work/api/auth/callback/google"
    error_message = "The OAuth client's redirect URI is Better Auth's Google callback under the public URL."
  }
}

run "the_bucket_is_goal_production_near_the_database" {
  command = plan

  assert {
    condition     = cloudflare_r2_bucket.main.name == "goal-production" && cloudflare_r2_bucket.main.location == "enam"
    error_message = "The bucket is goal-production in eastern North America."
  }

  assert {
    condition     = output.s3_endpoint == "https://fedcba9876543210fedcba9876543210.r2.cloudflarestorage.com"
    error_message = "s3store's endpoint is the account's R2 S3 API."
  }
}
