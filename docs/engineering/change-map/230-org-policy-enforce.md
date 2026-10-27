## When you change

A field of the organization policy that applies to definitions or runs (`domain.OrgPolicy` in `internal/core/domain/orgpolicy.go`: the allowed and default models, the denied commands, the box time limit), or how the engine applies it at run start (`applyPolicy` in `internal/core/engine/work.go`) ([ADR 0024](../adr/0024-organization-policy.md))

## Also change

The publish check `config.ModelProblems` (`internal/config/policy.go`), which authoring runs as check `policy`; the engine's run start (`internal/core/engine/work.go`, `boxLife` in `sandbox.go`) and the task token's `Model` and `DeniedCommands` claims (`internal/core/domain/token.go`); the gateway bundle's model and `tools_deny` (`internal/gateway/bundle.go`); `docs/engineering/factory-definition.md` ("Organization policy")

## Run

`go test -race -run 'TestModelProblems' ./internal/config`, `go test -race -run 'TestPublish_' ./internal/authoring`, `go test -race ./internal/core/engine`, `go test -race ./internal/gateway`
