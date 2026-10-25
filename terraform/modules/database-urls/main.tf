# The URLs goald and the auth service connect with, built from the administrator's URL and the
# role passwords so each password is written once (spec 4 §5.12, prototype 4-2):
# - the direct port 5432 and Goal's own database `goal`, as the admin URL names them;
# - users <role>.<branch-id>, the branch taken from the admin's user;
# - goald (pgx) verifies the server against the system CA bundle: sslmode=verify-full&sslrootcert=system;
# - the auth service (node-postgres) takes sslmode=verify-full alone: it reads sslrootcert as a file
#   path and verifies against Node's bundled CAs without one;
# - goald's pool is capped (pool_max_conns, read by pgxpool only); its listeners reuse the pool's
#   connection settings without the pool parameters, and migrations need no cap.
locals {
  admin = regex("^postgres(?:ql)?://[^.:@/]+\\.(?P<branch>[^:@/]+):[^@]+@(?P<host>[^:/?]+):5432/goal(?:\\?|$)", var.admin_url)

  base   = "${local.admin.host}:5432/goal"
  go_tls = "sslmode=verify-full&sslrootcert=system"
}
