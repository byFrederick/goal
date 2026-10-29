---
title: Skills, rules and standing rules
description: The three kinds of instruction a factory gives its agents, what each one does, and which name means which.
sidebar:
  order: 21
---

A skill is a folder of instructions that an agent loads when it needs them. A rule is guidance that is always on for the runs it targets. A standing rule is a permission that a person grants with "Always allow", so an agent can call one connection tool without asking. Goal uses "rule" only for the first kind, and "standing rule" only for the third.

## Skills

A skill is a folder with a `SKILL.md` file at its root, and optional files beside it. The definition's `skills` list names each one and says where it comes from:

- `./skills/<name>`: a folder in the definition itself.
- `skills.sh:<owner>/<repo>@<skill>`: a skill from skills.sh. The lock file pins the commit it was copied from, and the copy is stored in the definition as `./skills/<name>`.

An agent may load a skill only when its `skills` list names it. The worker offers the model only those skills, and not the skills that Claude Code bundles with it.

Limits, enforced when the catalog syncs a skill and again when it is read: at most 200 files and 1 MiB per skill folder, and paths of up to 200 characters with no hidden segments. Skill names use letters, digits, dots, dashes and underscores.

Skills from skills.sh are kept in the bucket's catalog. `goal catalog skills sync` fills it from a list of sources, and `goal catalog skills list` shows what it holds.

## Rules

A rule is a Markdown file under `rules/` in the definition, with frontmatter that holds a `description`, an `appliesTo` block and optional `paths`. The definition's `rules` list names each file, and its path must be `./rules/<name>.md`.

- `appliesTo` lists the agents, stations, lanes and work types the rule targets. A list left out matches every run. A run gets a rule when every list given contains the run's value.
- `paths` are globs for the files an agent works on, in Claude Code's path scoping. Left out, the rule covers every file.

The worker writes the rules a run gets into the agent's user-level configuration, never into the repository, so a rule cannot be committed. Rules are always on, so a rule that is too broad affects every matching run. Keep each one to the guidance it needs.

## Tools that ask first

An agent's connection tools can be `allow`, `ask` or `off` (the modes are set on the connection, see [connections](../connections/)). The strictest of the connection's mode, the factory's `toolRule` and the agent's own tool lists wins. An `ask` call does not run. The run ends, and the item waits for a person:

1. The person sees the call, with its input and the agent's role, and chooses Approve or Deny. Deny can carry a note that the next run reads.
2. Approve runs the call once, in the next run. Deny sends the item back with the denial in its brief.
3. Approve with Always allow also records a standing rule (below).

The built-in tools do not ask first. `builtins` set to `ask` is served as `off` for them.

## Standing rules

A standing rule lets one agent role call one tool of one organization connection without asking. It is created only by Always allow on a tool approval, and it covers the role, the connection and the tool name. It applies in every factory that uses that connection, until someone revokes it. Deleting the connection revokes its standing rules.

Approving with Always allow needs the permission to decide on the item and the permission to manage connections. A standing rule never turns a call that the factory's definition marks `approval: required` into an automatic one.

- `goal standing-rules list` shows the organization's rules, and `--revoked` adds the revoked ones.
- `goal standing-rules revoke <rule>` makes the agent ask again for that tool.

## Where you meet them

- Skills and rules are files in the factory definition. The builder edits them.
- Tool approvals appear with the item that waits for them, and the connection's page shows how many are open.
- `goal catalog skills` and `goal standing-rules` are the CLI commands. The [reference](../../reference/) lists their flags.

The decision behind tool approvals and standing rules is ADR 0031, `docs/adr/0031-tool-calls-ask-first-and-standing-rules.md` in the repository.
