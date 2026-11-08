---
title: Organization policy
description: The one set of limits every agent in an organization runs under, what each setting does to a run, and where a person changes it.
sidebar:
  order: 26
---

The organization policy is one set of limits per organization. It is a ceiling over every factory definition: a definition or an agent may narrow it and never widen it. Until someone saves a policy, the organization has the default one: every model of the install allowed and the first the default, no denied commands, and the tool settings below at their defaults, no MCP tool mode cap and a 3-hour box time limit (revision 0).

The engine reads the policy once each time an agent claims an item, so a change applies to the next run, not to one already running.

## Where to find and change it

Open **Models and policies** in the organization's navigation (`/o/{org}/models`). Every member can read it. Changing it takes the `org:manage` permission, which owners and admins hold; for anyone else Save changes stays disabled.

The screen has these cards: **Models agents may use**, **Commands no agent may run**, **Tool policy**, **Box sizes** (which holds the box time limit), **Where agents connect from** and a Harness choice. Save changes saves the whole policy at once.

Two other ways reach the same policy:

- `GET` and `PUT /v1/orgs/{org}/policy`. The `GET` answer carries an ETag, the policy's revision. The `PUT` replaces the policy and must send it as `If-Match`; a stale revision is refused with 409, so a change made in between is never lost. See the [API reference](/docs/reference/).
- `goal policy get` prints the policy, and `goal policy set` changes the models (`--models`, `--default`), the denied commands (`--deny-command`) and the box limit (`--box-limit`), leaving everything else as it was read. The tool settings and the MCP cap are not flags.

## Allowed and default models

The policy lists the models agents may use, a non-empty subset of the install's models, and one of them as the default. An agent runs on its own `model`, else the factory's `runtime.model`, else the policy's default model.

Use the **Models agents may use** card to switch models on and off and to choose the default. The last allowed model cannot be switched off.

When the run's model is not on the list, the engine does not start the run. The item waits at its station for a person, with the reason in its events: the policy no longer allows the model, so choose an allowed one for the agent or allow it in the policy. Publishing a definition checks the same thing, with the check "Every model it names is one the organization allows".

## Denied commands

Denied commands are Claude Code Bash permission patterns, such as `Bash(terraform apply:*)`, that no agent in the organization may run. The engine adds each to the run's deny rules, next to the rules the agent's own definition sets.

Use the **Commands no agent may run** card to add and remove patterns, or `goal policy set --deny-command`.

## Tool settings

The **Tool policy** card has four settings. Each is a default that narrows what an agent may do; none widens it.

- **Read the web and docs** (`read_web`): `off`, `ask` or `allowed`. With `off` or `ask`, the engine denies the `WebFetch` and `WebSearch` tools. `ask` behaves as `off`, because no tool approval is enforced for these tools.
- **Merge a pull request** (`merge_pull_request`): `off` or `review_approval`. With `off`, an approving gate whose action is merge does not merge, and the item waits at the gate for a person to merge. With `review_approval`, only a review station with merge on approval merges, as its definition says.
- **Use secrets** (`use_credentials`): `off` or `environment`. With `off`, the agent gets no connections, environment `env`, `files` or MCP servers. The model key and the repository access still reach the run, which cannot start without them. With `environment`, the agent gets what its environment names; see [access and secrets](/docs/concepts/access-and-secrets/).
- **Write outside the item's branch** (`write_outside_branch`): `off`, `ask` or `allowed`. The policy stores it and the screen shows it, but the engine does not apply it yet: comments, issues, tickets and messages go through connections, and no step can ask a person for them, refuse them or allow them.

## MCP tool mode cap

`mcp_tool_modes` caps the mode of MCP tools in every run. It maps a tool name or glob (`*`, `?`) to `allow`, `ask` or `off`, such as `{"*": "ask"}`, and the stricter of a tool's mode and the cap wins. An empty cap caps nothing.

The Models and policies screen has no control for it. Set it with `PUT /v1/orgs/{org}/policy`; a body that leaves the field out keeps the stored cap. [Access and secrets](/docs/concepts/access-and-secrets/) explains how the cap and an environment's modes combine, and the warning that publishing shows.

## Box time limit

The box time limit is how long a box may run before the engine stops it, in minutes, and the default is 180. A run's timeout is the smaller of the engine's task timeout and this limit.

Change it in the hours field of the **Box sizes** card, or with `goal policy set --box-limit 90m`. It must be above zero.
