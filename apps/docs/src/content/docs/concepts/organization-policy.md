---
title: Organization policy
description: "The organization policy limits every factory: the models agents may use, the commands they may not run, tool defaults and the box time limit. Where to find it, how to change it and what a run does when a model is not allowed."
sidebar:
  order: 26
---

The organization policy limits every factory in the organization. A factory's definition or an agent may narrow it and never widen it. This page covers its two best-known parts, the model allowlist and the denied commands. The policy also holds the tool policy defaults (writing outside the item's branch, merging pull requests, reading the web, using secrets), a cap on MCP tool modes and the box time limit.

An organization that never saved a policy has the default one, which allows the install's models. Every member can read the policy. Only owners and admins can change it.

## What the policy limits

- **Model allowlist.** The models agents may use: a non-empty subset of the models the install offers (`GOAL_MODELS`), with one of them as the default. An agent that names no model runs on the default.
- **Denied commands.** Claude Code Bash permission patterns, such as `Bash(terraform apply:*)`, that no agent in the organization may run. The engine adds them to the deny rules of every run.

## Where to find it

- **Web app:** the Models and policies screen of the organization.
- **CLI:** `goal policy get` prints the policy, who uses each model and box size, and the egress addresses. `goal policy set` changes what its flags name and keeps the rest: `--models`, `--default`, `--deny-command` and `--box-limit`. See the [CLI reference](/docs/reference/cli/).
- **API:** `GET /v1/orgs/{org}/policy` reads it, and `PUT /v1/orgs/{org}/policy` replaces it. Its operations are listed in the [reference](/docs/reference/).

## Change the policy

Save the policy on the revision you read. The API sends it as the `ETag`, and `PUT` needs it as `If-Match`. A save made after someone else changed the policy is refused with `409 policy-conflict`, and nothing changes. `--models` and `--deny-command` replace the whole list, not add to it. A model that is not one of the install's, a default that is not an allowed model, a blank denied command or a box time limit that is not positive is refused with `422 policy-invalid`.

## When a model is not allowed

- **Publishing:** a definition whose `spec.runtime.model` or an agent's `model` is not on the allowlist gets the error `model-not-allowed` at that key, naming the agent and the allowed models. Fix the model or allow it in the policy.
- **Running:** the engine checks the policy again when it claims an item. When the policy no longer allows the model an agent runs on, the run does not start. The item waits at its station with the reason in its event, which names the model, the agent and the allowed models. Choose an allowed model for the agent, or allow the model in the policy.

See also [Agents](/docs/concepts/agents/) and [Access and secrets](/docs/concepts/access-and-secrets/). The decision behind the policy is ADR 0024.
