# Phase 3 — Identity and access

Status: reviewed 2026-10-24, owner decisions applied · follows phase 2.2 (3-1 to 3-3 and 3-7 need only A's seams and can start now; 3-4, the first migration (`00040`), merges after 2.2's last, `00039` (E's `00030–00035`, the 2.2 review's `00036–00039`), since goose refuses a lower number after a higher one; the screens need C–E's boards) · migrations `00040–00049` (`00040–00047` planned, `00048–00049` the phase 3 review's fixes) · OpenAPI tag `identity` · PRs `3-1…3-16` and `3-12b` · decision record [ADR 0007](../adr/0007-identity-better-auth.md)

Phase 3 takes everything the README listed under "Out of 2.2". It replaces A's two seams (A §5.3), the request actor and the `Authorizer`, without changing handlers. Phase 4 (credentials, connectors and the MCP gateway) and phase 5 (the Goal MCP server) build on it.

## 1. Summary

People sign in to Goal without passwords, with a one-time code sent by email or with Google, through a small Better Auth service (`auth/`) that goald proxies on its own origin under `/api/auth/*`. goald checks the Better Auth session token, a Goal API key or a person's personal token inside PostgreSQL on every request and authorizes each OpenAPI operation from one permission table (`api/permissions.yaml`) against the caller's organization role (owner, admin, member), factory role (viewer, approver, editor) and, for keys and personal tokens, scopes. Sign-up is invite-only, organizations are created only by platform admins and only through goald, and every membership, role, invitation and key change is audited in the organization's audit log.

## 2. Scope

