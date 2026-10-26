---
name: goal-adapter
description: Add a new provider to Goal, as a connection kind (data) and, when goald itself must talk to the provider, a port adapter in internal/adapters wired in cmd/goald/main.go with its tests. Use when a factory needs a service Goal does not know yet, or goald needs a new code host, source reader, minter, embedder or store.
---

# Add a provider

A provider is a connection kind, never code in the engine. The boundary rules are in [architecture.md](../../rules/architecture.md) (depguard in `.golangci.yml` enforces them; never silence it). The connection kind row of the [change map](../../../docs/engineering/change-map.md) is the definition schema row. Read [architecture.md](../../../docs/engineering/architecture.md) and [testing.md](../../../docs/engineering/testing.md) first.

1. **Decide what is needed.** If runs only need credentials as environment variables (an API, a CLI, an MCP server), the provider is a kind file and nothing else: do steps 2, 3 and 9. If goald itself must call the provider (a code host, a knowledge source to sync, minted credentials, embeddings), you also need an adapter: do every step.
2. **Write the kind** as `internal/connkind/kinds/<name>.yaml` in the shape of the kinds beside it: `metadata` (name, title, description), `spec.fields` (secrets marked `secret: true`), `spec.env`, a `spec.test` request that proves the values work, `spec.docs` for the agents, and `sync:`, `mint` or `model` when they apply. It must validate against `contracts/definitions/connection-kind.schema.json`. Change the schema only when the kind needs a field no kind has, and then follow the change map's definition schema row.
3. **Test the kind.** `internal/connkind/connkind_test.go` loads every platform kind. Add a case when the kind renders env, a git login or a test request in a new way. Run `task test`.
4. **Find the port.** Pick the interface in `internal/core/ports/ports.go` that the adapter implements (`CodeHost`, `SourceReader`, `Minter`, `Embedder` and so on). A new port is a design change: write an ADR first with [goal-adr](../goal-adr/SKILL.md).
5. **Write the adapter** in its own package `internal/adapters/<port>/<name>`, under the group named after the port it implements (`codehost/gitlab`, `embedder/openai`); the package name is the leaf directory. Let it take its configuration through a constructor. It calls out through the public-only connection transport goald passes in, follows no redirects, and puts no secret in an error or a log line.
6. **Test the adapter.** Run the port's conformance suite from `internal/core/ports/porttest` when the port has one. Against a real dependency, write an integration test tagged `//go:build integration`, which `task test-integration` runs. Against an HTTP API you cannot run locally, write unit tests against a local test server that answers as the provider documents: listing, paging, refusals and errors.
7. **Wire it** in `cmd/goald/main.go`, by constructor, beside the adapters of the same port. A sync reader goes in the `readers` map of `openKnowledge` under the name the kind's `sync:` gives. Use no globals and no `init()`.
8. **Run the checks.** `task lint` (depguard included) and `task test`.
9. **Docs as built.** Update `docs/engineering/architecture.md` (the port's table and the adapter) and the spec that names the provider. A new environment variable follows the change map's row for it.
10. **Deliver** with [goal-deliver](../goal-deliver/SKILL.md). Name the integration tests you could not run and why.
