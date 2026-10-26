---
paths:
  - "**/*_test.go"
  - "**/*.test.ts"
  - "**/*.test.tsx"
---

# Tests

What to test at each layer, and the commands: [testing.md](../../docs/engineering/testing.md).

## What a test is for

- Test behavior, so a test fails when the behavior breaks and survives a refactor that keeps it.
- Name tests after the behavior (`TestClaim_ExpediteBeforePriority`, not `TestClaim2`), so a failure says what broke.
- Start every bug fix with a failing test that reproduces it, so the fix is proven and the bug cannot return.
- Ask "which behavior does this protect?" instead of chasing coverage, which is reported, never gated.

## What not to test

- Skip constructors, getters, struct literals, generated code (sqlc, oapi-codegen, the TypeScript schema) and wiring in `cmd/`, because they break nothing a test would catch.
- Skip the standard library, pgx, the Docker SDK and the Agent SDK themselves, because their authors test them.
- Never mock a type we own; use the in-memory fakes in `internal/core/ports/porttest`, because the conformance suites keep them faithful to the adapters.

## How

- Use the fake `Clock` and channels, never sleeps, to synchronize, so tests are fast and deterministic.
- Never assert on a ratio of timings; count the work done instead (bytes read, calls made), because timing varies by machine and flakes.
- Bound every run with `-run` and `-timeout` (`5m` unit, `10m` integration), so a hung test fails with its name instead of stalling.
- Tag adapter tests `//go:build integration` and give each a fresh database or bucket it cleans up, so they never depend on order.
- Fix or delete a flaky test within a day and never retry it silently, because a flaky test teaches people to ignore failures.
- Run `task test` for unit tests with `-race`; CI also runs `task test-integration` and `task check`.

## Skills

Load `testing-strategy`, `test-driven-development` and `golang-testing`; `vitest` for TypeScript tests.
