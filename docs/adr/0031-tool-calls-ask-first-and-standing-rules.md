# ADR 0031 — Tool calls that ask first: the run ends until a person approves, and Always allow records a standing rule

Status: accepted · 2026-10-27 · Builds on [ADR 0018](0018-agents-ask-questions.md) (a run that needs a person ends, and the item waits in Needs you; this ADR follows its decisions 2, 4 and 5) and [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (a run uses its organization connections directly from its box, so the box is where a call can be stopped)

## Context

An agent that may call a connection's tool calls it at once. A person cannot say "let the architect read the issue tracker, but show me every comment before it is posted". The screens ask for exactly that:

- [connection-add.html](../design/screens/connections/connection-add.html) lists the tools a connection's MCP server offers ("Tools agents may use, 14 found") and gives each tool three choices: Allow, Ask first and Off.
- [needs-you.html](../design/screens/daily-work/needs-you.html) and [mobile/approve.html](../design/screens/mobile/approve.html) show a waiting call ("waiting for your OK to post") with Approve, Deny and a checkbox "Always allow architect to comment on linked issues".
- [connections.html](../design/screens/connections/connections.html) shows "Writes waiting for your OK 2".

What the code offers today:

- The worker decides every connection tool call in `connectionPermission` ([agent.ts](../../apps/worker/src/agent.ts)). It can only allow or deny, from the agent's `tools_allow` and `tools_deny`.
- The factory definition accepts `approval: required|none` on a connection's tool (`toolRule` in [factory.schema.json](../../contracts/definitions/factory.schema.json)), and nothing enforces it.
- "Ask" on built-in tools is served as off: `ToolApprovals` is false ([skill.go](../../internal/core/domain/skill.go)).
- An organization connection ([connection.go](../../internal/core/domain/connection.go)) has no tool list and no per-tool setting. Its `ConnectionTest` records only `At`, `OK` and `Error`.
- The gateway serves the run bundle and notes at `/run` and Goal's own tools at `/mcp`, both authorized by the run's task token ([gateway.go](../../internal/gateway/gateway.go)).
- [ADR 0018](0018-agents-ask-questions.md) ends a run while a person answers, because processes are stateless ([AGENTS.md](../../AGENTS.md) rule 4) and a waiting box costs money. It adds the lease outcome `asked` to the `lease_spans.outcome` CHECK ([00031_lease_spans.sql](../../db/migrations/00031_lease_spans.sql)) and the inbox kind `question`. The `question` kind is decided but not built yet: `InboxEntryKind` in [openapi.yaml](../../contracts/api/openapi.yaml) still lists only `decision`, `andon`, `rework_warning`, `blocked` and `expedite`.
- Deciding on an item needs the `item:decide` scope, and changing a connection needs `connection:manage` ([permissions.yaml](../../contracts/api/permissions.yaml)).
- Two names are already taken. The item event `tool_denied` is a call the agent's policy refused, as the worker reports it at the end of a run ([event.go](../../internal/core/domain/event.go)). "Tool rules" are the agent's `tools_allow` and `tools_deny`, and `toolRule` is the definition's per-tool override.

The work is split into the parts of one assembly: store, engine, gateway, worker, the approvals API, the connection tools API, the CLI, Needs you and the connection screens. This ADR is the one decision they all follow, so the names below are exact.

## Decision

1. **Each tool of an organization connection has a mode: `allow`, `ask` or `off`.** The mode is set on the organization connection, by tool name (the name the MCP server lists, such as `create_comment`, without the connection prefix). A tool with no mode set is `allow`, so every existing connection behaves as it does today. `allow` runs the call, `ask` pauses it for a person (decision 3), and `off` refuses it like a denied call. In Go the type is `domain.ConnectionToolMode` with the constants `ToolAllow`, `ToolAsk` and `ToolOff`; in the API it is the schema `ConnectionToolMode` with the enum `[allow, ask, off]`. Modes are stored in PostgreSQL with the connection, never in the bucket.

2. **The tool list comes from the connection test.** Testing a connection whose kind has an MCP server calls its `tools/list` and records the tool names, sorted, on `ConnectionTest` as `Tools`. That list is what the connection screens show ("14 found"). A mode for a tool the last test did not list is kept, and applies again if the tool comes back. A tool the server offers that no test has listed yet has no mode, so it is `allow`. A connection without an MCP server (git logins, CLI tools, env only) has no tool list and no modes.

3. **The stricter setting wins.** A run's effective mode for a connection tool is the strictest of its inputs, where `off` beats `ask` and `ask` beats `allow`:
   - the organization connection's mode (decision 1);
   - the definition's `toolRule`: `enabled: false` makes the tool `off`, and `approval: required` makes it at least `ask`. `approval: none` never lowers an organization's `ask`;
   - the agent's `tools_deny`, which still denies the call before any mode is read, and its `tools_allow`, which still limits what the agent may call at all.

   An active standing rule (decision 7) then turns an effective `ask` into `allow` for its agent role, but only when the `ask` comes from the organization connection's mode. An `ask` from the definition's `approval: required` asks every time, because the factory's author asked for it, and a standing rule never turns `off` into anything. The gateway computes the effective modes when it serves the run bundle. Each connection in the bundle's `connections` gains `tool_modes`, a map from tool name to `ask` or `off` (a tool not in the map is `allow`), and `always_allow_offered`, the tool names whose `ask` comes from the organization's mode only. A rule created or revoked while a run works applies from the next run.

4. **An Ask-first call ends the run, and the item waits for a person.** The worker's `connectionPermission` does not run a call whose effective mode is `ask`. It reports the call to the gateway with `POST /run/tool-approval`, authorized by the run's task token, with the body `{connection, tool, input}`, and answers the agent that the call waits for a person. The gateway refuses an `input` larger than 16 KiB as canonical JSON with 413, and a second report while the item already has an open tool approval with 409. In both cases the worker denies the call and the run goes on. Otherwise the gateway passes the call to the engine and answers 202. The engine records the item event `tool_approval_requested` with `{connection, tool, input, agent, run_id, always_allow_offered}`, where `connection` is the connection's name, `agent` its agent role, and `always_allow_offered` whether decision 3 lets this tool get a standing rule. It then ends the run and the lease the way `Pause` stops an in-progress run. The lease ends with a new outcome, `awaiting_approval`, which the engine part adds to the `lease_spans.outcome` CHECK in a new migration. The item becomes `waiting_human` at its agent station with one open tool approval, identified by the id of its `tool_approval_requested` event. No box stays alive while a person decides.

5. **Approve grants that one call to the next run.** Approving records `tool_approval_granted` with `{approval_id, always_allow, actor}`. The item goes back to its station's arrival state (`Station.Arrival`), and its `rework_count` does not change. The next run's task spec carries the grant in `granted_calls`, a list of `{connection, tool, input}`. The worker allows a call that matches a grant's connection, tool and input exactly, compared as canonical JSON (keys sorted, no insignificant whitespace), once per grant. A call with any other input is decided by its mode again, so a run that rephrases the call asks again. This is intended: a person approved one call, not a tool. A grant the next run does not use ends with that run. The brief lists each granted call under "## Approved tool calls", next to "## Notes from people", in the same `GOAL_FEEDBACK`.

6. **Deny puts the item back with the denial in its brief.** Denying records `tool_approval_denied` with `{approval_id, note?, actor}`, where `note` is the person's reason, 1–2000 characters. The item goes back to `Station.Arrival`, and its `rework_count` does not change. The next run's brief lists the denied call and the note under "## Denied tool calls". The existing `tool_denied` event keeps its meaning (a call the agent's policy refused) and is not reused.

7. **Always allow records a standing rule.** Approving with `always_allow` also creates a standing rule: the organization lets one agent role call one tool of one connection without asking. Its key is (organization, connection id, tool name, agent role), and there is at most one active rule per key. The agent role is the agent's name in the definition, such as `architect`, so the rule applies to that role in every factory of the organization that uses the connection. The rule lives in PostgreSQL in the table `standing_rules`, with row-level security by organization, and holds `id`, `connection_id`, `tool`, `agent`, `created_by`, `created_at`, `item_id` (the item it was approved on), `revoked_by` and `revoked_at`. Creating it appends the audit event `standing_rule.created`, and revoking it sets `revoked_at` and `revoked_by` and appends `standing_rule.revoked`, both with the target `standing_rule`. A rule is never deleted. Deleting a connection revokes its active rules, with the person who deleted it as `revoked_by`. Always allow is refused when the open approval's `always_allow_offered` is false.

8. **Standing rules use the connection scopes.** Listing them needs `connection:read`, which every member holds. Approving with `always_allow` needs `item:decide` on the item's factory and `connection:manage` in the organization, because the rule changes how an organization connection behaves in every factory. `connection:manage` needs a recent sign-in, so an approver without it gets Approve and Deny but no Always allow checkbox. Revoking needs `connection:manage`. No new scope is added. Who created a rule gives no extra right to revoke it: a member who may not change the connection may not change its rules.

9. **Other decisions take precedence, as in ADR 0018 decision 4.** Approve on an item (`POST /v1/orgs/{org}/items/{item}/approve`) with an open tool approval is refused with 409 and the code `tool-approval-open`: the item waits for a decision on the call, not on the item. Move, Pause, Reject and Cancel withdraw the open tool approval: it is no longer open, and no `tool_approval_granted` or `tool_approval_denied` event is recorded. An item has at most one open question or tool approval at a time, because both end the run.

10. **The API adds these names.**
    - `InboxEntryKind` gains `tool_approval`: one entry per item with an open tool approval, with the id `tool_approval:<item id>`, and its details in the entry's `tool_approval` field.
    - `Item.tool_approval` holds the open tool approval, or is absent when there is none: `{id, connection, tool, input, agent, run_id, asked_at, always_allow_offered}`.
    - `POST /v1/orgs/{org}/items/{item}/tool-approval` decides it, with the body `{decision, always_allow, note}`: `decision` is `approve` or `deny`, `always_allow` (default false) is allowed only with `approve`, and `note` only with `deny`. It returns 409 with the code `no-open-tool-approval` when the item has none, and 409 with `always-allow-not-offered` when `always_allow` is set and the approval does not offer it.
    - `/v1/orgs/{org}/standing-rules` is the rules resource: `GET` lists the organization's rules (active by default, `?revoked=true` adds the revoked ones), and `DELETE /v1/orgs/{org}/standing-rules/{rule}` revokes one, returning 404 for a rule that does not exist and 409 with `standing-rule-revoked` for one already revoked. Its schema is `StandingRule`. The resource is not named `tool-rules`, because "tool rules" already means the agent's allow and deny lists.
    - `Connection` gains `tools`, a list of `{name, mode}` with `mode` a `ConnectionToolMode`, one entry for each listed tool and each tool with a mode set, and `waiting_approvals`, the number of open tool approvals on that connection in the organization. The connections list's "Writes waiting for your OK" is the sum of `waiting_approvals`. A connection's modes are changed by `PATCH /v1/orgs/{org}/connections/{connection}` with `tools`, which needs `connection:manage`.

11. **The UI names the agent by its role and the tool by its name.** A tool approval card and the Always allow checkbox say "Always allow `<agent role>` to `<tool>`", such as "Always allow architect to create_comment", never the agent's slot number.

Out of scope for this decision: Ask first on built-in tools (`builtins` `ask` stays served as off), "Edit, then approve", and keeping a box alive while a person decides.

## Consequences

- A person can let agents read freely and still see every write before it reaches people outside Goal. The cost of asking is one new run after the decision, which reads the same workspace branch and the decision in its brief.
- The parts ship in this order, each adding what it names:
  1. `store`: `ConnectionTest.Tools`, the per-tool modes on connections, the `standing_rules` table with row-level security, and their queries and migrations.
  2. `engine`: the event kinds `tool_approval_requested`, `tool_approval_granted` and `tool_approval_denied`, the lease outcome `awaiting_approval` and its migration, Approve, Deny and Always allow, withdrawal, `granted_calls`, and the "## Approved tool calls" and "## Denied tool calls" briefs.
  3. `gateway`: `POST /run/tool-approval`, and `tool_modes` and `always_allow_offered` in the bundle's connections.
  4. `worker`: `connectionPermission` reports an `ask` call instead of running it, refuses an `off` one, and allows the run's `granted_calls` once each.
  5. `api-approvals`: the `tool_approval` inbox kind, `Item.tool_approval`, the tool-approval endpoint, the `tool-approval-open` refusal on Approve, and the standing rules resource.
  6. `api-connection-tools`: `Connection.tools`, `ConnectionToolMode`, `waiting_approvals` and setting modes with `PATCH`.
  7. `cli`: deciding a tool approval with Always allow, and listing and revoking standing rules.
  8. `needs-you-ui`: Approve, Deny and Always allow on a tool approval in Needs you.
  9. `connections-ui`: Allow, Ask first and Off per tool on the connection screens, and the writes-waiting count.
- A standing rule is broad on purpose: it covers an agent role in every factory of the organization. Revoking it is the way back, and the audit log shows who created and revoked each one.
- A tool the server adds after the last test is `allow` until a person tests the connection again and sets its mode. The connection screens should say when the tool list was last read.
- A tool call's input is shown to people as the agent wrote it. An agent that puts a secret in a tool's input puts it in front of the approver; the worker's existing rule that secrets reach the box only as env variables is what keeps them out of inputs.
- A run that asks ends without an output, so a run's history has another ending, and flow metrics count the time an item waits for a decision as waiting time at its station, as they do for any `waiting_human` item.
- Clients that do not know the `tool_approval` inbox kind show it as a generic row, as the `InboxEntryKind` description already promises.
