---

title: Organizations
description: What an organization holds, who can create one, and how the CLI picks the one it acts in.
sidebar:
  order: 3
---

An organization holds your factories, their work items and runs, the secrets and environments they use, and the people who work in them. Everything in Goal belongs to one organization. The CLI acts in one organization at a time.

## Who can create one

Any person signed in with a session can create an organization, and becomes its owner. That covers a browser sign-in and the CLI's device sign-in. Onboarding's first step does the same thing in the web app.

```sh
goal org create acme --name "Acme"
```

The slug (`acme`) is what the CLI uses to name the organization. To rename it, cap the agents that run at once, or delete it, use `goal org update` and `goal org delete`. The [CLI reference](/docs/reference/) lists their flags.

The CLI's help text for `goal org create` says platform admins only. That text is out of date. The permission in `contracts/api/permissions.yaml` (`org:create`) lets any signed-in person create one, and the decision is recorded in [ADR 0032](https://github.com/byFrederick/goal/blob/main/docs/adr/0032-self-serve-onboarding.md). These pages follow the permission.

## Roles

An organization's roles are owner, admin and member. The person who creates an organization becomes its owner. To join, a person needs an invitation, which can name the factories and the roles they get. An invitation arrives by email and expires after 72 hours. The person accepts it on the invitation page after they [sign in](/docs/getting-started/sign-in/).

## Code hosts

An organization lists the code hosts its repositories live on. github.com (GitHub) and gitlab.com (GitLab) are built in: every organization lists them, and nobody adds or removes them. An owner adds a GitHub Enterprise or self-managed GitLab host, with its kind: `github` or `gitlab`.

In the app, the list is under **Code hosts** in the organization's Settings, between Notifications and Data. Every member sees it. Only an owner sees the **Add a host** form, with the host name and its kind (GitHub or GitLab), and a **Remove** button on each added host. The built-in hosts have no Remove. Saving needs a sign-in from the last 10 minutes: if yours is older, Goal asks you to confirm it's you, then saves.

Any member, and any API key or personal token with `org:read`, can read the list with `GET /v1/orgs/{org}/code-hosts`. The built-in hosts come first, then the added ones sorted by name.

An owner sets the added hosts with `PUT /v1/orgs/{org}/code-hosts`, in a session signed in within the last 10 minutes. API keys and personal tokens can't set them. The body lists every added host, so a host left out is removed and an empty list removes them all:

```json
{ "hosts": [{ "host": "ghe.example.com", "kind": "github" }, { "host": "gitlab.example.com", "kind": "gitlab" }] }
```

A host is a name such as `ghe.example.com`, with no `https://`, port or path. It must be reachable from the internet, because goald connects only to public addresses. goald refuses an IP address, `localhost`, a name with no dot, and a name ending in `.local`, `.internal` or `.localhost`. It also refuses github.com and gitlab.com (`code-host-built-in`), an unknown kind, a host listed twice and more than 20 hosts (`code-host-invalid`). Each change is in the audit log as `org.code_hosts_set`.

Each host in the answer has the API base goald derives for it:

| Host | Kind | API base |
| --- | --- | --- |
| `github.com` | `github` | `https://api.github.com` |
| `<name>.ghe.com` (GitHub Enterprise Cloud) | `github` | `https://api.<name>.ghe.com` |
| any other GitHub host (GitHub Enterprise Server) | `github` | `https://<host>/api/v3` |
| any GitLab host | `gitlab` | `https://<host>/api/v4` |

When goald first started with the list, it added, once, the hosts the organization's factories and knowledge sources already used: the host of every published version's `runtime.repository`, with its `forge`, and the host of every knowledge repository source, with its `code_host`. That entry is in the audit log as `org.code_hosts_set` by Goal. A host it couldn't add, such as one with a port or with no kind, is in goald's log as `code host not seeded` with the reason. goald doesn't add hosts again, so a host an owner removes stays removed, and a factory published later on a new host needs an owner to add that host.

To use a listed host in a factory, follow [Connect GitHub](/docs/guides/connect-github/#add-a-code-host-for-enterprise-or-self-managed-gitlab) and [Connect code to a factory](/docs/guides/connect-code/).

goald sends repository tokens and knowledge secrets only to hosts on the list. Publishing refuses a version whose repository is on another host. The repository test answers `repository-host-unknown` for another host without opening the secret. A knowledge source that reads a repository with a secret fails its sync on another host, with an error that names the host, and its secret is never opened. On a listed host, the kind on the list picks the API goald calls.

## Pick the organization the CLI acts in

The CLI picks an organization in this order, and the first one it finds wins:

1. The `--org` flag on a command.
2. The `GOAL_ORG` environment variable.
3. The organization set by `goal org use`, which the CLI keeps in its config file.
4. If none of those is set, the install's only organization.

A credential overrides this order. With `GOAL_API_KEY` set, or a key or personal token stored by `goal login --token`, commands act in that credential's own organization.

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
