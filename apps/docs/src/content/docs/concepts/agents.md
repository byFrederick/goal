---
title: Agents
description: What an agent is in a factory, how a definition declares one, and what happens in its box when it runs.
sidebar:
  order: 20
---

An agent is a role in a factory: a prompt, a model, the skills and connections it may use, and the tools it may call. A station gives its items to an agent, and each run starts that agent in its own box, a container that is created for the run and deleted after it.

## Where an agent is declared

An agent is one entry of the `agents` list in the factory definition. Its schema is `contracts/definitions/factory.schema.json` in the repository, and the [reference](../../reference/) lists the fields.

| Field | What it sets |
| --- | --- |
| `name` | The role's name, a slug. Events, tool approvals and standing rules name the agent by this name. |
| `prompt` | The agent's instructions, sent to the model at the start of every run. |
| `model` | The Claude model the agent uses. When it is left out, the factory's `runtime.model` applies. |
| `maxTurns`, `perItemUsd` | Limits for this agent. They override the factory's `runtime.budget` values. |
| `skills` | The names of the definition's skills this agent may load. See [skills and rules](../skills-and-rules/). |
| `connections` | The organization's connections this agent may use. See [connections](../connections/). |
| `tools` or `builtins` | Which tools the agent may call. An agent has one or the other, never both. |

`builtins` sets each group of Claude Code's built-in tools to `off`, `ask` or `allowed`:

- `read` (Read, Grep, Glob), `edit` (Edit, Write), `run` (Bash, optionally limited to named commands), `web` (WebFetch, WebSearch) and `subagents` (Task).
- `ask` is served as `off` for built-in tools. Only connection tools can ask a person first (see [skills and rules](../skills-and-rules/#tools-that-ask-first)).

`tools` takes custom allow and deny lists in Claude Code's permission syntax. Deny always wins. Rules for a connection's tools use the form `<connection>__<tool>`.

## Role and slot

A station can run several copies of its agent in parallel. Each copy has a slot, a number from the station's capacity. The **role** is the agent's name in the definition, and it is the name people see. Approvals, standing rules and the run list use the role, never the slot number.

## What happens when a run starts

1. goald gives the run a box from the base image `goal-agent`, one container per run.
2. The box's `prepare` step fetches the run bundle from the gateway with the run's task token. It clones the repository on the item's branch, installs tools with mise, and writes the factory's rules into the agent's user-level configuration, outside the repository, so they cannot be committed.
3. The worker starts the agent with the task's prompt, the skills the agent may load, its connections and its tool policy.
4. Each note a person leaves on the item while the run works is passed to the agent as the run continues.
5. The run ends with exactly one `finished` event, and the item moves on or waits. A run that needs a person ends early: see [questions](../questions/) and [tool approvals](../skills-and-rules/#tools-that-ask-first).

The worker source is in `apps/worker/src`. `agent.ts` builds the agent's options and its permission checks, `run.ts` runs one task, and `prepare.ts` sets the box up.

## Where you meet agents

- The factory builder edits the `agents` list. Its draft is published as a new version.
- `goal runs` lists the organization's agent runs, with the factory, item, station, agent, model and cost of each. The [CLI reference](../../reference/) lists its flags.
- `goal spend` reads what the agent runs cost. [Jonah](../jonah/) can compare an agent's models in a bake-off.

For the words used here, see the [glossary](../../reference/).
