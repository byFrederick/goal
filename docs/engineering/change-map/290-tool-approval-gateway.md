## When you change

The gateway's tool approvals or the bundle's tool fields: `internal/gateway/toolapproval.go` (`POST /run/tool-approval`, `ToolApprovals`, its refusals and the 16 KiB input limit) or `tool_modes`, `always_allow_offered` and `granted_calls` in `internal/gateway/bundle.go`

## Also change

A new gateway path in `api` (`internal/api/host.go`, `CheckHost`), `cmd/goald` (`routes`, `gatewayHandler`) and `internal/egress` (`gatewayPaths`) together; `engineToolApprovals` in `cmd/goald/main.go` when the engine's refusals change; `internal/gateway/testdata/run-bundle.json` (`go test -run TestServeRun_ServesTheRunsBundle -update`), the worker's `TaskSpec` in `apps/worker/src/spec.ts` and `apps/worker/README.md` (spec.json) when a bundle field changes; `docs/engineering/architecture.md` ("Ask-first tool calls")

## Run

`go test -race ./internal/gateway ./internal/api ./internal/egress ./cmd/goald` and `pnpm --dir apps/worker exec vitest run src/prepare.test.ts`
