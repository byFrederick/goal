---
title: Connect code to a factory
description: List the repositories a factory's agents may open, give each the access and token it needs, choose which agents write, and run a factory that only reads or has no code.
---

You end with a factory whose `runtime.repositories` lists the code it may use, a token of the right size for each entry, and agents that write only where you meant them to. Code is optional: a factory with no repositories works through its tools.

Before you start, the repository's host must be on the organization's list. github.com and gitlab.com are built in; for a GitHub Enterprise or self-managed GitLab host, follow [Add a code host](../connect-github/#add-a-code-host-for-enterprise-or-self-managed-gitlab). The entries, tokens and the rules that choose between them are defined in [Access and secrets](/docs/concepts/access-and-secrets/#repositories-and-what-agents-may-write); this guide shows the steps.

## Pick a token for each entry

Goal has no app to install. A token's own scope is the limit of what an agent can do, so give each entry the smallest token that does its job.

- For a `read` entry, create a fine-grained token on GitHub with read-only repository permissions: Contents and Metadata as read-only, plus Issues and Pull requests as read-only when agents use `gh` or the GitHub tools to read them. Limit it to the repositories you list, or to the organization's repositories for an owner pattern.
- For a `write` entry, add write access to Contents and Pull requests for the repositories that entry names, and nothing else.
- When a station waits for CI ([Wait for CI](/docs/concepts/stations-and-flow/#wait-for-ci)), goald reads the CI of the repository with the entry's token. On GitHub, the token then needs read access to Checks and Commit statuses as well. On GitLab, it needs the `read_api` scope and a role in the project that can see its pipelines. A factory with no station that waits for CI needs neither.
- Store each token as a secret and open it to the factory ([Access and secrets](/docs/concepts/access-and-secrets/)). One secret per access level keeps a write token away from agents that only read.
- A public repository you only read needs no token.

Test the token before you publish. The answer says what it can do, and warns when a `read` entry's token can also push:

```sh
goal repository test https://github.com/acme/app --secret github-acme-read --factory acme-line --access read
```

The test also checks whether the token can read the repository's CI, on its own `ci` line. A failed `ci` check doesn't fail the test, because only a station that waits for CI needs it; for such a factory, give the token the permission the line names. An owner pattern has no `ci` line, and neither does an answer Goal kept before it checked CI, until you test the repository again.

The same test is in the web app, in factory settings' Repository section. Goal keeps the answer, and `goal factory show acme-line` lists it with each repository.

## List one repository

Add the entry to `runtime.repositories` in the factory's definition ([Change a factory definition](../change-a-factory-definition/) shows how to edit and publish it):

```yaml
runtime:
  repositories:
    - name: app
      url: https://github.com/acme/app
      token: "{{github-acme}}"
      access: write
```

`name` is unique in the factory. `access: write` needs a `token`. With one entry that names one repository, the entry is the default: an item that names no repository works on it.

The older `runtime.repository` (`url` and `token`) is the shorthand for this same entry, with `access: write`. A definition uses one form or the other. The factory settings page edits the shorthand; see [Factory settings](../factory-settings/#repository).

## List several repositories

```yaml
runtime:
  repositories:
    - name: app
      url: https://github.com/acme/app
      token: "{{github-acme}}"
      access: write
      default: true
    - name: tools
      url: https://github.com/acme/tools
      token: "{{github-acme}}"
      access: write
```

`default: true` marks the entry an item uses when it names none. At most one entry has it, and it must name one repository. Items name their repositories when they are filed, with `goal run --repo` or a trigger's `create.repositories`: see [Name an item's repositories](../submit-and-follow-items/#name-an-items-repositories) and [Repositories of an item](../intake/#repositories-of-an-item). An item opens at most 10.

## List a whole organization

An owner pattern covers every repository of an owner, including ones created later:

```yaml
runtime:
  repositories:
    - name: acme-all
      url: https://github.com/acme/*
      token: "{{github-acme-read}}"
      access: read
```

An agent can clone any repository the pattern covers, into `/repos/<owner>/<name>` in its box. An item names a repository under a pattern as `owner/name`. A pattern can't be the default entry, so a factory with only a pattern has no default, and an item starts with no repository until it names one.

Entries can overlap. For each repository, the most specific entry wins, one repository before an owner pattern, so you can read all of `acme` with a read-only token and write to `acme/app` with another:

```yaml
runtime:
  repositories:
    - name: acme-all
      url: https://github.com/acme/*
      token: "{{github-acme-read}}"
      access: read
    - name: app
      url: https://github.com/acme/app
      token: "{{github-acme-app}}"
      access: write
      default: true
```

Test each entry with `goal repository test`. For a pattern, the answer counts the repositories the token can read.

## Choose which agents write

An agent's `code` is `write` or `read`, next to its `builtins`:

```yaml
agents:
  - name: investigator
    prompt: ./agents/investigator.md
    code: read
  - name: fixer
    prompt: ./agents/fixer.md
    code: write
```

An agent without `code` writes when the factory has a `write` entry and reads otherwise, so older definitions keep working. An agent that only reads takes a `read` entry's token whenever one covers the repository. Goal never pushes, opens a pull request or merges for it, and a push it tries from a repository it may not write fails with a message. Publishing warns with `reader-write-token` when such an agent would hold a `write` entry's token because no `read` entry covers the repository: add one.

## Let one lane write the fix

An investigation does not need a second factory to fix what it finds. The investigator reads, sets a lane in its output, and the route sends that lane to an agent that writes. With `board.flow.outputs.repositories` the investigator can also name the repositories the fix needs; see [Adding repositories](https://github.com/byFrederick/goal/blob/main/docs/engineering/factory-definition.md#adding-repositories-boardflowoutputsrepositories).

```yaml
board:
  stations:
    - id: investigate
      kind: agent
      agent: investigator
    - id: implement
      kind: agent
      route:
        fix: fixer
```

The investigator's output sets `lane: fix`, so the `implement` station (its `id`) sends the item to `fixer`, whose `code` is `write`. The names are samples; lanes are explained in [Lanes](/docs/concepts/lanes/). A line that merges, through a merge station or integration branches, needs a `write` entry and an agent that writes; publishing refuses one without. goald merges each pull request in its own repository with the token of the entry that covers it, and refuses a repository only a `read` entry covers.

## Run a team that only reads

For a team with read-only access to GitHub, list `read` entries only and keep every agent on `code: read`:

```yaml
runtime:
  repositories:
    - name: acme-all
      url: https://github.com/acme/*
      token: "{{github-acme-read}}"
      access: read
```

No `write` entry means no agent writes, and no station may merge. Give the agents the `github-read` tool template for issues, checks and pull request comments ([Connect GitHub](../connect-github/#give-agents-github-tools)). A check always only reads, whatever its agent's `code`.

## Run a factory with no code

Leave out `runtime.repository` and `runtime.repositories`. The agents' `/workspace` starts empty and they work through their tools: incidents, findings, messages and follow-ups need no repository. An item that names a repository is refused, because no entry covers it.

## Next

- [Connect GitHub](../connect-github/): add the webhook that sends events, the GitHub tools and a code host.
- [Access and secrets](/docs/concepts/access-and-secrets/): the rules behind entries and tokens.
- [Integration branches](/docs/concepts/integration-branches/): how an assembly lands changes in each repository.
