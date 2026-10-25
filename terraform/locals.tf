# General
#
# The workspace is the environment. Every per-environment value is a key of local.environments,
# and a workspace without a key fails the plan at local.settings: only `production` exists, so
# nothing is ever created by mistake in `default`. Adding an environment is one key here and one
# SOPS file with its creation rule in .sops.yaml.
locals {
  project_name     = "goal"
  environment      = terraform.workspace
  project_name_env = "${local.project_name}-${local.environment}"
  managed_by       = "managed by terraform (${local.environment})"

  environments = {
    production = {
      # The zone exists already and is account-wide: it is read, never managed here.
      zone = "byfrederick.work"
      host = "goal.byfrederick.work"
      # The control Worker's name in deploy/cloudflare's wrangler config, which wrangler deploys.
      worker = "goal"
      # Eastern North America: next to the PlanetScale database in us-east-1 (spec 4 §5.12).
      r2_location = "enam"
      # Google Cloud project of the sign-in OAuth client; project ids are global and permanent.
      google_project_id = "goal-byfrederick"
      # The one repository whose workflows may plan and apply this stack (ci.tf, ADR 0010), by the
      # ids GitHub puts in its OIDC tokens: a renamed or transferred repository keeps its id, and
      # a new one with the old name gets another. The two GitHub environments of its workflows:
      # plan holds read-only credentials, apply the write ones (deployable from main only).
      github = {
        repository    = "byFrederick/goal"
        repository_id = "1431390907"
        owner_id      = "43914037"
        environments = {
          plan  = "production-plan"
          apply = "production"
        }
      }
      # The PlanetScale Postgres database (PS-5, one node, us-east), made in the Cloudflare
      # dashboard and billed through Cloudflare: its production branch is adopted, never created.
      # Goal's tables live in the PostgreSQL database `goal` on it (CREATE DATABASE goal OWNER
      # postgres, runbook), not in the provider's default `postgres`.
      planetscale = {
        organization = "freddycruz5227"
        database     = "goal"
        branch       = "main"
      }
      postgres_database = "goal"
      # PS-5 allows 22 connections to Goal: goald's pool, its listeners, the auth service's pool
      # (10) and migrations share them (spec 4 §5.12).
      goald_pool_max_conns = 8
    }
  }

  settings   = local.environments[local.environment]
  account_id = data.cloudflare_zone.main.account.id
  public_url = "https://${local.settings.host}"
}
