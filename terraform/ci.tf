# GitHub Actions reach Google with no key (ADR 0010): Workload Identity Federation in the sign-in
# project trusts GitHub's OIDC tokens from this repository's two environments, and only them. A
# job in `production-plan` impersonates terraform-plan (project viewer); a job in `production`,
# deployable from main only, impersonates terraform-apply. google-github-actions/auth exchanges the
# job's token for the service account's access token (token_format: access_token), which the tf:*
# tasks take as GOOGLE_OAUTH_ACCESS_TOKEN when GITHUB_ACTIONS=true (Taskfile.yml).
#
# Bootstrap: the owner applies these resources once from a laptop (runbook, bootstrap step 13);
# no workflow can plan before they exist.

locals {
  # What the federation needs (iam, iamcredentials, sts), and what the provider calls as a service
  # account of this project, whose quota is then this project's: the project and its billing info
  # (google_project reads both) and the API list. None of them needs a billing account.
  ci_services = toset([
    "cloudbilling.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "serviceusage.googleapis.com",
    "sts.googleapis.com",
  ])

  # The token must come from this repository (by id, so a renamed or recreated one with the same
  # name is refused) and from one of its two environments: a job without an environment, a fork's
  # pull request or another repository gets nothing.
  ci_attribute_condition = join(" && ", [
    "assertion.repository_id == '${local.settings.github.repository_id}'",
    "assertion.repository_owner_id == '${local.settings.github.owner_id}'",
    "assertion.environment in ['${local.settings.github.environments.plan}', '${local.settings.github.environments.apply}']",
  ])

  # What terraform-apply may do in the project: exactly what this configuration manages there (the
  # project's name and labels, these APIs, the pool, the service accounts, their bindings and this
  # role). It can change the project's IAM, so it is as strong as an owner of this one project,
  # which holds only the sign-in client and this trust; `production`'s rules guard it.
  ci_apply_permissions = [
    "iam.roles.create",
    "iam.roles.delete",
    "iam.roles.get",
    "iam.roles.list",
    "iam.roles.undelete",
    "iam.roles.update",
    "iam.serviceAccounts.create",
    "iam.serviceAccounts.delete",
    "iam.serviceAccounts.get",
    "iam.serviceAccounts.getIamPolicy",
    "iam.serviceAccounts.list",
    "iam.serviceAccounts.setIamPolicy",
    "iam.serviceAccounts.update",
    "iam.workloadIdentityPoolProviders.create",
    "iam.workloadIdentityPoolProviders.delete",
    "iam.workloadIdentityPoolProviders.get",
    "iam.workloadIdentityPoolProviders.list",
    "iam.workloadIdentityPoolProviders.undelete",
    "iam.workloadIdentityPoolProviders.update",
    "iam.workloadIdentityPools.create",
    "iam.workloadIdentityPools.delete",
    "iam.workloadIdentityPools.get",
    "iam.workloadIdentityPools.list",
    "iam.workloadIdentityPools.undelete",
    "iam.workloadIdentityPools.update",
    "resourcemanager.projects.get",
    "resourcemanager.projects.getIamPolicy",
    "resourcemanager.projects.setIamPolicy",
    "resourcemanager.projects.update",
    "serviceusage.services.enable",
    "serviceusage.services.get",
    "serviceusage.services.list",
  ]
}

resource "google_project_service" "ci" {
  for_each = local.ci_services

  project = google_project.sign_in.project_id
  service = each.key

  # Turning an API off would break the trust under a running workflow; removing it from here
  # leaves it on.
  disable_on_destroy = false
}

resource "google_iam_workload_identity_pool" "github" {
  project                   = google_project.sign_in.project_id
  workload_identity_pool_id = "github-actions"
  display_name              = "GitHub Actions"
  description               = "OIDC tokens of ${local.settings.github.repository}'s workflows (${local.managed_by})."

  depends_on = [google_project_service.ci]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = google_project.sign_in.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "goal-repository"
  display_name                       = "GitHub ${local.settings.github.repository}"
  description                        = "Only ${local.settings.github.repository}'s ${local.settings.github.environments.plan} and ${local.settings.github.environments.apply} environments."

  attribute_mapping = {
    "google.subject"                = "assertion.sub"
    "attribute.repository_id"       = "assertion.repository_id"
    "attribute.repository_owner_id" = "assertion.repository_owner_id"
    "attribute.environment"         = "assertion.environment"
  }
  attribute_condition = local.ci_attribute_condition

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

# Plan: reads the project and nothing else.
resource "google_service_account" "terraform_plan" {
  project      = google_project.sign_in.project_id
  account_id   = "terraform-plan"
  display_name = "Terraform plan (GitHub ${local.settings.github.environments.plan})"
  description  = "Plans terraform/ from pull requests: project viewer (${local.managed_by})."

  depends_on = [google_project_service.ci]
}

resource "google_project_iam_member" "terraform_plan" {
  project = google_project.sign_in.project_id
  role    = "roles/viewer"
  member  = google_service_account.terraform_plan.member
}

# Apply: exactly what this configuration manages in the project.
resource "google_project_iam_custom_role" "terraform_apply" {
  project     = google_project.sign_in.project_id
  role_id     = "terraformApply"
  title       = "Terraform apply"
  description = "What terraform/ manages in this project (${local.managed_by})."
  permissions = local.ci_apply_permissions

  depends_on = [google_project_service.ci]
}

resource "google_service_account" "terraform_apply" {
  project      = google_project.sign_in.project_id
  account_id   = "terraform-apply"
  display_name = "Terraform apply (GitHub ${local.settings.github.environments.apply})"
  description  = "Applies terraform/ on merge to main (${local.managed_by})."

  depends_on = [google_project_service.ci]
}

resource "google_project_iam_member" "terraform_apply" {
  project = google_project.sign_in.project_id
  role    = google_project_iam_custom_role.terraform_apply.name
  member  = google_service_account.terraform_apply.member
}

# Who may impersonate each account: authoritative for the role, so the one environment and no
# one else.
resource "google_service_account_iam_binding" "terraform_plan" {
  service_account_id = google_service_account.terraform_plan.name
  role               = "roles/iam.workloadIdentityUser"
  members            = ["principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.environment/${local.settings.github.environments.plan}"]
}

resource "google_service_account_iam_binding" "terraform_apply" {
  service_account_id = google_service_account.terraform_apply.name
  role               = "roles/iam.workloadIdentityUser"
  members            = ["principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.environment/${local.settings.github.environments.apply}"]
}
