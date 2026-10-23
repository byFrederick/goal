# Testing

Tests exist to stop real regressions. A test that would still pass if the behavior broke is noise and gets deleted in review. Sources: the testing-strategy, test-driven-development, golang-testing and code-review skills.

## What to test

| Layer | How | Examples |
| --- | --- | --- |
| `internal/engine` | Unit tests with in-memory fakes from `internal/ports/porttest`, table-driven, `t.Parallel()` | expedite pulls first; WIP is never exceeded; buffer below target releases intake; rework past `maxRework` goes to a human; andon pauses intake |
| `internal/config` | Unit tests on real example factories plus small broken fixtures | every cross-reference error is reported (already in place) |
| `internal/adapters/postgres` | Integration tests (`//go:build integration`) against PostgreSQL 18 from `compose.yaml` | concurrent claims never return the same item; lease expiry requeues; migrations apply from zero |
| `internal/adapters/docker`, `ecs` | Integration tests behind build tags; ECS only in a sandbox account | a task starts, streams, is killed on timeout |
| HTTP API | Handler tests with `httptest` against the engine and fakes | auth, validation errors, SSE framing |
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
make test               # unit tests with -race
make test-integration   # starts PostgreSQL 18 via compose, runs integration-tagged tests
make check              # everything CI runs
```
