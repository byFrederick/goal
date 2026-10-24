# Phase 5 — Goal MCP server

Status: stub, to be specified after phase 4 · migrations `00060` up · decision record [ADR 0007](../adr/0007-identity-better-auth.md)

## Scope

Goal itself becomes an MCP server for outside MCP clients (for example Claude): people use tools such as "submit an item", "what is waiting on me", "approve", "show the board" from their own assistant, in one organization, with their own rights. API keys keep serving machines; OAuth serves people's MCP clients.

Out: agents' access to outside tools (that is phase 4's gateway at `/mcp`), client credentials grants (API keys cover machine access), multi-organization tokens.

## Decided authorization design

- **Authorization server: Better Auth** in the existing `auth/` service, adding `@better-auth/oauth-provider`, `@better-auth/mcp`, `@better-auth/cimd` and `jwt`. Client ID metadata documents preferred over dynamic client registration; S256 PKCE only; exact redirect URIs; `refreshTokenReuseInterval: 0`; no `client_credentials`.
- **Resource server: goald's MCP endpoint at `/v1/mcp`** (reserved by phase 3; the agents' gateway keeps `/mcp`, on an internal listener in the cloud). Access tokens must carry the exact canonical resource URI as `aud`; verified against Better Auth's JWKS with EdDSA only, refetch on an unknown `kid` at most once a minute, cache at most 10 minutes. RFC 9728 protected-resource metadata at `/.well-known/oauth-protected-resource`; the proxy allowlist gains the authorization server's metadata and OAuth endpoints.
- **One organization per token**, chosen at consent. Effective rights = the token's scopes ∩ the member's current role, resolved per request like a session (phase 3's `goal_principal`).
- **No token passthrough.** MCP tools call goald's domain services in-process with the principal; no Better Auth or Goal token reaches an upstream or an agent.
- Task tokens, sessions and API keys are never accepted at `/v1/mcp`; MCP access tokens are accepted nowhere else.

## What phase 3 already provides

- The scope vocabulary in `api/permissions.yaml` (`factory:read`, `item:create`, `item:decide`, …), shared by API keys now and OAuth scopes then.
- `domain.Principal` (phase 5 adds the kind `client` with `via` the OAuth client) and the `Authorizer` over the permission table.
- `GOAL_PUBLIC_URL` as the one public origin and issuer base; the `/api/auth/*` proxy and its hardening.
- The fresh-session rule for consent screens; `auth_events` for consent and token events.

## Open questions

1. Which tools the MCP server exposes first, and whether they map one-to-one to OpenAPI operations (and their `x-goal-scope`).
2. Consent screen and token lifetimes (access and refresh), and how a person lists and revokes connected clients.
3. Client metadata document fetching: egress allowlist and SSRF controls.
4. Whether the CLI moves from the device-authorization session to OAuth device grant tokens.
5. Audit: actor kind `client` with `via`, and how tool calls appear in item events.
