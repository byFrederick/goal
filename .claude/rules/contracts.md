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
- Name every generated file with `.gen.` before its extension (`internal/api/gen/api.gen.go`, `internal/api/permissions.gen.go`, `packages/api-client/src/schema.gen.ts`), so one glob finds them ([ADR 0013](../../docs/adr/0013-repository-layout.md), decision 4). The full list is the [change map](../../docs/engineering/change-map.md)'s "Generated files" table.
- Never edit generated code by hand, because the next `task api:gen` overwrites it and `task gen:check` fails on the difference.
- Declare a new permission in `contracts/api/permissions.yaml` and give every operation its `x-goal-scope`, then run `task api:gen` and `task gen:check`, so goald's table and the auth service's roles agree.
- Answer every error as an RFC 9457 problem (`application/problem+json`) through `s.problem`, so clients branch on a stable `type` code, never on a message.
- Return no secret, credential or token value from any endpoint, because connections are write-only.
- Keep adapter imports out of `internal/api`; reach them through the ports and the interfaces in `internal/api`, so handlers test against port fakes.
- Test handlers with `httptest` against the real engine and port fakes, checking status codes and problem types.

## Skills

Load `openapi-spec-generation` and `api-design-principles`; `golang-security` for request handling.
