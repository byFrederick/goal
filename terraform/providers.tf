provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project   = "goal"
      ManagedBy = "terraform"
      Repo      = "factory"
    }
  }
}
