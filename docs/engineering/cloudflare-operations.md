# Cloudflare operations

The runbook of Goal's Cloudflare install (ADR 0008, spec 4 §5.12, §14 step 7): what `terraform/` owns, the credentials it needs, the first install, routine deploys, the master key's escrow and rotation, and restores. Only the owner applies, or the coordinating agent the owner explicitly authorizes for that change; every other agent stops at `task tf:plan` (AGENTS.md rule 8).

## What lives where

| Thing | Owner | Where |
| --- | --- | --- |
| R2 bucket `goal-production` (definitions and knowledge blobs) | Terraform | `terraform/storage.tf` (`prevent_destroy`) |
| DNS `goal.byfrederick.work` (proxied `AAAA 100::`) and the Workers route `goal.byfrederick.work/*` → Worker `goal` | Terraform | `terraform/routing.tf` |
| Platform secrets in the account's Secrets Store, named `goal-production-<ENV NAME>` | Terraform, values from SOPS | `terraform/secrets.tf`, `modules/cloudflare-secrets-store` |
| Google Cloud project `goal-byfrederick` (sign-in) | Terraform | `terraform/google.tf` (`deletion_policy = PREVENT`) |
| Zone `byfrederick.work`, the account's Secrets Store | the account; read as data | `terraform/data.tf` |
| State bucket `goal-terraform-state` and its R2 token | made once by hand | below, bootstrap step 1 |
| Control Worker, its Durable Objects, goald and auth container images, Worker vars | wrangler | `deploy/cloudflare/` (4-21) |
| OAuth consent screen and web client | Google console | below, bootstrap step 4 |
| PlanetScale database, its branch and admin credential | PlanetScale console | below, bootstrap step 5 |
| Organizations, connections, knowledge sources, factories | goald's API | never Terraform, never a deploy |

## Workspaces and state

- One root module (`terraform/`), one Terraform CLI workspace: `production`. A workspace without a key in `local.environments` fails every plan, so nothing is created in `default` by mistake.
- State: `s3://goal-terraform-state/env:/production/terraform.tfstate` in R2 (the s3 backend, `use_lockfile = true`). It holds every secret's value in plain text (Secrets Store values are write-only at Cloudflare, Terraform keeps what it sent): the state bucket and its token are the operator's alone.
- Before every plan, apply, import or state command: confirm the directory is `terraform/` and `terraform workspace show` says `production`. The `tf:*` tasks select and check it for you.
- Never copy state between workspaces, never edit it by hand, never delete a workspace. Adopt an existing object with an `import {}` block, never by deleting and recreating it; delete the block after the apply that used it and note it in the commit.
- Never commit state, plans, `*.tfvars`, `override.tf` or a decrypted secrets file (`.gitignore` keeps them out). Commit `.terraform.lock.hcl` only when a provider change is intended (`terraform providers lock -platform=darwin_arm64 -platform=darwin_amd64 -platform=linux_amd64 -platform=linux_arm64`); the tasks init with `-lockfile=readonly`.

## Credentials

Nothing here is in the repository or in SOPS. Export them in the shell (or keep them in the git-ignored `.env`, which Task loads); `task tf:plan` checks them first (`tf:credentials`) and names what is missing.

| Variable | What |
| --- | --- |
| `CLOUDFLARE_API_TOKEN` | A user API token: Account › Workers R2 Storage: Edit, Account › Secrets Store: Edit, Zone › Zone: Read, Zone › DNS: Edit, Zone › Workers Routes: Edit, for this account and the zone `byfrederick.work` only. A 403 is the token's scope, not the state. |
| `CLOUDFLARE_ACCOUNT_ID` | The account id: the state bucket's endpoint `https://<id>.r2.cloudflarestorage.com`. |
| `GOAL_TF_STATE_ACCESS_KEY`, `GOAL_TF_STATE_SECRET_KEY` | An R2 API token with Object Read & Write on `goal-terraform-state` only (its S3 access key id and secret). The task hands them to the backend as `AWS_*` and to nothing else. |
| `SOPS_AGE_KEY_FILE` | The age private key, default `~/.config/sops/age/keys.txt`. Never print it. |
| gcloud configuration `goal` | `CLOUDSDK_ACTIVE_CONFIG_NAME=goal gcloud auth login` once. The tasks pass `GOOGLE_OAUTH_ACCESS_TOKEN=$(gcloud auth print-access-token)` from that configuration and unset every other Google credential variable: never the machine's application default credentials or another account. |

