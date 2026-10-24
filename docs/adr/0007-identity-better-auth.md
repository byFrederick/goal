# ADR 0007 — Identity with Better Auth beside goald

Status: accepted · 2026-10-24 · Builds on [ADR 0003](0003-hexagonal-architecture-and-deployment.md) and [ADR 0004](0004-frontend-and-monorepo.md) · Spec: [phase 3](../specs/3-identity-and-access.md)

## Context

Phase 3 adds people to Goal: sign-in without passwords (an email code or Google), invite-only organizations with owner, admin and member roles, factory roles, API keys for scripts and CI, and a CLI login. Phase 5 will expose Goal as an MCP server to outside clients, which needs an OAuth authorization server. goald is a stateless Go daemon, PostgreSQL is the only database, every org-owned row is bound by row-level security, and `api/openapi.yaml` is the only contract between goald and TypeScript (ADR 0004).

Better Auth (TypeScript) covers email codes, Google, organizations with invitations, the device authorization grant and, for phase 5, an OAuth provider with client metadata documents. There is no Go version. Writing the same in Go is about 2–3k lines for phase 3, but phase 5's authorization server (consent, client metadata, refresh rotation) is where a maintained library earns its place.

## Decision

1. **Better Auth runs as a small Node service, `auth/`** (Hono and an exactly pinned `better-auth` ≥ 1.7.12, in the pnpm workspace and `task check`), with only the email OTP, organization, device authorization and bearer plugins. On a laptop `goal up` runs it beside goald; on AWS it is a sidecar container in goald's ECS task. It listens on loopback only. It holds Better Auth's secrets, the Google client secret and its database password; goald holds none of them.
2. **goald is the one origin.** It reverse-proxies an exact allowlist of Better Auth paths under `/api/auth/*` (hardened: no forwarded headers from clients, a client IP header it writes itself, encoded-path refusal, body and time limits, cross-origin protection, no secrets in logs). The SPA is served by goald, or by Vercel with rewrites to goald; cookies are first-party, `SameSite=Lax`, httpOnly.
3. **goald verifies sessions in the database, per request.** It reads Better Auth's session token (the cookie, or `Authorization: Bearer` from the CLI) and resolves it with one `SECURITY DEFINER` function, `goal_principal(token, org)`, returning the user, the session's times, the role in the path's organization and the factory roles. No JWT, no JWKS, no cache: removal, role changes and sign-out apply on the next request, streams work with the cookie, and signed-in people keep working while the auth service is down. A contract test pins Better Auth's cookie and session format to the pinned version.
4. **goald owns API keys.** Keys are org-owned service accounts in a Goal table under RLS (`goal_sk_` prefix, SHA-256 hash, scopes from `api/permissions.yaml`, an optional factory restriction, a role ceiling, a mandatory expiry of at most 90 days), verified by goald with one hash lookup per request and revoked at once. Better Auth's api-key plugin is not used: its verify and permission setting are server-only, so goald would need an internal route into the auth service (a confused-deputy surface); a cached verify would leave a revocation window; its per-key limits write on every verify; and keys belong with Goal's RLS and audit log.
5. **goald owns the organization lifecycle.** Creating, renaming, soft-deleting and purging an organization run one definer function or trigger that writes Goal's `organizations` row and Better Auth's `auth.organization` row (same UUIDv7 id) in one transaction, with the owner's membership on create. Better Auth's own create, update and delete paths are off. Only platform admins (a Goal table) create organizations.
6. **Better Auth owns members, invitations and organization roles**, with its checks (a non-owner cannot make an owner; the last owner stays). PostgreSQL triggers on its tables write Goal's audit log atomically and keep at least one owner under concurrency. Factory roles are a Goal table under RLS.
7. **A second TypeScript contract, by exception.** The SPA and the CLI call the allowlisted `/api/auth/*` endpoints directly with Better Auth's request shapes: sign-in, sign-out, keeping the session alive, the device flow and membership changes (invite, cancel, accept, decline, remove, change role, leave). This is the one exception to "OpenAPI is the only contract": only `web/src/api/auth.ts` and `cmd/goal`'s login code call it, the client library version is pinned to the server's, and goald's OpenAPI still carries every read, including members and invitations.
8. **One permission source.** `api/permissions.yaml` names every permission with the roles that hold it, whether API keys may carry it and whether it needs a recent sign-in; it generates goald's permission table and Better Auth's access-control roles, and every OpenAPI operation names its permission in `x-goal-scope`. The same names become OAuth scopes in phase 5.
9. **Better Auth's schema lives in schema `auth` under goose.** Its SQL is generated by the pinned `auth generate` against a database at the goose head and committed as numbered migrations; `auth migrate` is never run, and a check fails when `generate` reports a difference. The service connects as `goal_auth` (DML on `auth.*` only); `goal_app` has no privilege on `auth` and reads it only through definer functions that return no secrets.

## Rejected

- **Short-lived JWTs verified by goald against JWKS.** `EventSource` cannot send an `Authorization` header, role claims go stale for the token's lifetime, an active-organization claim competes with the organization in the path, and phase 5's OAuth provider disables the JWT plugin's token endpoint. JWTs arrive with phase 5, for MCP clients only.
- **Creating organizations through Better Auth with a hook that creates Goal's row.** Its writes are separate transactions, so a failed hook leaves half an organization, and its delete is a hard delete that bypasses Goal's soft delete and purge.
- **Goal-owned users, memberships and invitations in Go.** Possible, but phase 5 would still need an authorization server; one identity system for both is simpler.
- **A separate ECS service for auth.** It would need service-to-service authentication; a sidecar keeps it on loopback, as on a laptop.
- **Cross-site deployments (UI and API on different sites).** Third-party cookies, Safari's tracking prevention and CSRF make them fragile; Vercel serves the UI same-origin through rewrites, and previews use the mock API.

## Consequences

- Two deployables per task (goald and auth) and a Node runtime in production; Better Auth advisories need a patch SLA (critical 48 hours, high 7 days).
- goald depends on Better Auth's session table and cookie format; upgrades go through the contract test and the schema drift check.
- Better Auth's tables have no row-level security; isolation there rests on Better Auth's checks, the proxy allowlist, Goal's hooks and definers bound to the transaction's organization, all covered by tests.
- Better Auth stores session tokens as they are; a database dump exposes live sessions until they expire, so backups are encrypted.
- Every handler is authorized from one table, and adding an operation starts with its `x-goal-scope`.
