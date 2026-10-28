## When you change

The engine's tool approvals or the domain functions they read: `internal/core/engine/toolapproval.go` (`RequestToolApproval`, `DecideToolApproval`, `RevokeStandingRule`, the "## Approved tool calls" and "## Denied tool calls" briefs) or `internal/core/domain/toolapproval.go` (`OpenToolApproval`, `ToolDecisionsSinceLastRun`, `GrantedCalls`, `EffectiveToolModes`, `CanonicalJSON`)

## Also change

`docs/engineering/architecture.md` ("A tool call that asks first"); `domain.OpenQuestion` and `domain.OpenToolApproval` together, since both are withdrawn by the same events and an item has at most one of them ([ADR 0031](../../adr/0031-tool-calls-ask-first-and-standing-rules.md) decision 9); a new lease outcome needs a migration that widens the `lease_spans.outcome` CHECK and a case in `porttest/storetest` (`Transition_TakeLeaseRecordsTheAskedOutcomes`)

## Run

`task test`, and `task migrate-check` and `task test-integration` after a migration
