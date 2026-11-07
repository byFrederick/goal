# ADR 0044 — goald waits for CI, not run boxes

Status: accepted · 2026-11-06 · Builds on [ADR 0043](0043-merge-stations-take-turns-per-target.md) (the merge turn an item keeps while it waits), [ADR 0041](0041-code-and-github-without-an-app.md) (host kinds, `runtime.repositories` and the tokens a code host is read with) and [ADR 0015](0015-overlapping-in-flight-work.md) (engine-authored waits, the claim gate and the `## Changed since your plan` brief section)

## Context

Run boxes bill by the second, and their agents wait for CI themselves: they push, then poll the host until the checks finish. The 2026-11-06 cost assessment found that waiting for CI takes 43% of all run-box time: about 70% of the reviewer's, 85% of the infrastructure engineer's and 30% of the integrator's. A deploy of goald cancels every run, so every run that was waiting starts its wait again.

Checks do not appear the moment a commit is pushed. On Goal they registered 4 to 8 s after a push, and at most 121 s later across 45 pushes. software#777 read "no checks reported" right after `gh pr ready`, before any check had registered.

goald already reads code hosts. `ports.CodeHost` (`internal/core/ports/ports.go`) reads and merges pull requests, and `mergeTargets` (`internal/core/engine/merge.go`) picks each repository's adapter and token. `deferMerge` sends an item whose merge was refused back to its station's queue so its agent runs again. The claim gate skips an item with an open edge, and `holdTurns` (`internal/core/engine/turns.go`) keeps one item per merge target holding the turn. `agent_run_repositories` (migration 00132) records, per run and repository, whether the run pushed (`wrote`) and the branch's commit on the remote when the agent ended (`pushed_head`). `changedSinceNote` (`internal/core/engine/basecommit.go`) writes a section at the top of a brief from what goald read.

## Decision

1. **A station waits for CI with one line, `waitFor: ci`.** The long form is `waitFor: {ci: {grace: 5m, max: 60m, rounds: 3}}`; a field left out takes its default (grace 5m, max 60m, rounds 3). Nothing in the setting names a host, a CI system, a workflow or a check. A station without `waitFor` works as it does today.
2. **A run at the station starts only once CI on the item's current commits has finished.** The claim gate skips the item until then. Its brief starts with a `## CI` section, as `## Changed since your plan` does: for each repository, the commit, and each check's name and result, with links. The agent never polls the host for CI.
3. **After a push, the item's move forward is held until CI on what it pushed finishes.** When a run at the station pushed commits and the item would move forward (an approval, or a station without a gate finishing), the item goes back to the station's queue with the outcome held on the wait, and the claim gate skips it until CI finishes.
   - Passed: the held outcome is applied, including the merge at an `action: merge` station.
   - Failed, or not finished within `max`: the station runs again (back to its own queue, as `deferMerge` sends it), with the failing checks in the brief.
   - After `rounds` CI rounds in one visit to the station, the item blocks for a person, naming the failing checks.
   - A rejection goes back at once, without waiting for CI.
   - While it waits, the item keeps its merge turn (ADR 0043's turn edges), so no other item merges into the same target in between.
4. **The commits waited on are the latest each repository was pushed to.** For each repository the item's runs pushed to (`agent_run_repositories` rows with `wrote`), the commit is the latest `pushed_head`. When one of the item's pull requests (`Item.PullRequests`) is in that repository, its current head is used instead, because someone else or goald's `UpdateBranch` may have pushed to it. A newer head replaces the commit being waited on. An item with no pushed commit has nothing to wait for.
5. **CI is read with the host and token a merge uses.** The lookup is `mergeTargets`': the adapter of the repository's host kind, and the token of the `runtime.repositories` entry that covers it, else `runtime.repository.token` (connections are gone, [ADR 0037](0037-secrets-and-environment-files.md)).
6. **The port gains one read-only method, `ports.CodeHost.Checks(ctx, repo, token, sha, url) (CommitChecks, error)`.** When `url` is set, it returns the pull request's current head as well. For each check it returns the name, the state (`pending`, `running`, `finished`), the result (`passed`, `failed`, `neutral`) and a link, and a few failed job names where the host gives them.
7. **A wait ends at whichever comes first:**
   - every check finished;
   - no check appeared within `grace` after the run ended, so that commit gets no CI;
   - `max` passed;
   - the head moved: the wait follows the new head.
8. **Hidden CI is not "no CI".** When the token cannot read CI (a GitHub 403; on GitLab, `jobsEnabled` false while `builds_access_level` is not `disabled`), the item blocks for a person, naming the token and the permission it needs. Hidden CI never counts as passed.
9. **goald polls each commit once, from a database row per wait.** Each wait is a row in PostgreSQL. goald polls each (host, repository, commit) once however many items wait on it: every 15 s at first, backing off to every 2 min. On GitHub it sends conditional requests with the ETag, since a 304 does not count against the rate limit. It honours `Retry-After`, `RateLimit-Reset` and `x-ratelimit-reset`. Instances share the polling through leases, a restart picks the waits up where they were, and no run is cancelled. goald polls instead of taking webhooks, so nobody has to set anything up on the host.
10. **A CI wait is visible on the item and counted in lead time.** A new `WaitingKind` `ci` shows in the item's waiting field, such as "Waiting for CI · 9 of 14" or "CI failed: task web:e2e (3/12)", with the details behind an info tip. Lead time counts it as a wait of its own. `RepositoryAccess` also reports whether the token can read CI: a GitHub fine-grained token needs read access to Checks and to Commit statuses, and a GitLab token needs `read_api`.
11. **Out of scope:** merge queues, hosts without a `CodeHost` adapter, and changing Goal's own factory definition.

## Consequences

- A run box no longer idles while CI runs, and a deploy no longer restarts anyone's wait: the wait is a row, not a running agent.
- Many items waiting on one commit cost one poll per interval, and conditional requests keep GitHub's rate limit mostly untouched.
- A factory opts in per station with one line. A station that stays without `waitFor` keeps its agents waiting in their boxes, as today.
- A token that could merge but not read CI now blocks items at `waitFor` stations until a person grants the permission; `RepositoryAccess` shows this before it happens.
- A commit whose checks register later than `grace` is treated as having no CI. The 5 min default is well above the 121 s measured on Goal; a slower host needs a longer `grace`.
- The parts of assembly software#830 build this: the setting, the port and its GitHub and GitLab adapters, the access check, the store and the poller, the gates before and after a run, and the API and UI that show the wait. They use the names here (`waitFor`, `CodeHost.Checks`, `CommitChecks`, `WaitingKind` `ci`), or amend this ADR with a date.
