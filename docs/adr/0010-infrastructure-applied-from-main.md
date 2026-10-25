# ADR 0010 — Infrastructure applied from main by CI

Status: accepted · 2026-10-25 · Builds on [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (the Cloudflare install and `terraform/`) · Changed AGENTS.md rule 8 on 2026-10-25, when the apply workflow landed (Rollout step 3): the apply-on-merge workflow applies, agents never run `tf:apply`

## Context

`terraform/` holds the platform's own infrastructure: the Cloudflare bucket, DNS, route, tokens and Secrets Store secrets, the PlanetScale branch and its administrator, and the sign-in Google project (AGENTS.md rule 8, [runbook](../engineering/cloudflare-operations.md)). Until now the owner applied it by hand from a laptop: `task tf:apply` with the operator's credentials, after reading the plan a pull request carried. On 2026-10-25 the owner decided that CI applies it instead: a plan on every pull request that touches `terraform/`, and an apply on merge to main.

`task tf:plan` and `task tf:apply` could not run in GitHub Actions. Google's token came only from a signed-in `goal` gcloud configuration, and the age key that opens `terraform/secrets/production.sops.yaml` was only a file on the operator's machine.

What the credentials can reach decides the design. The state holds every platform secret in plain text: the generated passwords, the database administrator's URL and the master keys, which are also in the SOPS file. So reading the state, or holding the age key, shows every secret, and with the master keys every organization's sealed credentials. A `pull_request` workflow from a branch of this repository runs that branch's own copy of the workflow file. Anyone who can push a branch here, agents included, can therefore change the plan job to print whatever credentials it gets.

## Decision

1. **The apply-on-merge workflow applies production from main.** On a push to main that touches `terraform/`, one job in the GitHub environment `production`, one run at a time (a concurrency group that never cancels a run in flight), plans to a saved file and applies that file, non-interactively. It applies what the merged commit planned, nothing newer. Then a `-detailed-exitcode` plan fails the job if anything is left: drift or an error fails loudly, never silently.
2. **Pull requests get a plan, from read-only credentials.** On a pull request that touches `terraform/`, from this repository only (never a fork), one job in the GitHub environment `production-plan` runs `task tf:check` and `task tf:plan -- -lock=false`. It posts the plan as one comment, updated on each push, with sensitive values masked as Terraform masks them.
3. **Agents never run `tf:apply` themselves.** They `fmt`, check, test and plan. A change reaches production by a pull request that a person merges.
4. **Two environments split the credentials.**
   - `production-plan` holds read-only tokens wherever the provider has them: Cloudflare read scopes, a PlanetScale read token, an R2 Object Read token for the state (hence `-lock=false`), and Google's `terraform-plan` service account (project viewer).
   - `production` holds the write tokens and Google's `terraform-apply` service account. Its deployment branches are `main` only, so only the apply job gets them.

   Every token is new and dedicated to CI, so revoking one breaks nothing else. The runbook lists each secret, its scopes and where its value comes from.
5. **Google without a key.** Workload Identity Federation in the sign-in project (`terraform/ci.tf`) trusts GitHub's OIDC tokens only from `byFrederick/goal`, matched by repository id and owner id so that a renamed or recreated namesake gets nothing, and only from the environments `production-plan` and `production`. Each environment can impersonate only its own service account. `terraform-apply` gets a custom role with exactly what `terraform/` manages in that project, never a basic role. The workflow exchanges its OIDC token for an access token, and the `tf:*` tasks take it as `GOOGLE_OAUTH_ACCESS_TOKEN` when `GITHUB_ACTIONS=true`. On a laptop nothing changes: gcloud's `goal` configuration, and any token left in the shell is overwritten.
6. **The bootstrap stays manual.** The owner applies the federation resources once from a laptop with `-target` (runbook, bootstrap step 13), because no workflow can plan before they exist.

## The risk we accept

Read-only tokens limit what a leak can **change**, not what it can **read**. Any branch pushed to this repository can rewrite the plan workflow and read `production-plan`'s secrets: the state token reads every secret in the state, and the age key, which cannot be made read-only, opens the master keys. That is the same exposure as the operator's laptop, now extended to everyone who can push a branch here. The mitigations:

- only people and agents the owner trusts can push here;
- forks get nothing;
- the owner may require reviewers on `production-plan`, so a person approves each plan job before it receives the credentials;
- every token expires and is rotated (runbook, Other rotations); a suspected leak is handled as one of the state token: revoke it, then rotate every secret and the master key.

`production`'s write credentials are reachable only from main, so only from merged code.

## Rollout

Three changes, in order:

1. *Done 2026-10-25 (PR #9).* The federation, the CI credential path of the `tf:*` tasks, the runbook's list of GitHub environment secrets, and this ADR. AGENTS.md rule 8 still says only the owner applies, and the owner applies the federation resources.
2. The plan workflow (`.github/workflows/terraform-plan.yml`).
3. *Done 2026-10-25.* The apply workflow (`.github/workflows/terraform-apply.yml`). In the same pull request, AGENTS.md rule 8, the runbook's Deploys section and the `tf:apply` comments in `Taskfile.yml` change to say that the apply-on-merge workflow applies and that agents never run `tf:apply`.

Until step 3 merged, the rule in force was the old one: only the owner applied, or the coordinating agent the owner authorized for that change. Step 3 merged before step 2, so until the plan workflow lands a pull request's plan is the one its author pastes in. The workflow applies nothing until the owner has done bootstrap step 13 and filled the `production` environment (runbook, GitHub environments).

## Rejected

- **One environment with the write credentials for both jobs.** A branch's plan job could then change production, not only read it.
- **A Google service account key in a secret.** It is long-lived and can be copied. Federation issues short-lived tokens bound to this repository and its environments.
- **Plans only after merge.** Reviewers need the plan before they merge, not after.
- **`pull_request_target` for forks.** It would run untrusted code with the credentials. Forks get no plan; a maintainer pushes the change to a branch here.
