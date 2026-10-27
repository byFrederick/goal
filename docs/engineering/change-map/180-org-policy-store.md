## When you change

The organization policy store ([ADR 0024](../adr/0024-organization-policy.md)): the `org_policies` table (`db/migrations/00066_org_policies.sql` and the migrations after it), its queries (`db/queries/org_policies.sql`) or the adapter (`internal/adapters/store/postgres/orgpolicies.go`)

## Also change

`domain.OrgPolicy` and `domain.DefaultOrgPolicy` (`internal/core/domain/orgpolicy.go`) when a field or a tool value changes, so `Validate` and the table's CHECKs list the same values; the `OrgPolicies` port (`internal/core/ports/orgpolicies.go`), its in-memory fake (`internal/core/ports/porttest/orgpolicies.go`) and the conformance suite `porttest/orgpolicytest`, which runs against both

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`
