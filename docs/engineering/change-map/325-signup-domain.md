## When you change

Sign-up by domain: `db/migrations/00078_signup_domains.sql` (`org_signup_domains`, `public_email_domains`, `goal_set_signup_domain`, `goal_signup_domain_org`, `goal_apply_signup_domain`, or a later migration that replaces them), the sign-up gate `allowOnlyInvitedOrBootstrap`, `recordUser` or `known` in `apps/auth/src/signin.ts`, or the signup-domain operations in `contracts/api/openapi.yaml`

## Also change

`signupDomainOrg` and `applySignupDomain` in `apps/auth/src/goal.ts`; `internal/adapters/store/postgres/signupdomains.go` (its constraint names) and `ports.SignupDomains` with its fake `internal/core/ports/porttest/signupdomains.go`; `signupDomainProblem` in `internal/api/settings.go`; `authFunctions`, `orgTables` and `withoutOrgID` in the postgres schema tests; the "Who may sign up" bullet of [architecture.md](../architecture.md); the webmail list changes only in a new migration

## Run

`task migrate-check`; the postgres adapter's `TestSignupDomain_*` and `TestSchema_*` with `-tags integration`; `go test -race -run TestSignupDomain ./internal/api` and `go test -run TwoOrgMatrix ./internal/api` (with `-update` after a permission change, then review `testdata/access.golden`); `pnpm --dir apps/auth run test:integration` and `pnpm --dir apps/auth run typecheck`
