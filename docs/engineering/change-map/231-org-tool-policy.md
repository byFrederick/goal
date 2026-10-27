## When you change

A tool default of the organization policy (`domain.OrgPolicy`'s `WriteOutsideBranch`, `MergePullRequest`, `ReadWeb` or `UseCredentials` in `internal/core/domain/orgpolicy.go`), or how the engine applies them at run start (`applyPolicy` and the merge-gate branch of `finish` in `internal/core/engine/work.go`) ([ADR 0024](../adr/0024-organization-policy.md))

## Also change

The task token's `DeniedCommands` and `Connections` claims (`internal/core/domain/token.go`); the `builtins.web` tools (`builtinGroups` in `internal/config/line.go`) when the engine's `webTools` change; the gate merge (`mergeOnApproval` in `internal/core/engine/merge.go`); `docs/engineering/factory-definition.md` ("Organization policy") and `docs/engineering/architecture.md` ("The organization policy at run start")

## Run

`go test -race -run 'Policy' ./internal/core/engine`, `go test -race -run 'TestGateMerge_' ./internal/core/engine`, `go test -race ./internal/gateway`
