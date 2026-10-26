# auth — Goal's identity service

Better Auth on Hono (`@hono/node-server`): sign-in by email code or Google, sessions, organization
members, invitations and the CLI's device sign-in ([spec 3](../../docs/specs/3-identity-and-access.md),
[ADR 0007](../../docs/adr/0007-identity-better-auth.md)). It listens on loopback only and is reached
only through goald's `/api/auth` proxy, which forwards an allowlist of its endpoints; goald alone
creates, renames and deletes organizations.

## Running it

- `goal up` runs `node auth/dist/server.js` under `GOAL_AUTH=session` (the default), with its own
  variables only (`.env.example`, "Sign-in"), and Mailpit for the codes, once `goal db migrate` has
  created its tables (it refuses a schema it does not expect), and starts it again should it stop.
- `task auth:build` builds `dist/`; `task auth:dev` runs it from `src/`, rebuilt on change, mailing to
  Mailpit (needs `goal db init` and `goal db migrate`, `BETTER_AUTH_SECRET` and `GOAL_AUTH_PROXY_SECRET`, the one goald is given too).
- It answers only requests that carry `GOAL_AUTH_PROXY_SECRET` in `X-Goal-Proxy-Secret`, as goald's proxy
  sends them, and its health check (`GET /api/auth/ok`); anything else that reaches its port is 403.
- It connects as the role `goal_auth` (`GOAL_AUTH_DATABASE_URL`), which has DML on schema `auth`
  and nothing in `public`; it refuses to start as a superuser, a `BYPASSRLS` role, a table owner,
  a member of `goal_owner` or `goal_app`, or on a non-loopback listen.

## Schema

Better Auth's tables are goose migrations (`db/migrations/00040`–`00041` and later), generated with
the pinned CLI (`auth generate`) against a database at the goose head. `auth migrate` is never
run. `task auth:schema-check` (part of `task migrate-check`) fails when `src/auth.ts` and the
migrations disagree.

## Tests

`task auth:check`: typecheck, Biome, Vitest unit tests and `pnpm audit --prod`.
`task test-integration` runs the `*.integration.test.ts` files against PostgreSQL 18 and Mailpit
from `compose.yaml`.

## Supply chain and patch SLA

- `better-auth`, `auth` (its CLI, `pnpm-workspace.yaml`'s catalog), `@better-auth/*`, `nodemailer`
  and every other dependency are pinned exactly; four Better Auth plugins only (email OTP,
  organization, device authorization, bearer); no install scripts.
- `pnpm audit --prod` runs in `task check` for this service and the web UI; an advisory fails the
  check until it is fixed or recorded as an exception in [SECURITY.md](../../SECURITY.md).
- **Patch SLA** for advisories in Better Auth or any other production dependency of this service:
  **critical within 48 hours, high within 7 days**, moderate and low with the next release. The
  fix is an exact-version upgrade (or a `pnpm-workspace.yaml` override of a transitive
  dependency), followed by `task check`, `task migrate-check` (the schema drift check) and
  `task test-integration`.
