# Sign in with Google (GOAL_AUTH_GOOGLE=on, spec 3) needs an OAuth client of type Web application.
# Only the project has an API: the OAuth consent screen (Google Auth Platform) and the web client
# have none Terraform can use (the IAP OAuth Admin API made IAP-only clients and is shut down), so
# they are console steps in the runbook, and the client's id and secret go into the SOPS file
# (google). Sign-in asks only for openid, email and profile, which needs no enabled API, no
# billing account and no verification. The project is kept if this stack is ever destroyed.
resource "google_project" "sign_in" {
  project_id          = local.settings.google_project_id
  name                = "Goal sign-in"
  auto_create_network = false
  deletion_policy     = "PREVENT"
}
