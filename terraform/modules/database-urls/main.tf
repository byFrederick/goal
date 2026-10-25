# The four URLs Goal connects with, by prototype 4-2's rules (spec 4 §5.12):
# - the direct port 5432, never PgBouncer's 6432 (no notifications, no session advisory locks);
# - Goal's own database, never the provider's default;
# - users <role>.<branch-id>;
# - goald and goal db init (pgx) verify the server against the system CA bundle:
#   sslmode=verify-full&sslrootcert=system;
# - the auth service (node-postgres) takes sslmode=verify-full alone: it reads sslrootcert as a file
#   path and verifies against Node's bundled CAs without one;
# - goald's pool is capped (pool_max_conns, read by pgxpool only); its listeners reuse the pool's
#   connection settings without the pool parameters, and migrations need no cap.
# Passwords are URL-encoded, so whatever the provider generates goes in unchanged.
locals {
  server = "${var.host}:5432/${var.database}"
  go_tls = "sslmode=verify-full&sslrootcert=system"

  user = {
    owner = "goal_owner.${var.branch_id}:${urlencode(var.passwords.owner)}"
    app   = "goal_app.${var.branch_id}:${urlencode(var.passwords.app)}"
    auth  = "goal_auth.${var.branch_id}:${urlencode(var.passwords.auth)}"
  }
}
