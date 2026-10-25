# The platform's own infrastructure on Cloudflare (ADR 0008, spec 4 §5.12), plus the Google Cloud
# project that holds the sign-in OAuth client. One root module, one Terraform CLI workspace per
# environment: `production` is the only one (docs/engineering/cloudflare-operations.md).
#
# State lives in an R2 bucket through the s3 backend. That bucket is created once by hand, never by
# this configuration, so destroying the platform can never take its own state with it. The backend
# is literal except two values the operator's shell gives (the Taskfile's tf:* tasks set them):
#   AWS_ENDPOINT_URL_S3                          https://<account id>.r2.cloudflarestorage.com
#   AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY    an R2 API token for the state bucket only
# Workspaces add `env:/<workspace>/` in front of the key: production's state is
# env:/production/terraform.tfstate. R2 has conditional writes, so the native lockfile replaces a
# lock table. R2 encrypts every object at rest; there is no customer-managed key.
terraform {
  required_version = ">= 1.15.7"

  backend "s3" {
    bucket       = "goal-terraform-state"
    key          = "terraform.tfstate"
    region       = "auto"
    use_lockfile = true

    # R2 is S3-compatible, not AWS: no STS, no region list, no account lookup, no instance metadata.
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
    use_path_style              = true
  }

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.27.0"
    }
    google = {
      source  = "hashicorp/google"
      version = "~> 8.7.0"
    }
    sops = {
      source  = "carlpett/sops"
      version = "~> 1.4.1"
    }
  }
}
