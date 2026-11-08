---
title: Organization policy, slots and models
description: Set what every factory in an organization may do with goal policy, cap how many agents run at once with slots, and know where the install's run cap and model list come from.
---

An organization's limits sit at four levels. The organization's policy decides which models, tools and commands agents may use. Its slots cap how many agents run at once. The install's run cap and model list are set by whoever operates goald. For what an organization is and who can create one, see [Organizations](/docs/getting-started/organizations/).

| Setting | Scope | Who may change it |
| --- | --- | --- |
| Policy | One organization | Owners and admins (`org:manage`) |
| Slots | One organization and each of its factories | Owners and admins (`org:manage`) |
| `GOAL_MAX_RUNS` | The whole install | The operator of the install |
| `GOAL_MODELS` | The whole install | The operator of the install |

Any member can read the policy and the slots (`org:read`).

## The organization's policy

The policy is a ceiling over every factory in the organization. A factory or an agent can choose less than the policy allows, never more. It holds:

- the models agents may use, and the default model for an agent that names none,
- four tool defaults: writing outside the item's branch, merging pull requests, reading the web and using credentials,
- Claude Code Bash patterns no agent may run, such as `Bash(terraform apply:*)`,
- how long a box may run before it is stopped, in minutes.

Until someone saves one, the organization has the default policy at revision 0, built from the install's models.

**Where to find it.** In the web app, open **Models and policies** from the organization's menu. It also shows which factories and agents use each model and box size, the box sizes, and the addresses agents connect from (`GOAL_EGRESS_ADDRESSES`). Members without `org:manage` see the page read-only. In the CLI:

```sh
goal policy get
```

**How to change it.** In the app, edit the page and choose **Save changes**. In the CLI, `goal policy set` reads the policy, changes only what its flags name and saves it, so everything else stays as it was:

```sh
goal policy set --models claude-opus-5-5,claude-sonnet-5-5 --default claude-sonnet-5-5
goal policy set --deny-command 'Bash(terraform apply:*)' --box-limit 90m
```

`--models` and `--deny-command` replace their whole list, and `--deny-command` can be repeated. `--box-limit` is whole minutes or hours (`90m`, `3h`). Give at least one flag. Use `--org` to act in another organization.

A save carries the revision it read. If someone saved in between, goald answers 409 `policy-conflict` and changes nothing: read the policy again and repeat the change. A model that is not in the install's model list, a default that is not an allowed model, a blank denied command or a box limit that is not positive answers 422 `policy-invalid`. Each save is one `org_policy.updated` entry in the audit log.

## The organization's slots

Slots cap how many agents run at once. The organization has one cap shared by every factory, and each factory can have its own. No cap is the default, and a cap is a whole number from 1 to 1000.

**Where to find it.** In the app, open the organization's **Settings**. The **Slots** section has **Slots shared by every factory** and, under **Most slots each factory may use**, one field per factory. Leave a field blank for no limit. The factory caps may add up to more than the organization's cap.

**How to change it.** In the app, edit the fields and choose **Save changes**; only owners and admins can, and others see the form read-only. In the CLI, `--max-slots` sets the organization's cap:

```sh
goal org update acme --max-slots 8
goal org update acme --max-slots 0
```

`--max-slots 0` removes the cap. The CLI sets only the organization's cap; set a factory's cap in the app, or with `PATCH /v1/orgs/{org}/settings` (`slots.factories`). A cap outside 1 to 1000 is refused.

## The install's run cap: GOAL_MAX_RUNS

`GOAL_MAX_RUNS` caps the agent runs one goald has in flight at once. It defaults to 0, which means no cap. With a cap, goald shares claims fairly across organizations, so one busy organization cannot take every run. Slots limit one organization inside this cap; they do not raise it.

**Where to find it.** It is an environment variable of goald, listed in the [environment reference](/docs/reference/environment/). Nothing in the app or the CLI shows it.

**How to use it.** The operator of the install sets it in goald's environment and restarts goald. Owners and admins of an organization cannot change it. The [self-hosting guide](/docs/guides/self-host-with-docker-compose/) shows where goald's environment lives.

## The install's model list: GOAL_MODELS

`GOAL_MODELS` lists the models the install offers: comma-separated model ids, each optionally `id=Label`, such as `claude-sonnet-5-5=Sonnet`. The first is the default. Unset, goald offers its built-in list of current Claude models.

**Where to find it.** It is an environment variable of goald, listed in the [environment reference](/docs/reference/environment/). Its models are the ones the factory editor offers, and the ones **Models and policies** and `goal policy get` list as allowed or not allowed.

**How to use it.** The operator of the install sets it and restarts goald. An organization's policy picks from this list: saving a model that is not on it is refused with `policy-invalid`. An organization's owner or admin cannot add a model; ask the operator.

## See also

- [`goal policy` and `goal org update`](/docs/reference/cli/) in the CLI reference.
- [Factory settings](/docs/guides/factory-settings/), where a factory's own settings live.
