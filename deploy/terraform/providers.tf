# Provider credentials never come from this repository or from SOPS:
# - cloudflare reads CLOUDFLARE_API_TOKEN from the environment (the token's scopes are in the runbook).
# - planetscale reads PLANETSCALE_SERVICE_TOKEN_ID and PLANETSCALE_SERVICE_TOKEN (a service token
#   of the organization, its access in the runbook).
# - google reads GOOGLE_OAUTH_ACCESS_TOKEN, which the tf:* tasks take from the `goal` gcloud
#   configuration (`gcloud auth print-access-token`); never the machine's application default
#   credentials or another gcloud account. In GitHub Actions (GITHUB_ACTIONS=true) the workflow
#   puts it there: a service account's token through Workload Identity Federation (ci.tf).
# - sops decrypts secrets/<workspace>.sops.yaml with the age key (SOPS_AGE_KEY_FILE): the
#   operator's file, or in a workflow a 0600 file it writes from the SOPS_AGE_KEY secret.
provider "cloudflare" {}

provider "planetscale" {}

provider "google" {
  default_labels = {
    managed_by  = "terraform"
    environment = local.environment
  }
}

provider "random" {}

provider "sops" {}