## Secrets (SOPS)

`terraform/secrets/production.sops.yaml` is committed, encrypted with age to `age1kttpj5ev6g08st6s20nezlqjdaa75l30z08nxhudpe2eapcv7fwqsv47mz` (`.sops.yaml`). Keys stay readable; values are encrypted. `terraform/secrets.tf` is the only place that knows its shape: every value but the database section becomes one store secret, and `local.secret_consumers` says which container gets which (the plan fails when a secret has no consumer or a consumer names a missing one; `terraform test` checks both).

| Section › key | Consumers | Make it with |
| --- | --- | --- |
| `master_key` › `GOAL_MASTER_KEYS` | goald | `k1:$(openssl rand -base64 32)`; escrow it first (below) |
| `internal` › `GOAL_GATEWAY_KEY` | goald | `openssl rand -hex 32` |
| `internal` › `GOAL_SANDBOX_KEY` | goald, the Worker | `openssl rand -hex 32` |
| `internal` › `GOAL_AUTH_PROXY_SECRET` | goald, auth | `openssl rand -hex 32`, unlike every other secret |
| `better_auth` › `BETTER_AUTH_SECRETS` | auth | `1:$(openssl rand -hex 32)` |
| `google` › `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | auth | the OAuth web client (bootstrap step 4) |
| `smtp` › `GOAL_SMTP_URL` | auth | the mail provider: `smtps://<user>:<password>@<host>:465` |
| `r2` › `GOAL_S3_ACCESS_KEY`, `GOAL_S3_SECRET_KEY` | goald | an R2 API token, Object Read & Write on `goal-production` only |
| `embedding` › `GOAL_EMBED_KEY` | goald | an API token with Workers AI Read |
| `database` › `GOAL_DATABASE_ADMIN_URL` | the operator only | PlanetScale's credential on database `goal` (bootstrap step 5) |
| `database` › `GOAL_PG_OWNER_PASSWORD`, `GOAL_PG_APP_PASSWORD`, `GOAL_PG_AUTH_PASSWORD` | the operator only | `openssl rand -hex 24` each |

The database section never reaches the store as it is: `modules/database-urls` builds the three URLs the containers get (`GOAL_DATABASE_URL`, `GOAL_MIGRATE_DATABASE_URL` for goald, `GOAL_AUTH_DATABASE_URL` for auth) from the admin URL's host and branch and the role passwords, following prototype 4-2: port 5432 (never PgBouncer's 6432, which drops notifications and session advisory locks), database `goal`, users `<role>.<branch-id>`, `sslmode=verify-full&sslrootcert=system` for goald (pgx; its image carries a CA bundle), `sslmode=verify-full` alone for the auth service (node-postgres reads `sslrootcert` as a file), and goald's pool capped at `pool_max_conns=8` (PS-5 leaves 22 connections: goald's pool and listeners, auth's pool of 10, migrations). The plan refuses an admin URL that breaks any of these, and any empty value.

| Task | What |
| --- | --- |
| `task secrets:edit` | `sops edit` in `$EDITOR`, decrypted in memory only. Preferred. |
| `task secrets:decrypt` / `task secrets:encrypt` | For bulk edits: a mode-0600 `production.decrypted.yaml` (git-ignored), encrypted back over the SOPS file through a temporary file, then deleted. |
| `task db:production -- <command>` | `goal db <command>` (init, migrate, rekey, recover-admin, recover-owner) on the install's database, with the values above in its environment only. |

