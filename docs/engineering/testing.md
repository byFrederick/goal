# Testing

Tests exist to stop real regressions. A test that would still pass if the behavior broke is noise and gets deleted in review. Sources: the testing-strategy, test-driven-development, golang-testing and code-review skills.

## What to test

| Layer | How | Examples |
| --- | --- | --- |
| `internal/engine` | Unit tests with in-memory fakes from `internal/ports/porttest`, table-driven, `t.Parallel()` | expedite pulls first; WIP is never exceeded; buffer below target releases intake; rework past `maxRework` goes to a human; andon pauses intake |
| `internal/config` | Unit tests on small, self-contained fixtures in `internal/config/testdata` (sample org `acme`) and in-memory `fstest.MapFS` trees; never on real factory definitions, which live in the bucket | every cross-reference error is reported; a path cannot leave the factory's tree |
| `internal/factorystore` | Unit tests with the in-memory `porttest.ObjectStore` | push then pull round-trips a factory; unchanged files keep their version; removed files are deleted but restorable; `knowledge/` is never touched; an invalid factory writes nothing |
| `internal/adapters/s3store` | Integration tests (`//go:build integration`) against RustFS from `compose.yaml`, one fresh bucket per test, removed afterwards | every version stays readable; deleted keys are not found but restorable by version; list returns current objects only; a conditional put writes a key once (and its HEAD-then-PUT fallback too) |
| `internal/ports/porttest/storetest` | The `Store` conformance suite, run by the in-memory fake (unit) and by the PostgreSQL adapter (integration), so engine tests on the fake hold for the real store | pull order; slot and lease rules; compare-and-set transitions; requeue fencing; concurrent claims never share a slot or an item |
| `internal/ports/porttest/definitionstest` | The `DefinitionStore` and `DefinitionNotices` conformance suite, run by `porttest.Definitions` (unit) and the PostgreSQL adapter (integration) | one draft per factory; stale revisions and moved current versions refused; gapless numbers under concurrent publishers; station moves or refusal; restore as a pointer move; blobs stored once and verified; audit entries; a notice after every commit; another org sees nothing |
| `internal/ports/porttest/sandboxtest` | The `Sandboxes` conformance suite, run by `porttest.Sandboxes` (unit, with `sandboxtest.Script` answering its commands) and by every sandbox adapter (`docker` integration; `cloudflare` against staging), so engine tests on the fake hold for real boxes | one box per run; streamed stdout, exit codes, timeouts and the stderr tail from the runtime; env per command; cancel and delete kill running commands; delete is idempotent; list by run and org; public HTTPS reachable, private addresses not (adapters with a network) |
| `internal/adapters/postgres` | Integration tests (`//go:build integration`) against PostgreSQL 18 from `compose.yaml`, one fresh database per test (schema `auth` exists once per database), migrated from zero and dropped afterwards (`DROP DATABASE … WITH (FORCE)`; `task test-integration` first drops `goal_test_*` databases over an hour old that a killed run left) | the conformance suite; migrations are repeatable and safe to run concurrently; schema `auth` is Better Auth's and `goal_auth`'s alone |
| `auth/` (identity service) | Vitest: unit tests without a database in `task check`; `*.integration.test.ts` against PostgreSQL 18 from `compose.yaml`, one fresh database per file migrated by the goose files, in `task test-integration` | settings refusals; the service starts only as `goal_auth` on a database at the goose head; the session format contract goald relies on |
| `internal/adapters/docker`, `ecs` | Integration tests behind build tags; ECS only in a sandbox account | a task starts, streams, is killed on timeout; a command check runs in the workspace without secrets and stops at its timeout |
| `internal/adapters/gitworkspace` | Unit tests against a local smart-HTTP git server (`git http-backend` behind `httptest`) that requires the token | the item's branch is checked out (new or existing); no file under the workspace holds the token; a failed clone leaves nothing |
| `internal/definitions` | Unit tests over the in-memory bucket | contracts validate outputs and cannot reference files or the network; agent files are staged; a broken push keeps the last valid definition |
| HTTP API (`internal/api`) | Handler tests with `httptest` against the real engine, the definitions cache and port fakes | status codes and problem details, work-type validation, event paging, human decisions |
| CLIs (`cmd/`) | Unit tests on argument parsing, settings and the follow loop against a stub API | flags after positionals; unsafe settings refused; each event printed once until a human must act |
| Worker (TypeScript) | Vitest on the policy hook and envelope parsing; one smoke test with a recorded SDK stream | deny rules block; contract violations fail |
| UI | Playwright on critical flows at 375, 768, 1440 px; accessibility checks | approve an item; move a card without dragging |
| Agents and factories | Jonah eval suites, not unit tests | see `docs/jonah/` |

## What not to test

- Constructors, getters, struct literals, generated sqlc code, wiring in `cmd/`.
- The standard library, pgx, the Docker SDK or the Agent SDK themselves.
- Implementation details that a refactor would change without changing behavior.
- Anything by mocking a type we own; use the port fakes.

## Rules

- A bug fix starts with a failing test that reproduces it.
- Test names describe behavior: `TestClaim_ExpediteBeforePriority`, not `TestClaim2`.
- No sleeps for synchronization; use the fake `Clock` and channels.
- Integration tests create their own schema in a fresh database and clean up; they never depend on order.
- Flaky tests are fixed or deleted within a day; they are never retried silently.
- Coverage is reported, never gated. Review asks "which behavior does this protect?"

## Commands

```bash
task test               # unit tests with -race
task test-integration   # starts PostgreSQL 18 and RustFS via compose, runs integration-tagged tests
task check              # everything the pre-push hook (and CI, when enabled) runs
```
