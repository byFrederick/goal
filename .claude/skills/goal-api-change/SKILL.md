---
name: goal-api-change
description: Add or change an HTTP endpoint of goald end to end, from api/openapi.yaml through the handler, its permission, the TypeScript client and the web screen that calls it. Use for any change to api/openapi.yaml or api/permissions.yaml.
---

# Change the API

`api/openapi.yaml` is the only contract between goald and TypeScript. The contract rules are in [contracts.md](../../rules/contracts.md); what else moves with the spec is the `api/openapi.yaml` and `api/permissions.yaml` rows of the [change map](../../../docs/engineering/change-map.md). Read [architecture.md](../../../docs/engineering/architecture.md) before the handler and [testing.md](../../../docs/engineering/testing.md) before the tests.

1. **Write the contract first.** Edit `api/openapi.yaml`: the path, the operation id, the request and response schemas, the problem responses, and the operation's `x-goal-scope`.
2. **Name the permission.** A new permission goes in `api/permissions.yaml`, with the roles that hold it. An existing permission only needs the operation's `x-goal-scope`.
3. **Regenerate.** Run `task api:gen`. It writes `internal/api/gen`, `internal/api/permissions_gen.go`, the auth roles in `auth/src/access.gen.ts` and the types in `packages/api-client/src/schema.ts`. Never edit those files by hand.
4. **Check the permission table.** Run `task api:permissions-check`.
5. **Write the handler** in `internal/api`, beside the handlers of the same resource. It takes ports, never adapters. It checks the permission the generated table names and returns problems in the shape the spec gives.
6. **Test the handler** in `internal/api` with the in-memory fakes of `internal/ports/porttest`: the answer, each refusal (missing permission, another organization, bad input) and what the call changed. A new permission also gets a case in `internal/api/permissions_internal_test.go`. Run `task test`.
7. **Extend the client.** Add the route and any helper in `packages/api-client/src/routes.ts`, with its test beside it. Run `task api-client:check`.
8. **Call it from the web.** Add the request to the matching file in `web/src/api` (`client.ts` or the resource's own file) and its query key in `queries.ts`. Add the mock's answer in `web/mock` so the screens work without goald. Build the screen with [goal-ui-screen](../goal-ui-screen/SKILL.md). Run `task web:check`.
9. **Auth roles.** When `api/permissions.yaml` changed, run `task auth:check`.
10. **Docs as built.** Update the spec in `docs/specs/` that describes the endpoint, and `docs/engineering/frontend.md` when the client or a screen changed.
11. **Deliver** with [goal-deliver](../goal-deliver/SKILL.md). In the pull request, list each of these checks you ran, with its result.
