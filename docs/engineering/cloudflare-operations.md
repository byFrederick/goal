# Cloudflare operations

The runbook of Goal's Cloudflare install (ADR 0008, spec 4 §5.12, §14 step 7): what `terraform/` owns, the credentials it needs, the first install, routine deploys, the master key's escrow and rotation, and restores. Only the owner applies, or the coordinating agent the owner explicitly authorizes for that change; every other agent stops at `task tf:plan` (AGENTS.md rule 8).

## What lives where

| Thing | Owner | Where |
| --- | --- | --- |
| R2 bucket `goal-production` (definitions and knowledge blobs) | Terraform | `terraform/storage.tf` (`prevent_destroy`) |
| DNS `goal.byfrederick.work` (proxied `AAAA 100::`) and the Workers route `goal.byfrederick.work/*` → Worker `goal` | Terraform | `terraform/routing.tf` |
| PlanetScale Postgres branch `main` of database `goal` (organization `freddycruz5227`, PS-5, us-east; made in the Cloudflare dashboard, billed through Cloudflare) | Terraform, adopted by an `import` block | `terraform/database.tf` (`prevent_destroy`) |
| The database administrator `goal_db_admin` (inherits `postgres`; goal db init runs as it) | Terraform | `terraform/database.tf` |
| Generated secrets: gateway, sandbox and auth proxy keys, the Better Auth secret, the goal_owner, goal_app and goal_auth passwords | Terraform (`random_password`) | `terraform/database.tf` |
| Account tokens for R2 (bucket objects only) and Workers AI (read) | Terraform | `terraform/tokens.tf` |
| Platform secrets in the account's Secrets Store, named `goal-production-<ENV NAME>` | Terraform | `terraform/secrets.tf`, `modules/cloudflare-secrets-store` |
| Master keys, the Google OAuth client, the SMTP URL | the operator, in SOPS | `terraform/secrets/production.sops.yaml` |
| Google Cloud project `goal-byfrederick` (sign-in) | Terraform | `terraform/google.tf` (`deletion_policy = PREVENT`) |
| Zone `byfrederick.work`, the account's Secrets Store | the account; read as data | `terraform/data.tf` |
| State bucket `goal-terraform-state` and its R2 token | made once by hand | bootstrap step 1 |
| PostgreSQL database `goal` on the branch (`CREATE DATABASE goal OWNER postgres`) | made once by hand | bootstrap step 4 |
| goal_owner, goal_app, goal_auth (PostgreSQL roles), their grants, `vector` | `goal db init` | `task db:production -- init` |
| Control Worker, its Durable Objects, goald and auth container images, Worker vars | wrangler | `deploy/cloudflare/` (4-21) |
| OAuth consent screen and web client | Google console | bootstrap step 5 |
| Organizations, connections, knowledge sources, factories | goald's API | never Terraform, never a deploy |

## Workspaces and state

