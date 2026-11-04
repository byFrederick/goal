## When you change

How triggers turn events and schedules into items: `domain.Trigger`, `domain.IntakeEvent`, `domain.RenderTemplate` or `domain.UntrustedEventBlock` (`internal/core/domain/trigger.go`), `domain.Cron` (`internal/core/domain/cron.go`), `Engine.Intake` (`internal/core/engine/intake.go`), `Engine.FireSchedules` (`internal/core/engine/schedules.go`) or the mapping of `board.triggers` (`internal/config/line.go`)

## Also change

`config.Trigger` and the trigger checks in `internal/config/triggers.go` (they check a cron with `domain.CronProblem`, the domain's one parser), the `Intake` port with its fake, adapter and `porttest/intaketest` when a check needs a new read or a firing a new column (`trigger_firings`, with its migration), the untrusted data section of the worker's system prompt (`apps/worker/src/agent.ts`) when the untrusted block's delimiters change, goald's `fireSchedules` job in `cmd/goald/main.go`, and the "Intake triggers in the engine" paragraph of `docs/engineering/architecture.md`

## Run

`go test -race ./internal/core/domain ./internal/core/engine ./internal/config`, `task test`, and `task test-integration` when the port changes
