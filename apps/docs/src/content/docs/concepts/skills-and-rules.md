---
title: Skills, rules and standing rules
description: The three kinds of instruction a factory gives its agents, what each one does, and which name means which.
sidebar:
  order: 21
---

A skill is a folder of instructions that an agent loads when it needs them. A rule is guidance that is always on for the runs it targets. A standing rule is a permission that a person grants with "Always allow", so an agent can call one MCP tool without asking. Goal uses "rule" only for the first kind, and "standing rule" only for the third.

## Skills

A skill is a folder with a `SKILL.md` file at its root, and optional files beside it. The definition's `skills` list names each one and says where it comes from:

- `./skills/<name>`: a folder in the definition itself.
- `skills.sh:<owner>/<repo>@<skill>`: a skill from skills.sh. Vendoring it from the catalog copies its files into the definition and writes `source: ./skills/<name>` plus `origin: skills.sh:<owner>/<repo>@<skill>#<commit>`, which records the commit it was copied from.

A skill whose `source` is `./skills/<name>` is staged into the run's box. A plain `skills.sh:` source is never staged: the agent gets that skill only when the repository's checkout has it in `.claude/skills`.

An agent may load a skill only when its `skills` list names it. The worker offers the model only those skills, and not the skills that Claude Code bundles with it.

Limits, enforced when the catalog syncs a skill and again when it is read: at most 200 files and 1 MiB per skill folder, and paths of up to 200 characters with no hidden segments. `skills[].name` must be a slug. Letters, digits, dots, dashes and underscores are the rule for the owner, repo and skill parts of a `skills.sh:` source.

Skills from skills.sh are kept in the bucket's catalog. `goal catalog skills sync` fills it from a list of sources, and `goal catalog skills list` shows what it holds.

## Rules

A rule is a Markdown file under `rules/` in the definition, with frontmatter. Only a one-line `description` is required; `appliesTo` and `paths` are optional. The definition's `rules` list names each file, and any `.md` file under `rules/` works.

- `appliesTo` lists the agents, stations, lanes and work types the rule targets. A list left out matches every run. A run gets a rule when every list given contains the run's value.
- `paths` are globs for the files an agent works on, in Claude Code's path scoping. Left out, the rule covers every file.

The worker writes the rules a run gets into the agent's user-level configuration, never into the repository, so a rule cannot be committed. Rules are always on, so a rule that is too broad affects every matching run. Keep each one to the guidance it needs.

## Tools that ask first

An agent's MCP server tools can be `allow`, `ask` or `off`. The strictest of three settings wins: the mode in the agent's environment file (see [access and secrets](/docs/concepts/access-and-secrets/)), the `mcp_tool_modes` cap in the [organization policy](/docs/concepts/organization-policy/), and the agent's own tool lists.

The agent's `tools.allow` and `tools.deny` rules name a server's tools as `<server>__<pattern>`, such as `github__merge_*`. A deny rule that matches a tool removes it, an allow list for a server keeps only the tools it matches, and then the tool's mode applies. A denied tool never asks first.

An `ask` call does not run. The run ends, and the item waits for a person:

1. The person sees the call, with its input and the agent's role, and chooses Approve or Deny. Deny can carry a note that the next run reads.
2. Approve runs the call once, in the next run. Deny sends the item back with the denial in its brief.
3. Approve with Always allow also records a standing rule (below). Always allow is offered only when the tool's mode in the agent's environment file is `ask`. When the ask comes from somewhere else, such as the policy's cap, a standing rule would lift nothing, so the call can only be approved once.

The built-in tools do not ask first. `builtins` set to `ask` is served as `off` for them.

## Standing rules

A standing rule lets one agent role call one tool of one MCP server without asking. It is created only by Always allow on a tool approval, and it lasts until someone revokes it. The rule covers the role, the tool name, and the server in the environment file the agent runs with (`mcp.<server>` in `environments/<name>.yaml`), in that factory only. The same server in another environment or another factory still asks.

A standing rule only turns `ask` into allowed for its role. A tool set to `off` stays off.

Approving needs `item:decide`. Always allow and Revoke need `standing_rule:manage`: owners and admins, signed in within the last 10 minutes, never with an API key or a personal token. Connection-keyed standing rules were removed with connections.

- The rules of a server show under its tools in the environment's MCP card (Definition, Environments). A person with `standing_rule:manage` can revoke them there, after signing in again if their last sign-in is older than 10 minutes.
- `goal standing-rules list` shows the organization's rules, and `--revoked` adds the revoked ones.
- `goal standing-rules revoke <rule>` makes the agent ask again for that tool.

### Scopes for keys and tokens

An API key or personal token can carry `standing_rule:read`, so it can list standing rules. It can never carry `standing_rule:manage`, so creating or revoking a rule always needs a person signed in recently. Members can read; owners and admins can manage.

## Factory rules and repository rules

A run's repository may have its own rules: `CLAUDE.md`, `AGENTS.md` and `.claude/rules`. When a factory rule and a repository rule conflict, the factory rule wins inside that factory's runs. The agent is told to say so in its output and to name both rules.

## Where you meet them

- Skills and rules are files in the factory definition. The builder edits them.
- Tool approvals appear with the item that waits for them, and the Access page shows the tools they concern.
- `goal catalog skills` and `goal standing-rules` are the CLI commands. The [reference](/docs/reference/) lists their flags.

The decision behind tool approvals and standing rules is ADR 0031, `docs/adr/0031-tool-calls-ask-first-and-standing-rules.md` in the repository.