**In**
- `auth/`: a Node service (Hono + `better-auth`, exact pin `1.7.12` or later 1.7.x) with the plugins email OTP, organization, device authorization and bearer, nothing else. Local process beside goald; a sidecar container in goald's ECS task in the cloud.
- goald's reverse proxy for an allowlist of Better Auth paths, a `Host` check, the gateway's own listener off loopback, and a Content-Security-Policy on the SPA.
- Schema `auth` (Better Auth's tables, generated and committed as goose migrations), the role `goal_auth`, Goal-owned tables `platform_admins`, `factory_roles`, `api_keys`, `api_key_factories`, `personal_tokens`, `auth_events`, and the definer functions and triggers of §6.
- Organization lifecycle owned by goald: create, rename, soft delete and purge write Goal's and Better Auth's rows in one transaction.
- `domain.Principal`, the real `Authorizer`, the permission table generated from `api/permissions.yaml` (with `x-goal-scope` on every operation), fail-closed modes, the fresh-session rule, 401/403/404 semantics, streams that end on access changes, the task token `aud`.
- Endpoints: `/v1/auth/config`, `/v1/auth/device/{user_code}`, `/v1/me`, membership reads, factory roles, ownership transfer, API keys, personal tokens (§4.5), platform admins, the security log; `role` and `can` on organizations and factories.
- CLI: `goal login`, `logout`, `whoami` (device flow, OS keychain), `goal login --token` (a personal token from stdin), `GOAL_API_KEY` (an API key or a personal token), `goal key …`, `goal admin …`, `goal db recover-admin|recover-owner`.
- Web: sign in (email code, Google), invitation acceptance, device approval, the account menu, Members and roles (with invitations and factory roles), API keys and personal tokens, the org switcher's roles, Transfer ownership, D's Who approves and read-only mode, E's role-aware controls, role-filtered inbox and attention.
- Email through a `Mailer` port in `auth/` with an SMTP adapter: Mailpit locally (compose, 127.0.0.1), Amazon SES SMTP in the cloud.
- Moving an existing install to sign-in: the bootstrap owner, existing organizations and `user:local` rows.

**Out** (deferred, unscheduled unless noted)
- Passwords, 2FA, passkeys, SSO/SAML, SCIM, magic links, teams, dynamic roles, per-station approvers, per-org allowed email domains or Google `hd`, JWTs and the OAuth provider (phase 5), rate limiting of goald's own API, a user erasure workflow (defined in §11.6, not built), a GeoIP location on device approval, a platform-admin screen (CLI only); for personal tokens, the later items of §17 and their use at `/v1/mcp` (phase 5 decides).
- Credentials, connectors, tool approvals and the agents' gateway changes beyond `aud` (phase 4).
- Terraform for the ECS sidecar, SES and Secrets Manager (the AWS deployment, later); this spec fixes the shape they must take.

## 3. Canvas boards covered

| Board (canvas.json) | Element | Delivered by |
| --- | --- | --- |
| `Members.dc.html` "Members and roles" | whole board: members, org roles, factory roles, invitations, Resend, the per-person factory panel, "What each role can do" | 3-11 |
| `Org-Switcher.dc.html` "Organization menu, open" | the role in each row ("3 factories, owner"), "Members and roles" link, Create organization (platform admins only), the account "F. Cruz, Owner of Acme" | 3-10, 3-11 |
| `Mobile-Org-Switcher.dc.html` "Organization sheet" | same | 3-10, 3-11 |
| `Org-Settings.dc.html` "Organization settings" | Transfer ownership; Danger zone for owners only; read-only for members | 3-11 |
| `New-Basics.dc.html`, `Define-Basics.dc.html` | Who approves | 3-14 |
| `Define.dc.html` and the other Definition boards | read-only mode for viewers and approvers | 3-14 |
| `Board.dc.html`, `Item.dc.html`, `Mobile-Board.dc.html`, `Phone.dc.html` | controls and the andon cord by permission; "waiting on you" by permission | 3-14 |
| New: `Sign-In.dc.html`, `Sign-In-Code.dc.html`, `Invite-Accept.dc.html`, `Device-Approve.dc.html`, `Api-Keys.dc.html`, `Account-Menu.dc.html` | §10.2 describes each board for the designer | 3-10, 3-11, 3-12, 3-12b (Api-Keys' personal tokens sections, Account-Menu's link), 3-13 |

Not delivered: billing ("Owner: everything, including billing"; no billing exists), the vault parts of Org settings (phase 4).

## 4. Domain and engine changes

### 4.1 Identity types (`internal/domain/identity.go`, new)

```go
type UserID string           // Better Auth user id (UUID)
type APIKeyID string         // UUIDv7
type PersonalTokenID string  // UUIDv7

type OrgRole string     // "owner" | "admin" | "member" (Better Auth organization roles)
type FactoryRole string // "viewer" | "approver" | "editor", a ladder: each includes the one before

// Includes reports whether r grants everything other grants (editor ⊇ approver ⊇ viewer).
func (r FactoryRole) Includes(other FactoryRole) bool

type Permission string // "org:read", "item:decide", … from api/permissions.yaml

type PrincipalKind string // "user" | "service" (an API key) | "agent" | "system"

// Principal is who a request acts for and what the database says about them right now. The
// API builds it per request (§8.3); agents and jobs carry an Actor only.
type Principal struct {
	Kind          PrincipalKind
	User          UserID           // user
	Key           APIKeyID         // service
	Name, Email   string           // Email: users only, never logged
	PlatformAdmin bool             // never set for a personal token
	Session       *Session         // user signed in with a session; nil for keys, personal tokens and GOAL_AUTH=none
	Token         *TokenGrant      // user acting through a personal token (§4.5); nil otherwise
	Org           OrgID            // the organization the role fields describe ("" outside one; a token's own org)
	OrgRole       OrgRole          // "" when not a member of Org; the CURRENT role, also for tokens
	FactoryRoles  map[FactoryID]FactoryRole
	// API keys only: the key's grants (§4.3).
	Scopes        []Permission
	KeyFactories  []FactoryID      // nil: every factory of Org
	KeyCeiling    FactoryRole
}
type Session struct { ID string; CreatedAt, ExpiresAt time.Time; Bearer bool } // Bearer: sent as Authorization (the CLI), not the cookie
type TokenGrant struct { ID PersonalTokenID; Name string; Scopes []Permission; ExpiresAt time.Time }

// Actor is how the principal is recorded: user:<uuid> (the user's name, also through a personal
// token), service:<key id> ("<key name> (API key)").
func (p Principal) Actor() Actor

func WithPrincipal(ctx context.Context, p Principal) context.Context
func PrincipalFrom(ctx context.Context) (Principal, bool)
```

`ActorKind` gains `ActorService = "service"`; `ActorFromRef` parses `service:<id>`. `LocalUser` stays (`user:local`, "Local user") for `GOAL_AUTH=none` and for rows written before sign-in; the API names it "Local user (before sign-in)" when auth is on (§14). Rows are never rewritten.

New errors: `domain.ErrUnauthenticated` (a missing, malformed, unknown, expired or revoked credential) and `domain.ErrReauthRequired` (a sensitive operation with a session older than 10 minutes).

### 4.2 Roles

| Role | Holds (from `api/permissions.yaml`) |
| --- | --- |
| Owner | every org permission (settings, audit, members, keys, delete, transfer) and every factory permission in every factory |
| Admin | the owner's, except `org:delete` and `org:transfer`; may not make or change an owner |
| Member | `org:read`, `member:read`, `personal_token:read`/`manage` (their own tokens, 3-12b), plus what each factory role gives |
| Viewer | sees the factory: floor, board, items, traces, metrics, definition (read-only) |
| Approver | the viewer's, plus submit work, decide (approve, reject, retry, merge), move, pause, notes, pull and clear the andon; phase 4: tool approvals |
| Editor | the approver's, plus edit drafts, publish and restore versions, pause the factory, delete an unpublished factory |

Platform admins create organizations and manage platform admins; inside an organization they have only the rights of their membership there, if any. A member without a role on a factory cannot see it: it is absent from lists and its routes are 404, like a factory of another organization. Owners and admins act as editors everywhere (the board's "Editor in every factory"). Factory creation is an org permission of owners and admins (`factory:create`), so 2.2's moved item "the creator becomes a factory's editor" is dropped: the creator already holds every factory permission.

### 4.3 API keys

An API key is an org-owned service account: name, scopes, an optional factory restriction, a role ceiling, a mandatory expiry (default and maximum 90 days), revocable at once. For an operation needing permission P on factory F, a key passes when P is in its scopes, P is `keys: true` in the table, P is held by its ceiling role (factory permissions), and F is in its factories (otherwise the factory is 404 to it). Scopes must be `keys: true` and held by the creator when the key is created. Keys never hold `member:*`, `invitation:read`, `api_key:*`, `personal_token:*`, `org:delete`, `org:transfer`, `org:manage`, `org:create` or `platform:*`.

### 4.4 Engine

No engine change. Decisions already take `domain.Actor` (A §4.2); the API passes `Principal.Actor()`.

### 4.5 Personal tokens (PR 3-12b)

A personal token lets a person use the CLI or a script on their own machine without the device flow. It belongs to one user in one organization (they must be a member there), and it acts as that user: the actor is `user:<id>`, and every write made with it records `via: pat:<id>` in `audit_log.data` and in item event data.

- **Rights = the token's scopes ∩ the owner's current roles.** The lookup returns the owner's org role and factory roles at that moment, so demotion narrows a token on the next request, and removal from the organization deletes the token (the foreign key to `auth.member` cascades; a re-invitation never brings one back). A token never carries platform admin rights.
- **Scopes** come from the permissions marked `pat: true` (§8.2): `authenticated`, `org:read`, `factory:read`, `item:create`, `item:decide`, `item:operate`, `andon:manage`, and the high-impact `definition:edit`, `definition:publish`, `factory:create` (setting factories up from the CLI). Never member, key, ownership, org deletion, platform, device approval, audit or phase 4 permissions.
- **Never fresh.** A token never satisfies `fresh: true`; no `fresh` permission is `pat: true`, so such an operation is 403 `forbidden`, never `reauth-required`.
- **Creation.** Only in the browser: a session signed in there, by an email code or Google (the session records how, §8.3), that is fresh (≤ 10 minutes). The CLI's device-flow session, however its token is sent, gets 403 `browser-session-required`, and an API key or a personal token 403 `forbidden`, so a phished device approval cannot mint a lasting token. Each scope must be `pat: true` and held by the creator now (an org permission by their org role, a factory permission in at least one factory), else 400 `scope-not-allowed`. Expiry is mandatory: 1–90 days, default 30. The secret (`goal_pat_` + base64url of 32 bytes from `crypto/rand`) is shown once; only its SHA-256 and the hint `goal_pat_XXXX` are stored.
- **Revocation.** The owner revokes their own tokens without a fresh sign-in (incident response stays fast); owners and admins list and revoke members' tokens with `api_key:read` / `api_key:manage`. Deleting the organization revokes them. Revocation, expiry and removal apply on the next request and end the token's streams.
- **Where it works.** `/v1` only. The agents' gateway refuses it, the `/api/auth` proxy answers 401 without forwarding it, and `/v1/mcp` is phase 5's decision (spec 5).

As built (3-12b): `Permissions.Authorize` sends a principal with `Token` to `authorizeToken`, which never reaches the session rules: for an org or factory permission it first hides what the owner may not see (another organization: the unknown org's 404; a factory the owner has no role on: the unknown factory's 404), then refuses a permission that is not `pat`, is `fresh` or a platform admin's, or is not among the token's scopes (403 `forbidden`), then applies the owner's rule with their current roles (`PlatformAdmin` and `Session` cleared whatever the lookup returned). Hiding first keeps a refused scope from telling an existing invisible factory from an unknown one; in the middleware pass a factory permission the token cannot carry is refused before any factory is looked up, the same answer for every name. `authenticated` is held by every token, as by every key (it needs no scope: `goal login --token` and `goal whoami` read `getMe` and `listOrgs`); `visible` treats a token without `factory:read` as seeing no factory. Creating a token: `browser-session-required` answers a session not signed in through the browser (`Session.Browser` false: goal login's device flow, or a session older than migration `00048`), checked before freshness, so a stale CLI session is not told to sign in again (review M1: until `00048` this was the transport, `Session.Bearer`, and goald does not check the cookie's signature, so a CLI token sent as `Cookie: goal.session_token=<token>.anything` passed for the browser's); an API key or a token holds no `personal_token:*` at all and gets 403 `forbidden`. A factory scope is grantable when the creator holds it on one factory they see, or as an owner or admin (editors of every factory, those made later included). Under `GOAL_AUTH=none` the local user has no tokens: the list is empty, creating is 403 `forbidden`, revoking 404.

## 5. Ports and adapters

### 5.1 New ports (`internal/ports/ports.go`)

```go
// Identity answers who a request is, per request, from the database (phase 3).
type Identity interface {
	// Session resolves a Better Auth session token (the 32-character token, never the signed
	// cookie value) for org ("" outside one). ErrUnauthenticated when unknown or expired. A user
	// who is not a member of a live org gets OrgRole "".
	Session(ctx context.Context, token string, org domain.OrgID) (domain.Principal, error)
	// APIKey resolves base64url(SHA-256(key)). ErrUnauthenticated when unknown, revoked, expired
	// or of a deleted org. Touches last_used_at at most once a minute.
	APIKey(ctx context.Context, hash string) (domain.Principal, error)
	// PersonalToken resolves base64url(SHA-256(token)) to its owner (Kind user, Token set, Org
	// the token's) with the owner's CURRENT org and factory roles. ErrUnauthenticated when
	// unknown, revoked, expired, of a removed member or of a deleted org. Touches last_used_at
	// at most once a minute (§6.7).
	PersonalToken(ctx context.Context, hash string) (domain.Principal, error)
	// OrgsFor lists the live organizations the user belongs to, by name, with the user's role.
	OrgsFor(ctx context.Context, user domain.UserID) ([]domain.OrgMembership, error)
	// DeviceRequest describes a pending CLI sign-in by its user code (§10.2 Device-Approve).
	DeviceRequest(ctx context.Context, userCode string) (domain.DeviceRequest, error)
}

// Access keeps the people and keys of the organization in ctx. Every change appends its audit
// entry in its transaction. (org)
type Access interface {
	Members(ctx context.Context) ([]domain.Member, error)
	Invitations(ctx context.Context) ([]domain.Invitation, error) // pending only
	SetFactoryRole(ctx context.Context, by domain.Actor, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) error // "" removes
	TransferOwnership(ctx context.Context, by domain.Actor, from, to domain.UserID) error
	APIKeys(ctx context.Context) ([]domain.APIKey, error)
	CreateAPIKey(ctx context.Context, by domain.Actor, k domain.NewAPIKey) (domain.APIKey, error) // k.Hash computed by the caller
	RevokeAPIKey(ctx context.Context, by domain.Actor, id domain.APIKeyID) error
	// Personal tokens (3-12b). owner nil means every member's (the admins' listing); the owner
	// filter is part of the statement, so a member's call can never reach another's token.
	PersonalTokens(ctx context.Context, owner *domain.UserID) ([]domain.PersonalToken, error)
	CreatePersonalToken(ctx context.Context, by domain.Actor, t domain.NewPersonalToken) (domain.PersonalToken, error) // t.Hash computed by the caller
	RevokePersonalToken(ctx context.Context, by domain.Actor, id domain.PersonalTokenID, owner *domain.UserID) error // ErrNotFound outside the filter
}

// Platform keeps install-level identity: platform admins, the security log and clean-up.
type Platform interface {
	PlatformAdmins(ctx context.Context) ([]domain.User, error)
	AddPlatformAdmin(ctx context.Context, by domain.Actor, email string) (domain.User, error) // ErrNotFound: no such user
	RemovePlatformAdmin(ctx context.Context, by domain.Actor, user domain.UserID) error       // ErrConflict: the last one
	AuthEvents(ctx context.Context, q AuthEventQuery) ([]domain.AuthEvent, error)
	CleanUpAuth(ctx context.Context, now time.Time) (AuthCleanUp, error) // system job
}
```

`Tenancy` changes: `NewOrg` gains `Owner domain.UserID` (the creating user; under `GOAL_AUTH=none` the earliest platform admin, or nobody before the bootstrap owner has signed in, who then owns every live org, §6.5; 3-5 always passes nobody, 3-9 sets it), and `CreateOrg` calls `goal_provision_org` (§6.3). `Tenancy.Orgs` stays the install-wide list for system use (engine, purge); the API lists a person's organizations with `Identity.OrgsFor`. `ErrUnauthenticated` errors from adapters wrap `domain.ErrUnauthenticated`.

### 5.2 Implementations

| Port | Package | Notes |
| --- | --- | --- |
| `Identity`, `Access`, `Platform` | `internal/adapters/postgres` | Through the definer functions of §6.5 (decoded from `jsonb`, because sqlc cannot type `RETURNS TABLE`, A §6.4) and, for `factory_roles`, `api_keys` and `personal_tokens`, sqlc queries under RLS. |
| fakes | `internal/ports/porttest` | `porttest.Identity` (principals by token and hash, memberships, keys, the ≥1-owner and last-platform-admin rules). |
| conformance | `internal/ports/porttest/identitytest` | Run by the fake and the PostgreSQL adapter (§13). |
| `Mailer` | `auth/src/mail/` (TypeScript) | `interface Mailer { send(m: Mail): Promise<void> }`; adapters `smtp.ts` (nodemailer, pinned) and `memory.ts` (tests). goald sends no mail in phase 3. |

### 5.3 `internal/api`

- `actor.go` becomes `credential.go`: one middleware on every `/v1` route reads the credential (§8.3) into the context and answers 401 for a malformed one. Under `GOAL_AUTH=none` it sets `domain.LocalUser` as today.
- `org.go`'s `scope` resolves the principal for the path's organization (`Identity.Session`/`APIKey`), sets `domain.WithPrincipal` and `domain.WithActor(p.Actor())`, then asks the `Authorizer`. It is the only place a request's principal and actor are made.
- `authz.go`: `AccessRequest` replaces `Actor` with `Principal domain.Principal` (`Actor` stays available as `Principal.Actor()`) and gains `Permission` (asked instead of the operation's scope by `visible` and `can`). `AllowAll` remains, wired only under `GOAL_AUTH=none` (3-2: `domain.LocalPrincipal` there). `refusal(err, hidden)` turns an `Authorizer` error into the API's answer: hidden → the resolver's unknown-resource error, `ErrUnauthenticated`/`ErrReauthRequired` as they are, anything else 403.
- `permissions.go`: `Permissions`, the real `Authorizer` over the generated `permissions_gen.go` (§8.2), with the clock for the fresh-session rule. Also `visible(ctx, factory)` and `can(ctx, factory)`, used by every handler that lists factories or their data.
- `authproxy.go`: the `/api/auth/*` reverse proxy (§5.5), mounted by goald beside `/v1/`.
- Interfaces declared in `internal/api` for the new ports (`Identity`, `Access`, `Platform`), satisfied by the postgres adapter; no adapter import.

### 5.4 depguard

No new Go package outside `internal/api` and `internal/adapters/postgres`. `cmd/permgen` (the generator, §8.2) imports only the standard library and yaml. `auth/` is a pnpm workspace app with its own lint rules (Biome) and the boundary `auth/src/mail` ← `auth/src/*` only.

### 5.5 Topology and the proxy

```
browser / CLI ──https──▶ goald :8080 ──┬─ /v1/*        API (Identity in PostgreSQL, per request)
  (one origin: GOAL_PUBLIC_URL)        ├─ /api/auth/*  allowlist proxy ──▶ auth 127.0.0.1:8081 ──┐
                                       ├─ /mcp         agents' gateway (task tokens only)          │
                                       └─ /            SPA (CSP)                                    │
                       PostgreSQL ◀── goal_app (definers, RLS)    goal_auth (auth.* DML) ◀─────────┘
```

| Target | auth service | goald listen |
| --- | --- | --- |
| Laptop | `goal up` runs it as a process (`node auth/dist/server.js`) when `GOAL_AUTH=session`, stopped with goald | `127.0.0.1:8080`, `/mcp` included (Docker Desktop, OrbStack); on Docker Engine for Linux `GOAL_GATEWAY_LISTEN` on the bridge address (below) |
| AWS (later) | a sidecar container in goald's ECS task, `127.0.0.1:8081`, health check `GET /api/auth/ok` on loopback | `0.0.0.0:8080` behind the ALB; `/mcp` only on `GOAL_GATEWAY_LISTEN`, reachable only from the runners' security group |
| Vercel UI (later) | as AWS | as AWS; Vercel rewrites `/v1/*` and `/api/auth/*` to goald so the SPA stays same-origin; preview deployments use the mock API and never sign in. Vercel's proxy ranges for `GOAL_TRUSTED_PROXIES` and SSE through rewrites are settled when the hosted deployment is scheduled. |

The auth service refuses a non-loopback `GOAL_AUTH_LISTEN`. There is no internal or service-to-service route: goald never calls Better Auth, it reads sessions in the database, and the auth service never passes an id from a request body to `auth.api.*` (its hooks take the actor from the session). goald mounts `/api/auth/*` only under `GOAL_AUTH=session`.

**Listeners.** Without `GOAL_GATEWAY_LISTEN`, the one loopback listener serves everything. With it (required, and different from `GOAL_LISTEN`, when `GOAL_LISTEN` is not loopback; goald refuses to start otherwise), it serves only `/mcp` (everything else 404), the main listener answers `/mcp` with 404, and `GOAL_GATEWAY_URL` defaults to it (`http://<GOAL_GATEWAY_LISTEN>/mcp`, a wildcard host becoming `127.0.0.1`, which the egress proxy reaches as `host.docker.internal`, [ADR 0009](../adr/0009-agent-network-isolation.md)); goald refuses a `GOAL_GATEWAY_URL` naming the main listener then, so agent tasks are given only the gateway's address. The gateway listener may be off loopback even while `GOAL_LISTEN` must be loopback: every request there needs a task token. `goal up` sets no `GOAL_GATEWAY_LISTEN` (as built in 3-3): a second loopback listener could only collide with another port. Agent containers never reach goald directly: since ADR 0009 they run on a network with no route to the host and reach exactly the gateway's path through the egress proxy, so neither mode lets them call `/v1` or `/api/auth`. On Docker Engine for Linux `host.docker.internal` is the bridge address, which a loopback listener does not serve: set `GOAL_GATEWAY_LISTEN=172.17.0.1:8090` (the `docker0` address) and containers reach the gateway and not the API.

**Host check** (against DNS rebinding): every request but `/mcp` and `/v1/health` whose `Host` is not `GOAL_PUBLIC_URL`'s host is refused with 421, except `localhost`, `127.0.0.1` and `[::1]` (on any port: the web dev server's proxy keeps the browser's `Host`, `localhost:5173`, and the name is what a rebinding page controls) when `GOAL_LISTEN` is loopback, and the `GOAL_LISTEN` host. 2.2 built it (`api.CheckHost`, `settings.hosts`, `GOAL_PUBLIC_URL` optional and https unless loopback); phase 3 adds the `GOAL_PUBLIC_URL` default and the non-loopback listeners.

**Proxy** (`internal/api/authproxy.go`, `httputil.ReverseProxy` with `Rewrite`):

| Rule | Value |
| --- | --- |
| Paths | exactly the table below (method and path, case-sensitive); anything else under `/api/auth` is 404 problem `not-found` and never reaches the service |
| Path hygiene | refuse (404) a `RawPath` containing `%2f`, `%2e` or `%5c` in any case, `//`, a dot segment, a trailing slash, or a path whose cleaned form differs |
| Headers in | drop `X-Forwarded-*`, `Forwarded`, `X-Real-Ip` and any `X-Goal-Client-Ip`; set `X-Goal-Client-Ip` to the client address: the right-most `X-Forwarded-For` entry not in `GOAL_TRUSTED_PROXIES` when the peer is a trusted proxy, else the peer address (all entries trusted: the left-most; a malformed entry stops the walk at the last trusted address); pass `X-Request-Id` (goald's, as on `/v1`) |
| Goal credentials | a request whose `Authorization` is `Bearer goal_sk_…` or `Bearer goal_pat_…` (any case of the scheme) is 401 problem `unauthenticated` and never forwarded: Better Auth has no use for Goal's keys or personal tokens (3-12b) |
| Headers out | drop `Set-Auth-Token` (bearer plugin) from every response; nothing else is added |
| Bodies out | the JSON answers of `/get-session`, `/sign-in/email-otp` and `/sign-in/social` lose their top-level `token` and `session.token` (review M2; the auth service removes them first); a compressed, oversized (256 KiB) or non-JSON answer there is 503 `auth-unavailable`; every other body passes as it is (`/device/token`'s `access_token` is the CLI's) |
| Limits | body 64 KiB (`http.MaxBytesReader`, 413); dial 2 s, response headers 10 s, whole request 15 s; upstream down or timed out is 503 problem `auth-unavailable` |
| Origin | wrapped in goald's `http.CrossOriginProtection`; Better Auth's own origin check stays on |
| Logs | method, path, status, duration, request id; never `Cookie`, `Set-Cookie`, `Authorization`, query strings or bodies |
| Upstream | `GOAL_AUTH_UPSTREAM` (default `http://127.0.0.1:8081`; http only on loopback) |

| Method | Path under `/api/auth` | Used by |
| --- | --- | --- |
| GET | `/get-session` | SPA (on load, then hourly) and CLI (daily) to keep the session alive (§8.3) |
| POST | `/sign-out`, `/revoke-sessions` | Account menu, `goal logout [--all]` |
| POST | `/update-user` | Edit name (the auth service accepts only `name`) |
| POST | `/email-otp/send-verification-otp`, `/sign-in/email-otp` | Sign-In, Sign-In-Code, re-auth |
| POST, GET | `/sign-in/social`, `/callback/google` | Google |
| POST | `/organization/invite-member`, `/organization/cancel-invitation`, `/organization/accept-invitation`, `/organization/reject-invitation`, `/organization/remove-member`, `/organization/update-member-role`, `/organization/leave` | Members, Invite-Accept |
| GET | `/organization/get-invitation` | Invite-Accept |
| POST, GET | `/device/code`, `/device/token` (POST), `/device` (GET), `/device/approve`, `/device/deny` (POST) | `goal login`, Device-Approve |

The SPA's fallback reserves `api/` like `v1/` and `mcp/` (`web/webui.go`), so an unknown `/api/…` path is a 404, never `index.html`.

**SPA headers** (`web/webui.go`, and the same in `vercel.json` later): `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`, `Cross-Origin-Opener-Policy: same-origin`, `Strict-Transport-Security: max-age=31536000` when `GOAL_PUBLIC_URL` is https, plus today's `X-Frame-Options`, `Referrer-Policy`, `X-Content-Type-Options`. `'unsafe-inline'` covers only styles (Radix positions popovers with style attributes). The UI shows initials, never Google profile images.
The CSP itself (`web/csp.txt`, with `worker-src 'self'`) and `X-Frame-Options: DENY` landed in the 2.2 hardening; 3-7 only extends that policy (`font-src`, `object-src`, `form-action`, COOP, HSTS).

### 5.6 Settings

| Variable | Read by | Default and rule |
| --- | --- | --- |
| `GOAL_AUTH` | goald | `none` until 3-16, then `session` (the default lives in `cmd/goald/settings.go`; `goal up` only passes it on) · `none` only with a loopback `GOAL_LISTEN`; read since 3-7, which refuses `session` until 3-9 makes it work |
| `GOAL_PUBLIC_URL` | goald, auth (as `BETTER_AUTH_URL`) | `http://<GOAL_LISTEN>` on loopback, required otherwise · https unless loopback; the origin people open (`localhost` and `127.0.0.1` are different origins to Better Auth) |
| `GOAL_AUTH_UPSTREAM` | goald | `http://127.0.0.1:8081` · loopback http only |
| `GOAL_TRUSTED_PROXIES` | goald | empty · comma-separated CIDRs (or single addresses) of proxies whose `X-Forwarded-For` is believed |
| `GOAL_GATEWAY_LISTEN` | goald | the main listener · required and different when `GOAL_LISTEN` is not loopback (§5.5) |
| `GOAL_AUTH_GOOGLE` | goald, auth | `off` · `on` needs `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` (auth) |
| `GOAL_AUTH_LISTEN` | auth | `127.0.0.1:8081` · loopback only |
| `GOAL_AUTH_DATABASE_URL` | auth | compose database as `goal_auth` with `GOAL_PG_AUTH_PASSWORD` |
| `BETTER_AUTH_SECRETS` or `BETTER_AUTH_SECRET` | auth | none · required, ≥ 32 bytes |
| `GOAL_AUTH_PROXY_SECRET` | goald, auth | `goal up` generates one per start when unset · required under `session`, ≥ 32 bytes, not a Better Auth secret; goald sends it as `X-Goal-Proxy-Secret` with every forwarded request and the auth service answers nothing else but `GET /api/auth/ok` (review of phase 3, L4; row 54) |
| `GOAL_BOOTSTRAP_OWNER_EMAIL` | auth | none · used only while no user exists |
| `GOAL_SMTP_URL`, `GOAL_SMTP_FROM` | auth | `smtp://127.0.0.1:1025`, `Goal <goal@localhost>` |

`compose.yaml` gains `mailpit` (`axllent/mailpit`, pinned) with `127.0.0.1:1025` (SMTP) and `127.0.0.1:8025` (inbox UI); it never appears in cloud configuration.

**Each process gets only its own variables.** `.env` holds both sets and `Taskfile.yml` loads it everywhere, so: `goal up` starts goald without the auth service's variables (`GOAL_AUTH_LISTEN`, `GOAL_AUTH_DATABASE_URL`, `BETTER_AUTH_SECRET(S)`, `GOAL_BOOTSTRAP_OWNER_EMAIL`, `GOAL_SMTP_*`, `GOOGLE_CLIENT_*`, `GOAL_PG_AUTH_PASSWORD`) and the auth service with only those plus `GOAL_PUBLIC_URL`, `GOAL_AUTH_GOOGLE`, `PATH`, `HOME` and `NODE_*`; and goald's `ownVariables` (the `env-reserved` list, B §4.7) gains every variable of this table, `GOOGLE_CLIENT_*`, `GOAL_PG_AUTH_PASSWORD`, `GOAL_API_KEY` and `GOAL_DATABASE_ADMIN_URL`, so no factory definition can hand them to an agent even when goald was started some other way. `goal up` refuses a `BETTER_AUTH_SECRET(S)` equal to `GOAL_GATEWAY_KEY`.

## 6. Data model

All migrations run as `goal_owner`. New roles and grants follow A §6.0.

### 6.0 The role `goal_auth`

`roles.sql` adds `CREATE ROLE goal_auth NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT` (idempotent like the others). `00040` grants to it, so goald checks the role exists before migrating and otherwise refuses to start with "role goal_auth is missing: run goal db init (goal up runs it)"; nothing is applied. `goal db init` gives it `LOGIN PASSWORD` from `GOAL_PG_AUTH_PASSWORD` (dev default), `GRANT CONNECT ON DATABASE` and `GRANT USAGE ON SCHEMA public` (functions only; it gets no table in `public`). The auth service connects as `goal_auth` (`GOAL_AUTH_DATABASE_URL`) and at startup refuses a role that is a superuser, has `BYPASSRLS`, owns a table, or is a member of `goal_owner` or `goal_app`.

| Role | `auth` schema | `public` |
| --- | --- | --- |
| `goal_owner` | owns it | owns everything (unchanged) |
| `goal_auth` | `USAGE`; `SELECT, INSERT, UPDATE, DELETE` on every table; no DDL | `EXECUTE` on the functions marked "auth" in §6.5 only |
| `goal_app` | nothing (no `USAGE`) | as today, plus the new tables' grants and `EXECUTE` on the functions marked "app" |

### 6.1 Migration plan

| File | PR | Content |
| --- | --- | --- |
| `00040_auth_schema.sql` | 3-4 | `CREATE SCHEMA auth AUTHORIZATION goal_owner`; `goal_uuidv7()`; `GRANT USAGE ON SCHEMA auth TO goal_auth` |
| `00041_better_auth.sql` | 3-4 | Better Auth's tables, generated (§6.2), with id defaults set to `goal_uuidv7()` and the grants to `goal_auth` |
| `00042_org_lifecycle.sql` | 3-5 | Goal's constraints and indexes on `auth.*`; the ≥1-owner trigger; `goal_provision_org`, the rename and retire triggers, `goal_transfer_ownership`; `goal_purge_org` replaced; backfill of `auth.organization` |
| `00043_identity_tables.sql` | 3-6 | `platform_admins`, `factory_roles`, `auth_events`; `audit_log.actor_kind` gains `service`; audit and access triggers on `auth.*` |
| `00044_auth_service_functions.sql` | 3-8 | Definers the auth service calls: events, caps, bootstrap claim, invitation roles, factory list |
| `00045_principal.sql` | 3-9 | Definers goald calls: `goal_principal`, `goal_orgs_for`, `goal_members`, `goal_invitations`, `goal_device_request`, `goal_identity_ready`, `goal_auth_cleanup`, platform admin functions |
| `00046_api_keys.sql` | 3-12 | `api_keys`, `api_key_factories`, `goal_api_key`; the retire trigger also revokes keys |
| `00047_personal_tokens.sql` | 3-12b | `personal_tokens`, `goal_personal_token`; the retire trigger also revokes personal tokens; `goal_access` kind `personal_token` (§6.7) |
| `00048_session_sign_in_method.sql` | review M1 | `auth.session."signInMethod"` (how the session was signed in, written by the auth service); `goal_principal` returns it |
| `00049_invitation_integrity.sql` | review L1 | `goal_org_live`, `goal_is_invitee` (the auth service's definers); the `invitation_live_org` trigger refusing an invitation to an organization that is not live; pending invitations of deleted organizations canceled |

Phase 4 uses `00050–00059`, phase 5 `00060` up.

### 6.2 `00040`/`00041`: Better Auth's schema under goose

- Better Auth is configured with `database: { dialect: PostgresDialect(pool), type: "postgres", schemaName: "auth" }` and `advanced.database.generateId: "uuid"`, so its ids and reference columns are `uuid` and PostgreSQL generates them.
- The SQL of `00041` comes from the pinned CLI, `pnpm --filter @goal/auth exec auth generate --config src/generate.ts --output <file>`, run against a database migrated to `00040`. (As built in 3-4: the CLI reads `auth/src/generate.ts`, which builds `createAuth` of `auth/src/auth.ts` on `GOAL_AUTH_DATABASE_URL` with a throwaway secret, so `auth.ts` itself opens no connection when imported; the CLI writes a file and has no stdout mode, and writes nothing when there is no difference.) The PR adds `-- +goose Up`, sets each id default to `goal_uuidv7()`, leaves out the generated `create schema if not exists "auth"` (`00040` creates it), and appends the grants. It is never hand-edited after it is applied; `auth migrate` is never run.
- Tables (Better Auth 1.7 with these plugins): `user`, `session`, `account`, `verification`, `organization`, `member`, `invitation`, `deviceCode`, `rateLimit`. Columns are Better Auth's camelCase, quoted (`"userId"`). Goal reads them only inside definer functions, so neither the quoting nor the reserved word `"user"` reaches Go.
- Additional fields (declared in `auth/src/auth.ts`, therefore generated): `member.changedBy text`, `member.changedAt timestamptz`; `invitation.factoryRoles text` (JSON `[{"factoryId","role"}]`, `input: true`), `invitation.changedBy text`, `invitation.changedAt timestamptz`. `changedBy` holds an actor ref.
- **Drift check** (`task auth:schema-check`, run by `task migrate-check` and by the pre-push hook whenever pushed commits touch `auth/` or `migrations/`): migrate a scratch database to head, run `auth generate` against it as `goal_auth` (`task auth:schema-diff DATABASE_URL=…`), fail on any non-empty output. Better Auth's `validateSchema` stays on at runtime, and the service refuses to start when its schema check fails.

```sql
-- 00040_auth_schema.sql
-- +goose Up
CREATE SCHEMA auth AUTHORIZATION goal_owner;
GRANT USAGE ON SCHEMA auth TO goal_auth;

-- UUIDv7 without PostgreSQL 18's uuidv7(), so the schema runs on older managed versions
-- (architecture "IDs"). Used as the default of Better Auth's id columns.
CREATE FUNCTION goal_uuidv7() RETURNS uuid LANGUAGE sql VOLATILE AS $$
  SELECT encode(set_bit(set_bit(overlay(uuid_send(gen_random_uuid())
    PLACING substring(int8send((extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3)
    FROM 1 FOR 6), 52, 1), 53, 1), 'hex')::uuid
$$;
GRANT EXECUTE ON FUNCTION goal_uuidv7() TO goal_auth, goal_app;
```

The drift check does not report the changed defaults (checked in 3-4), so Better Auth's ids are UUIDv7 from `goal_uuidv7()`; ids Goal generates (organizations, owner members, keys) stay UUIDv7 from Go. Generated as built: `session."activeOrganizationId"` and `"deviceCode"."userId"` are `text`, every other id and reference column `uuid`.

### 6.3 `00042_org_lifecycle.sql`

```sql
-- +goose Up
-- Goal's rules on Better Auth's rows. Better Auth enforces most of them in code; these hold
-- under concurrency and against bugs.
ALTER TABLE auth.member     ADD CONSTRAINT member_role_check     CHECK (role IN ('owner', 'admin', 'member'));
ALTER TABLE auth.invitation ADD CONSTRAINT invitation_role_check CHECK (role IN ('owner', 'admin', 'member'));
ALTER TABLE auth.invitation ADD CONSTRAINT invitation_status_check CHECK (status IN ('pending', 'accepted', 'rejected', 'canceled'));
CREATE UNIQUE INDEX member_org_user_key   ON auth.member ("organizationId", "userId");
CREATE INDEX invitation_email_status_idx  ON auth.invitation (lower(email), status);
-- (As built in 3-5: the generated SQL already has session(token) unique, session("userId"),
-- member("userId") and invitation("organizationId").)

-- An organization keeps at least one owner. Owner changes of one organization are serialized
-- on its auth.organization row, then counted at commit, so two owners demoting each other at
-- once cannot leave none (READ COMMITTED, Better Auth's level). FOR NO KEY UPDATE (as built)
-- conflicts with itself but not with a new member's foreign-key lock.
-- +goose StatementBegin
CREATE FUNCTION goal_keep_an_owner() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path FROM CURRENT AS $$
BEGIN
  PERFORM 1 FROM auth.organization WHERE id = OLD."organizationId" FOR NO KEY UPDATE;
  IF NOT FOUND OR NOT EXISTS (SELECT 1 FROM organizations
                              WHERE id = OLD."organizationId" AND deleted_at IS NULL) THEN
    RETURN NULL;                                   -- deleted, purged or being purged
  END IF;
  IF NOT EXISTS (SELECT 1 FROM auth.member
                 WHERE "organizationId" = OLD."organizationId" AND role = 'owner') THEN
    RAISE EXCEPTION 'organization % must keep an owner', OLD."organizationId"
      USING ERRCODE = 'check_violation', CONSTRAINT = 'member_keeps_an_owner';
  END IF;
  RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER member_keeps_an_owner AFTER UPDATE OF role OR DELETE ON auth.member
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (OLD.role = 'owner')
  EXECUTE FUNCTION goal_keep_an_owner();
```

Definer functions (owned by `goal_owner`, `SET search_path FROM CURRENT`, `REVOKE ALL … FROM PUBLIC`, no dynamic SQL):

| Function | Does | Granted to |
| --- | --- | --- |
| `goal_provision_org(p_id uuid, p_slug text, p_name text, p_mark text, p_owner uuid, p_owner_member uuid, p_by text, p_request_id text)` → `SETOF organizations` | In the caller's transaction: insert `organizations`, `auth.organization` (same id, name, slug) and, when `p_owner` is set, the owner's `auth.member` (`changedBy = p_by`); append `org.created`. Ids come from Go (UUIDv7). | app |
| `goal_rename_org()` trigger, `AFTER UPDATE OF slug, name ON organizations` | Copies slug and name to `auth.organization` in the same transaction | — |
| `goal_retire_org()` trigger, `AFTER UPDATE OF deleted_at ON organizations` when it becomes non-null | Cancels the org's pending invitations (`status = 'canceled'`, `changedBy = system:org-deleted`); `00046` adds: revokes its live API keys. Sessions lose the org at once because `goal_principal` ignores deleted orgs. | — |
| `goal_transfer_ownership(p_from uuid, p_to uuid, p_by text, p_request_id text)` | For `goal_org()`: `p_from` must be an owner and `p_to` an admin; sets `p_to` owner and `p_from` admin (`changedBy = p_by`); appends `org.ownership_transferred {from, to}`. Raises `check_violation` otherwise, naming `goal_transfer_from_owner` or `goal_transfer_to_admin` (→ 409 `not-an-admin`); `no_data_found` for a deleted org (→ `ErrNotFound`). | app |
| `goal_purge_org(p_org uuid, p_before timestamptz)` | As 2.2 (`00037`: retention checked inside, false when not due), then, after the `organizations` row, deletes `auth.invitation`, `auth.member` and `auth.organization` of the org, so their triggers see no organization (`factory_roles`, `api_keys` cascade from `organizations`) | app (purge job) |
| `goal_append_audit(p_org, p_by, p_action, p_target_kind, p_target_id, p_data jsonb, p_request_id)` (as built) | The one audit writer of these definers and triggers: the actor from its ref with `ActorFromRef`'s name, a user's name read from `auth."user"`; `00046` replaces it to name keys | — (definers only) |

Backfill: `INSERT INTO auth.organization (id, name, slug, "createdAt") SELECT id, name, slug, created_at FROM organizations` (every row, deleted ones included, so the purge stays uniform). Existing organizations have no members until the bootstrap owner signs in (§14). As built, `goal_app` loses `INSERT` on `organizations`: an organization is created only through `goal_provision_org`, with its `auth.organization` row.

### 6.4 `00043_identity_tables.sql`

```sql
-- +goose Up
-- Install-level: who may create organizations and manage platform admins. No RLS (no org);
-- goal_app reaches it only through definers (§6.5).
CREATE TABLE platform_admins (
  user_id  uuid PRIMARY KEY REFERENCES auth."user" (id) ON DELETE RESTRICT,
  added_by text NOT NULL,                                -- Actor.Ref()
  added_at timestamptz NOT NULL DEFAULT now()
);
-- (3-6) a deferred constraint trigger (platform_admins_keep_one) refuses deleting the last row
-- once any user exists, serialized first. As built the serialization is the transaction lock
-- pg_advisory_xact_lock(hashtext('goal.platform_admins')), which goal_claim_bootstrap takes too:
-- a self-conflicting LOCK TABLE also conflicts with each DELETE's row-exclusive lock, so two
-- removals would deadlock instead of one being refused. added_by and granted_by are CHECKed to
-- be actor refs.

-- Factory roles of members. RLS like every org table; a role exists only for a member of the
-- same organization and a factory of it, enforced by composite keys.
CREATE TABLE factory_roles (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id uuid NOT NULL,
  user_id    uuid NOT NULL,
  role       text NOT NULL CHECK (role IN ('viewer', 'approver', 'editor')),
  granted_by text NOT NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, factory_id, user_id),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, user_id) REFERENCES auth.member ("organizationId", "userId") ON DELETE CASCADE
);
CREATE INDEX factory_roles_user_idx ON factory_roles (org_id, user_id);
ALTER TABLE factory_roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON factory_roles
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT, UPDATE, DELETE ON factory_roles TO goal_app;

-- The install's security log: sign-ins, codes, lockouts, device approvals, platform admins.
-- No org, no RLS; written by the auth service and goald through definers only. Never holds a
-- raw email, code, token or key: subject is base64url(SHA-256(lower(email) or user code)).
CREATE TABLE auth_events (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at         timestamptz NOT NULL DEFAULT now(),
  kind       text NOT NULL CHECK (kind ~ '^[a-z_]+\.[a-z_]+$'),
  user_id    uuid,                                   -- no FK: the log outlives users
  subject    text CHECK (subject ~ '^[A-Za-z0-9_-]{43}$'),
  ip         inet,
  user_agent text CHECK (length(user_agent) <= 512),
  data       jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX auth_events_subject_idx ON auth_events (kind, subject, at DESC);
CREATE INDEX auth_events_user_idx    ON auth_events (user_id, at DESC);
CREATE INDEX auth_events_at_idx      ON auth_events (at);

-- As built: added NOT VALID, then validated (the 2.2 database review's pattern).
ALTER TABLE audit_log DROP CONSTRAINT audit_log_actor_kind_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_actor_kind_check
  CHECK (actor_kind IN ('user', 'agent', 'system', 'service')) NOT VALID;
ALTER TABLE audit_log VALIDATE CONSTRAINT audit_log_actor_kind_check;
```

`auth_events.kind` values: `sign_in.succeeded {method: email_code|google|device}`, `sign_up.refused`, `user.created`, `otp.sent`, `otp.suppressed {reason: unknown|cap|locked}`, `otp.failed`, `otp.locked`, `device.requested {client_id}`, `device.approved`, `device.denied`, `session.revoked`, `platform_admin.added`, `platform_admin.removed`, `recovery.used`, and from `00047` `personal_token.refused {token_id, reason}` (written by `goal_personal_token`, §6.7).

**Triggers on `auth.*`** (definer functions, atomic with Better Auth's write):

| Table, event | `audit_log` action (org = the row's organization) | Data |
| --- | --- | --- |
| `member` INSERT | `member.added` | `{user_id, role, via: invitation\|org_created\|bootstrap\|recovery}` |
| `member` UPDATE OF role | `member.role_changed` | `{user_id, from, to}` |
| `member` DELETE | `member.removed` | `{user_id, role, left: bool}` |
| `invitation` INSERT | `invitation.created` | `{invitation_id, email, role, factory_roles}` |
| `invitation` UPDATE OF status | `invitation.accepted` / `.rejected` / `.canceled` | `{invitation_id, email}` |

The actor is `changedBy` when `changedAt` is within the last minute (set by the auth service's stamp hook, §8.6, or by a definer), else `inviterId` for a new invitation, else the member themself for a new member (they accepted an invitation), else `system:auth`. The name is the user's name at that moment. Two people changing the same row at the same moment can be attributed to either one; both changes are recorded. Nothing is written for an organization being purged.

As built (3-6): `goal_audit_member()` and `goal_audit_invitation()` behind the triggers `member_audit_added`, `member_audit_role_changed` (only when the role changes), `member_audit_removed`, `invitation_audit_created`, `invitation_audit_answered` (only into `accepted`, `rejected` or `canceled`: Better Auth's return of a failed acceptance to `pending` writes nothing); the stamp rule is `goal_auth_actor(changedBy, changedAt, fallback)` (±1 minute of `clock_timestamp()`, a malformed ref ignored), the entries go through `goal_append_audit` (00042) with target `member:<user id>` or `invitation:<id>` and no request id. `via` is `bootstrap`/`recovery` by the stamps `system:bootstrap`/`system:recovery`, `invitation` when the user's accepted invitation of the organization exists (Better Auth marks it accepted before inserting the member), else `org_created`. A soft delete's canceled invitations are by `system:org-deleted`. "Being purged" is "no `organizations` row": `goal_purge_org` deletes it before Better Auth's rows.

`goal_notify_access()` triggers send `pg_notify('goal_access', {kind, org_id?, user_id?, session_id?, key_id?})` on `auth.member` UPDATE OF role or DELETE, `auth.session` DELETE, `factory_roles` changes, `organizations.deleted_at` set, (from `00046`) `api_keys.revoked_at` set and (from `00047`) `personal_tokens.revoked_at` set (kind `personal_token {org_id, user_id, token_id}`). A notice only ends streams (§8.8); it never grants anything, since any role can `NOTIFY`. As built, kinds are `member {org_id, user_id}`, `session {user_id, session_id}`, `factory_role {org_id, user_id, factory_id}` and `org {org_id}`; triggers `member_access_role_changed`, `member_access_removed`, `session_access_ended`, `factory_roles_access`, `organizations_access_deleted`.

### 6.5 `00044` and `00045`: definer functions

Every function: owned by `goal_owner`, `SECURITY DEFINER`, `SET search_path FROM CURRENT`, `REVOKE ALL FROM PUBLIC`, static SQL, never returns a token, code, hash or secret. goald's functions that act on an organization use `goal_org()` (the caller's transaction org) instead of an org parameter, so a bug cannot aim them at another organization; only the principal lookups, which run before an organization is bound, take one. The auth service binds no organization, so its functions take the invitation or organization of the row Better Auth is writing. Results are `jsonb`, decoded by the adapter.

| Function | Returns | For |
| --- | --- | --- |
| `goal_auth_event(p_kind, p_user, p_subject, p_ip inet, p_ua, p_data jsonb)` | void; refuses any kind but the auth service's (§6.4), cuts the user agent to 512 characters | auth (goald's own events are written by the platform functions below) |
| `goal_auth_count(p_kind text, p_subject text, p_since timestamptz)` | int | auth: send caps and lockout |
| `goal_claim_bootstrap(p_user uuid)` | bool. When `platform_admins` is empty (table locked) and `p_user` is the earliest user: adds it as platform admin (`system:bootstrap`, with `platform_admin.added {via: bootstrap}`) and as owner (`changedBy = system:bootstrap`) of every live org; idempotent: true whenever `p_user` is a platform admin afterwards, false when another user is or `p_user` is not the earliest. | auth |
| `goal_org_factories(p_org uuid)` | jsonb `[{id, name}]` of a live org, to validate an invitation's factory roles | auth |
| `goal_org_live(p_org uuid)`, `goal_is_invitee(p_email text, p_now timestamptz)` (`00049`, review L1) | bool: the org is not deleted; the email has a pending, unexpired invitation to a live org | auth: inviting, the sign-up gate and who gets a code |
| `goal_apply_invitation_roles(p_invitation uuid)` | void. For an accepted invitation whose invitee is now a member: inserts its factory roles of factories that still exist (`ON CONFLICT DO NOTHING`, `granted_by` = the inviter), audits `factory_role.granted {via: invitation}`. Idempotent. | auth |
| `goal_principal(p_token text, p_org uuid)` | jsonb `{user_id, name, email, session_id, created_at, expires_at, platform_admin, org_role, factory_roles: [{factory_id, role}]}` for an unexpired session, else NULL; `org_role` is null unless `p_org` is live and the user is a member | app |
| `goal_orgs_for(p_user uuid)` | jsonb `[{id, slug, name, mark, role, factory_count}]`, live orgs only, by name; `factory_count` counts the factories the user can see (all for owners and admins) | app |
| `goal_members()` | jsonb of the members of `goal_org()`: `{member_id, user_id, name, email, role, joined_at, last_active_at (newest session's updatedAt), factory_roles}` | app |
| `goal_invitations()` | jsonb of pending, unexpired invitations of `goal_org()`: `{id, email, role, factory_roles, inviter: {id, name}, created_at, expires_at}` | app |
| `goal_device_request(p_user_code text)` | jsonb `{client_id, status, requested_at, expires_at, ip, user_agent}` of a pending code (from `auth."deviceCode"` and its `device.requested` event), else NULL | app |
| `goal_platform_admins()`, `goal_add_platform_admin(p_email, p_by)`, `goal_remove_platform_admin(p_user, p_by)` | jsonb / void; add and remove write `auth_events` | app |
| `goal_auth_events(p_before bigint, p_limit int, p_kind text)` | jsonb page, newest first | app |
| `goal_auth_cleanup(p_now timestamptz)` | jsonb counts. Deletes sessions expired over a day ago, expired `verification` and `deviceCode` rows, `rateLimit` rows idle for a day, `auth_events` older than 90 days | app (job) |
| `goal_identity_ready()` | bool: the `auth` schema and phase 3's functions exist and `goal_app` has no `USAGE` on `auth` | app (startup) |

As built (3-9, `00045_principal.sql`): `goal_orgs_for` also returns each org's `max_slots` and `created_at` (the adapter builds a `domain.Org`); `goal_device_request` matches the typed code with dashes and case ignored and finds its `device.requested` event by `goal_user_code_subject(code)`, the same hash as `auth/src/goal.ts userCodeSubject` (not a definer, revoked from everyone); `goal_add_platform_admin` returns `{id, name, email}` or NULL for an unknown email and writes `platform_admin.added {by}` once; `goal_remove_platform_admin` raises `no_data_found` for a non-admin and leaves the last one to `platform_admins_keep_one`, then writes `platform_admin.removed {by}`; `goal_auth_events(p_before, p_limit 1–500, p_kind)` pages by id. `goal_identity_ready` checks the tables `session`, `user`, `member`, `invitation`, `deviceCode` and the fifteen definers of `00044`/`00045` by name. `NewOrg.Owner` empty makes the earliest platform admin the owner (read through `goal_platform_admins()` in the provisioning transaction), else nobody.

### 6.6 `00046_api_keys.sql`

```sql
-- +goose Up
CREATE TABLE api_keys (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  hint         text NOT NULL CHECK (hint ~ '^goal_sk_[A-Za-z0-9_-]{4}$'),   -- shown in lists
  hash         text NOT NULL CHECK (hash ~ '^[A-Za-z0-9_-]{43}$'),          -- base64url(SHA-256(key))
  scopes       text[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 64),
  role_ceiling text NOT NULL CHECK (role_ceiling IN ('viewer', 'approver', 'editor')),
  all_factories boolean NOT NULL,
  created_by   text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '90 days'),
  revoked_at   timestamptz,
  revoked_by   text,
  last_used_at timestamptz,
  UNIQUE (org_id, id)
);
CREATE UNIQUE INDEX api_keys_hash_key ON api_keys (hash);
CREATE UNIQUE INDEX api_keys_name_key ON api_keys (org_id, lower(name)) WHERE revoked_at IS NULL;

CREATE TABLE api_key_factories (
  org_id     uuid NOT NULL DEFAULT goal_org(),
  key_id     uuid NOT NULL,
  factory_id uuid NOT NULL,
  PRIMARY KEY (org_id, key_id, factory_id),
  FOREIGN KEY (org_id, key_id)     REFERENCES api_keys (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);

ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON api_keys
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE api_key_factories ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON api_key_factories
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT ON api_keys, api_key_factories TO goal_app;
GRANT UPDATE (revoked_at, revoked_by) ON api_keys TO goal_app;
```

`goal_api_key(p_hash text)` (app) returns jsonb `{id, org_id, name, scopes, role_ceiling, factories (null = all), expires_at}` for a key that is unrevoked, unexpired and of a live org, and sets `last_used_at = now()` when it is null or older than a minute. It is a definer because the key names its organization before any org is bound. `goal_retire_org` is replaced to revoke the org's keys (`revoked_by = 'system:org-deleted'`).

As built (3-12, `00046_api_keys.sql`): `created_by` and `revoked_by` are CHECKed to be actor refs and set together with `revoked_at`; `api_key_factories` has an index on `(org_id, factory_id)` for the factory's cascade; `goal_app` may only insert keys and revoke them (no `DELETE`: revoked keys stay listed 30 days and go with the organization's purge). `00046` also replaces `goal_append_audit` (a `service:<key id>` actor is named "<key name> (API key)"), `goal_notify_access` (kind `api_key {org_id, key_id}`, trigger `api_keys_access_revoked` when `revoked_at` is set) and `goal_identity_ready` (it requires `goal_api_key` too). The expiry is counted by the database (`now() + make_interval(days => n)`), so the table's 90-day check never meets goald's clock. Lists name the creator and the revoker as their `api_key.created`/`api_key.revoked` audit entries name them (the person's name at that moment); a revocation by the organization's deletion is `system:org-deleted`. The adapter is `postgres.Access` (`internal/adapters/postgres/access.go`, queries `queries/api_keys.sql`), the fake `porttest.Identity` (its `ports.Access` methods in `apikeys.go`), the conformance `identitytest.RunAPIKeys`. The operationIds are `listAPIKeys`, `createAPIKey`, `revokeAPIKey` (Go's initialism rule: the generated handlers are `ListAPIKeys`, …). Creation also refuses a factory scope above the key's ceiling (400 `scope-not-allowed`: the key could never use it), and a factory scope must be held by the creator on every factory the key reaches (every factory they see for an unrestricted key).

### 6.7 `00047_personal_tokens.sql`

A table of its own, not a column on `api_keys`: an API key is an org's service account, a personal token a member's credential, and keeping them apart keeps each lookup, grant and test about one thing.

```sql
-- +goose Up
CREATE TABLE personal_tokens (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  user_id      uuid NOT NULL,
  name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  hint         text NOT NULL CHECK (hint ~ '^goal_pat_[A-Za-z0-9_-]{4}$'),  -- shown in lists
  hash         text NOT NULL CHECK (hash ~ '^[A-Za-z0-9_-]{43}$'),          -- base64url(SHA-256(token))
  scopes       text[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 64),
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL DEFAULT now() + interval '30 days'
               CHECK (expires_at > created_at AND expires_at <= created_at + interval '90 days'),
  revoked_at   timestamptz,
  revoked_by   text,
  last_used_at timestamptz,
  -- Removal from the organization deletes the member's tokens; a re-invitation starts empty.
  FOREIGN KEY (org_id, user_id) REFERENCES auth.member ("organizationId", "userId") ON DELETE CASCADE
);
CREATE UNIQUE INDEX personal_tokens_hash_key   ON personal_tokens (hash);
CREATE UNIQUE INDEX personal_tokens_name_key   ON personal_tokens (org_id, user_id, lower(name)) WHERE revoked_at IS NULL;
CREATE INDEX        personal_tokens_member_idx ON personal_tokens (org_id, user_id);  -- the cascade and "my tokens"

ALTER TABLE personal_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON personal_tokens
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT ON personal_tokens TO goal_app;
GRANT UPDATE (revoked_at, revoked_by, last_used_at) ON personal_tokens TO goal_app;
```

- `goal_personal_token(p_hash text)` (app; definer, `SET search_path FROM CURRENT`, `REVOKE ALL FROM PUBLIC`, `EXECUTE` to `goal_app` only) returns jsonb `{id, org_id, user_id, name, email, token_name, scopes, expires_at, org_role, factory_roles: [{factory_id, role}]}` for an unrevoked, unexpired token, inner-joined with `auth.member` (the current role), `auth."user"` and a live `organizations` row, else NULL. It never returns the hash and never reports platform admin. It sets `last_used_at = now()` when null or older than a minute. When the hash matches a revoked or expired token it appends `auth_events` `personal_token.refused {token_id, reason: revoked|expired}` (user = the owner), at most once a minute per token; an unknown hash writes nothing (no unauthenticated writes; the metric counts it, §12). A definer because the token names its organization before any org is bound.
- `goal_retire_org` is replaced again to revoke the org's live personal tokens too (`revoked_by = 'system:org-deleted'`).
- `goal_notify_access` gains the trigger `personal_tokens_access_revoked` (`AFTER UPDATE OF revoked_at` when it becomes non-null): kind `personal_token {org_id, user_id, token_id}`. A member's removal already sends `member`.
- Audit (goald, in the write's transaction): `personal_token.created {token_id, name, user_id, scopes, expires_at}`, `personal_token.revoked {token_id, name, user_id}` (the actor is the owner or the admin who revoked it).

As built (3-12b, `00047_personal_tokens.sql`): `revoked_by` is CHECKed to be an actor ref and set together with `revoked_at`; `goal_app` gets `UPDATE (revoked_at, revoked_by)` only (the stricter of the two reviews: `last_used_at` is the definer's to set). `goal_personal_token`'s refused event is written straight to `auth_events` by the definer, at most once a minute per token (`auth_events_user_idx`). `goal_identity_ready` now requires seventeen definers (`goal_personal_token` too). The lists name each token's owner from `goal_members()` (goal_app cannot read schema `auth`), and its revoker as the `personal_token.revoked` entry names them; the owner filter is a parameter of the statement (`queries/personal_tokens.sql`). The adapter is `postgres.Access` (`personaltokens.go`) and `postgres.Identity.PersonalToken`; the fake `porttest.Identity` (`personaltokens.go`: a member's removal deletes their tokens, as the cascade does); the conformance `identitytest.RunPersonalTokens`. `appendAudit` and `appendEvents` add `via` from `domain.ViaFrom(ctx)` (the fake's audit log too).

### 6.8 sqlc and conformance

- New query files: `queries/identity.sql` (calls of the definers), `factory_roles.sql`, `api_keys.sql`, `personal_tokens.sql`. `factory_roles`, `api_keys` and `personal_tokens` writes run under RLS in `inTx` with `appendAudit`. `appendAudit` and the item event writer add `via: pat:<id>` to their data when the principal in `ctx` carries a `Token`.
- Audit actions written by goald: `factory_role.granted|changed|removed {user_id, factory_id, from?, to?}`, `api_key.created {key_id, name, scopes, role_ceiling, factories, expires_at}`, `api_key.revoked {key_id, name}`, `org.ownership_transferred {from, to}`.
- `TestSchema_EveryOrgTableHasRLS` (A §6.6): `factory_roles`, `api_keys`, `api_key_factories`, `personal_tokens` carry the policy; `platform_admins`, `auth_events` join the allowlist; schema `auth` is out of its scope and covered by `TestSchema_AuthSchemaIsolation` (§13).

## 7. Bucket layout changes

None.

## 8. API

### 8.1 OpenAPI

- Tag `identity`. Security schemes `session` (`apiKey` in cookie `goal.session_token`; `__Secure-goal.session_token` when `GOAL_PUBLIC_URL` is https) and `bearer` (`http`, bearer: a session token from `goal login`, an API key `goal_sk_…` or a personal token `goal_pat_…`). Global `security: [{session: []}, {bearer: []}]`; `getHealth` and `getAuthConfig` declare `security: []`.
- Every operation carries `x-goal-scope: <permission>` (§8.2). A conformance test fails when an operation lacks one, names an unknown permission, or a permission is unused.
- New schemas: `AuthConfig`, `Me`, `User` (`{id, name, email}`), `OrgRole`, `FactoryRole`, `Permission` (enum generated from the table), `Member`, `FactoryRoleGrant`, `Invitation`, `ApiKey`, `NewApiKey`, `CreatedApiKey`, `TransferRequest`, `PlatformAdmin`, `AuthEvent`, `DeviceRequest`; from 3-12b `PersonalToken` (`{id, name, hint, scopes, owner: User, created_at, expires_at, last_used_at, revoked_at, revoked_by: Actor|null}`), `NewPersonalToken` (`{name, scopes, expires_in_days}`) and `CreatedPersonalToken` (`{token: PersonalToken, secret}`).
- `Me` gains `token?: {id, name, org_id, scopes, expires_at}` when the request carries a personal token (3-12b; `kind` stays `user`, `session` is absent).
- Changed: `OrgSummary` + `role: OrgRole|null` (null for a key); `Org` + `role`, `can: Permission[]` (org permissions held); `Factory` + `role: FactoryRole|null` (effective: `editor` for owners and admins) and `can: Permission[]` (factory permissions held); `Actor.kind` + `service`; `Problem.code` + `unauthenticated`, `reauth-required`, `auth-unavailable`, `factory-not-found`, `item-not-found`, `run-not-found`, `last-owner`, `not-an-admin`, `last-platform-admin`, `api-key-name-taken`, `scope-not-allowed`, and from 3-12b `personal-token-name-taken`, `browser-session-required` (§8.5).
- **The Better Auth client contract** (ADR 0007): the SPA and CLI call the allowlisted `/api/auth/*` paths of §5.5 directly, with Better Auth's own request and response shapes, pinned to the exact `better-auth` version (one pnpm catalog entry for `auth/` and `web/`). Only `web/src/api/auth.ts` and `cmd/goal/login.go` call them. Membership *changes* go to Better Auth (so its owner rules apply); membership *reads* come from goald (`listMembers`, `listInvitations`), joined with factory roles and last activity.

### 8.2 The permission table (`api/permissions.yaml`)

```yaml
# The one source of Goal's permissions. cmd/permgen (task api:gen) generates
# internal/api/permissions_gen.go and auth/src/access.gen.ts; task check fails on drift.
version: 1
org_roles: [owner, admin, member]
factory_roles: [viewer, approver, editor]   # a ladder: each includes the one before
permissions:
  # level: install | org | factory. An install permission is held by every signed-in user,
  # by platform admins only with platform_admin, by keys only with keys. org_roles: who holds
  # it (owners and admins hold every factory permission). factory_role: the lowest factory
  # role holding it. keys: API keys may carry it. fresh: needs a session signed in within 10
  # minutes. better_auth: the Better Auth access-control statements it maps to. pat: personal
  # tokens may carry it (3-12b; absent = false). permgen refuses pat on a fresh, platform_admin
  # or install permission other than authenticated. "High impact": never in a preset.
  - {name: public,             level: install}                       # no credential needed
  - {name: authenticated,      level: install, keys: true, pat: true} # any valid principal
  - {name: device:read,        level: install}                       # signed-in users, not keys
  - {name: org:create,         level: install, platform_admin: true}
  - {name: platform:read,      level: install, platform_admin: true}
  - {name: platform:manage,    level: install, platform_admin: true, fresh: true}
  - {name: org:read,           level: org, org_roles: [owner, admin, member], keys: true, pat: true}
  - {name: org:manage,         level: org, org_roles: [owner, admin]}
  - {name: org:delete,         level: org, org_roles: [owner], fresh: true,
     better_auth: {organization: [update, delete]}}   # Better Auth's own paths for both are off
  - {name: org:transfer,       level: org, org_roles: [owner], fresh: true}
  - {name: audit:read,         level: org, org_roles: [owner, admin], keys: true}
  - {name: member:read,        level: org, org_roles: [owner, admin, member]}
  - {name: invitation:read,    level: org, org_roles: [owner, admin]}
  - {name: member:manage,      level: org, org_roles: [owner, admin], fresh: true,
     better_auth: {member: [create, update, delete], invitation: [create, cancel]}}
  - {name: api_key:read,       level: org, org_roles: [owner, admin]}
  - {name: api_key:manage,     level: org, org_roles: [owner, admin], fresh: true}
  - {name: personal_token:read,   level: org, org_roles: [owner, admin, member]}               # one's own (3-12b)
  - {name: personal_token:manage, level: org, org_roles: [owner, admin, member], fresh: true}  # create one's own
  - {name: factory:create,     level: org, org_roles: [owner, admin], keys: true, pat: true}   # high impact
  - {name: factory:read,       level: factory, factory_role: viewer,   keys: true, pat: true}
  - {name: item:create,        level: factory, factory_role: approver, keys: true, pat: true}
  - {name: item:decide,        level: factory, factory_role: approver, keys: true, pat: true}
  - {name: item:operate,       level: factory, factory_role: approver, keys: true, pat: true}
  - {name: andon:manage,       level: factory, factory_role: approver, keys: true, pat: true}
  - {name: definition:edit,    level: factory, factory_role: editor,   keys: true, pat: true}  # high impact
  - {name: definition:publish, level: factory, factory_role: editor,   keys: true, pat: true}  # high impact
  - {name: factory:manage,     level: factory, factory_role: editor,   keys: true}
```

The owner role's Better Auth statements also include `organization: [update, delete]` (their paths are disabled; §8.6); as built in 3-4 they are `org:delete`'s `better_auth`, so permgen derives every role from the table alone (a role holds the `better_auth` actions of the permissions it holds; `better_auth` is for org permissions only). The same vocabulary names API key scopes and personal token scopes now and OAuth scopes in phase 5. `pat` is its own flag, not `keys`: the two sets differ (`audit:read` and `factory:manage` are `keys` only), and each new permission decides both explicitly. `TestPermissions_RoleGrants` pins the `pat: true` set to §4.5's list, so widening it is a reviewed diff.

**Operations by scope** (2.2's and phase 3's; phase 4 adds `credential:*`, `connector:*`, `approval:decide` in its PRs):

| Scope | operationIds |
| --- | --- |
| `public` | getHealth, getAuthConfig |
| `authenticated` | getMe, listOrgs (the caller's) |
| `device:read` | getDeviceRequest |
| `org:create` | createOrg |
| `platform:read` / `platform:manage` | listPlatformAdmins, listAuthEvents / addPlatformAdmin, removePlatformAdmin |
| `org:read` | getOrg, getOrgSettings, getOrgAttention, getFactoryOverview, getInbox, listFactories, streamEvents, B's listTemplates, getTemplate, validateDefinition, formatDefinition, D's getBuilderMeta, searchSkills, getSkillFiles |
| `org:manage` | patchOrgSettings |
| `org:delete`, `org:transfer` | deleteOrg, transferOwnership |
| `audit:read` | listAudit |
| `member:read`, `invitation:read` | listMembers, listInvitations |
| `member:manage` | setFactoryRole, removeFactoryRole (and Better Auth's invite, cancel, remove, change role) |
| `api_key:read`, `api_key:manage` | listAPIKeys, createAPIKey, revokeAPIKey; listMemberTokens, revokeMemberToken (3-12b) |
| `personal_token:read` | listPersonalTokens, revokePersonalToken (one's own; a write under `read` on purpose: revoking never waits for a fresh sign-in) |
| `personal_token:manage` | createPersonalToken (cookie session only, §4.5) |
| `factory:create` | createFactory |
| `factory:read` | listItems, getBoard, getMetrics and E's metrics, constraint, agent stats and andon reads, getItem, listItemEvents, E's runs, B's draft, model, files, versions and compare reads |
| `item:create` | submitItem |
| `item:decide` | approveItem, rejectItem, retryItem, mergeItem |
| `item:operate` | E's moveItem, pauseItem, addItemNote |
| `andon:manage` | E's pullAndon, clearAndon |
| `definition:edit` | B's draft create, save, discard, file and section writes, draft validate, deleteFactory (unpublished only) |
| `definition:publish` | publishDraft, restoreVersion |
| `factory:manage` | pauseFactory, resumeFactory |

The exact operationIds are whatever `api/openapi.yaml` holds when 3-1 lands; 3-1 assigns each one its scope by this table, and the golden matrix (§13) makes every later change visible. From 3-1 on, every new operation, 2.2's included, carries an `x-goal-scope`, or `task check` fails.

### 8.3 Authentication

1. **Credential** (`credential.go`, every `/v1` request):
   - `Authorization: Bearer <v>`: `goal_sk_` + 43 base64url characters is an API key; `goal_pat_` + 43 base64url characters is a personal token (3-12b); 32 characters `[A-Za-z0-9]` is a session token; anything else (an agent task token included) is 401. A request with a bearer value ignores cookies.
   - Else the session cookie (`goal.session_token`, or `__Secure-goal.session_token` on https; the other name is ignored): URL-decoded, the part before the first `.` must be 32 `[A-Za-z0-9]`. goald does not check Better Auth's cookie signature, by design: it is an HMAC under `BETTER_AUTH_SECRET`, which only the auth service holds (rule 7, §11.2), and it would only repeat what the database lookup says (the auth service issued this token). The token is the credential, as with bearer. So nothing is decided by transport: anyone holding a token can send it as a cookie with any signature. How the person signed in is the session's own record, `auth.session."signInMethod"` (`00048`: `email_code`, `google`, `device` for goal login, `other`), written by the auth service when Better Auth creates the session and returned by `goal_principal`; `Session.Browser` is true for `email_code` and `google` only (an untagged session is not the browser's) (review M1).
   - None: 401, except `public` operations, which read no credential at all (a stale cookie never keeps anyone from `getHealth` or the sign-in page's `getAuthConfig`).
2. **Principal** (`scope`, per operation), in this order of answers: the credential (`Identity.Session(token, org)`, `Identity.APIKey(sha256)` or `Identity.PersonalToken(sha256)`; unknown, expired or revoked → 401 whatever the path, so a stranger learns nothing about organizations), then the path's organization (`Tenancy.Org`; unknown or deleted → 404 `org-not-found`), then membership (a user who is not a member, or a key or personal token of another organization → the same 404). The lookups run in one transaction. Outside an organization (`listOrgs`, `getMe`, platform routes) the principal is resolved with org "".
3. **Per request, no cache.** Removal, role changes, revocation and sign-out take effect on the next request. Signed-in users keep working while the auth service is down (only signing in, out and membership changes need it).
4. **Keeping sessions alive.** Better Auth extends a session (7 days, `updateAge` 1 day) only when its own endpoints see it, so the SPA calls `GET /api/auth/get-session` on load and hourly, and the CLI at most daily (recorded beside the token).
5. **`GOAL_AUTH`** (fail closed): `session` (the default from 3-16) or `none`; any other value refuses to start. `none` requires a loopback `GOAL_LISTEN`, wires `AllowAll` and `LocalUser`, ignores well-formed credentials (a malformed one, an agent task token included, is 401 in every mode, as built in 3-3), mounts no `/api/auth`, logs a warning at startup and makes `getAuthConfig` report `mode: none`. Under `none` any process that reaches the listener, agent containers included, acts as the local user: it is one developer's laptop mode, never a shared install. With `session`, goald wires `Permissions` (never `AllowAll`) and refuses to start unless `goal_identity_ready()` is true, `GOAL_PUBLIC_URL` is valid (https unless loopback) and the listener rule of §5.5 holds. It starts when the auth service is unreachable (signed-in people keep working) and when no user exists yet, but logs a warning naming what is missing ("no one can sign in: set GOAL_BOOTSTRAP_OWNER_EMAIL and BETTER_AUTH_SECRET and start the auth service (goal up does), or set GOAL_AUTH=none"). A missing or invalid credential is 401, never `LocalUser`.

As built (3-9): `credential.go`'s `withCredential` replaces `withLocalUser`. A malformed `Authorization` header is 401 in both modes and on every route (3-3); under `session` the cookie is parsed there and resolved per operation in `identity.go`'s `bindRequest`, which public operations skip (a broken or stale cookie never reaches them). The organization is looked up before the credential (for the session's roles there) but answered only after it, in two statements rather than one transaction: `goal_principal` re-checks that the organization is live, so a deletion in between gives no role. `api.New` refuses `session` without an `Identity` or with `AllowAll`, and any other mode. Under `none` `OrgSummary.role`, `Org.role` and `Factory.role` are absent (3-10's contract) and `can` lists every permission of the level; `Permission` is a patterned string, not an enum.

### 8.4 Authorization (`Permissions.Authorize`)

For `AccessRequest{Principal, Operation, Org, Factory}`, from the generated table:

1. An operation missing from the table is refused (fail closed; the conformance test keeps it from happening).
2. **Personal tokens first** (3-12b), before the level rules: when `Principal.Token != nil`, the permission must be `pat: true` in the table and in `Token.Scopes`, else 403 `forbidden` (never `reauth-required`), decided from the token alone, so it says nothing about the organization or factory asked for. A rule that cannot be read refuses. Then the rules below apply to the owner as a user, with their current roles, so the result is scopes ∩ roles; `listOrgs` returns only the token's organization.
3. `public` passes; `authenticated` needs a principal; `device:read` a user; `org:create` and `platform:*` a user who is a platform admin (403 otherwise).
4. Org level: a non-member user or a key of another org → `ErrNotFound` (404). Then the org role (users) or scopes (keys) must hold the permission → else 403 `forbidden`.
5. Factory level, in `s.factory`/`s.item`: the effective factory role is `editor` for owners and admins, else `FactoryRoles[f]`; none, or a key restricted to other factories → `ErrNotFound` (404 `factory-not-found` / `item-not-found`). The role, and for keys the ceiling and scopes, must hold the permission → else 403. In the middleware pass (Factory "") a factory-level operation only needs membership (or a key of the org).
6. `fresh: true` permissions: users need `now − Session.CreatedAt ≤ 10 min` → else 403 `reauth-required`; keys never hold them, and personal tokens never reach this step for them (2).

Lists and aggregates show only visible factories: `listFactories`, `getOrgAttention`, `getFactoryOverview`, `getInbox` (decisions only where `item:decide` is held; E's other kinds where `factory:read` is held), `listTemplates` (unchanged), `getOrg.factory_count` (visible ones). For a personal token, "held" also needs the scope, and `can` lists only what the token can do.

### 8.5 Status codes

| Status | Code | When |
| --- | --- | --- |
| 401 | `unauthenticated` | no, malformed, unknown, expired or revoked credential; `WWW-Authenticate: Bearer realm="goal"`; one body, no detail |
| 403 | `forbidden` | the principal sees the resource but lacks the permission; a key on a never-for-keys operation; cross-origin refusal (2.2) |
| 403 | `reauth-required` | a fresh-only operation with an older session |
| 403 | `browser-session-required` | creating a personal token with a session not signed in through the browser (goal login's, however its token is sent) (3-12b, review M1); the auth service's `BROWSER_SESSION_REQUIRED`: a bearer credential on an endpoint that takes the browser's cookie only, or a goal login session approving a CLI sign-in |
| 404 | `org-not-found` | unknown, deleted, or not a member (users), another org (keys): one body |
| 404 | `factory-not-found`, `item-not-found`, `run-not-found` | unknown, or invisible to the principal: one fixed body each, no detail, so a factory's existence never leaks (a run's factory is as visible as its items) |
| 400 | `scope-not-allowed` | a key scope that is not `keys: true`, or a personal token scope that is not `pat: true`, or either not held by its creator |
| 409 | as 2.2, plus `last-owner`, `not-an-admin`, `confirmation-mismatch` (A), `last-platform-admin`, `api-key-name-taken`, `personal-token-name-taken` | the rule named |
| 421 | — | `Host` check (§5.5) |
| 503 | `auth-unavailable` | proxy only |

### 8.6 Better Auth configuration (`auth/src/auth.ts`)

```ts
betterAuth({
  appName: "Goal",
  baseURL: settings.publicURL,                 // BETTER_AUTH_URL = GOAL_PUBLIC_URL, required, static
  basePath: "/api/auth",
  // secrets: BETTER_AUTH_SECRETS ("2:new,1:old", versioned rotation) or BETTER_AUTH_SECRET, ≥ 32 bytes
  trustedOrigins: [settings.publicURL],        // never a wildcard
  database: { dialect: new PostgresDialect({ pool }), type: "postgres", schemaName: "auth" },
  disabledPaths: [
    "/organization/create", "/organization/update", "/organization/delete", "/organization/check-slug",
    "/organization/set-active", "/delete-user", "/change-email", "/link-social", "/unlink-account",
    "/get-access-token", "/refresh-token", "/account-info",
    "/email-otp/check-verification-otp", "/email-otp/verify-email", "/email-otp/request-password-reset",
    "/email-otp/reset-password", "/email-otp/request-email-change", "/email-otp/change-email",
    "/forget-password/email-otp",
  ],
  session: { expiresIn: 7 * 86400, updateAge: 86400, freshAge: 600, cookieCache: { enabled: false } },
  account: {
    accountLinking: { enabled: true, trustedProviders: ["google"], allowDifferentEmails: false },
    encryptOAuthTokens: true,
  },
  verification: { storeIdentifier: "hashed" },
  rateLimit: { enabled: true, storage: "database", window: 60, max: 300, customRules: {
    "/email-otp/send-verification-otp": { window: 60, max: 3 },
    "/sign-in/email-otp":               { window: 60, max: 5 },
    "/sign-in/social":                  { window: 60, max: 10 },
    "/device/code":                     { window: 60, max: 5 },
    "/device/token":                    { window: 60, max: 20 },
    "/device/approve":                  { window: 60, max: 10 },
    "/device/deny":                     { window: 60, max: 10 },
    "/organization/get-invitation":     { window: 60, max: 10 },
    "/organization/accept-invitation":  { window: 60, max: 10 },
    "/organization/invite-member":      { window: 60, max: 20 },
  } },
  advanced: {
    cookiePrefix: "goal",
    useSecureCookies: settings.publicURL.startsWith("https:"),
    defaultCookieAttributes: { httpOnly: true, sameSite: "lax", path: "/" },
    ipAddress: { ipAddressHeaders: ["x-goal-client-ip"], ipv6Subnet: 64 },
    trustedProxyHeaders: false,
    database: { generateId: "uuid" },
    backgroundTasks: { handler: (p) => { p.catch((e) => log.error("background task", e)); } },
  },
  socialProviders: settings.google ? { google: { clientId, clientSecret, prompt: "select_account" } } : {},
  databaseHooks: {
    user:    { create: { before: allowOnlyInvitedOrBootstrap } },          // §8.7
    session: { create: { after: recordSignInAndClaimBootstrap } },
  },
  hooks: { before: goalBeforeHook, after: goalAfterHook },               // below
  onAPIError: { errorURL: `${settings.publicURL}/sign-in` },
  telemetry: { enabled: false },
  plugins: [
    emailOTP({ otpLength: 6, expiresIn: 300, allowedAttempts: 3, storeOTP: "hashed",
               disableSignUp: false, sendVerificationOTP: sendCodeIfAllowed }),   // §8.7
    organization({
      ac, roles,                                 // generated: auth/src/access.gen.ts
      allowUserToCreateOrganization: false, disableOrganizationDeletion: true, creatorRole: "owner",
      invitationExpiresIn: 72 * 3600, requireEmailVerificationOnInvitation: true,
      cancelPendingInvitationsOnReInvite: true, invitationLimit: 100, membershipLimit: 1000,
      teams: { enabled: false },
      sendInvitationEmail: sendInvitation,
      schema: { member: { additionalFields: { changedBy, changedAt } },
                invitation: { additionalFields: { factoryRoles, changedBy, changedAt } } },
      organizationHooks: {
        beforeCreateInvitation: checkInvitation,      // one role; factoryRoles ⊆ goal_org_factories
        beforeUpdateMemberRole: checkRoleChange,      // one role; only owners touch an owner
        beforeRemoveMember: checkRemoval,             // only owners remove an owner
        afterAcceptInvitation: applyFactoryRoles,     // goal_apply_invitation_roles; failure is logged
      },
    }),
    deviceAuthorization({ expiresIn: "10m", interval: "5s", userCodeLength: 8,
                          validateClient: (id) => id === "goal-cli", verificationUri: "/device" }),
    bearer(),                                    // CLI: get-session, sign-out with the device token
  ],
});
```

- **`goalBeforeHook`**: (1) on `/organization/invite-member`, `/cancel-invitation`, `/remove-member`, `/update-member-role` and `/device/approve`, a session older than 10 minutes is refused with `FORBIDDEN` code `REAUTH_REQUIRED`; (2) every organization endpoint must name `organizationId` (or `invitationId`) explicitly, since the active organization is unused; (3) the stamp: for the organization paths, set `changedBy = user:<id>`, `changedAt = now()` on the target member or invitation row before Better Auth writes it (§6.4); (4) `/update-user` accepts only `name` (1–80 characters, trimmed).
- **`goalAfterHook`**: `otp.failed` on a failed `/sign-in/email-otp`; `device.requested` (client id, IP, user agent, subject = hash of the user code) on `/device/code`; `device.approved|denied`; `session.revoked` on sign-out.
- Roles: owner `{organization: [update, delete], member: [create, update, delete], invitation: [create, cancel]}`, admin `{member: [...], invitation: [...]}`, member `{}`; generated from §8.2.
- As built in 3-8 (`auth/src/hooks.ts`, `signin.ts`): `rateLimit.customRules` keys are paths under `basePath`, without `/api/auth` (Better Auth matches the path after it; the Vitest shows Goal's 10-a-minute rule replacing Better Auth's own 3-in-10-seconds one on `/sign-in/social`). Organization hooks get no session, so the rules that need the actor live in `goalBeforeHook`: one role on invite and role change, only an owner changes, removes or makes an owner (Better Auth's rule too), and the stamp; `beforeCreateInvitation` checks the organization is live (`goal_org_live`, else 400 `ORGANIZATION_NOT_FOUND`, as for an unknown one; review L1, with `00049`'s trigger refusing the row too), the role and the factory roles (members only, factories of `goal_org_factories`, one role each) and stamps the inviter; `beforeUpdateMemberRole` checks the role again. The before-hook also refuses `/email-otp/send-verification-otp` for any type but `sign-in` (400 `SIGN_IN_ONLY`) and requires an explicit `organizationId`/`invitationId`/`memberId` (400 `ID_REQUIRED`). `session.revoked` is written by `databaseHooks.session.delete.after` for `/sign-out` and `/revoke-sessions`, which sees the deleted session's user. Google's `getUserInfo` is Goal's: it accepts only an ID token with `email_verified: true` (so `trustedProviders` never joins an unverified Google email to a user) and keeps no picture; `databaseHooks.account` drops any refresh token. Refusal codes: `SIGN_UP_NOT_ALLOWED` (403; Google redirects to `/sign-in?error=SIGN_UP_NOT_ALLOWED`, an unverified Google email to `/sign-in?error=unable_to_get_user_info`), `REAUTH_REQUIRED` (403), `ONE_ROLE`, `OWNERS_ONLY` (403), `FACTORY_ROLES`, `UNKNOWN_FACTORY`, `FACTORY_ROLES_FOR_MEMBERS`, `NAME_ONLY` (400); a locked address gets Better Auth's own `INVALID_OTP` body.
- As built (review M1): `databaseHooks.session.create.before` writes `signInMethod` (a session additional field, `input: false`) from the endpoint that creates the session. Better Auth runs Goal's before-hook ahead of the bearer plugin's, which turns `Authorization: Bearer` into the session cookie, so the hook cannot see a bearer session: every path whose rule reads the session (`/update-user`, `/device/approve`, `/organization/invite-member`, `cancel-invitation`, `accept-invitation`, `reject-invitation`, `remove-member`, `update-member-role`, `leave`) refuses a request with an `Authorization` header, 403 `BROWSER_SESSION_REQUIRED` (until then a stale bearer session passed the fresh rule there, unstamped). `/device/approve` also refuses a session not signed in through the browser (`browserSignIn`: `email_code` or `google`), fresh or not, before the fresh rule, so a goal login session can never approve another code and keep itself fresh; a device session is fresh only in the 10 minutes after a person approved it in a fresh browser session.
- The service refuses to start when `BETTER_AUTH_URL` differs from `GOAL_PUBLIC_URL`, is http off loopback, the secret is shorter than 32 bytes, Google is on without its client id and secret, or the role check fails.

### 8.7 Sign-in rules

- **Who may get a user.** `allowOnlyInvitedOrBootstrap` refuses creating a user (`sign_up.refused`) unless the email has a pending, unexpired invitation to a live organization (`goal_is_invitee`, `00049`: a deleted organization's invitations open no sign-up, review L1), or no user exists yet and the email equals `GOAL_BOOTSTRAP_OWNER_EMAIL` (trimmed, case-insensitive). It applies to email codes and Google alike. Email OTP's `disableSignUp` stays `false` on purpose: `true` would stop an invitee, who has no user row until the first sign-in, from signing in at all; the hook is the single gate.
- **Who gets a code.** `sendCodeIfAllowed` runs in the background (`backgroundTasks`), so `/email-otp/send-verification-otp` answers the same `{success: true}` in the same time for anyone. It sends only for type `sign-in`, to an existing user or a pending invitee, at most 5 codes per address per hour, and never while the address is locked; otherwise it records `otp.suppressed`.
- **Codes.** 6 digits, 5 minutes, 3 attempts (then the code is void), stored hashed, identifiers hashed. After 10 failed codes for an address in 24 hours, sign-in by code is locked for that address until the oldest failure is 24 hours old (`otp.locked`); the person gets one email saying so. A locked attempt answers like a wrong code. Ten guesses a day give an attacker about a 0.4 % chance per year against one address.
- **Google.** No offline access, no refresh token kept; tokens encrypted at rest. A Google account and email codes with the same verified email are one user (`trustedProviders`). Risk: whoever controls an email domain later (an expired domain) can sign in as its former users; owners remove people who leave.
- **Bootstrap.** `recordSignInAndClaimBootstrap` calls `goal_claim_bootstrap` on every session created for the bootstrap email until it succeeds, so a failed first attempt heals on the next sign-in.
- **Email.** `Mailer` over SMTP: `GOAL_SMTP_URL` (default `smtp://127.0.0.1:1025`, Mailpit), `GOAL_SMTP_FROM` (default `Goal <goal@localhost>`). Plain text plus minimal HTML, no remote images or tracking, user-supplied names escaped, links only from `GOAL_PUBLIC_URL`. Messages: sign-in code ("Your Goal sign-in code is 123456. It expires in 5 minutes. If you didn't ask for it, ignore this email."), invitation ("M. Okafor invited you to Acme on Goal. Join: https://goal.example.com/invite/<id> · expires Oct 27"), lockout notice. Cloud: Amazon SES SMTP endpoint, SMTP credentials from Secrets Manager, SPF, DKIM and DMARC on the sending domain.

### 8.8 Stream

`streamEvents` (`org:read`; `?factory=` passes the resolver with `factory:read`) remembers the subscriber's principal: user and session, key, or user and personal token, and its visible factories. The feed delivers an event with a `factory_id` only for a visible factory; events without one go to every member. A stream ends (the client reconnects and is checked again) when a `goal_access` notice names its session, user in its org, key, personal token or org; at the session's, key's or personal token's expiry; and on every listener `Resync`, since access notices may have been missed. The browser's `EventSource` sends the same-origin cookie, so no token is put in a URL.

As built (3-9): `ports.AccessNotices` (`postgres.AccessNotices`, LISTEN `goal_access`, notices not read back: they only end streams) is wired with `api.Feed.EndOnAccessChanges` under `session` only, so `none`'s streams are unchanged. Visibility is asked per event with the remembered principal (owners and admins see factories created after they subscribed). Expiry is checked at each 15 s heartbeat; a key's is `domain.Principal.KeyExpiresAt` (3-12). An unknown notice kind ends the stream.

### 8.9 Endpoints

Under `/v1`; problems as §8.5; F = fresh session required.

| Method and path | operationId · scope | Request → response |
| --- | --- | --- |
| `GET /auth/config` | getAuthConfig · public | → `AuthConfig{mode: session\|none, methods: [email_code, google?]}` (`google` when `GOAL_AUTH_GOOGLE=on`) |
| `GET /auth/device/{user_code}` | getDeviceRequest · device:read | → `DeviceRequest{client_id, client_name: "Goal CLI", requested_at, expires_at, ip, user_agent, same_ip: bool}`; 404 unknown or expired |
| `GET /me` | getMe · authenticated | → `Me{kind: user\|service, mode, user?: User, key?: {id, name, org_id}, token?: {id, name, org_id, scopes, expires_at}, platform_admin, session?: {created_at, expires_at, fresh_until}}`; under `mode: none` the local user; with a personal token `token` is set, `session` absent and `platform_admin` false |
| `GET /orgs` | listOrgs · authenticated | → `{orgs: OrgSummary[]}`: the user's organizations with `role`; a key's or personal token's own org |
| `POST /orgs` | createOrg · org:create | `NewOrg` → 201 `Org`; the caller becomes its owner |
| `GET /orgs/{org}/members` | listMembers · member:read | → `{members: Member[]}`, `Member{member_id, user: User, role, factory_roles: FactoryRoleGrant[], joined_at, last_active_at, you, can_change_role, can_remove}` |
| `GET /orgs/{org}/invitations` | listInvitations · invitation:read | → `{invitations: Invitation[]}` (pending) |
| `PUT /orgs/{org}/members/{user}/factory-roles/{factory}` | setFactoryRole · member:manage F | `{role: FactoryRole}` → 200 `FactoryRoleGrant`; 404 non-member or unknown factory; 409 for an owner or admin (they already hold every role) |
| `DELETE /orgs/{org}/members/{user}/factory-roles/{factory}` | removeFactoryRole · member:manage F | → 204 |
| `POST /orgs/{org}/transfer` | transferOwnership · org:transfer F | `TransferRequest{to: user id, confirm: <org name>}` → 200 `Org`; 409 `not-an-admin`, `confirmation-mismatch` |
| `GET /orgs/{org}/api-keys` | listAPIKeys · api_key:read | → `{keys: ApiKey[]}`: `{id, name, hint, scopes, role_ceiling, factories: [id]\|null, created_by: Actor, created_at, expires_at, last_used_at, revoked_at}`; revoked ones for 30 days |
| `POST /orgs/{org}/api-keys` | createAPIKey · api_key:manage F | `NewApiKey{name, scopes, role_ceiling, factories?, expires_in_days (1–90, default 90)}` → 201 `CreatedApiKey{key: ApiKey, secret}`; the secret appears once, `Cache-Control: no-store`; 400 `scope-not-allowed`; 409 `api-key-name-taken` |
| `DELETE /orgs/{org}/api-keys/{key}` | revokeAPIKey · api_key:manage F | → 204 |
| `GET /orgs/{org}/personal-tokens` | listPersonalTokens · personal_token:read | → `{tokens: PersonalToken[]}`: the caller's own in this org (the owner filter is in the statement); revoked ones for 30 days |
| `POST /orgs/{org}/personal-tokens` | createPersonalToken · personal_token:manage F, cookie session only | `NewPersonalToken{name, scopes, expires_in_days (1–90, default 30)}` → 201 `CreatedPersonalToken{token, secret}`; the secret appears once, `Cache-Control: no-store`; 403 `browser-session-required`; 400 `scope-not-allowed`; 409 `personal-token-name-taken` |
| `DELETE /orgs/{org}/personal-tokens/{token}` | revokePersonalToken · personal_token:read | → 204; the caller's own only (another's is 404); no fresh sign-in |
| `GET /orgs/{org}/member-tokens` | listMemberTokens · api_key:read | → `{tokens: PersonalToken[]}`: every member's, with `owner` |
| `DELETE /orgs/{org}/member-tokens/{token}` | revokeMemberToken · api_key:manage F | → 204 |
| `GET /platform/admins` | listPlatformAdmins · platform:read | → `{admins: PlatformAdmin[]}` |
| `POST /platform/admins` | addPlatformAdmin · platform:manage F | `{email}` → 201; 404 no such user |
| `DELETE /platform/admins/{user}` | removePlatformAdmin · platform:manage F | → 204; 409 `last-platform-admin` |
| `GET /platform/auth-events?before=&limit=&kind=` | listAuthEvents · platform:read | → `{events: AuthEvent[], next_before}` |

As built (3-11): `ports.Access` holds `Members`, `Invitations`, `SetFactoryRole` and `TransferOwnership` (3-12 and 3-12b add the key and token methods); `porttest.Identity` is the fake of both ports over one state (its test setter is `GrantFactoryRole`), and `identitytest.RunAccess` is their conformance suite. `Member.can_change_role` is true when the caller holds `member:manage`, sign-in is on, and the member is not an owner unless the caller is an owner and the organization keeps another; `can_remove` is the same but never for oneself (who leaves); `factory_roles` lists only factories the caller sees. `setFactoryRole` resolves `{factory}` among the definition store's factories (drafts included), 409 `editor-everywhere` for an owner or an admin, 404 (with detail) for someone who is not a member; the same role again writes no audit entry. `transferOwnership` is 403 under `GOAL_AUTH=none` (nobody owns anything there) and maps `goal_transfer_from_owner` to a 409 without a code. Invite-Accept needs the inviter's name and the factories' names, which the invitee may not read from goald: the auth service's after-hook adds `inviterName` and `factories: [{id, name, role}]` to Better Auth's `get-invitation` answer (its recipient only, as Better Auth checks).

Key and personal token secrets: `goal_sk_` or `goal_pat_` + base64url of 32 random bytes from `crypto/rand`; goald stores the hash and the hint, never the secret, and never logs request or response bodies of these operations.

## 9. CLI changes

| Command | Does |
| --- | --- |
| `goal login [--store keychain\|file]` | Device flow (RFC 8628, client `goal-cli`): `POST /api/auth/device/code`, prints "Open https://goal.example.com/device and enter ABCD-EFGH", opens the browser at `/device` (never the complete URI with the code), polls `/device/token` at the server's interval (`authorization_pending`, `slow_down`, `expired_token`, `access_denied`). Stores the session token in the OS keychain (`github.com/zalando/go-keyring`, service `goal`, account = the API origin); `--store file` (remembered as `credential_store` in `config.json`) writes `$XDG_CONFIG_HOME/goal/credentials.json` with mode 0600 for headless machines. |
| `goal login --token [--store keychain\|file]` | (3-12b) reads a personal token from stdin only (`goal login --token < file`, or typed without echo on a terminal); a token on the command line (`--token=…` or an argument) is refused, since argv shows in `ps` and shell history. Checks it with `GET /v1/me`, then stores it like a session token: keychain keyed by the API origin, or the 0600 file with `--store file`. There is no `goal token create`: tokens are created in the browser only (§4.5). |
| `goal logout [--all]` | `POST /api/auth/sign-out` (or `/revoke-sessions`) with the token, then deletes it locally; a server error still deletes it and says so. A stored personal token is only deleted locally, with "Revoke it in Goal under API keys → Personal tokens if it may have leaked" |
| `goal whoami` | `GET /v1/me` and `GET /v1/orgs`: name, email or key, platform admin, session expiry, organizations with roles; with a personal token its name, organization, scopes and expiry ("Personal token laptop in Acme · expires Nov 23") |
| `goal key create <name> --scope S… [--factory F…] [--role viewer\|approver\|editor] [--expires 90d]` | prints the secret once to stdout, the rest to stderr; `goal key list`, `goal key revoke <name\|id>`. As built (3-12): `--scope` and `--factory` repeat or take commas, `--role` defaults to `viewer`, `--expires` takes `30d` or `30` (1 to 90); `revoke` finds a live key by id or name, case ignored |
| `goal admin list\|add <email>\|remove <email>`, `goal admin events [--kind K]` | platform admins and the security log |
| `goal db recover-admin <email>`, `goal db recover-owner <org> <email>` | with `GOAL_DATABASE_ADMIN_URL` (proof of database access; default compose's superuser, as `goal db init`): makes the user a platform admin, or an owner of the organization (`changedBy = system:recovery`), creating the user first when the email has none (name = the email, `emailVerified` true), so they can then sign in by code; records `recovery.used` and `member.added {via: recovery}`. Works with goald and the auth service stopped. |

- **Which credential.** As built: `GOAL_API_KEY` is 3-13's `cmd/goal/credentials.go`; 3-12 adds that with it set the config file's `org` is skipped (`resolveOrg`: the key lists its own organization only), and 3-16 does the same for a key or personal token stored with `goal login --token`. `GOAL_API_KEY` from the environment (never written anywhere) wins; it holds an API key (`goal_sk_…`) or a personal token (`goal_pat_…`), and any other value is refused before a request is sent. Else the stored session or personal token for the API origin. Tokens are keyed by origin, so changing `api_url` or `GOAL_API_URL` never sends a token to another host. Credentials are sent only over https, or http to a loopback address (the existing `GOAL_GITHUB_API_URL` rule).
- **Errors.** A 401 prints "Not signed in to https://goal.example.com: run goal login" and exits 77 (`EX_NOPERM`); `reauth-required` prints "This needs a recent sign-in: run goal login again"; 403 exits 77 too.
- `goal org create` needs a platform admin. Org resolution (A §9) is unchanged; with an API key or a personal token the default organization is its own.
- `goal up` resolves `GOAL_AUTH` with goald's default. Under `session` it also starts Mailpit (compose) and the auth service, splits the environment (§5.6), and prints `GOAL_PUBLIC_URL` and the Mailpit URL; it refuses to start, naming the variable and the `GOAL_AUTH=none` alternative, when `BETTER_AUTH_SECRET(S)` is unset or reused, or when no user exists and `GOAL_BOOTSTRAP_OWNER_EMAIL` is unset. Under `none` it starts neither. As built (3-9): the auth service gets exactly the Taskfile's `AUTH_VARIABLES` (a test keeps the two lists equal) with `GOAL_PUBLIC_URL` defaulted to `http://<GOAL_LISTEN>` for both processes; goald never gets the auth service's own variables, in either mode (it reads none of them); a missing `auth/dist/server.js` is refused with "run task auth:build"; the auth service stopping leaves goald running. `task e2e:auth` (`cmd/goald/signin_e2e_test.go`, tag `e2e`) runs the whole flow on a throwaway goald (127.0.0.1:18089), database, bucket and auth service (127.0.0.1:18091) with compose's Mailpit, and `WEB=1` runs `task web:e2e-auth`'s Playwright flow against it.
- Stdlib `flag`, no `init()`, as today; tests against a stub API cover each command's requests, the keychain fake, file permissions, origin scoping and exit codes.
- As built (3-15): `goal admin remove <email>` finds the admin's id with `listPlatformAdmins` and sends `removePlatformAdmin`; `goal admin events` also takes `--before N` (the page cursor it prints). The recoveries are `postgres.RecoverAdmin` / `RecoverOwner` on the admin connection, no migration: one transaction checks the database is migrated (else "start goald once"), finds the user by lower-case email or creates it (`user.created {method: recovery}`), then `goal_add_platform_admin(email, 'system:recovery')` or the `auth.member` insert/promotion stamped `changedBy = system:recovery` (an unknown or deleted organization changes nothing), and appends `recovery.used {command, created_user, org_id?, was?}` with the email's subject. `Platform.AddPlatformAdmin` returns `domain.PlatformAdmin` (the user, `added_by`, `added_at`) and `addPlatformAdmin` answers 201 `PlatformAdmin`. `Platform.CleanUpAuth` runs `goal_auth_cleanup` in a transaction holding `pg_try_advisory_xact_lock(hashtext('goal.auth.cleanup'))` (`AuthCleanUp.Skipped` when another goald holds it); goald runs it at start and daily in both modes. The org menu offers Create organization when `/v1/me` says `platform_admin`, or `mode: none` (the local user).

As built (3-13): `goal login` remembers either store as `credential_store` (keychain too), and commands read a stored credential only when `config.json` names a store, so a machine where nobody signed in never touches its keychain. It reads `GET /v1/auth/config` first and says so under `GOAL_AUTH=none`; it opens `$BROWSER` when set, else the system's opener, at the service's `verification_uri` only when that is an http(s) `/device` page without a query, else `<origin>/device`; it polls at the service's interval (+5 s per `slow_down` or 429) and checks the session with `GET /v1/me` before storing it; a second `goal login` signs the replaced session out. `access_denied` exits 77 like a refused credential. Every request carries `User-Agent: goal/<version> (<os>)`, the tool Device-Approve shows; the CLI keeps a stored session alive with `GET /api/auth/get-session` at most daily (`kept_alive_at` beside the token). `getDeviceRequest` answers one fixed 404 `device-code-not-found` (also under `none` and for a malformed code), `requested_at`, `ip` and `user_agent` are absent when the security log has none, and `same_ip` compares the browser's address (goald's `X-Goal-Client-Ip` rule, `GOAL_TRUSTED_PROXIES`) with the recorded one, IPv6 by /64. The page claims the code (`GET /api/auth/device?user_code=`) only after goald found it pending, and treats a code another person opened as not valid. As built (3-12b): `goal whoami` with a personal token prints the person, then "Personal token laptop in Acme · expires Nov 23" and its scopes, never a session or platform admin.

## 10. Web UI

### 10.1 Routes, data and states

| Route | Board | Data |
| --- | --- | --- |
| `/sign-in?next=` | Sign-In | `['auth', 'config']`; Better Auth `email-otp/send-verification-otp`, `sign-in/social` |
| `/sign-in/code` | Sign-In-Code | the email in router state (never in the URL); `sign-in/email-otp` |
| `/invite/:invitation` | Invite-Accept | Better Auth `get-invitation`, `accept-invitation`, `reject-invitation` |
| `/device` | Device-Approve | `getDeviceRequest`; Better Auth `GET /device`, `device/approve`, `device/deny` |
| `/o/:org/members` | Members | `['org', id, 'members']`, `['org', id, 'invitations']`, `['org', id, 'factories']` |
| `/o/:org/api-keys` | Api-Keys | `['org', id, 'api-keys']`, `['org', id, 'factories']`; from 3-12b `['org', id, 'personal-tokens']`, `['org', id, 'member-tokens']` |
| rail avatar | Account-Menu | `['me']`, the current org's `role` |

- New query keys: `['auth', 'config']`, `['me']`, `['org', id, 'members']`, `['org', id, 'invitations']`, `['org', id, 'api-keys']`, `['org', id, 'personal-tokens']`, `['org', id, 'member-tokens']`. `['orgs']`, `['org', id, 'detail']` and `['org', id, 'factories']` now carry `role` and `can`. Mutations through Better Auth invalidate `members` and `invitations`; a stream that ends and reconnects resyncs as today.
- `web/src/api/auth.ts` is the only module calling `/api/auth/*` (`createAuthClient` from `better-auth/client` with the email OTP, organization and device client plugins, same pinned version), exposed as TanStack mutations.
- **Session handling.** The `/v1` client middleware: 401 clears the query cache and navigates to `/sign-in?next=<current path>`; `next` is accepted only as a same-origin path starting with `/` and not `//`. 403 `reauth-required` opens the re-auth dialog and retries the action once after a fresh sign-in (email code inline; Google returns to the same page with "Signed in again. Try that once more."). After two stream errors in a row the app refetches `['me']`. `mode: none` skips every sign-in screen.
- **Role-aware UI.** Controls render from `can`, never from role names: absent permission → the control is hidden when it would only confuse (Invite, New key, Danger zone) or disabled with a reason (Approve: "Approvers of sre decide here"). The server checks everything again.
- Org sidebar: "Members and roles" for everyone; "API keys" with `api_key:read` or `personal_token:read` (so every member reaches their personal tokens). Org switcher rows show "3 factories, owner"; Create organization only for platform admins.
- `web/mock` serves `/v1/auth/config`, `/v1/me` (F. Cruz, owner of `acme`, platform admin), members, invitations, keys, personal tokens, and a mock `/api/auth/*` subset (any address gets code `000000`) so preview deployments and Playwright run without goald.
- Phone: every new screen at 375 px; dialogs become sheets; the code input fills the width; Members becomes a list of person cards with a details sheet.
- Accessibility: one `h1` per page; the code input is one field (`inputmode="numeric"`, `autocomplete="one-time-code"`, paste-friendly) with an associated error; errors are announced (`aria-live="polite"`); role selects have visible labels; destructive dialogs focus Cancel first.

### 10.2 Boards for the designer

Every board follows `design-system/goal/MASTER.md`, desktop 1440 and phone 375. Copy below is the content; exact layout is the designer's.

**Sign-In** (`Sign-In.dc.html`, no rail; centered card on the app background)
- Goal wordmark; h1 "Sign in to Goal"; field "Email" (placeholder `you@example.com`); primary "Email me a code"; divider "or"; secondary "Continue with Google" (only when enabled); note "Goal is invite-only. Ask an owner of your organization to invite you."
- States: sending (button busy); can't reach Goal ("Can't reach Goal. Check your connection and try again."); rate limited ("Too many tries. Try again in 2 minutes."); Google refused on return ("That Google account isn't invited to Goal."); generic Google failure ("Google sign-in didn't finish. Try again.").
- Re-auth dialog variant "Confirm it's you": "This change needs a recent sign-in. We'll email a code to f.cruz@example.com." Buttons "Email me a code", "Continue with Google", "Cancel".
- No-organization state (signed in, no orgs): "You're signed in as f.cruz@example.com but not in an organization yet. Ask an owner for an invitation." Button "Sign out".

**Sign-In-Code** (`Sign-In-Code.dc.html`)
- h1 "Check your email"; "We sent a 6-digit code to f.cruz@example.com. It expires in 5 minutes."; one code field (six visual cells, `123 456` spacing); primary "Sign in"; links "Send a new code" (disabled 30 s with countdown "Send a new code in 0:24") and "Use a different email".
- States: verifying; wrong code ("That code didn't work. Check it, or send a new one."; the same text when locked); expired ("That code has expired. Send a new one."); used up after 3 tries ("Send a new code to try again."); new code sent ("We sent a new code."); success (redirect).

**Invite-Accept** (`Invite-Accept.dc.html`)
- Signed out: "Sign in to see your invitation", button "Sign in" (returns here).
- Invitation: org mark and name; "M. Okafor invited you to join Acme as a member."; "In factories: sre, viewer"; "Expires Oct 27"; primary "Join Acme", secondary "Decline".
- States: joining; joined (navigate to `/o/acme`, toast "You joined Acme"); wrong account ("This invitation is for another email address. You're signed in as j.doe@example.com." Button "Sign out and switch"); can't be used ("This invitation can't be used. It may have expired or been cancelled. Ask for a new one."); already a member (button "Open Acme").

**Device-Approve** (`Device-Approve.dc.html`)
- Step 1: h1 "Sign in to the Goal CLI"; "Enter the code shown in your terminal."; one 8-character field shown as `ABCD-EFGH`; "Continue". The page never fills the code from its URL (`verification_uri_complete`), so a link someone sends cannot pre-approve anything.
- Step 2 card: "Goal CLI wants to sign in as F. Cruz (f.cruz@example.com)"; rows "Requested from 203.0.113.7 · same network as this browser" (or "a different network"), "Browser or tool: goal/1.4 (darwin)", "Requested 1 minute ago · expires in 9 minutes"; warning callout "Only approve if you just ran goal login yourself. Never approve a code someone sent you."; primary "Approve", secondary "Deny".
- Step 3: approved ("The CLI is signed in. You can close this tab."); denied ("Denied. The CLI was not signed in.").
- States: unknown or expired code ("That code isn't valid any more. Run goal login again."); re-auth needed (the dialog); signed out (sign in first, then back here).

**Api-Keys** (`Api-Keys.dc.html`, org page with the org sidebar, "API keys" current)
- h1 "API keys"; "Keys let scripts and CI act for Acme. A key never manages people, keys or ownership. Keys expire after at most 90 days."; primary "New key".
- Table: Name; Key (`goal_sk_Ab3x…`, Fira Code); Can (scope chips, "+3 more"); Factories ("All" or names); Acts at most as (Viewer/Approver/Editor); Created ("F. Cruz · Oct 2"); Expires ("Dec 31", amber "in 9 days" under 14 days); Last used ("2 h ago", "Never"); action "Revoke…". Revoked keys greyed, "Revoked Oct 20 by F. Cruz".
- New key dialog: Name; Expires in (30, 60, 90 days; default 90); Acts at most as (radio cards Viewer, Approver, Editor); Factories (All, or a checklist); What it can do (grouped checkboxes: Read — see the organization, see factories, read the audit log; Work — submit items, decide items, move and pause items, pull and clear the andon; Build — edit drafts, publish versions, pause factories, create factories) with presets "Read only", "CI: submit work", "Publish definitions"; "Create key".
- Created dialog: "Copy your key now. You won't see it again."; the secret in a mono field with "Copy"; checkbox "I've stored it somewhere safe"; "Done" enabled after the checkbox.
- Revoke dialog: "Revoke ci-deploy? Anything using it stops working at once." Cancel / "Revoke key".
- States: loading; empty ("No keys yet. Create one for CI or a script."); error; re-auth.
- As built (3-12): `/o/$org/api-keys` (`web/src/features/api-keys/`), listed in the organization sidebar and menu for `api_key:read` (`Org.can`, 3-14's `useOrgGrants`/`useOrgHolds`); New key and Revoke… for `api_key:manage`. Revoked keys are greyed in the table and counted on narrow screens (Mobile-Api-Keys). The Key created dialog closes only by Done (no Escape, no click outside: `DialogContent dismissible={false}`). "Use a key from CI" shows commands the CLI has (`goal factory list`, `goal run <factory> "…" --detach`); the board's `goal whoami` arrives with 3-13. The mock's keys are `web/mock/keys.ts`.
- **Personal tokens section** (3-12b; a designer follow-up on the Api-Keys and Api-Keys-New boards; `id="personal-tokens"`, shown with `personal_token:read`; members without `api_key:read` see only this section): h2 "Personal tokens"; "A personal token lets the CLI or a script on your machine act as you in Acme, with no more than your role allows. Tokens expire after at most 90 days."; secondary "New token". Table: Name; Token (`goal_pat_Q7mk…`); Can (scope chips); Expires; Last used; "Revoke…" (no re-auth). Empty: "No personal tokens. Use goal login for the CLI, or create a token for a headless machine."
- New token dialog: Name; Expires in (7, 30, 60, 90 days; default 30); What it can do: Read — see the organization, see factories; Work — submit items, decide items, move and pause items, pull and clear the andon; **High impact** — edit drafts, publish versions, create factories, under the callout "Publishing a version controls the prompts agents run and the repository they push to. Give this only to a token you keep safe."; preset "CLI: work" (Read and Work; never High impact); "Create token" (needs a recent sign-in: the re-auth dialog). The dialog takes nothing from the URL: name, scopes and expiry are never prefilled from a query string or a link, so a sent link cannot set up a token for someone else's use.
- Token created dialog: "Copy your token now. You won't see it again."; the secret with "Copy"; warning callout "Anyone with this token can act as you in Acme. Goal never asks for it."; checkbox "I've stored it somewhere safe"; "Done".
- **Members' tokens section** (`api_key:read`): h2 "Members' tokens"; the same columns plus Owner ("J. Lindqvist"); "Revoke…" ("Revoke J. Lindqvist's token laptop? Anything using it stops working at once."); needs a recent sign-in.
- As built (3-12b): both sections are `web/src/features/api-keys/personal-tokens.tsx` on the Api-Keys page, which shows its organization keys only with `api_key:read` (the sidebar and menu list the page with `api_key:read` or `personal_token:read`); the tables also show Created. Scopes the creator's role does not hold are disabled with "Your role doesn't allow it"; "CLI: work" never ticks a high-impact scope; the dialog reads nothing from the URL (`tokens.test.ts` checks its source). Token created reuses `created-dialog.tsx` (`dismissible={false}`) with the warning callout. Under sign-in off the section says personal tokens need sign-in and offers no New token. The Account menu's "Personal tokens" shows with `personal_token:read`. The mock is `web/mock/keys.ts` over 3-11's people (`mock/people.ts`); Playwright `web/e2e/personal-tokens.spec.ts`.

**Account-Menu** (`Account-Menu.dc.html`, popover from the rail avatar; bottom of the org sheet on phones)
- Initials avatar; "F. Cruz"; `f.cruz@example.com`; "Owner of Acme" (role in the current org); tag "Platform admin" when true.
- Items: "Edit name…" (dialog with one field), "Personal tokens" (3-12b; to `/o/:org/api-keys#personal-tokens` of the current org), "Sign out", "Sign out everywhere…" (confirm: "Sign out of every browser and the CLI?"; personal tokens stay valid until revoked, and the dialog says so).
- `mode: none` variant: "Local user · This install · Sign-in is off", no items.

**Updated boards**
- **Members**: as drawn, plus: Invite dialog (emails, one per line, up to 20; role Member/Admin/Owner, Owner only for owners; factory roles per factory, default None; "Send invitations"); each row's org role select disabled with a reason where not allowed ("Only owners change an owner", "Acme needs at least one owner"); row menu "Factory roles…", "Remove from Acme…"; your own row "Leave Acme…"; pending invites "Resend" and "Cancel"; the per-person factory panel lists owners and admins as "Editor in every factory (from their organization role)", disabled; members without `member:manage` see the same page read-only; "Last active" by day ("Today", "Yesterday", "Oct 20").
- **Org-Switcher / Organization sheet**: role in each row; "Members and roles" link; Create organization only for platform admins; the account block opens Account-Menu.
- **Org-Settings**: Transfer ownership dialog (select among admins; none: "Make someone an admin first" linking to Members; type the org name; "Transfer ownership"; after: "You're now an admin of Acme"); Danger zone shown to owners only; members see General read-only.
- **Basics (New-Basics, Define-Basics)**: "Who approves" read-only: "Owners and admins of Acme, and this factory's approvers and editors: M. Okafor, J. Lindqvist." Link "Change in Members" with `member:manage`. New factory: "Owners and admins approve until you add approvers in Members."; human stations' "Who works here: approvers of {factory}".
- **Definition boards**: read-only mode without `definition:edit`: banner "View only. Editors of goal change its definition."; inputs read-only, Publish, Discard and YAML editing absent.
- **Board, Item, Phone**: decisions, Move, Pause, notes and "Pull the andon cord" from `can`; for viewers the andon button is disabled with "Approvers and editors can pull the cord".
- As built (3-14): controls follow `can` as listed in `docs/engineering/frontend.md` ("What the caller may do"). A viewer sees the running version, not someone's open draft ("M. Okafor is working on a draft of v9."); a factory never published shows its draft. Decisions wait "on you" only where `item:decide` is held, "on an approver" elsewhere. A 403 `forbidden` refetches `can`. The mock plays each role (`POST /__mock/reset {role}`) and refuses by `x-goal-scope`, as goald does.

## 11. Security and tenancy

### 11.1 Threat model

| # | Threat | Control | Test |
| --- | --- | --- | --- |
| 1 | A member of org B reads or changes org A, or learns it exists | org from the path, resolved to its id before any check; principal resolved per org; non-members, other orgs' keys and invisible factories and items get the unknown resource's exact body; `listOrgs` only the caller's | `TestAuthorize_TwoOrgMatrix` and `TestAPI_TwoOrgMatrixOverHTTP` (fake and PostgreSQL; `access.golden` holds every status and problem code); `TestOrgs_UnknownAndDeletedAreTheSame404` (one body); `identitytest` `OrgsFor` |
| 2 | A removed member, revoked key or personal token, or signed-out session keeps access | per-request lookup, no cache; streams end on `goal_access`, expiry, resync | `identitytest` revocation cases; stream test |
| 3 | Anonymous access | 401 on every operation but `public` | matrix |
| 4 | Misconfigured install opens up | `GOAL_AUTH=none` loopback only; non-loopback needs https and a separate gateway listener; startup self-check; never falls back to `LocalUser` | `TestSettings_RefusesUnsafeConfiguration`; `TestAPI_RefusesTaskTokensAndCredentialsItCannotUse` (an invalid credential is 401, never `LocalUser`); `TestRoutes_TheGatewayListenerServesOnlyTheGateway` (gateway listener serves only `/mcp`, main one 404s it) |
| 5 | Session theft by script | httpOnly cookie; no token in JS, storage, URLs or answers: Better Auth puts it in get-session's `session.token` and a sign-in's `token`, which the auth service's after-hook removes and goald's proxy removes again (a session answer it cannot read is 503) (review M2); `Set-Auth-Token` stripped; CSP | header test; proxy tests (`TestAuthProxy_DropsSetAuthToken`, `TestAuthProxy_AnswersNoSessionToken`); Vitest `session-format` (neither the sign-in's nor get-session's answer holds the token); e2e (mock and `web/e2e-auth`) asserts `document.cookie` lacks the session and get-session's answer to a page script lacks the cookie's token |
| 6 | CSRF | SameSite=Lax; `CrossOriginProtection` on `/v1` and the proxy; Better Auth origin check with `trustedOrigins` = `GOAL_PUBLIC_URL` only | cross-origin POST tests (`TestAPI_CrossOriginChangesAreRefused`, `TestAuthProxy_RefusesCrossOriginWrites`); Vitest: a cookie change from another Origin is 403 `INVALID_ORIGIN` (checks on whatever `NODE_ENV` says), extra trusted origins refused, `SameSite=Lax` pinned |
| 7 | Reaching disabled Better Auth endpoints, or tying up the auth service | exact allowlist; encoded-path refusal; `disabledPaths`; `api/` reserved in the SPA fallback; 64 KiB and timeouts | proxy table test (incl. `/organization/create`, `//`, `%2F`, `..`, case, trailing slash, an unknown `/api/…` never serving `index.html`, size and timeout) |
| 8 | IP spoofing to dodge rate limits | inbound forwarding headers dropped; `X-Goal-Client-Ip` from `GOAL_TRUSTED_PROXIES`; auth listener on loopback | proxy header tests |
| 9 | Link poisoning in emails | static `BETTER_AUTH_URL`; links built from it | auth test with forged `Host`/`X-Forwarded-Host` |
| 10 | Code brute force | 6 digits, 5 min, 3 tries, 5 codes/h, lockout after 10 failures/24 h, DB rate limits | vitest with fake clock |
| 11 | Account enumeration | uniform response; background sends; same timing | vitest with a slow fake mailer |
| 12 | Email bombing | codes only to users and invitees; per-address cap; IP rate limit | vitest |
| 13 | Unauthorized sign-up | `user.create.before` gate for codes and Google; invitees of live organizations only, and a deleted organization invites no one (`beforeCreateInvitation` and the `invitation_live_org` trigger, `00049`, review L1); Google without offline access | vitest (both methods, no `access_type=offline`, a pending invitation of a deleted organization refused, inviting for a deleted organization 400 `ORGANIZATION_NOT_FOUND` with no row and no mail), bootstrap cases; `TestAuthTriggers_InvitationsOnlyToALiveOrg` |
| 14 | Invitation hijack | email match plus `requireEmailVerificationOnInvitation`; invitation ids only to `invitation:read`, never in logs (no query strings; `/invite/…` paths logged without the id) | Vitest (another account, unverified; invitation id and emails never in the auth service's log); `TestAPI_AccessLogNamesThePrincipalNeverItsSecrets` (goald logs operations, never paths or query strings) |
| 15 | Admin escalates to owner or removes one | Better Auth's rule plus Goal's hooks; CHECK on roles | vitest |
| 16 | Last owner lost under concurrency | deferred trigger with the org row locked | integration: two concurrent demotions |
| 17 | Org lifecycle split | goald is the only writer; definer is atomic; Better Auth paths off | `TestProvisionOrg_Atomic`; proxy table test (create, update, delete refused); Vitest: `/organization/create`, `/update`, `/delete` are 404 and change nothing |
| 18 | Over-privileged key | scopes ⊆ `keys: true` ∩ creator; ceiling; factory restriction; never-for-keys set | role grants test; API tests |
| 19 | Key leak | hash only, shown once, ≤ 90 days, immediate revocation, `no-store` | `identitytest`; API test |
| 20 | Device-code phishing | client validated; 10 min; code typed; details shown; a fresh browser session to approve (never a goal login session, review M1); rate limits | Vitest (client validated, code expires after 10 minutes, fresh session to approve, a goal login session refused as a bearer and as a signed cookie: `session-format.integration.test.ts`); `web/e2e/device.spec.ts` |
| 21 | Token confusion | task tokens need `aud: goal-gateway` and work only at `/mcp`; `/v1` refuses them; distinct secrets | gateway and API tests |
| 22 | Forged audit actor | actor from the session stamp or definer; triggers atomic with the change | integration per audited path |
| 23 | Database role abuse | `goal_app` has nothing on `auth`; `goal_auth` only DML there; startup role checks | `TestSchema_AuthSchemaIsolation`; auth startup test |
| 24 | Secrets in logs | never `Cookie`, `Authorization`, tokens, codes, keys, emails in goald logs | log-capture tests: `TestAuthProxy_LogsNoSecrets`, `TestAPI_AccessLogNamesThePrincipalNeverItsSecrets` (credential middleware and access log), auth service (sign-in by code logs no code, address or session; settings never print a secret) |
| 25 | Better Auth vulnerability | exact pin ≥ 1.7.12, four plugins, patch SLA, `pnpm audit` | `task check` (`pnpm audit --prod` for `auth/` and `web/`; exceptions and overrides in `SECURITY.md`); the catalog pin in `pnpm-workspace.yaml` |
| 26 | Stream leaks an invisible factory | per-subscriber factory filter | stream test |
| 27 | Open redirect | `next` same-origin paths only; Better Auth checks callback URLs against trusted origins | Vitest (`safeNext`); auth test: a Google sign-in whose `callbackURL` is off `GOAL_PUBLIC_URL` is 403 |
| 28 | Database dump | session tokens stored as Better Auth stores them (plain: 1.7.12 has no hashing option), 7-day sessions; OTPs and identifiers hashed; Google tokens encrypted; backups encrypted (AWS deployment) | accepted (§17); vitest asserts the hashed and encrypted columns |
| 29 | Confused deputy in the auth service | no internal route; loopback listener; only the allowlist is proxied; hooks take ids from the session, never the body | proxy allowlist test (`TestAuthProxy_ForwardsTheAllowlist`); settings Vitest (a public `GOAL_AUTH_LISTEN` refused, so the service does not start); Vitest: hooks stamp the owner from the session, whatever the body says |
| 30 | Stolen or unattended session makes a sensitive change | `fresh: true` in goald and the before-hook on member paths and device approval; those paths take the browser's cookie only, and a goal login session never approves a device code, so it cannot renew its own freshness (review M1) | matrix "stale session" column; vitest fresh-session refusals and "takes the browser's cookie only" (`organization.integration.test.ts`) |
| 31 | Permissions drift between goald and Better Auth | one `api/permissions.yaml` generates both; `x-goal-scope` on every operation | `TestPermissions_EveryOperationHasAKnownScope`; permgen drift check |
| 32 | Better Auth schema drift on upgrade | `auth generate` diff against goose head; `auth migrate` never run; `validateSchema` on | `task auth:schema-check` |
| 33 | Auth secrets or user credentials reach an agent | each process gets only its own variables; goald's `env-reserved` list holds every auth variable, `GOAL_API_KEY` and `GOAL_DATABASE_ADMIN_URL`; sessions, keys and personal tokens never in task env | `TestSettings_ReservedIncludesAuthVariables`; `goal up` environment test |
| 34 | DNS rebinding against a loopback goald | `Host` check (§5.5) | API and SPA tests with a foreign `Host` |
| 35 | CLI token leak | OS keychain (0600 file only by opt-in); keyed by origin, sent only there, https unless loopback; `GOAL_API_KEY` never written; `logout` revokes | CLI tests |
| 36 | Local processes under `GOAL_AUTH=none` act as the local user (agents cannot: row 51) | `none` only on loopback, never the default from 3-16, startup warning | accepted for one developer's laptop; `TestSettings_AuthDefaults` (session by default), `TestSettings_RefusesUnsafeConfiguration` (none off loopback), `TestAuthentication_NoneWarnsThatEveryRequestIsTheLocalUser` |
| 37 | Owner locked out | bootstrap by configured email; signed-in sessions survive an auth outage; `goal db recover-admin` and `recover-owner`; `GOAL_AUTH=none` on loopback | `TestDB_RecoverAdmin`, `TestDB_RecoverOwner`; `task e2e:auth` (`TestE2E_SignInWithAnEmailedCode`, `web/e2e-auth`) bootstrap; goald resolves sessions in PostgreSQL only (API tests run without an auth service); upgrade rehearsal (§14, run for 3-16 on a copy of a 2.2 database) |
| 38 | A removed member's personal token keeps working, or comes back on re-invitation | FK to `auth.member` `ON DELETE CASCADE`; the lookup inner-joins the member | `identitytest`: remove → 401; re-invite → still 401, no row |
| 39 | A demoted owner's token keeps the old rights | rights = scopes ∩ current roles, resolved per request | matrix column "token, owner demoted to member"; `identitytest` role change on the next call |
| 40 | A token carries a permission it should never hold | explicit `pat` flag; permgen refuses it on fresh, platform and install permissions; the token check runs first and fails closed | `TestPermissions_PersonalTokenGrants` and `TestPermissions_PersonalTokensCarryExactlyThese` (the `pat` set); permgen refusal test (`TestLoad_RefusesATableThatDoesNotHoldTogether`); matrix |
| 41 | A token passes a fresh-only operation | no `fresh` permission is `pat: true`; 403 `forbidden`, never `reauth-required` | matrix (every `fresh` operation with a token) |
| 42 | A token approves a device code | `device:read` is not `pat: true`; Better Auth never sees the token (row 45) | matrix; proxy test |
| 43 | A platform admin's token creates organizations or manages admins | `PlatformAdmin` never set for a token principal | matrix column "token of a platform admin" |
| 44 | A token is used against another organization | the token names its org; any other path org is the unknown org's 404 | matrix column "token of the other org" |
| 45 | A Goal key or token is forwarded to Better Auth | the proxy answers 401 to `Bearer goal_sk_…`/`goal_pat_…` and never forwards it | proxy table test |
| 46 | A token reaches the agents' gateway | the gateway accepts task tokens only | gateway test with a `goal_pat_` bearer |
| 47 | A phished device approval mints a lasting token | creation needs a fresh session signed in through the browser (`signInMethod`, `00048`): a device-flow session is 403 `browser-session-required` as a bearer and as a cookie with any signature; keys and tokens never hold `personal_token:manage` (403 `forbidden`) | `TestAPI_PersonalTokens_Refusals` (the device-flow session 403 `browser-session-required`, as a bearer and as a forged cookie; a key and a token 403 `forbidden`); `identitytest` `OnlyABrowserSignInIsABrowserSession` |
| 48 | The secret is cached or seen twice | shown once; `Cache-Control: no-store`; only the hash stored; never logged | API test (header, body once, list has the hint only); log-capture test |
| 49 | A link tricks someone into creating a token for an attacker | the create dialog takes nothing from the URL; the warning copy; secrets never asked for by Goal | Vitest (the dialog reads no URL); `web/e2e/personal-tokens.spec.ts` (a link's query string fills nothing, warning shown) |
| 50 | A leaked high-impact token publishes a definition | high-impact scopes never in a preset, warned in the dialog; default 30 days; `via: pat:<id>` in audit and item events; owner revokes without re-auth | API test (audit `via`); Vitest (preset excludes them) |
| 51 | A prompt-injected agent, sidecar or check reaches the host's loopback services (goald's `/v1` and `/api/auth` with a forged `Host`, the auth service, Mailpit's codes, PostgreSQL), a private network or the metadata service, or sends its tokens anywhere (review of phase 3, H1) | [ADR 0009](../adr/0009-agent-network-isolation.md): agents on an internal network and sidecars and checks on another, neither with a route to the host, no `host.docker.internal`; the egress proxy is the only way out: agents reach the model API (`GOAL_AGENT_EGRESS`) and the gateway at exactly its path, tools reach public hosts; every destination resolved by the proxy and refused when any address is not public (rebinding included); Claude Code's nonessential traffic off | `internal/egress` (`TestProxy_RefusesNonPublicDestinations`, `TestProxy_AgentsReachOnlyTheirHosts`, `TestProxy_ServesTheGatewayToAgentsOnly`, `TestPublic`); Docker integration (`TestEgress_AgentsReachOnlyTheGatewayAndTheirHosts`: host loopback, the host's and bridges' addresses and 169.254.169.254 unreachable directly and through the proxy, the gateway and an allowlisted stub reachable; `TestEgress_ToolsReachNeitherTheHostNorTheGateway`; `TestRunner_RealWorkerGoesOutOnlyThroughTheProxy`: the real worker connects to a stub gateway through the proxy and asks for nothing but the model API) |
| 52 | Something on the machine (an agent past row 51, another local process) uses the database with the passwords this repository publishes | under `GOAL_AUTH=session` `goal up` refuses, before it starts anything, any role password (set or by default, in a `GOAL_PG_*_PASSWORD` or a database URL) that is one of the dev-only values, naming each variable and how to change it | `TestUp_SignInRefusesPublishedDatabasePasswords` |
| 53 | Something on the machine reads sign-in codes from Mailpit's API and signs in as the owner | Mailpit's UI and API need basic auth: user `goal` and a random password `goal up` (or `task test-integration`/`e2e:auth`) writes once into `~/.config/goal/mailpit-ui-auth` (mode 0600), which compose mounts; without the file Mailpit does not start; tests use the running Mailpit and never recreate it (`--no-recreate`) | `TestUp_MailpitAuthIsGeneratedOnceAndPrivate`; `task e2e:auth` (Go and Playwright read codes with the credentials); the SMTP adapter's integration test |
| 54 | Something that reaches the auth service's loopback port directly skips goald's allowlist, `Origin` check and rate-limit address (forged or absent `X-Goal-Client-Ip`) (review of phase 3, L4) | goald's proxy sends `GOAL_AUTH_PROXY_SECRET` as `X-Goal-Proxy-Secret` with every forwarded request, replacing a client's; the auth service refuses every request without it (403, compared in constant time) but its health check, and refuses to start without one; `goal up` gives both processes the same, generated when `.env` has none | `TestAuthProxy_SendsTheSharedSecret`, `TestAuthProxy_LogsNoSecrets`; Vitest: the service refuses requests without the secret or with a wrong one, settings refuse a missing, short or reused secret; `TestUp_GoaldAndTheAuthServiceShareAProxySecret`; `task e2e:auth` signs in through the proxy |

### 11.2 Secrets (rule 7)

| Holder | Secrets |
| --- | --- |
| auth service | `BETTER_AUTH_SECRETS` (or `BETTER_AUTH_SECRET`), `GOOGLE_CLIENT_SECRET`, `GOAL_PG_AUTH_PASSWORD` (in `GOAL_AUTH_DATABASE_URL`), SMTP credentials (in `GOAL_SMTP_URL`) |
| goald | none of the above; its own `GOAL_GATEWAY_KEY` (≥ 32 bytes, distinct) and database password |
| both | `GOAL_AUTH_PROXY_SECRET` (≥ 32 bytes), the proof that a request to the auth service came through goald's proxy |

`.env` locally; ECS `secrets` from Secrets Manager in the cloud; separate values per environment. User sessions and API keys never reach agent environments or prompts.

### 11.3 Supply chain

`better-auth`, `@better-auth/*` and `nodemailer` pinned exactly; no install scripts allowed; `pnpm audit --prod` for `auth/` and (from 3-16) `web/` in `task check`, exceptions and overrides recorded in `SECURITY.md`. Patch SLA for Better Auth advisories: critical within 48 hours, high within 7 days, others with the next release (`auth/README.md`).

### 11.4 Task tokens

`domain.TaskClaims` gains `Audience string \`json:"aud"\`` (`domain.TaskAudience`); `SignTaskToken` sets `goal-gateway` whatever the claims say, `VerifyTaskToken` requires it exactly (tokens issued before stop working; runs are released on restart). The gateway accepts only task tokens; `/v1` never does (§8.3) and `/api/auth` has no use for them. `GOAL_GATEWAY_KEY` stays ≥ 32 bytes and is never a Better Auth secret. Off loopback the gateway has its own listener serving only `/mcp` (§5.5), which on AWS only the runners' security group reaches.

### 11.5 Tenancy

Unchanged from A, plus: Better Auth's tables have no RLS (Better Auth cannot set `goal.org_id`); isolation there is Better Auth's checks, the proxy allowlist, Goal's hooks, and definers that act only on `goal_org()`. Every new org-owned Goal table uses A's tenancy kit.

### 11.6 Personal data

Names and emails live in `auth."user"`; emails appear in invitations, `audit_log` invitation entries and Members (to the org's members). Session IP and user agent live with the session and go a day after it expires; `auth_events` keeps hashed subjects, IPs and user agents for 90 days. Erasure (deferred): delete the user's sessions, accounts and memberships, replace `audit_log.actor_name` with "Deleted user" and keep ids.

## 12. Observability

- **goald logs:** per request `principal` (`user:<id>`, `service:<key id>` or `none`), `credential=pat:<id>` with a personal token, `org_id`, `operation`, `status`, `request_id`; never emails, tokens or cookies. Startup logs the auth mode, the public URL, the self-check result and the upstream reachability (a warning, not fatal). As built (3-16): one `api request` line per `/v1` request (`internal/api/accesslog.go`) with `method`, `operation` (the operationId, never the path, which may name an invitation), `status`, `request_id`, `principal` (`user:local` under `none`), `credential` (`session`, `key:<id>` or `pat:<id>`; absent without one), `org_id` and `duration_ms`; the durable record stays `via: pat:<id>` in the audit log and item events. A refused API key or personal token also logs `credential refused` with `metric=goal_api_key_verifications_total kind=key|pat result=refused|error`, never the secret.
- **auth service logs** (JSON, stdout): event, user id, hashed subject, IP, request id (from goald), outcome; Better Auth's logger at `warn`.
- **Metrics** (logs until OTel lands): `goal_auth_requests_total{result=ok|unauthenticated|forbidden|not_found|reauth}`, `goal_auth_proxy_requests_total{route,status}`, `goal_auth_otp_total{result=sent|suppressed|failed|locked}`, `goal_api_key_verifications_total{kind=key|pat,result}` (failed lookups are counted, not written to `auth_events`), `goal_streams_ended_total{reason=access|expiry|resync}`.
- **Security log:** `auth_events`, read with `goal admin events` or `GET /v1/platform/auth-events`; org-level changes in `audit_log`.
- **Jobs:** `goal.auth.cleanup` daily under `pg_try_advisory_lock`, logging its counts.

## 13. Testing

| What | How |
| --- | --- |
| Permission source | `TestPermissions_EveryOperationHasAKnownScope` (openapi ↔ yaml); permgen output committed and checked for drift; `TestPermissions_RoleGrants`: table-driven product rules ("admin cannot delete the org", "viewer cannot decide", "approver cannot publish", "member without a role cannot see the factory", "no key holds member:manage", "the `pat` set is exactly §4.5's"); permgen refuses `pat` on a fresh, platform or install permission other than `authenticated` |
| Two-org matrix | `TestAuthorize_TwoOrgMatrix` extends A's seam test: every operationId × principals {anonymous, owner, admin, member with viewer/approver/editor on one factory, member without roles, member of the other org only, platform admin who is not a member, stale session, key of acme (scoped, restricted), key of the other org, expired key, revoked key; from 3-12b: device-flow (bearer) session, personal token of an approver (scopes `factory:read`, `item:create`), personal token of an owner with every `pat` scope, token whose owner was demoted to member, token of a removed member, token of a platform admin, token of the other org, expired token, revoked token} × both orgs. Fixed properties: anonymous 401 (but `public`), other org 404 with the unknown org's body, an invisible factory or item with the unknown one's body, no handler side effect on refusal; for tokens: never more than the same user's session would get, 403 `forbidden` (never `reauth-required`) on every `fresh` or non-`pat` operation, nothing on platform routes, and the bearer session and every token refused on `createPersonalToken`. The member-without-roles column catches a handler that skips the resolvers. 3-2 runs it against `Authorize` directly; 3-9 runs it over HTTP with the fake and the PostgreSQL `Identity`. Every status goes to the golden file `internal/api/testdata/access.golden` so a permission change is a reviewed diff. |
| Authentication | credential parsing table (cookie names by scheme, signed value, bearer kinds including `goal_pat_` and its malformed lengths, task token refused, both present); `GOAL_AUTH`, `GOAL_PUBLIC_URL` and listener combinations refuse or start; invalid credential never becomes `LocalUser`; `Host` check; `TestSettings_ReservedIncludesAuthVariables`; `goal up`'s environment split and refusals; goald refuses to migrate without `goal_auth` |
| Identity adapter | `identitytest` on the fake and PostgreSQL: session lookup (expired, unknown, deleted org, non-member), keys (revoked, expired, other org, `last_used_at` throttle), personal tokens (revoked, expired, deleted org, removed member and re-invited, demotion on the next call, `last_used_at` throttle, `personal_token.refused` once a minute, owner filter on list and revoke), removal and role changes visible on the next call, ≥1 owner under two concurrent demotions, last platform admin, invitation roles applied once, transfer rules, audit entries and actors |
| Schema guards | `TestSchema_AuthSchemaIsolation`: `goal_app` has no `USAGE` on `auth`; `goal_auth` has exactly DML on `auth.*`, nothing on `public` tables, `EXECUTE` only on the "auth" functions; every definer pins `search_path` and revokes `PUBLIC`; the RLS guard includes the new tables |
| Proxy | `httptest` upstream: allowlist both ways, path hygiene table, header stripping and `X-Goal-Client-Ip` with and without trusted proxies, 64 KiB, timeouts, 503, `Set-Auth-Token` removed, `Bearer goal_sk_…`/`goal_pat_…` answered 401 and never forwarded, cross-origin refusal, logs without secrets, an unknown `/api/…` path 404 (never `index.html`), not mounted under `none` |
| Auth service (Vitest, PostgreSQL 18 from compose, fake clock via `vi.useFakeTimers` and an injected clock for Goal's queries) | codes: length, expiry at 300 s, 3 tries, hashed at rest; uniform send response and timing; caps and lockout with the notice; the sign-up gate for codes and Google (Google's token endpoint stubbed on `fetch`, as built in 3-8); account linking; invitation email match and verification; owner rules; fresh-session refusals; the stamp hook's actor reaching `audit_log`; `/update-user` field allowlist; startup refusals |
| Session format contract | Vitest pins the cookie name, attributes (HttpOnly, SameSite=Lax, Path=/, Secure on https), the value `<32 alnum>.<signature>`, `auth.session.token` = the part before the dot, and the device token = the raw 32 characters. A Go integration test resolves a session row of that shape. The Playwright flow below runs both together. |
| Stream | per-subscriber filter; ends on access notices for its session, user, key, personal token and org, at expiry, and on resync |
| CLI | stub API: login polling states, keychain fake, `--store file` mode 0600, origin scoping, http refusal off loopback, `GOAL_API_KEY` precedence and both prefixes, `login --token` from stdin (argv refused, `/v1/me` checked, stored by origin), `whoami` with a token, exit 77; `recover-admin` and `recover-owner` against PostgreSQL (existing and new user, audit, sign-in afterwards) |
| Web | Vitest: `next` validation, 401 and `reauth-required` handling, `can`-driven rendering. Playwright on the mock at 375/768/1440 with axe: sign in by code, re-auth then retry, accept an invitation, approve a device code, create and revoke a key, create a personal token (no prefill from the URL, warning shown) and revoke it, change a factory role, viewer read-only Definition. One Playwright flow against real goald, auth and Mailpit (`task web:e2e-auth`, tagged): bootstrap sign-in, code from Mailpit's API, the API answers, sign-out ends access. |

No tests for generated code, the Better Auth library itself or wiring.

## 14. Rollout and migration

The default lives in goald (`cmd/goald/settings.go`): a goald started by `goal up`, `task goald` or by hand behaves the same.

1. **3-1 to 3-15 keep `GOAL_AUTH=none` as the default**, so a running install changes nothing it does. From 3-4, goald's startup migrations need the role `goal_auth`: `goal up` creates it (`goal db init`); a goald started otherwise refuses with "run goal db init" and applies nothing. `00040–00047` add the schema, backfill `auth.organization` for every organization and add no members. `goal up` starts neither Mailpit nor the auth service under `none`. Trying sign-in early means setting `GOAL_AUTH=session` explicitly.
2. **3-16 flips the default to `session`**, once web sign-in (3-10), API keys for CI (3-12), personal tokens (3-12b), `goal login` (3-13) and recovery (3-15) exist. Upgrade: `pg_dump`; add `BETTER_AUTH_SECRET` (`openssl rand -hex 32`) and `GOAL_BOOTSTRAP_OWNER_EMAIL` to `.env` (`.env.example` lists every new variable); `goal up`, which refuses with the variable's name, and the `GOAL_AUTH=none` way back, when either is missing. As built (3-16): `defaultAuth` (goald) and `defaultAuthMode` (`goal up`) are `session`; with neither variable set `goal up` refuses before it starts anything ("GOAL_AUTH=session (the default) needs BETTER_AUTH_SECRET in .env (openssl rand -hex 32, not GOAL_GATEWAY_KEY) and, until the first owner signs in, GOAL_BOOTSTRAP_OWNER_EMAIL (that owner's address); or set GOAL_AUTH=none to keep the single local user …"), and without the bootstrap address while nobody has signed in it refuses before the roles, the bucket, the auth service and goald; `task up` builds the auth service too.
3. **The bootstrap owner signs in** at `GOAL_PUBLIC_URL` with a code from Mailpit (`http://127.0.0.1:8025`) or Google. That user becomes a platform admin and owner of every existing organization, `default` included, then runs `goal login` for the CLI.
4. **Earlier rows stay as they are.** `user:local` in `*_by` columns, events and `audit_log` is shown as "Local user (before sign-in)"; nothing is rewritten.
5. **The owner invites people** from Members; invitees sign in and join.

**Nobody is locked out.** Before the first sign-in, a mistyped bootstrap email is fixed in `.env` and a restart (the gate reads it while no user exists). An auth service outage stops signing in, not signed-in work. A lost mailbox or a removed last admin is fixed with `goal db recover-admin <email>` or `recover-owner <org> <email>`, which need only database access and create the user if needed. On a laptop, `GOAL_AUTH=none` restores the single local user at any time without losing data.

Forward-only migrations; back up first (`pg_dump`). Rolling back the binary after `00040` is not supported; turning sign-in off (`GOAL_AUTH=none`, loopback) is. 3-16 is verified by this sequence on a copy of the user's 2.2 database.

## 15. Work breakdown

Each row is one PR on `goal/3-<n>-<slug>`, passing `task check`, plus `task migrate-check` and `task test-integration` when it touches SQL, adapters or `auth/`. Skills (AGENTS.md): the Go skills on every Go PR; Identity skills (better-auth-best-practices, better-auth-security-best-practices, organization-best-practices, create-auth as reference, security-and-hardening, golang-security) on 3-2 to 3-16 and 3-12b; PostgreSQL skills on PRs with migrations; `golang-cli` on CLI PRs; UI skills on web PRs; `openapi-spec-generation` and `api-design-principles` on PRs that change `api/openapi.yaml`; monorepo skills on 3-1 and 3-4.

| # | Pull request | Acceptance criteria | Verified by |
| --- | --- | --- | --- |
| 3-1 | **Permission source:** `api/permissions.yaml` (§8.2), `x-goal-scope` on every current operation (§8.2 table), `cmd/permgen` run by `task api:gen` → `internal/api/permissions_gen.go` (`permissionRules map[string]rule{level, orgRoles, factoryRole, keys, fresh, platformAdmin}` and `operationScopes map[string]string`, both sorted), drift check in `task check`; the TypeScript output arrives with `auth/` in 3-4 | every operation has a known scope and every permission is used; changing the yaml or an `x-goal-scope` without regenerating fails `task check`; no runtime change (`AllowAll` stays wired) | `TestPermissions_EveryOperationHasAKnownScope`, drift check |
| 3-2 | **Principal and the real Authorizer:** `domain.Principal`, roles, `ActorService`, the two errors; `ports.Identity` and `porttest.Identity`; `AccessRequest.Principal`; `api.Permissions` with the fresh rule; `credential.go` (parsing only, `none` still the only mode); visibility helpers; the fixed 404 bodies | §8.4 rules hold for fabricated principals; matrix and golden file exist; `AllowAll` used only under `none` | `TestPermissions_RoleGrants`, `TestAuthorize_TwoOrgMatrix` (against `Authorize`) |
| 3-3 | **Task token audience and the gateway listener:** `aud`, `GOAL_GATEWAY_LISTEN`, `GOAL_GATEWAY_URL` following it | tokens without `aud: goal-gateway` refused at `/mcp`; `/v1` refuses task tokens; §5.5's listener rule (still loopback-only `GOAL_LISTEN` until 3-9) | `domain`, gateway and settings tests |
| 3-4 | **auth service and schema:** `auth/` (Node 22.12+ through mise, Hono with `@hono/node-server`, pinned Better Auth, settings, role check, health, `auth/Dockerfile` for the sidecar, `access.gen.ts` from permgen), pnpm workspace (no turbo: the repo has none, `Taskfile.yml` orchestrates), `task auth:build` (from 3-9 `goal up` runs `node auth/dist/server.js` and says how to build it when missing), `goal_auth` in `roles.sql` and `goal db init`, goald's role check before migrating, `00040`, generated `00041` (with §6.2's additional fields), `task auth:schema-check`, `task auth:dev`, session format contract | the service starts as `goal_auth` and refuses bad settings; drift check empty at head and fails on a config change; ids are uuid; contract test green; goald without `goal_auth` refuses and applies nothing; `goal up` unchanged under `none` | Vitest, migrate-check, settings test |
| 3-5 | **Org lifecycle (00042):** constraints, owner trigger, `goal_provision_org` used by `Tenancy.CreateOrg` (no owner yet), rename and retire triggers, transfer function, purge, backfill | create writes both rows or none; rename syncs; soft delete cancels invitations; purge removes auth rows; two concurrent demotions cannot leave zero owners | `tenancytest`, integration |
| 3-6 | **Identity tables and auth triggers (00043):** `platform_admins`, `factory_roles`, `auth_events`, `actor_kind` widened, audit and `goal_access` triggers | each audited path writes one entry with the stamped actor; notices sent; RLS guard and isolation test green | integration, `TestSchema_AuthSchemaIsolation` |
| 3-7 | **Proxy, Host check and SPA headers:** `authproxy.go` (mounted only under `session`, so inert until 3-9), `GOAL_AUTH_UPSTREAM`, `GOAL_TRUSTED_PROXIES`, the `Host` check, `api/` reserved, CSP and related headers, `ownVariables` gains §5.6's variables | every rule of §5.5 holds | proxy tests, `web` handler tests, `TestSettings_ReservedIncludesAuthVariables` |
| 3-8 | **Sign-in (00044):** email OTP, Google, `Mailer` with SMTP and Mailpit in compose (127.0.0.1), the sign-up gate, send caps and lockout, bootstrap claim, rate limits, before/after hooks, `auth_events` | every §8.7 rule; §11.1 rows 9–15 and 29 tested | Vitest |
| 3-9 | **goald authentication (00045):** `Identity` in postgres, principal per request, `GOAL_AUTH=session` available (default still `none`) with §8.3's startup rules and non-loopback listen, `NewOrg.Owner` from the principal, `/v1/auth/config`, `/v1/me`, `listOrgs` by membership, `role`/`can` fields, filtered lists, stream rules, "Local user (before sign-in)"; `goal up` under `session` (auth service, Mailpit, environment split, refusals) | matrix green over HTTP with the real adapter; removal effective at once; streams end on access changes; self-check refusals | API tests, `identitytest`, stream tests, `goal up` tests |
| 3-10 | **Web sign-in:** Sign-In, Sign-In-Code, Account-Menu, re-auth dialog, 401 handling, session keep-alive, mock auth | Playwright sign-in flows on the mock; the real e2e flow passes under `GOAL_AUTH=session` | Playwright, `task web:e2e-auth` |
| 3-11 | **Members:** `Access` (postgres, fake, `identitytest` cases), `listMembers`, `listInvitations`, factory role endpoints, `transferOwnership`; invitation with factory roles; Members, Invite-Accept, Org-Switcher roles, Transfer ownership | §10.2 states; owner rules; factory roles applied on accept; transfer atomic | API tests, Vitest, Playwright |
| 3-12 | **API keys (00046):** tables, `goal_api_key`, create/list/revoke, retire revokes keys, Api-Keys screen, `GOAL_API_KEY` in the CLI, `goal key …` | §4.3 rules; secret once; revocation immediate; deleted org's keys fail | `identitytest`, API tests, Playwright |
| 3-12b | **Personal tokens (00047):** `personal_tokens`, `goal_personal_token`, retire and `goal_access` triggers; the `pat` flag in `api/permissions.yaml` and permgen (refusing it on fresh, platform and install permissions), `personal_token:read`/`manage`; `Principal.Token`, `Session.Bearer` (`Session.Browser` since review M1), `Identity.PersonalToken`, the `Access` token methods; the `goal_pat_` branch of `credential.go`; the token check first in `Authorize`; the five endpoints of §8.9 and `Me.token`; `via: pat:<id>` in audit and item events; the proxy's 401 on Goal bearers; streams ending on token notices; the Personal tokens and Members' tokens sections and the Account-Menu link; `GOAL_API_KEY` with both prefixes, `goal login --token` (stdin), `whoami` | §4.5 rules: rights never exceed scopes ∩ current roles; removal, demotion, revocation, expiry and org deletion apply on the next request and end streams; created only with a fresh cookie session (403 `browser-session-required` otherwise), secret once with `no-store`; owner revokes without re-auth, admins list and revoke members' tokens; never fresh, never platform admin, 404 in another org, refused at `/api/auth` and the gateway; §11.1 rows 38–50 each pass their test | `identitytest`, matrix and golden file with the token columns, permgen and `TestPermissions_RoleGrants`, proxy, gateway, stream and CLI tests, Vitest, Playwright. Skills: the Go, PostgreSQL, Identity, UI and `golang-cli` skills; `openapi-spec-generation`, `api-design-principles` |
| 3-13 | **CLI login:** `goal login/logout/whoami`, device flow, keychain and file store, Device-Approve page, `getDeviceRequest` | §9 rules; approve needs the typed code and a fresh session | CLI tests, Vitest, Playwright |
| 3-14 | **Role-aware screens:** D's Who approves and read-only mode, E's controls and andon, inbox and attention by permission | screens follow `can`; the server refuses anyway | Vitest, Playwright |
| 3-15 | **Platform admins and recovery:** platform endpoints, `goal admin …`, `goal db recover-admin/recover-owner`, Create organization gated, `Platform.CleanUpAuth` and the cleanup job | last admin protected; recovery works with goald stopped, creates a missing user, and is audited | API tests, CLI recovery tests (§13) |
| 3-16 | **Sign-in by default, docs and hardening:** `GOAL_AUTH` defaults to `session`; `.env.example`; architecture.md identity section as built; threat model rows checked against tests; `pnpm audit`; patch SLA in `auth/README.md` | §14's upgrade on a copy of a 2.2 database ends with the owner signed in, every org owned and `user:local` rows intact; every §11.1 row names a passing test | upgrade rehearsal, `task web:e2e-auth`, review |

Migration numbers rise with PR order (3-4 → 00040–00041, 3-5 → 00042, 3-6 → 00043, 3-8 → 00044, 3-9 → 00045, 3-12 → 00046, 3-12b → 00047), and no PR needs a later one. 3-1, 3-2, 3-3 and 3-7 have no migration and can start now (3-7 tests against an `httptest` upstream). A PR that lands out of this order renumbers its migration before merging.

## 16. Cross-spec contracts

**Phase 3 provides**
- `domain.Principal`, `PrincipalFrom`, roles, `ActorService`; `ports.Identity`, `Access`, `Platform`; `api.Permissions` and `api/permissions.yaml` (phase 4 adds its permissions there, phase 5 reuses the vocabulary as OAuth scopes).
- The fresh-session rule (`fresh: true`, 403 `reauth-required`) for phase 4's credential and approval operations.
- Factory role `approver` (`item:decide`) as "who approves"; phase 4 adds `approval:decide` with `factory_role: approver`.
- `GOAL_PUBLIC_URL` as the one public origin (`BETTER_AUTH_URL` equals it; phase 4's OAuth callback and client metadata use it).
- Platform admins as the platform operator (phase 4's `goal catalog push` and any platform role).
- The signed-in user as `requested_by` for phase 4's OAuth flows; the principal in `ctx` for every handler.
- `goal_access` notices and stream termination; `auth_events` for install-level security events.
- Personal tokens (3-12b): a person's credential for one organization, `/v1` only; phase 5 decides whether `/v1/mcp` accepts them (spec 5, open question 6).
- Reserved for phase 5: the public path `/v1/mcp`, `/.well-known/oauth-authorization-server`, `/.well-known/oauth-protected-resource` (added to the proxy allowlist then).

**Phase 3 needs**
- **A:** the seams of §5.3, `Tenancy`, `appendAudit`, the tenancy kit, `goal_purge_org`, `CheckAppRole`, `roles.sql`/`goal db init`.
- **B–E:** every operation behind the resolvers (already a 2.2 rule); the inbox `InboxSource` reading the principal from `ctx`; D's and E's screens reading `can`.
- **Phase 4** (after this): redefine `claude-subscription` for multi-user installs; put its permissions in `api/permissions.yaml`.

## 17. Open questions

None. Settled by the owner (2026-10-24):

1. **Session tokens at rest: accepted** as Better Auth stores them (plain; 1.7.12 has no hashing option, checked in its internal adapter), with 7-day sessions and encrypted backups. Revisit if a Better Auth release adds hashed storage.
2. **Editor includes approver: kept.** One role per factory; no separation of duties in phase 3.
3. **Andon: kept.** Approvers and editors pull and clear it; viewers do not.
4. **Vercel proxy ranges and SSE through rewrites: deferred** to when the hosted deployment is scheduled (§5.5).
5. **Personal tokens: in phase 3 as PR 3-12b** (owner's ask, settled with the architecture and security reviews): own table and `goal_pat_` prefix, explicit `pat` flag, the token check first and fail closed, browser-only creation, `/v1` only (§4.5). Later, not blocking: an owner switch to turn personal tokens off for an organization; email on creation and before expiry (needs goald or the auth service to mail); `last_used_ip`; a checksum suffix for secret scanners; "Sign out everywhere" offering to revoke tokens; refusing `goal_(sk|pat)_` strings in item text and definition files. Accepted for phase 3: no rate limiting of goald's API (§2).
