---
paths:
  - "auth/**"
---

# Auth

The identity service runs sign-in, sessions, members and invitations. Its boundary and why: [ADR 0007](../../docs/adr/0007-identity-better-auth.md). The phase's behavior: [spec 3](../../docs/specs/3-identity-and-access.md).

## Boundary

- Reach the service only through goald's `/api/auth` proxy. ADR 0007 makes that the one client contract, so browsers never talk to the service's origin.
- Keep goald from holding the service's secrets. The service reads its own from its environment, so a goald compromise does not give away sign-in keys.
- Never pass a user's session or an API key to an agent. A run is code acting for a factory, not for a person.
- Check every permission on the server. The UI's `can` only decides what to show, and goald checks again on each request.

## Building it

- Use Better Auth's plugins for email codes, Google, organizations and the device flow. Re-implementing them means owning their security fixes.
- Pin `better-auth` through the workspace catalog. Auth and web must run the same version of the client and the server.
- Write the service's tables as goose migrations in `db/migrations/`. Better Auth's generated migrations would bypass the one migration history, so check them with `task migrate-check`.
- Test the handlers with Vitest. CI runs `task migrate-check` whenever `auth/` changes.

## Skills

Vendored skills for this area: better-auth-best-practices, better-auth-security-best-practices, organization-best-practices, create-auth (a scaffolding reference only), security-and-hardening.
