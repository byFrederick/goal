# ADR 0018 — Agents ask questions: the run ends until a person answers

Status: accepted · 2026-10-27 · Builds on [ADR 0016](0016-ui-revamp.md) (decision 6 plans "Needs you questions" as a wave 3 feature, and decision 5 keeps API names such as `inbox` while the UI says Needs you)

## Context

An agent that is unsure what a person wants has no way to ask today. It can guess, block, or escalate through a gate, and each of these loses something: a guess wastes a run and a review, a block stops the item with no question attached, and an escalation sends the whole item to a person who must work out what the agent wanted to know.

[MASTER.md](../design/MASTER.md) ("Vocabulary") gives Needs you four types: Approval, Decision, Question and Alert. The screens show the Question type. [item.html](../design/screens/daily-work/item.html) has a card "frontend-engineer asks you" with the question text, quick-answer buttons, "Write an answer" and "12 min ago, the run is paused". [needs-you.html](../design/screens/daily-work/needs-you.html) has a Questions filter and a row "frontend-engineer: organization or factory spend?" with "#147 at Build, run paused".

What the code offers to build on:

- goald's gateway serves Goal's own MCP tools to every run, named `<server>__<tool>` such as `knowledge__search` ([session.go](../../internal/gateway/session.go)).
- `Engine.Pause` already stops an in-progress run: it takes the lease and stops the box ([human.go](../../internal/core/engine/human.go)).
- A run's brief carries what people said under "## Notes from people" ([notes.go](../../internal/core/engine/notes.go)), joined into `GOAL_FEEDBACK` by `feedbackOf` ([work.go](../../internal/core/engine/work.go)).
- Each lease ends with an outcome, and `lease_spans.outcome` has a CHECK that allows only `advanced`, `rejected`, `escalated`, `blocked`, `released`, `expired` and `taken` ([00031_lease_spans.sql](../../db/migrations/00031_lease_spans.sql)).
- Answering a question is a decision about an item, which the `item:decide` scope already covers ([permissions.yaml](../../contracts/api/permissions.yaml)).
- Processes are stateless ([AGENTS.md](../../AGENTS.md) rule 4), and a box costs money for every minute it runs.

The work is split into parts of one assembly: engine, gateway, API, CLI, the item page and Needs you. This ADR is the one decision they all follow, so the names below are exact.

## Decision

1. **The tool is `question__ask`, a Goal gateway tool.** goald serves it to every agent run, next to the knowledge tools; the worker image does not change. Its input is `question`, 1–500 characters, and `choices`, 0–4 strings of 1–60 characters each. An input outside these limits is a tool error. A second `question__ask` while the item already has an open question is a tool error too, and records nothing.
2. **Asking ends the run, and the item waits for a person.** The gateway passes the question to the engine, which records the item event `question_asked` with `{question, choices, agent, run_id}`. It then ends the run and the lease the way `Pause` stops an in-progress run. The lease ends with a new outcome, `asked`; the engine part adds it to the `lease_spans.outcome` CHECK in a new migration. The item becomes `waiting_human` at its agent station, with that open question. No box stays alive while a person decides: a waiting box breaks rule 4 and costs money for nothing. The UI still says "the run is paused", because that is what a person sees: the work stops and resumes on the answer.
3. **An answer puts the item back in line, with the answer in its next brief.** A person answers with one of the choices or with free text of 1–2000 characters. The engine records `question_answered` with `{answer, choice_index?}`; `choice_index` is set only when a choice was picked, and `answer` then holds that choice's text. The item goes back to its station's arrival state (`Station.Arrival`), and its `rework_count` does not change: an answer is not a rework. The item's next run carries the question and the answer under "## Answers from people", next to "## Notes from people", in the same `GOAL_FEEDBACK`.
4. **Other decisions take precedence, and answering needs `item:decide`.** Approve on an item with an open question is refused: the item waits for an answer, not an approval. Move, Pause, Reject and Cancel withdraw the open question: it is no longer open, and no `question_answered` event is recorded. Answering needs the `item:decide` scope, the same as approving and rejecting.
5. **The API adds three things.** `Item.question` holds the open question (its text, choices, the asking agent and when it was asked), or is absent when there is none. `InboxEntryKind` gains `question`, an entry per item with an open question, with the id `question:<item id>`. `POST /v1/orgs/{org}/items/{item}/answer` records an answer; it returns 409 with the code `no-open-question` when the item has none.
6. **The UI names the agent by its role.** A question card and a Needs you row show the agent's role name, such as `frontend-engineer`, never the number of the agent's copy (its slot).

Out of scope for this decision: a per-agent switch in the factory definition to turn asking off, and keeping a box alive while it waits for an answer.

## Consequences

- An agent can ask instead of guessing. The cost of asking is one new run after the answer, which reads the same workspace branch and the answer in its brief.
- The engine part comes first: it adds the event kinds, the `asked` outcome and its migration, and the answer and withdraw steps. The gateway part offers the tool and calls the engine. The API part adds `Item.question`, the `question` inbox kind and the answer endpoint. The CLI part adds `goal answer` and shows questions in an item's history. The item page and Needs you parts build the screens above.
- A run that asks ends without an output, so a run's history has a new ending, and flow metrics count the time an item waits for an answer as waiting time at its station, as they do for any `waiting_human` item.
- Clients that do not know the `question` inbox kind show it as a generic row, as the `InboxEntryKind` description already promises.
- An agent that asks too often costs people time. Without a per-agent switch, the definition's prompts and rules are the only way to limit asking until a later decision adds one.
