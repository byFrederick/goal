# ADR 0011 — Production deploys from main through GitHub Actions

Status: accepted · 2026-10-25 · Builds on [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (the Cloudflare install) and [ADR 0010](0010-infrastructure-applied-from-main.md) (the `production` environment)

## Context

Until now a person deployed the app by hand: `wrangler deploy` from `deploy/cloudflare`, which builds the goald, auth and goal-agent images, pushes them to Cloudflare's registry and deploys the control Worker. The owner decided that a merge to main which changes the app deploys production on its own, once CI has passed for that commit.

## Decision

1. **`.github/workflows/deploy.yml` deploys on `workflow_run` of `ci`**, only for a successful run of a push to main from this repository, and checks out that run's `head_sha`. Pull requests never deploy.
2. **Only when the app changed.** `workflow_run` has no paths filter. A first job compares the commit with the one the last successful deploy job deployed (the run name carries the sha) over the app paths: `cmd/`, `internal/`, `web/`, `worker/`, `auth/`, `packages/`, `deploy/cloudflare/`, `migrations/`, `api/`, `schema/`, `go.mod`, `go.sum` and the pnpm files. Comparing with the last deploy, not with the parent commit, means a failed or superseded deploy is never forgotten. `terraform/`-only and docs-only pushes deploy nothing.
3. **The same tasks a person runs.** `task deploy:worker` (wrangler deploy), then `task deploy:health`: `GET /v1/health` on the public host must answer `{"status":"ok"}` within 5 minutes, else the job fails.
4. **One deploy at a time.** A concurrency group that never cancels a deploy in flight.
5. **Its own credential.** The deploy job runs in the GitHub environment `production`, which admits every branch for the pull request plans (ADR 0010); the workflow itself runs only for main and accepts a manual run only from main. It reads only `CLOUDFLARE_DEPLOY_API_TOKEN`, an account token made by hand with what `wrangler deploy` needs, and `CLOUDFLARE_ACCOUNT_ID`. It never reads terraform's stronger `CLOUDFLARE_API_TOKEN` from the same environment. The token goes only into the env of the wrangler step. There is no other registry login: wrangler pushes the images with the same token.
6. **Rollback is a deploy of an earlier commit.** Run the workflow by hand with the sha of a commit of main that `ci` passed for. It deploys that commit without the paths check. Migrations are forward-only and are not rolled back.

## Consequences

- A merge of an app change is a deploy. A change to the boxes' networking passes the network gate (runbook, bootstrap step 9) before it is merged, not before a deploy.
- The health check shows that the public host is healthy, not which version answers.
- The runbook (`docs/engineering/cloudflare-operations.md`, Deploys) lists every secret and variable the workflow reads, and where each value comes from.