- One root module (`terraform/`), one Terraform CLI workspace: `production`. A workspace without a key in `local.environments` fails every plan, so nothing is created in `default` by mistake.
- State: `s3://goal-terraform-state/env:/production/terraform.tfstate` in R2 (the s3 backend, `use_lockfile = true`). It holds every secret in plain text, the generated ones only there (Secrets Store values are write-only at Cloudflare), and also the master keys (the SOPS data source and the store secrets' values are saved in it) and goal db init's administrator URL (BYPASSRLS, CREATEROLE): **the state bucket is as sensitive as the master key**, since with the two together every organization's credentials can be decrypted. The bucket and its token are the operator's alone: a token of its own (neither the Cloudflare API token nor goald's R2 token), Object Read & Write on `goal-terraform-state` only, with an expiry (renew it rather than keep a long-lived one), kept in the password manager and exported only for the shell that runs Terraform, never into `.env`. Revoke it at once if it may have leaked, then rotate every secret (Other rotations) and the master key.
- Before every plan, apply, import or state command: confirm the directory is `terraform/` and `terraform workspace show` says `production`. The `tf:*` tasks select and check it for you.
- Never copy state between workspaces, never edit it by hand, never delete a workspace. Adopt an existing object with an `import {}` block, never by deleting and recreating it; delete the block after the apply that used it and note it in the commit (the branch's block in `database.tf` goes after the first apply).
- Never commit state, plans, `*.tfvars`, `override.tf` or a decrypted secrets file (`.gitignore` keeps them out). Commit `.terraform.lock.hcl` only when a provider change is intended (`terraform providers lock -platform=darwin_arm64 -platform=darwin_amd64 -platform=linux_amd64 -platform=linux_arm64`); the tasks init with `-lockfile=readonly`.

## Credentials

Nothing here is in the repository or in SOPS. Export them in the shell (or keep them in the git-ignored `.env`, which Task loads); `task tf:plan` checks them first (`tf:credentials`) and names what is missing.

| Variable | What |
| --- | --- |
| `CLOUDFLARE_API_TOKEN` | A user API token for this account and the zone `byfrederick.work` only: Account › Workers R2 Storage: Edit, Account › Secrets Store: Edit, Account › Account API Tokens: Edit (it makes the R2 and Workers AI tokens, so it can mint any account token: keep it in the password manager, never on a shared machine), Zone › Zone: Read, Zone › DNS: Edit, Zone › Workers Routes: Edit. A 403 is the token's scope, not the state. |
| `CLOUDFLARE_ACCOUNT_ID` | The account id: the state bucket's endpoint `https://<id>.r2.cloudflarestorage.com`. |
| `GOAL_TF_STATE_ACCESS_KEY`, `GOAL_TF_STATE_SECRET_KEY` | An R2 API token with Object Read & Write on `goal-terraform-state` only (its S3 access key id and secret), with an expiry: it reads every secret, the master keys included (Workspaces and state). Export it for the session only, never into `.env`. The task hands them to the backend as `AWS_*` and to nothing else. |
| `PLANETSCALE_SERVICE_TOKEN_ID`, `PLANETSCALE_SERVICE_TOKEN` | A service token of the organization `freddycruz5227` with access to the database `goal`: read the database and its branches, manage the branch's roles (create, read, delete). |
| `SOPS_AGE_KEY_FILE` | The age private key, default `~/.config/sops/age/keys.txt`. Never print it. |
| gcloud configuration `goal` | `CLOUDSDK_ACTIVE_CONFIG_NAME=goal gcloud auth login` once. The tasks pass `GOOGLE_OAUTH_ACCESS_TOKEN=$(gcloud auth print-access-token)` from that configuration and unset every other Google credential variable: never the machine's application default credentials or another account. |

## Secrets

Every secret comes from the one place that makes it, and nobody copies one by hand. `terraform/secrets.tf` maps each into one Secrets Store secret, named by the environment variable its consumers read, and `local.secret_consumers` says which container gets which; the plan fails when a secret has no consumer or a consumer names a missing one (`terraform test` checks both, and each container's set).

| Secret | Made by | Consumers |
| --- | --- | --- |
| `GOAL_MASTER_KEYS` | the operator, SOPS `master_key` (`k1:$(openssl rand -base64 32)`; escrow first, below) | goald |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | Google's console (bootstrap step 5), SOPS `google` | auth |
| `GOAL_SMTP_URL` | the mail provider (`smtps://<user>:<password>@<host>:465`), SOPS `smtp` | auth |
| `GOAL_GATEWAY_KEY`, `GOAL_SANDBOX_KEY`, `GOAL_AUTH_PROXY_SECRET`, `BETTER_AUTH_SECRET` | `random_password` | goald; goald and the Worker; goald and auth; auth |
| `GOAL_S3_ACCESS_KEY`, `GOAL_S3_SECRET_KEY` | the R2 account token: its id, and the SHA-256 of its value | goald |
| `GOAL_EMBED_KEY` | the Workers AI account token | goald |
| `GOAL_DATABASE_URL`, `GOAL_MIGRATE_DATABASE_URL` | `modules/database-urls` | goald |
| `GOAL_AUTH_DATABASE_URL` | `modules/database-urls` | auth |

`goal db init`'s administrator URL (`GOAL_DATABASE_ADMIN_URL`: BYPASSRLS, CREATEROLE) and the role passwords (`GOAL_PG_OWNER_PASSWORD`, `GOAL_PG_APP_PASSWORD`, `GOAL_PG_AUTH_PASSWORD`; the PlanetScale role and `random_password`) are not store secrets: nothing on Cloudflare runs `goal db init`, and the store is account-wide, so any other Worker of the account (or a token that may edit Workers scripts) could bind one. They are sensitive Terraform outputs that only `task db:production` reads. A Cloudflare-side `goal db init` runner, if one is ever built, adds them back with that runner as their one consumer.

The master keys stay in SOPS, not `random_password`, on purpose: they must be escrowed outside the state (losing them loses every organization's credentials), they rotate as a list (`k2:<new>,k1:<old>`, then `goal db rekey`), and a generated one would be replaced, with no way back, by any `-replace` or recreation of its resource. Everything else Goal can make again: a lost generated secret is rotated, not recovered.

The URLs follow prototype 4-2 (`modules/database-urls`): port 5432 (never PgBouncer's 6432, which drops notifications and session advisory locks), the PostgreSQL database `goal`, users `<role>.<branch-id>` with the branch id of the administrator's user name, `sslmode=verify-full&sslrootcert=system` for goald and goal db init (pgx; goald's image carries a CA bundle), `sslmode=verify-full` alone for the auth service (node-postgres reads `sslrootcert` as a file), goald's pool capped at `pool_max_conns=8` (PS-5 leaves 22 connections: goald's pool and listeners, auth's pool of 10, migrations), passwords URL-encoded.

`terraform/secrets/production.sops.yaml` is committed, encrypted with age to `age1kttpj5ev6g08st6s20nezlqjdaa75l30z08nxhudpe2eapcv7fwqsv47mz` (`.sops.yaml`); keys stay readable, values are encrypted. The plan refuses any value still empty.

| Task | What |
| --- | --- |
| `task secrets:edit` | `sops edit` in `$EDITOR`, decrypted in memory only. Preferred. |
| `task secrets:decrypt` / `task secrets:encrypt` | For bulk edits: a mode-0600 `production.decrypted.yaml` (git-ignored), encrypted back over the SOPS file through a temporary file, then deleted. |
| `task db:production -- <command>` | `goal db <command>` (init, migrate, rekey, recover-admin, recover-owner) on the install's database: the administrator's URL, goal_owner's URL and the role passwords from the state (`terraform output -raw`), the master keys from SOPS, in its environment only. |

Rules: never `sops decrypt` or `terraform output -raw` to the terminal; after every SOPS edit `git diff` the file and check only the keys you meant changed; a value that leaked is rotated at its source first, then here. To add a secret: make it in Terraform when Goal can generate it, in SOPS only when it comes from outside; then a line in `local.secret_values`, its consumers in `local.secret_consumers`, the binding in the wrangler config.

## Bootstrap (first install)

1. **Hand-made prerequisites.** The state bucket: dashboard › R2 (or `wrangler r2 bucket create goal-terraform-state --location enam`), then an R2 API token with Object Read & Write on that bucket only (`GOAL_TF_STATE_*`). The Cloudflare API token and the PlanetScale service token of the table above. Terraform never creates or manages the state bucket.
2. **Secrets Store.** The account has exactly one store; if Secrets Store was never opened, create it once (dashboard › Secrets Store, or `wrangler secrets-store store create default --remote`). Terraform reads it and adds only Goal's secrets.
3. **What needs no outside value.** `task tf:plan -- -target=cloudflare_r2_bucket.main -target=google_project.sign_in -target=planetscale_postgres_branch_role.admin -target=random_password.generated`: the branch shows as an import with no changes (if not, stop: the configuration differs from the live branch), the rest as creates. Then `task tf:apply` with the same targets. Targeting skips the store secrets, whose SOPS values do not exist yet. If `goal-byfrederick` is taken (project ids are global), change `google_project_id` in `terraform/locals.tf` first.
4. **The database.** Connected as the new administrator to the branch's default database (`terraform output -raw goal_database_admin_url` with `/goal` changed to `/postgres`, into `psql` without echoing it): `CREATE DATABASE goal OWNER postgres;` unless prototype 4-2 left it there (Goal's own database, owned by `postgres`, not by a credential's role, which can be deleted with its password). If an earlier administrator created goal_owner, goal_app and goal_auth (the API-made `goal_admin`, user `pscale_api_…`, of prototype 4-2), hand them over first, connected as that one on `goal`: `GRANT goal_owner, goal_app, goal_auth TO <terraform output database_admin_role> WITH ADMIN OPTION;` (only the role that created them may alter them on PostgreSQL 16+). Then `task db:production -- init`: it creates the three roles if missing with the generated passwords (or sets them), grants the database and schema, revokes PUBLIC's CONNECT and TEMPORARY, creates `vector`, and grants goal_owner to the administrator `WITH INHERIT TRUE, SET TRUE`. goald migrates at its first start.
5. **Google sign-in (console, project `goal-byfrederick`, Google Auth Platform).** Branding: app name Goal, the owner's support and developer contact address, authorized domain `byfrederick.work`. Audience: External, then Publish app (in production; openid, email and profile need no verification). Data access: `openid`, `.../auth/userinfo.email`, `.../auth/userinfo.profile`. Clients › Create client › Web application, name `goal-production`, authorized JavaScript origin `https://goal.byfrederick.work`, authorized redirect URI `https://goal.byfrederick.work/api/auth/callback/google` (`terraform output google_redirect_uri`). Put the client id and secret into `google` (`task secrets:edit`); the auth service runs with `GOAL_AUTH_GOOGLE=on` (a Worker var).
6. **The outside values.** `task secrets:edit`: the master key (escrow it and the age key first, below) and the SMTP URL. `task tf:plan` now passes with no empty-value error.
7. **Secrets, tokens and DNS.** `task tf:apply -- -target=module.secrets -target=cloudflare_dns_record.goal`.
8. **Worker.** Deploy `deploy/cloudflare` with wrangler (4-21): its `secrets_store_secrets` bindings take `store_id = terraform output secrets_store_id` and each `secret_name` from `terraform output secret_names`, passed to each container as `terraform output secret_consumers` says; its vars take `GOAL_PUBLIC_URL` (`public_url`), `GOAL_S3_BUCKET` (`bucket`) and `GOAL_S3_ENDPOINT` (`s3_endpoint`). The wrangler config declares no routes.
9. **Network gate (blocks the route).** Spec 4 threat row 5 holds on Cloudflare only once it is shown there: a box must reach the internet and nothing private. Before the route serves anyone, from `deploy/cloudflare`, deploy sandboxtest's Worker on its own with a throwaway key: `key=$(openssl rand -hex 32)`, `wrangler deploy --config test/sandboxtest/wrangler.jsonc`, `printf %s "$key" | wrangler secret put GOAL_SANDBOX_KEY --config test/sandboxtest/wrangler.jsonc`; then from the repository root `GOAL_TEST_SANDBOX_URL=https://goal-sandboxtest.<subdomain>.workers.dev GOAL_TEST_SANDBOX_KEY="$key" GOAL_TEST_SANDBOX_IMAGE=sandboxtest GOAL_TEST_SANDBOX_NETWORK=on go test -tags cloudflare -count=1 -run TestSandboxes_Conformance -v ./internal/adapters/cloudflare`; then `wrangler delete --config test/sandboxtest/wrangler.jsonc`. Every case must pass, `169.254.169.254`, RFC 1918, loopback and `fd00::/8` unreachable from a box above all. **If one fails, stop: do not apply the route** (nobody can sign in, so no factory runs) and fix the Worker's box networking first. Record the result in spec 4 §5.12 (staging check 1) and threat row 5. Run the gate again before deploying any change to `src/sandbox.ts`, the outbound handlers or the containers' network settings.
10. **Route.** `task tf:apply`: Cloudflare refuses a route to a Worker that does not exist, so the route comes last; the plan shows only it. Then delete the branch's `import` block, and `task tf:plan` shows no changes.
11. **Retire the old administrator.** Once `task db:production -- init` has run as `goal_db_admin`, delete the API-made `goal_admin` role of prototype 4-2 (PlanetScale console › the branch › Roles, reassigning what it owns to `postgres`), and revoke its password wherever it was kept. Nothing of Goal's uses it any more.

## Deploys

- **Infrastructure:** a pull request changing `terraform/` passes `task check` (it runs `task tf:check`: fmt, validate, tflint, `terraform test` on mock providers), carries the output of `task tf:plan`, and the owner (or the authorized coordinator) runs `task tf:apply` on main after reviewing the same plan. Do not merge a change whose plan needs fixes.
- **Code:** build and push the images, then deploy the Worker with wrangler (4-21); a change to the boxes' networking passes the network gate (bootstrap step 9) first. goald migrates at start as goal_owner. A goald redeploy ends its runs in flight; their leases expire and the items are claimed again (spec 4 §11.3).
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

## Other rotations

- **A generated secret:** `task tf:apply -- -replace='random_password.generated["<name>"]'`, then redeploy its consumers in one go. `gateway_key` ends the task tokens in flight (their runs fail and are claimed again); `better_auth_secret` signs everyone out; `sandbox_key` and `auth_proxy_secret` change for both consumers at once. A role password (`goal_owner`, `goal_app`, `goal_auth`): replace it, `task db:production -- init` (sets it), then redeploy goald and auth.
- **The administrator:** only the role that created goal_owner, goal_app and goal_auth may alter them, so the old one hands them over before it goes: add a second role resource (`admin_next`, another `name`), apply it, connect as the old administrator and `GRANT goal_owner, goal_app, goal_auth TO <the new base user name> WITH ADMIN OPTION;`, point `module.database_urls` at `admin_next` and apply (the URLs change), `task db:production -- init`, then remove the old resource and apply (what it owns goes to `postgres`).
- **A Cloudflare token:** `task tf:apply -- -replace='module.r2_token.cloudflare_account_token.this'` (or `module.workers_ai_token`), then restart goald.
- **From outside:** SMTP and the Google client secret: make the new one at the provider, `task secrets:edit`, `task tf:apply`, redeploy auth, then revoke the old one.

## Restore

- **Database.** PlanetScale's backups restore into a new branch, which changes the branch id of every user name. Adopt it without touching the old one: set `planetscale.branch` in `terraform/locals.tf` to its name, `terraform state rm planetscale_postgres_branch.main planetscale_postgres_branch_role.admin` (forgets both, deletes nothing), put the `import` block back, `task tf:apply` (a new administrator on the new branch, new URLs), hand the three roles to it as in bootstrap step 4, `task db:production -- init`, redeploy goald and auth, then delete the old branch's administrator in the console. From a dump (including moving a local install, spec 4 §14 step 7): `task db:production -- init` on the empty `goal` database first, then `pg_restore --no-owner` connected as goal_owner (`GOAL_MIGRATE_DATABASE_URL`, `terraform output -raw goal_migrate_database_url`), then start goald with the same master keys the dump was sealed with.
- **Bucket.** R2 keeps no object versions: a restore is a copy from a copy. Blobs are immutable and content-addressed, so `rclone sync` (or any S3 copy) from the backup or from a local install's RustFS into `goal-production` restores definitions and uploads; nothing else is needed.
- **Terraform state** lost or damaged: recreate the workspace and adopt the bucket, the DNS record, the route, the branch and the project with `import {}` blocks (each resource's import id is in its provider's documentation), plan, then delete the blocks. The generated secrets, the administrator and the tokens existed only there: let the plan make new ones, then run `task db:production -- init` and redeploy every container, as for a rotation of all of them. The master keys are safe in SOPS, which is why they live there. Never `terraform destroy` to start over: the bucket, the branch and the project are protected for that reason.
- **age key lost:** the deployed secrets keep working, but `production.sops.yaml` cannot be read: restore the key from escrow. If it is gone, make a new age key and a new SOPS file with every value made again, the master keys from escrow; a lost master key means every organization re-enters its credentials.

## Validation checklist

Before committing a change to `terraform/`: `task tf:check`; `git diff --check`; `git diff terraform/secrets/` shows only intended keys; the lock file changed only if a provider changed. Before applying: `task tf:plan` reviewed, in the `production` workspace, from `terraform/`.
