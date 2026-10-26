# GitHub Actions' keyless access to Google (ci.tf, ADR 0010) planned against mock providers: only
# byFrederick/goal's one environment, production, gets in, and it impersonates only terraform-apply.

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

mock_provider "google" {
  # What Google would return for the pool and the service account, so the bindings are known at
  # plan time.
  override_resource {
    target          = google_iam_workload_identity_pool.github
    override_during = plan
    values = {
      workload_identity_pool_id = "github-actions"
      name                      = "projects/123456789012/locations/global/workloadIdentityPools/github-actions"
    }
  }

  override_resource {
    target          = google_iam_workload_identity_pool_provider.github
    override_during = plan
    values = {
      name = "projects/123456789012/locations/global/workloadIdentityPools/github-actions/providers/goal-repository"
    }
  }

  override_resource {
    target          = google_service_account.terraform_apply
    override_during = plan
    values = {
      name   = "projects/goal-byfrederick/serviceAccounts/terraform-apply@goal-byfrederick.iam.gserviceaccount.com"
      email  = "terraform-apply@goal-byfrederick.iam.gserviceaccount.com"
      member = "serviceAccount:terraform-apply@goal-byfrederick.iam.gserviceaccount.com"
    }
  }

  override_resource {
    target          = google_project_iam_custom_role.terraform_apply
    override_during = plan
    values          = { name = "projects/goal-byfrederick/roles/terraformApply" }
  }
}

mock_provider "random" {
  override_resource {
    target          = random_password.generated
    override_during = plan
    values          = { result = "generated0000000000000000000000000000000000000000" }
  }
}

mock_provider "planetscale" {
  # The branch is adopted, never created; a mock can only stand in for what it would read.
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

variables {
  pool = "projects/123456789012/locations/global/workloadIdentityPools/github-actions"
}

run "only_this_repository_and_its_one_environment_gets_a_token" {
  command = plan

  assert {
    condition = google_iam_workload_identity_pool_provider.github.attribute_condition == join(" && ", [
      "assertion.repository_id == '1431390907'",
      "assertion.repository_owner_id == '43914037'",
      "assertion.environment == 'production'",
    ])
    error_message = "The provider must admit byFrederick/goal by repository and owner id, from production only."
  }

  assert {
    condition     = google_iam_workload_identity_pool_provider.github.oidc[0].issuer_uri == "https://token.actions.githubusercontent.com"
    error_message = "The provider must trust GitHub Actions' OIDC issuer only."
  }

  assert {
    condition = alltrue([
      for attribute in ["attribute.repository_id", "attribute.repository_owner_id", "attribute.environment"] :
      contains(keys(google_iam_workload_identity_pool_provider.github.attribute_mapping), attribute)
    ])
    error_message = "The provider must map the attributes the condition and the bindings use."
  }

  assert {
    condition = alltrue([
      for service in ["iam.googleapis.com", "iamcredentials.googleapis.com", "sts.googleapis.com"] :
      contains(keys(google_project_service.ci), service) && google_project_service.ci[service].project == "goal-byfrederick"
    ])
    error_message = "The sign-in project must enable iam, iamcredentials and sts."
  }
}

run "only_production_impersonates_terraform_apply_with_the_custom_role" {
  command = plan

  assert {
    condition     = google_service_account_iam_binding.terraform_apply.role == "roles/iam.workloadIdentityUser" && google_service_account_iam_binding.terraform_apply.members == toset(["principalSet://iam.googleapis.com/${var.pool}/attribute.environment/production"])
    error_message = "terraform-apply must be impersonable from production and nothing else."
  }

  assert {
    condition     = google_service_account_iam_binding.terraform_apply.service_account_id == google_service_account.terraform_apply.name
    error_message = "The production binding must be on terraform-apply."
  }

  assert {
    condition     = google_project_iam_member.terraform_apply.role == "projects/goal-byfrederick/roles/terraformApply" && google_project_iam_member.terraform_apply.member == "serviceAccount:terraform-apply@goal-byfrederick.iam.gserviceaccount.com"
    error_message = "terraform-apply must get the custom role, never a basic role."
  }

  assert {
    condition     = !anytrue([for permission in google_project_iam_custom_role.terraform_apply.permissions : startswith(permission, "iam.serviceAccountKeys.")])
    error_message = "terraform-apply must not make service account keys."
  }
}

run "the_workflows_get_the_provider_and_the_apply_account" {
  command = plan

  assert {
    condition     = output.ci_workload_identity_provider == "${var.pool}/providers/goal-repository"
    error_message = "ci_workload_identity_provider must be the provider's full resource name."
  }

  assert {
    condition     = output.ci_apply_service_account == "terraform-apply@goal-byfrederick.iam.gserviceaccount.com"
    error_message = "ci_apply_service_account must be terraform-apply's email."
  }
}
