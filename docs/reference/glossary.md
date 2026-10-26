# Glossary

The words Goal's code, API and UI use, one sentence each, with the place in the repository where each one lives. Where a term is planned but not built, the entry says so.

| Term | What it means | Where it lives |
| --- | --- | --- |
| Actor | Who did something (a user, an API key, an agent or a job), stored as `Actor.Ref()` such as `user:<id>` or `agent:<name>`. | `internal/domain/org.go` (`Actor`) |
| Agent | A model with its prompt, skills and connections that works at an agent station. | `internal/domain/line.go` (`AgentSpec`); `agents/*.md` in a definition |
| Andon | A stop of a factory's whole line, pulled by a person and open until someone clears it; while it is open the engine releases and claims nothing there. | `internal/domain/attention.go` (`AndonPull`), `internal/domain/andon.go` |
| API key | An organization's service account, acting with its scopes and at most its role ceiling on the factories it reaches. | `internal/domain/apikey.go` (`APIKey`) |
| Assembly | An item split into parts that are worked as separate items and ship together. | `internal/domain/item.go` (`Assembly`, `StateAssembly`), `internal/domain/part.go`; [ADR 0012](../adr/0012-assemblies-and-parts.md) |
| Attention | What a factory needs from people right now, as the counts behind the app's badges. | `internal/domain/attention.go` (`Attention`) |
| Board | The UI screen that shows a factory's items in columns by station. | `apps/web/src/features/board`, `apps/web/src/app/screens.ts` |
| Box | The disposable container one run works in, started through the `Sandboxes` port. | `internal/ports/sandboxes.go`; `apps/worker` (the image) |
| Bridge | The explanation of the gap between a factory's actual lead time and its target, largest step first. | `internal/domain/bridge.go` (`Bridge`) |
| Budget | A factory's weekly spending limit and what the engine does once it is used up. | `internal/domain/budget.go` (`BudgetPolicy`, `BudgetStatus`) |
| Class of service | The pull order of an item: expedite items are pulled before any standard item. | `internal/domain/item.go` (`ClassOfService`) |
| Connection | An organization's sign-in to an outside system, with write-only secret fields, that the runs of the factories naming it may use. | `internal/domain/connection.go` (`Connection`); `internal/adapters/postgres` |
| Connection kind | A template for connections: the fields an admin fills in, which are secret, and what a run's box gets from them. | `internal/domain/connection.go` (`ConnectionKind`), `internal/connkind`; `contracts/definitions/` |
| Constraint | The agent station that limits how fast a factory's line can deliver, measured over a window. | `internal/domain/constraint.go` (`Candidate`) |
| Contract | A file that two parts of the system agree on, such as the API, the permissions, the sandbox protocol, the environment registry or the definition schemas. | `contracts/` |
| Definition | A factory's files (`factory.yaml`, agents, rules, skills, evals), stored as content-addressed blobs in the bucket. | `internal/domain/definition.go` (`Manifest`), `internal/config`, `internal/definitions`; [factory-definition.md](../engineering/factory-definition.md) |
| Drum-buffer-rope | A release policy that lets work in at the pace of the constraint; the policy can be chosen, but its enforcement is not built yet. | `internal/domain/policy.go` (`ReleaseDBR`) |
| Edge, need | A stored dependency between two items: an item needs another item before it can be pulled. | `internal/domain/edge.go` (`Edge`, `OpenNeed`) |
| Event | One recorded change of an item, run or factory, which the stream and the audit read. | `internal/domain/event.go` (`Event`) |
| Factory | One organization's configured production line of stations and agents, keyed by its `metadata.id` UUID and never by name. | `internal/domain/item.go` (`FactoryID`), `internal/factorymodel` |
| Floor | The UI screen that shows a factory's line live: its stations, their load and what needs people. | `apps/web/src/features/floor`, `apps/web/src/app/screens.ts` |
| Gate | A check at a station that sends rejected work back to an earlier station, at most a set number of times per item. | `internal/domain/line.go` (`Gate`), `internal/domain/andon.go` (`GateVerdict`) |
| Inbox | The list of an organization's open items that may need a person: waiting for a decision, blocked or expedite. | `internal/domain/inbox.go` (`AttentionItem`), `apps/web/src/shared/inbox.ts` |
| Item | One unit of work flowing through a factory's line, with a state at its current station. | `internal/domain/item.go` (`Item`, `ItemState`) |
| Jonah | Goal's planned continuous-learning loop (retros, evals and proposed changes to a definition); it is designed but not built. | [ADR 0002](../adr/0002-jonah-evals.md), [docs/history/](../history/); the `jonah` entry in `apps/web/src/app/screens.ts` |
| Knowledge source | One source of an organization's knowledge base (uploads or a synced connection), searched by agents through the gateway. | `internal/domain/knowledge.go` (`KnowledgeSource`), `internal/knowledge` |
| Lane | A route through a station that an earlier output chooses, so that items of one lane go to that lane's agents. | `internal/domain/run.go` (`LaneFrom`), `internal/domain/line.go` (`ErrNoRoute`) |
| Lease | An agent slot's hold on an item; it expires unless renewed, so the work of a crashed instance returns to the queue. | `internal/domain/run.go` (`Lease`) |
| Learned | The planned built-in knowledge source of facts learned from retros; it is not built yet. | [architecture.md](../engineering/architecture.md) ("Phases 5 and 6") |
| Line | A factory's stations in order, with its agents and flow policy, as the engine reads them. | `internal/domain/line.go` (`Line`) |
| Organization | The tenant: every org-owned row carries its id and is scoped by row-level security. | `internal/domain/org.go` (`Org`, `OrgID`) |
| Part | One item of an assembly, counted in the assembly's progress. | `internal/domain/item.go` (`Part`), `internal/domain/part.go` |
| Personal token | A member's own token for scripts, limited to the scopes it was created with. | `internal/domain/personaltoken.go` (`PersonalToken`) |
| Principal | Who a request acts for, and what the database says about them at that moment. | `internal/domain/identity.go` (`Principal`) |
| Rework | A round in which a gate sends an item back for another try. | `internal/domain/line.go` (`Gate.MaxRework`), `internal/domain/flowstats.go` (`ReworkRound`) |
| Rule | Always-on guidance a factory gives the agents, stations, lanes or work types it targets, from a `rules/<name>.md` file of the definition. | `internal/domain/rule.go` (`Rule`), `internal/config/rules.go` |
| Run | One agent's work on one item at one station, in its own box. | `internal/domain/run.go` (`Run`), `internal/engine` |
| Skill | A folder of instructions an agent loads, from the definition or a skills.sh source. | `internal/domain/line.go` (`SkillRef`), `internal/domain/skill.go` |
| Station | One step of the line, worked by a queue, agents, people or nobody (done). | `internal/domain/line.go` (`Station`, `StationKind`) |
| Task token | The short-lived token a run uses to reach Goal, scoped to its organization, factory, item, station and connections. | `internal/domain/token.go` (`TaskClaims`) |
| Touch | One action by a person on an item, counted to show how much human help work needed. | `internal/domain/touch.go` (`Touch`) |
| Version | One saved state of a factory's definition, draft or published, with its manifest. | `internal/domain/definition.go` (`DefinitionVersion`) |
| Waiting | Why a waiting item is held: the open needs that hold it and the file paths that overlap. | `internal/domain/waiting.go` (`Waiting`) |
| WIP | The number of items in progress, capped per station and, for expedite items, per factory. | `internal/domain/line.go` (`Station.WIP`), `internal/domain/policy.go` (`ExpediteWIP`) |
| Work type | The kind of an item (such as a story or an outage), from the types a factory's board declares. | `internal/domain/item.go` (`Item.Type`), `board.workTypes` in `contracts/definitions/factory.schema.json` |
