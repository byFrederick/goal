# ADR 0007 — Identity with Better Auth beside goald

Status: accepted · 2026-10-24 · decision 4 amended the same day ([personal tokens](#amendment--personal-tokens-decision-4)) · decision 5 amended 2026-10-27 ([who creates organizations](#amendment--who-creates-organizations-decision-5)) · Builds on [ADR 0003](0003-hexagonal-architecture-and-deployment.md) and [ADR 0004](0004-frontend-and-monorepo.md) · Spec: [phase 3](../history/specs/3-identity-and-access.md) · (roadmap 2026-10-24) the Goal MCP server this ADR calls phase 6 was phase 5 when it was written; continuous learning took phase 5

## Context

Phase 3 adds people to Goal: sign-in without passwords (an email code or Google), invite-only organizations with owner, admin and member roles, factory roles, API keys for scripts and CI, and a CLI login. Phase 6 will expose Goal as an MCP server to outside clients, which needs an OAuth authorization server. goald is a stateless Go daemon, PostgreSQL is the only database, every org-owned row is bound by row-level security, and `contracts/api/openapi.yaml` is the only contract between goald and TypeScript (ADR 0004).

Better Auth (TypeScript) covers email codes, Google, organizations with invitations, the device authorization grant and, for phase 6, an OAuth provider with client metadata documents. There is no Go version. Writing the same in Go is about 2–3k lines for phase 3, but phase 6's authorization server (consent, client metadata, refresh rotation) is where a maintained library earns its place.

## Decision

1. **Better Auth runs as a small Node service, `apps/auth/`** (Hono and an exactly pinned `better-auth` ≥ 1.7.12, in the pnpm workspace and `task check`), with only the email OTP, organization, device authorization and bearer plugins. On a laptop `goal up` runs it beside goald; on AWS it is a sidecar container in goald's ECS task. It listens on loopback only. It holds Better Auth's secrets, the Google client secret and its database password; goald holds none of them (`goal up` starts each process with only its own variables, and goald's reserved list keeps them out of factory definitions).
2. **goald is the one origin.** It reverse-proxies an exact allowlist of Better Auth paths under `/api/auth/*` (hardened: no forwarded headers from clients, a client IP header it writes itself, encoded-path refusal, body and time limits, cross-origin protection, no secrets in logs). The SPA is served by goald, or by Vercel with rewrites to goald; cookies are first-party, `SameSite=Lax`, httpOnly.
3. **goald verifies sessions in the database, per request.** It reads Better Auth's session token (the cookie, or `Authorization: Bearer` from the CLI) and resolves it with one `SECURITY DEFINER` function, `goal_principal(token, org)`, returning the user, the session's times, the role in the path's organization and the factory roles. No JWT, no JWKS, no cache: removal, role changes and sign-out apply on the next request, streams work with the cookie, and signed-in people keep working while the auth service is down. A contract test pins Better Auth's cookie and session format to the pinned version.
4. **goald owns API keys.** Keys are org-owned service accounts in a Goal table under RLS (`goal_sk_` prefix, SHA-256 hash, scopes from `contracts/api/permissions.yaml`, an optional factory restriction, a role ceiling, a mandatory expiry of at most 90 days), verified by goald with one hash lookup per request and revoked at once. Better Auth's api-key plugin is not used: its verify and permission setting are server-only, so goald would need an internal route into the auth service (a confused-deputy surface); a cached verify would leave a revocation window; its per-key limits write on every verify; and keys belong with Goal's RLS and audit log.
5. **goald owns the organization lifecycle.** Creating, renaming, soft-deleting and purging an organization run one definer function or trigger that writes Goal's `organizations` row and Better Auth's `auth.organization` row (same UUIDv7 id) in one transaction, with the owner's membership on create. Better Auth's own create, update and delete paths are off. Only platform admins (a Goal table) create organizations.
6. **Better Auth owns members, invitations and organization roles**, with its checks (a non-owner cannot make an owner; the last owner stays). PostgreSQL triggers on its tables write Goal's audit log atomically and keep at least one owner under concurrency. Factory roles are a Goal table under RLS.
7. **A second TypeScript contract, by exception.** The SPA and the CLI call the allowlisted `/api/auth/*` endpoints directly with Better Auth's request shapes: sign-in, sign-out, keeping the session alive, the device flow and membership changes (invite, cancel, accept, decline, remove, change role, leave). This is the one exception to "OpenAPI is the only contract": only `apps/web/src/api/auth.ts` and `cmd/goal`'s login code call it, the client library version is pinned to the server's, and goald's OpenAPI still carries every read, including members and invitations.
8. **One permission source.** `contracts/api/permissions.yaml` names every permission with the roles that hold it, whether API keys may carry it and whether it needs a recent sign-in; it generates goald's permission table and Better Auth's access-control roles, and every OpenAPI operation names its permission in `x-goal-scope`. The same names become OAuth scopes in phase 6.
9. **Better Auth's schema lives in schema `auth` under goose.** Its SQL is generated by the pinned `auth generate` against a database at the goose head and committed as numbered migrations; `auth migrate` is never run, and a check fails when `generate` reports a difference. The service connects as `goal_auth` (DML on `auth.*` only); `goal_app` has no privilege on `auth` and reads it only through definer functions that return no secrets.

## Rejected

- **Short-lived JWTs verified by goald against JWKS.** `EventSource` cannot send an `Authorization` header, role claims go stale for the token's lifetime, an active-organization claim competes with the organization in the path, and phase 6's OAuth provider disables the JWT plugin's token endpoint. JWTs arrive with phase 6, for MCP clients only.
- **Creating organizations through Better Auth with a hook that creates Goal's row.** Its writes are separate transactions, so a failed hook leaves half an organization, and its delete is a hard delete that bypasses Goal's soft delete and purge.
- **Goal-owned users, memberships and invitations in Go.** Possible, but phase 6 would still need an authorization server; one identity system for both is simpler.
- **A separate ECS service for auth.** It would need service-to-service authentication; a sidecar keeps it on loopback, as on a laptop.
- **Cross-site deployments (UI and API on different sites).** Third-party cookies, Safari's tracking prevention and CSRF make them fragile; Vercel serves the UI same-origin through rewrites, and previews use the mock API.

## Consequences

- Two deployables per task (goald and auth) and a Node runtime in production; Better Auth advisories need a patch SLA (critical 48 hours, high 7 days).
- goald depends on Better Auth's session table and cookie format; upgrades go through the contract test and the schema drift check.
- Better Auth's tables have no row-level security; isolation there rests on Better Auth's checks, the proxy allowlist, Goal's hooks and definers bound to the transaction's organization, all covered by tests.
- Better Auth stores session tokens as they are (1.7.12 has no hashing option); a database dump exposes live sessions until they expire (at most 7 days), so backups are encrypted. Accepted by the owner; revisited if Better Auth adds hashed storage.
- Every handler is authorized from one table, and adding an operation starts with its `x-goal-scope`.

## Amendment — personal tokens (decision 4)

Status: accepted · 2026-10-24 · extends decision 4. Specified in [phase 3](../history/specs/3-identity-and-access.md) §4.5 and §6.7; built in PR 3-12b.

**Context.** The owner wants people to use the CLI and scripts on their own machines, headless ones included, and to set factories up from the CLI, without the browser device flow on each machine.

**Decision.** goald owns personal tokens too, beside API keys and like them: a person's credential in one organization, in a Goal table of its own (`personal_tokens`, RLS by org, a foreign key to the membership that cascades on removal), with its own prefix `goal_pat_`, a SHA-256 hash, scopes from `contracts/api/permissions.yaml` limited by an explicit `pat` flag, and a mandatory expiry (default 30 days, at most 90). A request with one acts as the user (`user:<id>`, `via: pat:<id>` in the audit log) with the token's scopes ∩ the user's current roles, looked up per request by one definer; the token check runs first in the Authorizer and fails closed, never satisfies a fresh sign-in, and never carries platform admin rights. Tokens are created only in the browser with a fresh cookie session, work only on `/v1`, and the owner revokes them without re-authenticating.

**Why a separate table and prefix**, not a column on `api_keys`: a key is the organization's service account, a token a member's credential; separate rows keep each lookup, grant, revocation rule and test about one thing, the prefix tells logs, secret scanners and the proxy which is which, and a bug in one cannot widen the other.

**Rejected.**
- *Better Auth's api-key plugin with user-owned keys*: decision 4's reasons hold unchanged (server-only verify needing an internal route, a cached verify's revocation window, writes on every verify, keys outside Goal's RLS and audit).
- *Long-lived sessions* (a bearer session with a long `expiresIn`): sessions carry the user's whole role and satisfy the fresh rule when new, have no scopes, are stored unhashed (the accepted risk of decision 3 would grow with their lifetime), and one `expiresIn` applies to every session.
- *Creating tokens from the CLI* (`goal token create`): a bearer session comes from the device flow, whose approval can be phished; if it could mint a 90-day token, one phished approval would outlive the 7-day session. Browser-only creation with a fresh cookie session keeps a phished CLI session from minting one; the CLI only stores a token it is given on stdin (`goal login --token`).

**Consequences.** One more lookup function, table and credential branch in goald; the permission table gains a `pat` column that each new permission must decide; high-impact scopes (`definition:edit`, `definition:publish`, `factory:create`) can be given to a token, behind a warning and never in a preset. Whether the phase 6 MCP endpoint accepts personal tokens is left to phase 6 (spec 6).

## Amendment — who creates organizations (decision 5)

Status: accepted · 2026-10-27 · changes decision 5's last sentence. Decided in [ADR 0027](0027-self-serve-onboarding.md), decision 1.

Any person signed in with a session (in the browser or through the CLI's device sign-in) may create an organization and becomes its owner, so self-serve onboarding starts without a platform admin. Platform admins keep the right; API keys and personal tokens never had it and still do not. goald still owns the organization lifecycle as decision 5 says.
