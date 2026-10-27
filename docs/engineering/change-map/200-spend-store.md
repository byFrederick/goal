## When you change

The spend store: its queries (`db/queries/spend.sql`), the indexes they use (`db/migrations/00067_org_spend_indexes.sql`) or the adapter (`internal/adapters/store/postgres/spend.go`)

## Also change

The `Spend` port (`internal/core/ports/spend.go`), the layout in `domain.NewOrgSpend` (`internal/core/domain/spend.go`), the in-memory fake (`internal/core/ports/porttest/spend.go`) and the storetest subtest `OrgSpend` (`internal/core/ports/porttest/storetest/spend.go`), which runs against both; `TestSpendReads_UseTheirIndexes` when a query's range or filter changes

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`
