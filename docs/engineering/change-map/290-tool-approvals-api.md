## When you change

The tool-approval routes and schemas: `ToolApproval`, `ToolDecision`, `StandingRule`, the `tool_approval` inbox kind, `Item.tool_approval`, `decideToolApproval`, `listStandingRules` or `revokeStandingRule` in `contracts/api/openapi.yaml`, or their handlers in `internal/api/toolapproval.go` and `internal/api/inbox.go`

## Also change

`task api:gen`'s generated files; the handlers and `internal/api/toolapproval_test.go`; the probes in `internal/api/org_test.go` and `internal/api/testdata/access.golden` (`-update`) for a new route; `docs/engineering/architecture.md` ("HTTP API"); the client, CLI and Needs you screens that read them ([ADR 0031](../../adr/0031-tool-calls-ask-first-and-standing-rules.md) decision 10)

## Run

`task api:gen`, `task api:permissions-check`, `task test` and `task api-client:check`
