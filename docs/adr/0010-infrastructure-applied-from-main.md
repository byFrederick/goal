# ADR 0010 — Infrastructure applied from main by CI

Status: accepted · 2026-10-25 · Amended 2026-10-25: one GitHub environment, `production`, for both jobs; it replaces the planned `production-plan` environment and its viewer-only `terraform-plan` service account, which were never set up · Builds on [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (the Cloudflare install and `terraform/`) · Changes AGENTS.md rule 8 when the apply workflow lands (below)

## Context

`terraform/` holds the platform's own infrastructure: the Cloudflare bucket, DNS, route, tokens and Secrets Store secrets, the PlanetScale branch and its administrator, and the sign-in Google project (AGENTS.md rule 8, [runbook](../engineering/cloudflare-operations.md)). Until now the owner applied it by hand from a laptop: `task tf:apply` with the operator's credentials, after reading the plan a pull request carried. On 2026-10-25 the owner decided that CI applies it instead: a plan on every pull request that touches `terraform/`, and an apply on merge to main.

`task tf:plan` and `task tf:apply` could not run in GitHub Actions. Google's token came only from a signed-in `goal` gcloud configuration, and the age key that opens `terraform/secrets/production.sops.yaml` was only a file on the operator's machine.

What the credentials can reach decides the design. The state holds every platform secret in plain text: the generated passwords, the database administrator's URL and the master keys, which are also in the SOPS file. So reading the state, or holding the age key, shows every secret, and with the master keys every organization's sealed credentials. A `pull_request` workflow from a branch of this repository runs that branch's own copy of the workflow file. Anyone who can push a branch here, agents included, can therefore change the plan job to print whatever credentials it gets.

## Decision

1. **The apply-on-merge workflow applies production from main.** On a push to main that touches `terraform/`, one job in the GitHub environment `production`, one run at a time (a concurrency group that never cancels a run in flight), plans to a saved file and applies that file, non-interactively. It applies what the merged commit planned, nothing newer. Then a `-detailed-exitcode` plan fails the job if anything is left: drift or an error fails loudly, never silently.
2. **Pull requests get a plan.** On a pull request that touches `terraform/`, from this repository only (never a fork), one job in the GitHub environment `production` runs `task tf:check` and `task tf:plan -- -lock=false` (by choice: a pull request's plan never holds the state lock against an apply). It posts the plan as one comment, updated on each push, with sensitive values masked as Terraform masks them.
3. **Agents never run `tf:apply` themselves.** They `fmt`, check, test and plan. A change reaches production by a pull request that a person merges.
4. **One environment holds the credentials.** `production` holds the operator's existing credentials (the Cloudflare, PlanetScale and R2 state tokens and the age key, as the runbook lists them) and is the only environment Google's `terraform-apply` service account trusts. Its deployment branches cannot be limited to `main`, because pull request plans run from other branches. The owner may require reviewers on it. Revoking one of these credentials breaks the laptop too, so a rotation renews it in both places.
5. **Google without a key.** Workload Identity Federation in the sign-in project (`terraform/ci.tf`) trusts GitHub's OIDC tokens only from `byFrederick/goal`, matched by repository id and owner id so that a renamed or recreated namesake gets nothing, and only from the environment `production`, which alone can impersonate `terraform-apply`. `terraform-apply` gets a custom role with exactly what `terraform/` manages in that project, never a basic role. The workflow exchanges its OIDC token for an access token, and the `tf:*` tasks take it as `GOOGLE_OAUTH_ACCESS_TOKEN` when `GITHUB_ACTIONS=true`. On a laptop nothing changes: gcloud's `goal` configuration, and any token left in the shell is overwritten.
6. **The bootstrap stays manual.** The owner applies the federation resources once from a laptop with `-target` (runbook, bootstrap step 13), because no workflow can plan before they exist.

## The risk we accept

Any branch pushed to this repository can rewrite the plan workflow and get `production`'s credentials. They read everything (the state token reads every secret in the state, and the age key opens the master keys) and they can change production: the Cloudflare, PlanetScale and state tokens are write tokens, and `terraform-apply` can change the sign-in project's IAM. That is the operator's laptop's power, extended to everyone who can push a branch here. The owner accepted this on 2026-10-25 because only the owner and the factory push branches to this repository. The mitigations:

- only the owner and the factory can push here;
- forks get nothing;
- the owner may require reviewers on `production`, so a person approves each job before it receives the credentials;
- every token expires and is rotated (runbook, Other rotations); a suspected leak is handled as one of the state token: revoke it, then rotate every secret and the master key.

## Rollout

Three changes, in order:

1. The federation, the CI credential path of the `tf:*` tasks, the runbook's list of GitHub environment secrets, and this ADR. AGENTS.md rule 8 still says only the owner applies, and the owner applies the federation resources.
2. The plan workflow (`.github/workflows/terraform-plan.yml`).
3. The apply workflow (`.github/workflows/terraform-apply.yml`). In the same pull request, AGENTS.md rule 8, the runbook's Deploys section and the `tf:apply` comments in `Taskfile.yml` change to say that the apply-on-merge workflow applies and that agents never run `tf:apply`.

Until step 3 merges, the rule in force is today's: only the owner applies, or the coordinating agent the owner authorizes for that change.

## Rejected

- **One environment with the write credentials for both jobs: accepted on 2026-10-25.** Rejected at first because a branch's plan job could then change production, not only read it. The owner accepted it (amendment above): only the owner and the factory push branches here.
- **A Google service account key in a secret.** It is long-lived and can be copied. Federation issues short-lived tokens bound to this repository and its environments.
- **Plans only after merge.** Reviewers need the plan before they merge, not after.
- **`pull_request_target` for forks.** It would run untrusted code with the credentials. Forks get no plan; a maintainer pushes the change to a branch here.
