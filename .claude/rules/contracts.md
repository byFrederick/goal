---
paths:
  - "contracts/api/**"
  - "internal/api/**"
  - "packages/api-client/**"
---

# API contract

The HTTP API's layers and the permission model: [architecture.md](../../docs/engineering/architecture.md) ("HTTP API") and [ADR 0003](../../docs/adr/0003-hexagonal-architecture-and-deployment.md).

- Treat `contracts/api/openapi.yaml` and `contracts/api/events.schema.json` as the only contract between goald and TypeScript, so both sides change together.
- Change the contract first, then run `task api:gen`, and only then write the handler or client code, because the generated types are what both sides compile against.
- Never edit generated code by hand (`internal/api/gen`, `internal/api/permissions_gen.go`, `packages/api-client/src/schema.ts`), because the next `task api:gen` overwrites it.
- Declare a new permission in `contracts/api/permissions.yaml` and give every operation its `x-goal-scope`, then run `task api:permissions-check`, so goald's table and the auth service's roles agree.
- Answer every error as an RFC 9457 problem (`application/problem+json`) through `s.problem`, so clients branch on a stable `type` code, never on a message.
- Return no secret, credential or token value from any endpoint, because connections are write-only.
- Keep adapter imports out of `internal/api`; reach them through the ports and the interfaces in `internal/api`, so handlers test against port fakes.
- Test handlers with `httptest` against the real engine and port fakes, checking status codes and problem types.

## Skills

Load `openapi-spec-generation` and `api-design-principles`; `golang-security` for request handling.
