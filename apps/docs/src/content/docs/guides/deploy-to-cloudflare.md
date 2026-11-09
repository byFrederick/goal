---
title: Deploy to Cloudflare
description: Run your own Goal install on Cloudflare, with the control Worker, goald and the auth service in Containers, R2 and a managed PostgreSQL, made by Terraform and deployed with wrangler.
---

You end with Goal running at your own host, such as `https://goal.example.com`, on a Cloudflare account. Terraform in `deploy/terraform/` makes the platform's infrastructure, wrangler deploys the code from `deploy/cloudflare/`, and after the first install a merge to main applies infrastructure changes while a person dispatches each code deploy.

This page gives the order and says where each step happens. The full procedure for each step, with its commands, checks and failure handling, is in the operations runbook, `docs/engineering/cloudflare-operations.md` in the repository. Follow the runbook for the details; this page does not repeat them.

## What is deployed

| Part | What it is | Made by |
| --- | --- | --- |
| The control Worker | The Cloudflare entry in front of goald, the auth service and the run boxes. Its code is `apps/control-worker`; it also serves this docs site at `/docs` as static assets. | wrangler, from `deploy/cloudflare/wrangler.jsonc` |
| `goal-goald` | The container application that runs goald, the engine and the API. | wrangler |
| `goal-auth` | The container application that runs the auth service, which handles sign-in. | wrangler |
| `goal-runbox` | The container application of the run boxes: one `goal-agent` container per run. | wrangler |
| The R2 bucket | Factory definitions and knowledge files, as immutable content-addressed blobs. | Terraform, `deploy/terraform/storage.tf` |
| Secrets Store secrets | The platform secrets, one per environment variable, named `goal-<workspace>-<NAME>`. The Worker binds them and hands each container its own. | Terraform, `deploy/terraform/secrets.tf` |
| Account tokens | goald's R2 token (bucket objects only) and its Workers AI token. | Terraform, `deploy/terraform/tokens.tf` |
| DNS and the Workers route | A proxied DNS record for your host and the route `<host>/*` to the Worker. | Terraform, `deploy/terraform/routing.tf` |
| The database | A managed PostgreSQL branch, made in the Cloudflare dashboard and billed through Cloudflare, adopted by Terraform with its administrator role. Goal's tables live in a database named `goal` on it. | Terraform, `deploy/terraform/database.tf` |
| Google sign-in | A Google Cloud project for the sign-in client. | Terraform, `deploy/terraform/google.tf`; the client itself in Google's console |
| CI's access to Google | A workload identity pool and a service account, so the Terraform workflows sign in to Google with no stored key. | Terraform, `deploy/terraform/ci.tf` |

Organizations, connections, knowledge sources and factories are none of these. They are made through goald's API once Goal runs, never by Terraform or a deploy.

## Adapt the install to your account

`deploy/terraform/` is one root module for one install, not a module that takes inputs. Its values for your account live in these files, and you change them before the first plan:

- `deploy/terraform/locals.tf`, under `local.environments.production`: the zone and host (for example `example.com` and `goal.example.com`), the Worker's name, the R2 location, the Google project id, the GitHub repository and its ids, the database's organization, name and branch, and goald's pool size.
- `deploy/terraform/.sops.yaml` and the SOPS file `deploy/terraform/secrets/production.sops.yaml`: the age recipient that encrypts it, and the outside values in it (the master keys, the Google client, an optional SMTP URL).
- `deploy/cloudflare/wrangler.jsonc`: the Worker's vars (`GOAL_PUBLIC_URL`, `GOAL_S3_BUCKET`, `GOAL_S3_ENDPOINT`, `GOAL_WORKERS_AI_URL`) and the Secrets Store id of each binding, which take their values from `terraform output`.
- `deploy/cloudflare/Taskfile.yml`: the health URL that `task deploy:health` polls, or set `DEPLOY_HEALTH_URL`.

The one Terraform workspace is `production`. A workspace with no key in `local.environments` fails every plan.

Every variable the Worker, goald and the auth service read is in the [environment reference](/docs/reference/environment/).

## What you need

