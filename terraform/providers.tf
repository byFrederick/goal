# Provider credentials never come from this repository or from SOPS:
# - cloudflare reads CLOUDFLARE_API_TOKEN from the environment (the token's scopes are in the runbook).
# - planetscale reads PLANETSCALE_SERVICE_TOKEN_ID and PLANETSCALE_SERVICE_TOKEN (a service token
#   of the organization, its access in the runbook).
# - google reads GOOGLE_OAUTH_ACCESS_TOKEN, which the tf:* tasks take from the `goal` gcloud
#   configuration (`gcloud auth print-access-token`); never the machine's application default
#   credentials or another gcloud account.
# - sops decrypts secrets/<workspace>.sops.yaml with the operator's age key (SOPS_AGE_KEY_FILE).
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
