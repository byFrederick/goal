# ADR 0023 — Knowledge use and verdicts

Status: proposed · 2026-10-27 · Builds on [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (the knowledge base and the gateway's knowledge tools), [ADR 0015](0015-overlapping-in-flight-work.md) (decision 6: the generated ADR index) and [ADR 0016](0016-ui-revamp.md) (decision 2: the screens are the contract)

## Context

Each organization has one knowledge base, and agents reach it only through two gateway tools, `knowledge__search` and `knowledge__read` ([`internal/gateway/knowledge.go`](../../internal/gateway/knowledge.go), `callKnowledge`). A search returns passages (`domain.KnowledgeHit`: chunk, document, source, title, snippet and score, in [`internal/core/domain/knowledge.go`](../../internal/core/domain/knowledge.go)). A read returns one document's text, at most 200 KiB. The gateway records each call as a tool call with its sizes and outcome, but nothing records which documents a run used.

So nobody can tell which documents the agents rely on, or which ones lead them wrong. The Knowledge page's picture ([`docs/design/screens/connections/knowledge.html`](../design/screens/connections/knowledge.html), line 194) has a "Where it was used" card: runs that opened each document over the last 7 days, with the agents' verdicts as up and down counts, and a call to make an item when a document misled runs. [The screens' index](../design/screens/README.md) lists it under `knowledge`. Neither the data nor the API behind it exists.

The knowledge tables are in `db/migrations/00054_knowledge.sql`: `knowledge_sources`, `knowledge_documents` and `knowledge_chunks`, each keyed by `org_id` under row-level security, with documents and chunks cascading from their source. Permissions are `knowledge:read` (owner, admin and member, keys and personal tokens) and `knowledge:manage` (owner and admin, keys) in [`contracts/api/permissions.yaml`](../../contracts/api/permissions.yaml).

## Decision

1. **A knowledge use is one row per run, document and kind.** The kind is `search` when `knowledge__search` returned at least one of the document's chunks, and the row keeps those chunk ids. The kind is `read` when `knowledge__read` opened the document. A second search in the same run that returns the same document adds its new chunk ids to the existing row. Each row carries the organization, factory, item, run, source, document and the time of the first use. The gateway writes it after the tool has its answer. Writing is best effort: a failure is logged (`gateway knowledge use failed`) and never fails or delays the tool call the agent made.
2. **A verdict is `helped` or `misled` on one document.** It may name a run and that run's item, and it may carry a note of at most 500 characters. It is given by an actor, stored as `Actor.Ref()`:
   - `agent:<name>`, the agent's name in the definition, through a new gateway tool `knowledge__verdict` (arguments `document`, `verdict` and an optional `note`). The gateway ties it to the calling run and its item. It accepts only a document that run has a knowledge use of; any other document is a tool error the agent can read.
   - `user:<id>`, through the API and the CLI, with an optional run of the same organization.

   There is at most one verdict per actor, document and run (a verdict without a run counts as its own run for this rule). A later verdict replaces the earlier one, its kind, note and time.
3. **"Where it was used" counts distinct runs per document, per source, over a window.** The window defaults to 7 days and is at most 90; a longer one is refused. For each document it gives the number of distinct runs with a knowledge use in the window, and the number of `helped` and `misled` verdicts given in the window, split into agents' and people's. The Knowledge page's card shows the agents' counts, as its picture says "Agents' verdict". Reading uses and verdicts needs `knowledge:read`.
4. **A new permission, `knowledge:verdict`, lets a person give a verdict.** It is an org permission held by owner, admin and member, with `keys: true` and `pat: true`. Agents need no permission: the gateway's run claims and decision 2's limit decide what they may judge.
5. **Deleting a document or a source deletes its uses and verdicts.** Both tables reference the document with `ON DELETE CASCADE`, so the source's existing cascade to its documents reaches them too. Deleting a run's organization removes them with the rest of its data.
6. **Prompts and logs never hold document text from these rows.** A use holds ids and times, and a verdict holds its kind and the actor's note. Neither copies a document's text, a snippet or a title. No prompt is built from them, and a log line about them names ids only.

## Consequences

- People can see which documents the agents depend on and which ones mislead them, which is the signal for fixing the knowledge base.
- Recording uses costs one write per tool call that found a document. Because it is best effort, an outage of the write loses rows, not runs; the counts are a measure, not an audit trail.
- The item page and Knowledge page pictures in [`docs/design/screens/`](../design/screens/README.md) have no control for a person to give a verdict; the Knowledge page shows only the counts. Until the owner adds one on the canvas ([ADR 0016](0016-ui-revamp.md), decision 2), people give verdicts through the API and the CLI. The web part builds only the "Where it was used" card.
- The work ships in the parts of one assembly, after this ADR:
  - store: a migration with both tables and row-level security, sqlc queries and the port's conformance tests;
  - gateway: recording uses in `knowledge__search` and `knowledge__read`, and the `knowledge__verdict` tool;
  - API: the usage and verdict endpoints in `contracts/api/openapi.yaml`, and `knowledge:verdict` added to `contracts/api/permissions.yaml`;
  - CLI: `goal knowledge usage` and `goal knowledge verdict`;
  - web: the Knowledge page's "Where it was used" card.
- A change to the window's bounds, the verdict kinds or the note's length is an amendment to this ADR.
