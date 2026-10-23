terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  # Partial configuration: `terraform init -backend-config=backend.hcl` (see backend.hcl.example).
  backend "s3" {}
}
