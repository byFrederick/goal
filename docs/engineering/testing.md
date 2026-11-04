# Testing

Tests exist to stop real regressions. A test that would still pass if the behavior broke is noise and gets deleted in review. Sources: the testing-strategy, test-driven-development, golang-testing and code-review skills.

## What to test

| Layer | How | Examples |
| --- | --- | --- |
| `internal/core/engine` | Unit tests with in-memory fakes from `internal/core/ports/porttest`, table-driven, `t.Parallel()` | expedite pulls first; WIP is never exceeded; buffer below target releases intake; rework past `maxRework` goes to a human; andon pauses intake |
| `internal/config` | Unit tests on small, self-contained fixtures in `internal/config/testdata` (sample org `acme`) and in-memory `fstest.MapFS` trees; never on real factory definitions, which live in the bucket | every cross-reference error is reported; a path cannot leave the factory's tree |
| `internal/adapters/objectstore/s3` | Integration tests (`//go:build integration`) against RustFS from `compose.yaml`, one fresh bucket per test (created by `EnsureBucket`, so without versioning, as on R2), removed afterwards | versioning is never turned on; a put replaces the object and its ETag moves; a delete is for good; list returns the objects under a prefix; a prefix purge removes every page and nothing beside it; a conditional put writes a key once (and its HEAD-then-PUT fallback too); a bucket with versioning on behaves the same |
| `internal/core/ports/porttest/storetest` | The `Store` conformance suite, run by the in-memory fake (unit) and by the PostgreSQL adapter (integration), so engine tests on the fake hold for the real store | pull order; slot and lease rules; compare-and-set transitions; requeue fencing; concurrent claims never share a slot or an item |
| `internal/core/ports/porttest/definitionstest` | The `DefinitionStore` and `DefinitionNotices` conformance suite, run by `porttest.Definitions` (unit) and the PostgreSQL adapter (integration) | one draft per factory; stale revisions and moved current versions refused; gapless numbers under concurrent publishers; station moves or refusal; restore as a pointer move; blobs stored once and verified; audit entries; a notice after every commit; another org sees nothing |
| `internal/core/ports/porttest/sandboxtest` | The `Sandboxes` conformance suite, run by `porttest.Sandboxes` (unit, with `sandboxtest.Script` answering its commands) and by every sandbox adapter (`docker` integration; `cloudflare` against staging), so engine tests on the fake hold for real boxes | one box per run; streamed stdout, exit codes, timeouts and the stderr tail from the runtime; env per command; cancel and delete kill running commands; delete is idempotent; list by run and org; public HTTPS reachable, private addresses not (adapters with a network) |
| `internal/core/ports/porttest/knowledgetest` | The `Knowledge` conformance suite, run by `porttest.Knowledge` (unit) and the PostgreSQL adapter (integration) | sources and the connections they read (one in use is not deleted); a lease that ended is taken over and the lost holder's renewal fails; the schedule and Sync now, and an on-demand source due only after Sync now; failures in a row; uploads by name, replaced by name, and the content no upload holds; snapshots and current files, the read counts; another org sees and writes nothing |
| `internal/knowledge` | Unit tests over the port fakes and an in-memory source reader | incremental syncs fetch only changed versions and leave out what went; a file that fails is listed, not fatal; failures recorded on the source; a goald killed mid-sync keeps its renewed lease until it ends, then another syncs; uploads stored once, replaced and deleted with their last holder; the snapshot's copied entries, limits, key-like files and retention; extraction of HTML |
| `internal/adapters/store/postgres` | Integration tests (`//go:build integration`) against PostgreSQL 18 from `compose.yaml`, one fresh database per test (schema `auth` exists once per database), migrated from zero and dropped afterwards (`DROP DATABASE … WITH (FORCE)`; `task test-integration` first drops `goal_test_*` databases over an hour old that a killed run left) | the conformance suite; migrations are repeatable and safe to run concurrently; schema `auth` is Better Auth's and `goal_auth`'s alone |
| `apps/auth/` (identity service) | Vitest: unit tests without a database in `task check`; `*.integration.test.ts` against PostgreSQL 18 from `compose.yaml`, one fresh database per file migrated by the goose files, in `task test-integration` | settings refusals; the service starts only as `goal_auth` on a database at the goose head; the session format contract goald relies on |
| `internal/adapters/sandboxes/docker`, `ecs` | Integration tests behind build tags; ECS only in a sandbox account | a task starts, streams, is killed on timeout; a command check runs in the workspace without secrets and stops at its timeout |
| `internal/definitions` | Unit tests over the in-memory bucket | contracts validate outputs and cannot reference files or the network; agent files are staged; a broken push keeps the last valid definition |
| HTTP API (`internal/api`) | Handler tests with `httptest` against the real engine, the definitions cache and port fakes | status codes and problem details, work-type validation, event paging, human decisions |
| CLIs (`cmd/`) | Unit tests on argument parsing, settings and the follow loop against a stub API | flags after positionals; unsafe settings refused; each event printed once until a human must act |
| Worker (TypeScript) | Vitest on the policy hook and envelope parsing; one smoke test with a recorded SDK stream | deny rules block; contract violations fail |
| UI | Playwright on critical flows at 375, 768, 1440 px; accessibility checks (CI's e2e job runs `task web:e2e`) | approve an item; move a card without dragging |
| Agents and factories | Jonah eval suites, not unit tests | see `docs/history/jonah/` (the design phase 5 replaced) |

## Rules

The rules for writing tests (what not to test, fakes instead of mocks, failing test first, bounded runs, no timing ratios) are in [`.claude/rules/tests.md`](../../.claude/rules/tests.md), which Claude Code loads whenever a test file is touched.

Tests and helpers that run git must not inherit the GIT_* repository variables (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE` and the others). git sets them when it runs a hook, and they override `git -C`, so a test that inherits them commits, checks out and changes config in the repository being pushed instead of its temporary one. Build the command's environment with a filter like `gitEnv` in `tools/checkplan/main.go`. The pre-push hook also unsets these variables before `task check`.

## Commands

```bash
task test               # unit tests with -race
task test-integration   # starts PostgreSQL 18 and RustFS via compose, runs integration-tagged tests
task check              # everything the pre-push hook and CI (pull requests into main) run
task web:e2e            # the web Playwright suite; CI runs it in the e2e job, on pull requests into main that change apps/web/, packages/api-client/ or contracts/, and on every push to main
task check:changed      # only the checks of the parts this branch changed since origin/main (README, Development)
task e2e:secrets        # a run on secrets against a private repository, the check before deploying main (below)
```

### A run on secrets end to end

`task e2e:secrets` runs `TestE2E_RunOnSecrets` (`cmd/goald/secrets_e2e_test.go`, build tag `e2e`). It is the check to run before deploying main ([cloudflare-operations.md](cloudflare-operations.md#deploys)). It starts a throwaway goald and auth service as `task e2e:auth` does, on their own database and bucket, with an agents network and egress proxy of its own, so the install's goald, network and proxy are never touched. It then publishes a version whose repository token and model key are organization secrets, runs one item with a real model on `claude-haiku-5-5`, and checks:

- the coder's push lands on the part's branch, and the part's pull request has that commit as its head;
- the assembly's integration branch (`goal/a<n>-…`) exists on the remote and holds the part's file;
- the integrator's approval merges the pull request into the integration branch, with a `merged` event, and the default branch does not move;
- the environment's MCP server tool that its `tools` map allows runs, with no `tool_denied` or `tool_approval_requested` event for it;
- `task --version` runs in the box;
- every `sandbox_started` event of the part has `size` large.

It needs Docker, `goal db init` (the roles) and three variables, in `.env` or the shell:

| Variable | Example | What it is |
| --- | --- | --- |
| `GOAL_E2E_REPOSITORY_URL` | `https://github.com/acme/goal-e2e.git` | a private github.com repository used for nothing else, with a default branch |
| `GOAL_E2E_REPOSITORY_TOKEN` | `github_pat_example` | a fine-grained token for that repository: contents and pull requests, read and write |
| `GOAL_E2E_MODEL_KEY` | `sk-ant-api03-example` | an Anthropic API key; a run costs a few dollars of it |

Without any one of them the test skips, as in CI. It takes up to 45 minutes (`-timeout 45m`). It never writes the repository's default branch: the line holds the merged part at a human station, so the assembly never lands. Whether it passes or fails, it deletes every `goal/` branch it created on the remote. `task e2e:auth` leaves this test out.
