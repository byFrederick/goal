---
paths:
  - "**/*.go"
---

# Go

How the code is laid out and why: [architecture.md](../../docs/engineering/architecture.md). Lint rules: `.golangci.yml`, run by `task lint`.

## Packages

- Give each package one responsibility and at least one caller, so its name says what it does.
- Never create `util`, `common`, `helpers` or `misc` packages, because they collect unrelated code and import cycles.
- Delete code in the change that makes it dead, so nobody maintains what nothing calls.

## Errors

- Wrap errors with `%w` and the context of the step that failed, so `errors.Is` still works and the log says where.
- Declare sentinel errors (`var ErrX = errors.New(...)`) for conditions callers branch on, and compare them with `errors.Is`, never by message.
- Handle an error in one place: either return it or log it, never both, so each failure is reported once.

## Context

- Take `ctx context.Context` as the first parameter of anything that does I/O or may block, so cancellation and deadlines reach it.
- Pass the caller's context down instead of creating `context.Background()` mid-call, because a detached call outlives its request.

## Logging

- Log with `log/slog` and stable event names (`"api request"`, not formatted sentences), so logs can be searched and alerted on.
- Never log a secret, token, cookie, credential or email, because logs leave the platform.

## CLI (`cmd/goal`)

- Parse arguments with the standard library `flag` package, never cobra or another framework, so the CLI stays small and its parsing testable.
- Use no `init()` in any binary, so wiring happens only in `main`.

## Skills

Load `golang-code-style`, `golang-error-handling`, `golang-concurrency`, `golang-lint`, `golang-security`, `golang-performance` and `golang-observability`; `golang-cli` for `cmd/goal`; `golang-database` for pgx code.
