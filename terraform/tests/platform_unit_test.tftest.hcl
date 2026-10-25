# The production stack planned against mock providers: no Cloudflare, PlanetScale or Google
# credentials and no age key needed (task tf:test runs it in the production workspace). The SOPS
# file, the generated passwords, the PlanetScale role and the tokens get test values.

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

mock_provider "random" {
  override_resource {
    target          = random_password.generated
    override_during = plan
    values          = { result = "generated0000000000000000000000000000000000000000" }
  }
}

mock_provider "planetscale" {
  # The import block adopts the branch; a mock can only stand in for what it would read.
  override_resource {
    target          = planetscale_postgres_branch.main
    override_during = plan
    values          = { id = "br4nch", organization = "freddycruz5227", database = "goal", name = "main" }
  }

  override_resource {
    target          = planetscale_postgres_branch_role.admin
    override_during = plan
    values = {
      username        = "pscale_api_test.br4nch"
      base_username   = "pscale_api_test"
      password        = "pscale_pw_test+/="
      access_host_url = "db.example.com"
    }
  }
}

mock_provider "sops" {
  override_data {
    target = data.sops_file.secrets
    values = {
      raw = <<-EOT
        master_key:
          GOAL_MASTER_KEYS: k1:test-master-key
        google:
          GOOGLE_CLIENT_ID: test-client.apps.googleusercontent.com
          GOOGLE_CLIENT_SECRET: test-client-secret
        smtp:
          GOAL_SMTP_URL: smtps://goal:test-smtp@smtp.example.com:465
      EOT
    }
  }
}

override_module {
  target  = module.r2_token
  outputs = { id = "r2-token-id", value = "r2-token-value" }
}

override_module {
  target  = module.workers_ai_token
  outputs = { id = "ai-token-id", value = "ai-token-value" }
}

run "every_secret_reaches_the_store_under_its_env_name" {
  command = plan

  assert {
    condition     = length(module.secrets.names) == 18
    error_message = "Want 18 store secrets, got ${length(module.secrets.names)}."
  }

  assert {
    condition     = module.secrets.names["GOAL_MASTER_KEYS"] == "goal-production-GOAL_MASTER_KEYS"
    error_message = "Store secrets are named goal-production-<ENV NAME>."
  }

  assert {
    condition     = output.secrets_store_id == "5ec7e75000000000000000000000000e"
    error_message = "The account's one store holds the secrets."
  }
}

run "each_container_gets_only_its_own_secrets" {
  command = plan

  assert {
    condition     = length(setintersection(output.secret_consumers.goald, ["BETTER_AUTH_SECRET", "GOOGLE_CLIENT_SECRET", "GOAL_DATABASE_ADMIN_URL"])) == 0
    error_message = "goald must never hold the auth service's secrets or the database administrator."
  }

  assert {
    condition     = length(setintersection(output.secret_consumers.auth, ["GOAL_MASTER_KEYS", "GOAL_DATABASE_URL", "GOAL_DATABASE_ADMIN_URL"])) == 0
    error_message = "The auth service must never hold goald's secrets or the database administrator."
  }

  assert {
    condition     = output.secret_consumers.worker == ["GOAL_SANDBOX_KEY"]
    error_message = "The control Worker itself reads only the sandbox key."
  }
}

run "the_database_urls_follow_the_planetscale_rules" {
  command = plan

  assert {
    condition     = local.secret_values.GOAL_DATABASE_ADMIN_URL == "postgres://pscale_api_test.br4nch:pscale_pw_test%2B%2F%3D@db.example.com:5432/goal?sslmode=verify-full&sslrootcert=system"
    error_message = "The admin URL: the Terraform-managed role's own user and password (URL-encoded), port 5432, database goal, verify-full."
  }

  assert {
    condition     = local.secret_values.GOAL_DATABASE_URL == "postgres://goal_app.br4nch:generated0000000000000000000000000000000000000000@db.example.com:5432/goal?sslmode=verify-full&sslrootcert=system&pool_max_conns=8"
    error_message = "goald's URL: goal_app.<branch>, port 5432, database goal, verify-full against the system CAs, pool capped at 8."
  }

  assert {
    condition     = local.secret_values.GOAL_MIGRATE_DATABASE_URL == "postgres://goal_owner.br4nch:generated0000000000000000000000000000000000000000@db.example.com:5432/goal?sslmode=verify-full&sslrootcert=system"
    error_message = "The migrations' URL: goal_owner.<branch>, verify-full against the system CAs."
  }

  assert {
    condition     = local.secret_values.GOAL_AUTH_DATABASE_URL == "postgres://goal_auth.br4nch:generated0000000000000000000000000000000000000000@db.example.com:5432/goal?sslmode=verify-full"
    error_message = "The auth service's URL: goal_auth.<branch>, verify-full without sslrootcert (node-postgres reads it as a file)."
  }

  assert {
    condition     = local.secret_values.GOAL_PG_APP_PASSWORD == random_password.generated["goal_app"].result
    error_message = "goal db init sets the very password goald's URL carries."
  }
}

run "the_existing_branch_is_adopted_and_the_admin_inherits_postgres" {
  command = plan

  assert {
    condition     = planetscale_postgres_branch.main.organization == "freddycruz5227" && planetscale_postgres_branch.main.database == "goal" && planetscale_postgres_branch.main.name == "main"
    error_message = "The branch is the existing goal database's main branch."
  }

  assert {
    condition     = planetscale_postgres_branch_role.admin.inherited_roles == toset(["postgres"]) && planetscale_postgres_branch_role.admin.successor == "postgres"
    error_message = "The administrator inherits postgres and hands what it owns to postgres when dropped."
  }
}

run "r2_keys_come_from_the_bucket_scoped_token" {
  command = plan

  assert {
    condition     = local.secret_values.GOAL_S3_ACCESS_KEY == "r2-token-id" && local.secret_values.GOAL_S3_SECRET_KEY == sha256("r2-token-value")
    error_message = "R2's S3 access key id is the token id, its secret the SHA-256 of the token value."
  }

  assert {
    condition     = local.secret_values.GOAL_EMBED_KEY == "ai-token-value"
    error_message = "The embedder's key is the Workers AI token."
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