Rules: never `sops decrypt` to the terminal; after every edit `git diff` the file and check only the keys you meant changed; a value that leaked is rotated at its source first, then here. To add a secret: a key in the SOPS file, a line in `local.secret_values`, its consumers in `local.secret_consumers`, the binding in the wrangler config.

## Bootstrap (first install)

1. **State bucket.** In the Cloudflare dashboard (or `wrangler r2 bucket create goal-terraform-state --location enam`) create the bucket `goal-terraform-state`, then an R2 API token with Object Read & Write on that bucket only: `GOAL_TF_STATE_ACCESS_KEY`/`GOAL_TF_STATE_SECRET_KEY`. Create the Cloudflare API token of the table above. Terraform never creates or manages the state bucket.
2. **Secrets Store.** The account has exactly one store; if Secrets Store was never opened, create it once (dashboard › Secrets Store, or `wrangler secrets-store store create default --remote`). Terraform reads it and adds only Goal's secrets.
3. **Bucket and Google project.** `task tf:apply -- -target=cloudflare_r2_bucket.main -target=google_project.sign_in`. Targeting skips the secrets, whose values do not exist yet. If `goal-byfrederick` is taken (project ids are global), change `google_project_id` in `terraform/locals.tf` first.
4. **Google sign-in (console, project `goal-byfrederick`, Google Auth Platform).** Branding: app name Goal, the owner's support and developer contact address, authorized domain `byfrederick.work`. Audience: External, then Publish app (in production; openid, email and profile need no verification). Data access: `openid`, `.../auth/userinfo.email`, `.../auth/userinfo.profile`. Clients › Create client › Web application, name `goal-production`, authorized JavaScript origin `https://goal.byfrederick.work`, authorized redirect URI `https://goal.byfrederick.work/api/auth/callback/google` (`terraform output google_redirect_uri`). Put the client id and secret into `google` (`task secrets:edit`); the auth service runs with `GOAL_AUTH_GOOGLE=on` (a Worker var).
5. **Database (PlanetScale Postgres, us-east-1).** Connected as the console's credential to the default database: `CREATE DATABASE goal OWNER postgres;` (Goal's own database, owned by `postgres`, not by the credential's role, which can be deleted with its password). Put that credential's URL on database `goal` into `database` › `GOAL_DATABASE_ADMIN_URL` (`postgres://pscale_api_<id>.<branch-id>:<password>@<host>:5432/goal?sslmode=verify-full`) and three new role passwords beside it. Then `task db:production -- init`: it creates goal_owner, goal_app and goal_auth with those passwords, grants the database and schema, revokes PUBLIC's CONNECT and TEMPORARY, creates `vector`, and grants goal_owner to the admin `WITH INHERIT TRUE, SET TRUE` (a non-superuser administrator gets ADMIN only on the roles it creates; without it `recover-admin`, `recover-owner` and handing objects to goal_owner fail). goald migrates at its first start.
6. **The other values.** R2 token for `goal-production`, the Workers AI token, the SMTP URL, and the generated ones of the table; escrow the master key and the age key (below) before anything uses them. `task tf:plan` must now pass with no empty-value error.
7. **Secrets and DNS.** `task tf:apply -- -target=module.secrets -target=cloudflare_dns_record.goal`.
8. **Worker.** Deploy `deploy/cloudflare` with wrangler (4-21): its `secrets_store_secrets` bindings take `store_id = terraform output secrets_store_id` and each `secret_name` from `terraform output secret_names`, passed to each container as `terraform output secret_consumers` says; its vars take `GOAL_PUBLIC_URL` (`public_url`), `GOAL_S3_BUCKET` (`bucket`) and `GOAL_S3_ENDPOINT` (`s3_endpoint`). The wrangler config declares no routes.
9. **Route.** `task tf:apply`: Cloudflare refuses a route to a Worker that does not exist, so the route comes last; the plan shows only it. Then `task tf:plan` shows no changes.

## Deploys

