## When you change

Draft requests: the `draft_requests` table and its definers `goal_claim_draft_request` and `goal_requeue_draft_requests` (`db/migrations/00080_draft_requests.sql` and the migrations after it), their queries (`db/queries/draft_requests.sql`) or the adapter (`internal/adapters/store/postgres/draftrequests.go`)

## Also change

`ports.DraftRequests` (`internal/core/ports/draftrequests.go`) and its domain types and limits (`internal/core/domain/draftrequest.go`, ADR 0021), the in-memory fake (`internal/core/ports/porttest/draftrequests.go`) and the conformance suite `porttest/draftrequesttest`, which runs against both (`TestDraftRequests_Fake`, `TestDraftRequests_Conformance`); `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` when the table is renamed; ADR 0021 when the statuses, the text's length or the one-open-request rule change

## Run

`task sqlc`, `task gen:check`, `task migrate-check`, `task test`, `task test-integration`