- A Cloudflare account on Workers Paid, with R2 and a zone for your domain.
- The managed PostgreSQL database, made in the Cloudflare dashboard.
- A Google Cloud account for the sign-in project.
- A checkout of the repository with [Task](https://taskfile.dev), the tools in `.mise.toml`, sops, an age key and the gcloud CLI. A deploy from a laptop also needs Docker with buildx.
- The credentials the runbook's Credentials section lists. `task tf:plan` checks them first and names what is missing.

## First install

Each step names where it happens. A laptop step runs from your checkout with your own credentials.

1. **The state bucket and the tokens.** In the Cloudflare dashboard: make the R2 bucket that holds Terraform's state, an R2 token for that bucket alone, the Cloudflare API token and the database provider's service token. Terraform never makes or manages the state bucket. The state holds every secret, so keep the bucket and its token as safe as the master key.
2. **The Secrets Store.** In the Cloudflare dashboard, open Secrets Store once if the account has none. Terraform reads the store and adds only Goal's secrets.
3. **What needs no outside value.** On a laptop: a targeted `task tf:plan` and `task tf:apply` of the bucket, the Google project, the database administrator and the generated secrets. The other resources need SOPS values that do not exist yet.
4. **The database.** On a laptop: create the `goal` database on the branch as the new administrator, then run `task db:production -- init`. It creates the database roles with the generated passwords and grants them. goald applies the migrations when it first starts.
5. **Google sign-in.** In Google's console, in the new project: the branding, the audience and a web client whose redirect URI is `https://goal.example.com/api/auth/callback/google` (`terraform output google_redirect_uri`).
6. **The outside values.** On a laptop: `task secrets:edit` puts the Google client, the master key and, if the install sends mail, the SMTP URL into the SOPS file. Escrow the master key and the age key before you use them. `task tf:plan` then passes with no empty-value error.
7. **Secrets, tokens and DNS.** On a laptop: a targeted `task tf:apply` of `module.secrets` and the DNS record.
8. **The Worker.** On a laptop: deploy `deploy/cloudflare/wrangler.jsonc` with wrangler, with `--var GOAL_BOOTSTRAP_OWNER_EMAIL:<your address>` so that you can sign in first. The deploy makes `goal-goald`, `goal-auth` and `goal-runbox`.
9. **The network gate.** On a laptop, before the route serves anyone: deploy the sandbox test Worker, run the sandbox conformance test against it on Cloudflare, then delete the test Worker and its container application. Every case must pass: a box reaches the internet and nothing private. If one fails, stop and do not apply the route.
10. **The route.** On a laptop: `task tf:apply`. Cloudflare refuses a route to a Worker that does not exist, so the route comes last. `task tf:plan` then shows no changes.
11. **First sign-in.** In a browser: sign in at `https://goal.example.com` with Google, as the bootstrap owner. You become the platform admin and the owner of the Default organization.
12. **CI's access to Google.** On a laptop, once: a targeted apply of `ci.tf`'s service account and bindings. Then copy `terraform output ci_workload_identity_provider ci_apply_service_account` into the variables of the GitHub environment `production`, whose secrets are the same credentials your laptop uses.
13. **Starter templates.** On a laptop, after the first deploy: `task catalog:production` pushes the starter templates in `deploy/catalog/templates/` into the bucket, so onboarding offers them.

## After the first install

**Infrastructure changes go through a pull request.** A pull request that changes `deploy/terraform/` gets its plan from `.github/workflows/terraform-plan.yml`, posted as one comment that each push updates. A person reviews the plan and merges. The merge to main starts `.github/workflows/terraform-apply.yml`, which applies the plan and plans again to check that nothing is left (ADR 0010, `docs/adr/0010-infrastructure-applied-from-main.md` in the repository). Agents never apply. The owner applies from a laptop only for the first install's targeted applies, a `-replace`, and break-glass, when the workflow cannot run.

**Code is deployed by hand.** A merge to main does not deploy. A person dispatches `.github/workflows/deploy.yml` with the sha of a commit of main (`gh workflow run deploy.yml -f sha=<sha>`); it refuses a sha that is not on main. It runs the same tasks you can run from a laptop:

1. `task docs:site:build` builds this docs site, which the Worker serves.
2. `task deploy:worker` builds the goald, auth and goal-agent images, pushes them to Cloudflare's registry and runs `wrangler deploy`.
3. `task deploy:health` waits until `GET /v1/health` answers `{"status":"ok"}`, and fails after 5 minutes. While goald starts (it opens the database, applies migrations and loads the definitions), every path answers 503 at once with the code `goald-starting`, a `Retry-After` header in seconds and `Cache-Control: no-store`; the health check keeps waiting through it. The control Worker holds requests until goald's container answers `/v1/health`, so browsers do not see this 503 yet.

Production runs the last dispatched commit until the next dispatch. To roll back, dispatch an earlier commit of main; migrations are forward-only and stay applied.

**A renamed or removed secret takes a merge, a deploy and a second merge.** Terraform applies on merge, but the Worker deploys later, so the deployed Worker still binds the old name. The workflows' bound-secrets guard refuses a plan that destroys a secret the deployed Worker binds.

**A new starter template version** needs `task catalog:production` again, from a laptop.

## Operations

The runbook, `docs/engineering/cloudflare-operations.md`, owns the rest: the credentials and their scopes, the GitHub environment, the secrets and their consumers, the master key's escrow and rotation, the other rotations, container sizes, what to do when a deploy fails, and restoring the database, the bucket or the state.

## Related

- [Self-host with Docker Compose](/docs/guides/self-host-with-docker-compose/), to run Goal on one machine.
- [Environment variables](/docs/reference/environment/), for every variable a process reads.
- [Concepts](/docs/concepts/), for the parts of the platform.