- **Infrastructure:** a pull request changing `terraform/` passes `task check` (it runs `task tf:check`: fmt, validate, tflint, `terraform test` on mock providers), carries the output of `task tf:plan`, and the owner (or the authorized coordinator) runs `task tf:apply` on main after reviewing the same plan. Do not merge a change whose plan needs fixes.
- **Code:** build and push the images, then deploy the Worker with wrangler (4-21). goald migrates at start as goal_owner. A goald redeploy ends its runs in flight; their leases expire and the items are claimed again (spec 4 §11.3).
- **A secret's new value** reaches a container when it starts: `task tf:apply`, then redeploy (or restart) the containers that consume it.

## Master key: escrow and rotation

`GOAL_MASTER_KEYS` seals every organization's connection credentials (AES-256-GCM, key id in the AAD). Secrets Store cannot give a value back, and without the key every stored credential is lost (spec 4 §11.3 risk 6).

- **Escrow.** Before a key is first used, keep it offline in two places (the owner's password manager and a printed or hardware copy). Escrow the age private key the same way: it is what opens `production.sops.yaml`, which holds every master key in use.
- **Rotation** (yearly, or at once if a key may have leaked):
  1. `task secrets:edit`: put the new key first, `k2:<new>,k1:<old>`; escrow `k2`.
  2. `task tf:apply`, then restart goald so it seals with `k2` and still opens `k1`.
  3. `task db:production -- rekey`: reseals every credential under `k2`.
  4. Remove `k1` from the value, `task tf:apply`, restart goald. Backups keep `k1`'s ciphertext until they expire; keep `k1` escrowed until then.
  5. If a key leaked, also revoke the credentials it protected at their providers: the ciphertext may have leaked with it.
- **Other secrets:** `BETTER_AUTH_SECRETS` rotates like the master key (`2:<new>,1:<old>`, drop `1` later). `GOAL_GATEWAY_KEY` ends the task tokens in flight (their runs fail and are claimed again). `GOAL_SANDBOX_KEY` and `GOAL_AUTH_PROXY_SECRET` change for both consumers in one apply and one redeploy. A role password: change it in the SOPS file, `task db:production -- init` (sets it), `task tf:apply` (new URLs), redeploy. R2, Workers AI, SMTP and the Google client secret: make the new one at the provider, put it here, apply, redeploy, then revoke the old one.

## Restore

- **Database.** PlanetScale's backups restore into a branch: point `GOAL_DATABASE_ADMIN_URL` at it (a new branch id changes every user name), `task tf:apply`, redeploy goald and auth. From a dump (including moving a local install, spec 4 §14 step 7): `task db:production -- init` on the empty `goal` database first, then `pg_restore --no-owner` connected as goal_owner (`postgres://goal_owner.<branch-id>:<password>@<host>:5432/goal?sslmode=verify-full`), then start goald with the same master keys the dump was sealed with.
- **Bucket.** R2 keeps no object versions: a restore is a copy from a copy. Blobs are immutable and content-addressed, so `rclone sync` (or any S3 copy) from the backup or from a local install's RustFS into `goal-production` restores definitions and uploads; nothing else is needed.
- **Terraform state** lost or damaged: recreate the workspace and adopt every object with `import {}` blocks (each resource's import id is in the provider's documentation: the bucket, the DNS record, the route, each store secret, the project), plan to no changes, then delete the blocks. Never `terraform destroy` to start over: the bucket and the project are protected for that reason.
- **age key lost:** the deployed secrets keep working, but `production.sops.yaml` cannot be read: restore the key from escrow. If it is gone, make a new age key, a new SOPS file with every value made again, and keep the master keys from escrow; a lost master key means every organization re-enters its credentials.

## Validation checklist

Before committing a change to `terraform/`: `task tf:check`; `git diff --check`; `git diff terraform/secrets/` shows only intended keys; the lock file changed only if a provider changed. Before applying: `task tf:plan` reviewed, in the `production` workspace, from `terraform/`.
