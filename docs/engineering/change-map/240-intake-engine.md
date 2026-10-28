## When you change

How triggers turn events and schedules into items: `domain.Trigger`, `domain.IntakeEvent`, `domain.RenderTemplate` or `domain.UntrustedEventBlock` (`internal/core/domain/trigger.go`), `domain.Cron` (`internal/core/domain/cron.go`), `Engine.Intake` (`internal/core/engine/intake.go`), `Engine.FireSchedules` (`internal/core/engine/schedules.go`) or the mapping of `board.triggers` (`internal/config/line.go`)

## Also change

`config.Trigger` and the trigger checks in `internal/config/triggers.go` (its `cronProblem` must accept exactly what `domain.ParseCron` does), the `Intake` port with its fake, adapter and `porttest/intaketest` when a check needs a new read, `ports.ModelCheck` (`internal/core/ports/modelcheck.go`) with its fake `porttest.ModelCheck` and `Engine.modelAgrees` (`internal/core/engine/modelcheck.go`) when the model check changes, goald's `fireSchedules` job in `cmd/goald/main.go`, and the "Intake triggers in the engine" paragraph of `docs/engineering/architecture.md`

## Run

`go test -race ./internal/core/domain ./internal/core/engine ./internal/config`, `task test`, and `task test-integration` when the port changes
