# ADR 0020 — Asked definition changes: draft requests

Status: accepted · 2026-10-27 · Builds on [ADR 0002](0002-jonah-evals.md) (decision 7's allowlist of definition files, and Jonah's proposal loop that this ADR leaves alone), [ADR 0005](0005-factory-definitions-in-object-storage.md) (the draft and its files live in the bucket and PostgreSQL) and [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (runs, their boxes and the model connection)

## Context

Two screens ask an agent to change a factory's definition:

- The Definition screen ([`docs/design/screens/build/definition.html`](../design/screens/build/definition.html), line 184) has an Ask bar: "Ask the factory to change it, for example: give Review a 4th reviewer".
- The Agent screen ([`docs/design/screens/build/agent.html`](../design/screens/build/agent.html), line 186) has "Ask Jonah for a fix" next to "Why review sent work back".

Neither has anything to run on today:

- **Every run belongs to an item.** A run is one agent's work on one item at one station ([architecture.md](../engineering/architecture.md), "One run, end to end"): the engine claims an item with a lease, starts a box, clones the repository and checks out the item's branch. A change to a definition has no item, no station and no repository.
- **Jonah is not built.** [ADR 0002](0002-jonah-evals.md) designs a proposal loop (retros, evals, a review list), and its decision 7 limits Jonah's diffs to definition files, never `evals/`, schemas, migrations or its own code. None of it exists.
- **The draft already takes edits.** A factory's open draft is written file by file through `putDraftFile` and `deleteDraftFile` (`contracts/api/openapi.yaml`), each under an If-Match of the file's sha256 (412 `draft-conflict` when it is stale). `validateDraft` runs the publish checks (`config.Check`, [factory-definition.md](../engineering/factory-definition.md)), and `publishDraft` makes the draft the next version. Writing needs `definition:edit`; publishing needs `definition:publish` (`contracts/api/permissions.yaml`).
- **The runtime gives the model and the budget.** `spec.runtime.modelConnection` is the connection every run gets its model from, and `spec.runtime.budget` caps a factory's spend (`contracts/definitions/factory.schema.json`).

## Decision

1. **One mechanism, the draft request.** Both screens create a draft request: a row per factory in PostgreSQL holding
   - `id` (UUID), the organization and the factory (`metadata.id`, AGENTS.md rule 2);
   - `requester`, the actor who asked (`Actor.Ref()`);
   - `text`, what was asked, 1 to 2000 characters;
   - `agent`, an optional agent name of the definition, set for Ask Jonah for a fix (decision 5) and empty for the Ask bar;
   - `status`, one of `queued`, `running`, `done`, `failed`;
   - `summary`, the agent's account of what it changed, or why the request failed;
   - `changed_paths`, the definition paths it wrote or deleted;
   - `cost_usd` and `turns`;
   - `created_at`, `started_at` and `finished_at`.

   The domain type lives in `internal/core/domain`, the table is org-scoped with row-level security, and the store is reached through a port, like every other table.
2. **goald runs it like a run, in a box with no repository.** A queued request is claimed with a lease and kept alive with a heartbeat, so any goald instance recovers a crashed instance's request (AGENTS.md rule 4). The box is started through the `Sandboxes` port with the goal-agent image. It has no repository and no repository connection: it gets only the factory's `spec.runtime.modelConnection`. Its `/workspace` holds the files of the factory's open draft, at the revision the request read (a request on a factory with no open draft starts from the published version, as opening a draft does). goal-agent runs in a new **definition mode** that may change only definition files: `factory.yaml` and files under `agents/`, `rules/` and `skills/`. It never creates, changes or deletes anything under `evals/` or `contracts/` (the schemas), mirroring [ADR 0002](0002-jonah-evals.md) decision 7's allowlist. A change outside the allowlist fails the request and writes nothing.
3. **goald validates, writes into the draft and never publishes.** When the box finishes, goald runs the publish checks on the changed tree. It then writes each changed file into the factory's draft with the If-Match of that file's sha256 as read in decision 2 (If-None-Match `*` for a new file, If-Match for a deleted one), and never calls publish. A person reviews the draft and publishes it as usual, with `definition:publish`.
   - A failed validation still writes the files, so the Definition screen's Problems panel shows what is wrong; the request is `done` and its summary says the draft has problems.
   - A draft file that changed since the request read it (an If-Match conflict) fails the request with a clear message naming the file, for example "factory.yaml changed in the draft while the request ran; ask again". Files written before the conflict stay in the draft, and `changed_paths` lists only those.
4. **Cost counts against the factory's budget, one request at a time.** The request's cost counts against `spec.runtime.budget` like any run's, and a factory whose budget is used up (`onExhausted`) starts no request. A factory has at most one queued or running request; creating a second one while one is queued or running is refused (409), and the database enforces it with a partial unique index.
5. **Ask Jonah for a fix is a draft request with its agent set.** The Agent screen's button creates a draft request with `agent` set to that agent and `text` filled in from that agent's sent-back reasons (the reasons review gave when it sent work back), which the person may edit before sending. It is not Jonah's proposal loop: it runs no retro or eval and makes no proposal. [ADR 0002](0002-jonah-evals.md) still governs that loop, and building it later does not change this mechanism.
6. **Scopes.** Creating a draft request needs `definition:edit`, the scope that writes the draft. Listing and reading a factory's requests needs `factory:read`.

## Consequences

- One path serves both buttons: a store (migration, queries, domain type, port), an API and a `goal factory ask` command, goal-agent's definition mode, a runner in goald and the two UI controls. The parts of the build-a-factory assembly build them against these decisions.
- No agent can publish a definition: every change waits in the draft for a person, and the publish checks and the Problems panel are the same ones people already use.
- A run no longer always belongs to an item. The runner reuses the lease, heartbeat and box lifecycle but not the item's claim, so its code keeps the two apart; architecture.md describes it once it is built.
- A person editing the draft while a request runs can make the request fail on a conflict. That is the price of never overwriting a person's edit.
- One request per factory at a time keeps cost and conflicts small, but a busy factory waits for its turn.
