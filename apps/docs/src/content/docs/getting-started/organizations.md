---

title: Organizations
description: What an organization holds, who can create one, and how the CLI picks the one it acts in.
sidebar:
  order: 3
---

An organization holds your factories, their work items and runs, the connections they use, and the people who work in them. Everything in Goal belongs to one organization. The CLI acts in one organization at a time.

## Who can create one

Any person signed in with a session can create an organization, and becomes its owner. That covers a browser sign-in and the CLI's device sign-in. Onboarding's first step does the same thing in the web app.

```sh
goal org create acme --name "Acme"
```

The slug (`acme`) is what the CLI uses to name the organization. To rename it, cap the agents that run at once, or delete it, use `goal org update` and `goal org delete`. The [CLI reference](/docs/reference/) lists their flags.

The CLI's help text for `goal org create` says platform admins only. That text is out of date. The permission in `contracts/api/permissions.yaml` (`org:create`) lets any signed-in person create one, and the decision is recorded in [ADR 0032](https://github.com/byFrederick/goal/blob/main/docs/adr/0032-self-serve-onboarding.md). These pages follow the permission.

## Roles

An organization's roles are owner, admin and member. The person who creates an organization becomes its owner. To join, a person needs an invitation, which can name the factories and the roles they get. An invitation arrives by email and expires after 72 hours. The person accepts it on the invitation page after they [sign in](/docs/getting-started/sign-in/).

## Pick the organization the CLI acts in

The CLI picks an organization in this order, and the first one it finds wins:

1. The `--org` flag on a command.
2. The `GOAL_ORG` environment variable.
3. The organization set by `goal org use`, which the CLI keeps in its config file.

To see the organizations you can reach, and which one is current:

```sh
goal org list
```

The current one is marked with `*`. To make another one current:

```sh
goal org use acme
```

This changes the default for every later command on this machine. A script can pass `--org` or set `GOAL_ORG` instead, and leave the config file alone.

## Check it

```sh
goal whoami
```

`goal whoami` names the account and the organizations it can reach. If the organization you expect is missing, you are not a member of it yet: accept the invitation or ask an owner to invite you.

## Next

Create the factory that will take work: [Create a factory](/docs/getting-started/create-a-factory/).
