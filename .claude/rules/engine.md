---
paths:
  - "internal/core/domain/**"
  - "internal/core/engine/**"
---

# Domain and engine

The flow engine and its failure model: [architecture.md](../../docs/engineering/architecture.md) ("Hexagonal core", "Scale and failure").

- Import only the standard library in `internal/core/domain`, so the plain types stay free of every dependency.
- Import only `domain` and `ports` in `internal/core/engine`, because the engine must run on any adapter; depguard refuses anything outside `internal/core`.
- Test every engine decision (claims, WIP, constraint detection, gates, andon) with the in-memory fakes in `internal/core/ports/porttest`, because those tests are fast and the conformance suites make them hold for the real adapters.
- Add a port method to its conformance suite (`porttest/storetest`, `definitionstest`, `sandboxtest`, `knowledgetest`) in the same change, so the fake and the adapter cannot drift apart.
- Hold work under a lease with heartbeats, so a dead instance's work returns to the queue when its lease ends.
- Make each step idempotent, keyed by item, station and attempt, so a retry after a crash does no work twice.
- Fence a lease's writes with its token, so a holder that stalled past its lease changes nothing once another took over.
- Rely on the store's `FOR UPDATE SKIP LOCKED` claims for concurrency, never on locks in memory, because instances share nothing but PostgreSQL.
- Read time from the `Clock` port, never `time.Now`, so tests control it.

## Skills

Load `golang-design-patterns`, `golang-concurrency` and `golang-testing`.
