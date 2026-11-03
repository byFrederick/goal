---
title: Access and secrets
description: Where an organization keeps its keys, how an environment file says what an agent's box reaches, and what a run receives.
sidebar:
  order: 22
---

A secret is where a key lives: a token, an API key or a password, kept sealed in goald. An environment file says what an agent's box installs and which secrets and servers it can reach. The Access page shows the result for every factory. Secret values are write-only, and they reach a run only in that run's box, never in a prompt, a log, an API answer or a commit.

## Secrets

A secret is an organization's named value. It has:

- a name of 3 to 40 lowercase letters, digits and inner dashes, starting with a letter, such as `github-token`. The name is unique in the organization;
- a sealed value, which nothing returns. Replacing it is the only way to change it;
- an optional description, an optional expiry and a reminder of 1 to 90 days before it;
- a scope: all factories, including ones created later, or a list of factories.

The Secrets page lists them with where each is used, when it was last used and what expires soon. **New secret** opens a side sheet, and **Import** reads a `.env` or YAML file and checks it before anything is saved. A secret's page shows its scope, its expiry and its uses, and has Replace value and Delete. Delete is disabled, with the reason, while something uses the secret: a factory's current published version that refers to it as its repository token, its model key, a trigger's model-agrees key or from an environment file, a knowledge source that reads with it, or a connection that refers to it. An archived factory doesn't count. Changing a secret needs an owner or admin who has signed in within the last 10 minutes. Everyone in the organization can read the list, never a value.

Values come in only by pasting them, through standard input or from an import file, never on the command line, and no command prints one.

## Environment files

An environment is a file in the factory's definition, `environments/<name>.yaml`. It says what an agent's box installs and what it can reach. Every factory has `environments/default.yaml`. An agent names another with `environment: <name>` in its definition, and an agent without one gets `default`. On an agent's page, the Environment select does that.

```yaml
apiVersion: goal/v1
kind: Environment
metadata: {name: default}
spec:
  size: medium                     # small | medium | large
  tools: [go@1.26, node@22, gh@2]  # mise specs, after the repository's own mise.toml
  setup:                           # run in order after clone and tools
    - pnpm install --frozen-lockfile
  env:                             # what CLIs and API calls read
    GH_TOKEN: "{{github-token}}"
  files:                           # written before setup, mode 0600
    ~/.kube/config: "{{kube-staging}}"
  mcp:
    linear:
      url: https://mcp.linear.app/mcp
      headers: {Authorization: "Bearer {{linear-key}}"}
      tools: {create_comment: ask, "delete_*": off}
```

The `{{name}}` placeholder names a secret. It is valid in an environment file's `env`, `files` and `mcp`, in `runtime.repository.token`, in `runtime.modelKey` and in a trigger's `modelAgrees.key`, and nowhere else. Publishing checks that each secret exists and is open to the factory. Each MCP server's name is 3 to 40 lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit, so `playwright` publishes and `play_wright` does not. Each MCP server's tools are `allow`, `ask` or `off`; a tool with no mode is `allow`, and the most specific match wins. See [skills and rules](../skills-and-rules/#tools-that-ask-first).

An environment can set `image` to run its box from an image of your own. A box on Cloudflare runs only the Worker's own images, so a run there fails on it. Publishing shows a warning at `image` and still publishes, since a box on Docker Compose runs it.

In the Definition, the **Environments** section has a tab per environment, and **Add a tool** adds an MCP server, a command-line tool or an API from a catalog, or a custom one, to the draft. You pick a secret or create one in place, and the page shows the lines it adds before you add them. The same file is under `environments/` in the YAML view. Remote MCP servers are tested with the draft's secrets, and the result is shown, never required to publish.

## The repository and the model

Two secrets are named in the factory's runtime, not in an environment file:

- `runtime.repository` is `{url, token}`. The token clones, pushes and reads and merges pull requests. There is no app to install: the token's own scope is the limit, so a fine-grained token for one repository is the right choice. The host picks the code host: `github.com` is GitHub and `gitlab.com` is GitLab. Any other host needs `forge: github` or `forge: gitlab`; publishing refuses a version without it, and so does a merge.
- `runtime.modelKey` is the secret that gives every run its model. A value that starts with `sk-ant-oat` is a Claude subscription token and a run gets it as `CLAUDE_CODE_OAUTH_TOKEN`. Any other value is an Anthropic API key and becomes `ANTHROPIC_API_KEY`.

Neither reaches the agent's environment unless an environment file names it, for example as `GH_TOKEN` for `gh`. Both are set on the factory's Settings page (see [Factory settings](../../guides/factory-settings/)).

The repository token signs git in to the repository's host for three things: the clone, the push of a new base branch (an assembly's integration branch), and the agent's own `git push`. Git signs in as `x-access-token` on GitHub and as `oauth2` on GitLab, with the token as the password. Goal keeps the token in git's credential store inside the run's box, outside the workspace, so the agent's push works without the token in its environment. The box and its store are thrown away after the run. The agent's prompt says that git signs in with the repository token. A public repository needs no token: it clones without a login.

goald reads and merges pull requests with the same token: a person's merge, a gate's merge on approval, the landing pull request of an integration branch, and the reads of a part's pull request base and of the default branch's head and history that an item's base commit and its "changed since your plan" note come from. A pull request on a host other than the repository's is refused before the token is opened, and so is a merge for a repository with no token. Each of these opens the token and writes one `secret.used` audit entry for the factory, with no run.

## What a run receives

1. The run starts the agent's environment from the version the run is pinned to. A secret you replace applies to runs that start afterwards.
2. goald checks that every referenced secret is open to the factory, then opens each one.
3. It puts each `env` value, each file's content and each MCP secret in the box's environment, and renders the MCP configuration. A header that names a secret reaches Claude Code as a variable the box expands, so the run's bundle never holds a value.
4. Every opened value goes on the run's scrub list, and one `secret.used` audit entry is written per secret per run, without a value.
5. The box clones the repository and installs its tools, then the environment's `tools`.
6. Before the agent starts, the box writes each of the `files` at its path, `~/` being the agent's home, with mode 0600. Then it runs the `setup` commands in order in the checkout, each with a 30-minute limit. A command that exits non-zero or runs past its limit fails the run, and the run's error names the command and shows its last 50 lines of output with secret values removed. Setup runs again when a run is retried in the same box, so write commands that are safe to repeat.
7. The agent gets the environment's MCP servers. The prompt has a **Your box** section that lists the box's command-line tools, the names of its variables, its file paths and its MCP servers, never a value. It takes the place of the Connections section that runs on connections get.
8. Each call to an MCP server's tool follows the environment's mode for it: `allow` runs, `ask` waits for a person, and `off` is refused. The worker checks the agent's other tool calls against its rules (see [skills and rules](../skills-and-rules/)).

An agent holds the secrets of its environment for the whole run, and it has network access from the box. A prompt-injected agent can misuse what its tokens reach. The control for that is a narrow token, with only the repositories and permissions the agent needs. The organization policy's **Use secrets** can switch an agent's environment off: it then gets no `env`, `files` or MCP servers, while the repository and model keys still reach the run. ADR 0008 and ADR 0037 (`docs/adr/` in the repository) record these decisions and their accepted risks.

## Access

Access is read only. It shows every factory's environments, the tools each reaches, the secret each uses, the last test and what is about to expire. Switch the view between By what they reach, By factory and By secret. A row opens the environment in the factory's definition, where you change it. It is the page to answer "what can this agent reach?" and "where does this key go?".

## From the command line

- `goal secret list`, `show`, `create`, `replace`, `delete` and `import` manage the organization's secrets. A value comes only from stdin or the import file and is never shown. `goal credential` and `goal connection` moved: each prints where its commands went and exits 2.
- `goal repository test <url> --secret <name>` checks, before any factory exists, whether the secret's token can read a repository on github.com or gitlab.com and push to it. goald asks the code host's API, never git, and prints each check with the host's reason. A host that gives no answer within 10 seconds answers "couldn't test". For a classic GitHub token, push ok reflects the account's role rather than the token's scopes; a fine-grained token answers its own grant. Owners and admins can run it, and the secret must be open to all factories. The API route is `POST /v1/orgs/{org}/repositories/test`.

Flags and output are in the [CLI reference](../../reference/).

## Moving over from connections

Organizations that used connections, kinds and credentials were moved to secrets and environment files once, when goald started, with no person needed ([ADR 0037](https://github.com/byFrederick/goal/blob/main/docs/adr/0037-secrets-and-environment-files.md), decision 19). For each organization it:

1. Makes each sealed connection field a secret open to every factory. The secret is named after the connection, or `<connection>-<field>` when the kind has several secret fields. A field that referred to a credential uses that credential, which is a secret already. A secret that already exists by name is left as it is.
2. Converts each factory's current version into a new version, published by Goal with the message "Moved connections to environments (ADR 0037)":
   - the factory's connections become `environments/default.yaml`, rendered from their kinds: the kind's variables, its command-line tool and its MCP server, with each secret field as a `{{secret}}` reference;
   - an agent whose connections differ from the factory's gets `environments/<agent>.yaml` and `environment: <agent>`;
   - `runtime.size` and `runtime.tools` move into every environment file;
   - `runtime.repository.connection` becomes `runtime.repository.token`, `runtime.modelConnection` becomes `runtime.modelKey`, and a trigger's `modelAgrees.connection` becomes `modelAgrees.key`;
   - each tool mode set on a connection, other than `allow`, becomes that server's `tools:` entry.
3. Tests the converted version before it publishes it. The repository token must be able to read `runtime.repository.url` and push to it: goald asks the code host's API, as `goal repository test` does, since it runs no git. Each remote MCP server in each environment file must answer `tools/list` with its headers' secrets filled in, as the environment test does. A check that fails, or gets no answer within 10 seconds, publishes nothing.
4. Reopens a draft that was open on the new version, converted, for its author. A draft that does not convert is reopened as it was, and publishing it shows what to fix.

It also points each synced knowledge source still on a connection at the secret its connection's token became, and copies the reader and the connection's other values into the source's config ([Knowledge](../knowledge/)).

Running items keep the version they started on. goald logs one `moved over` line per organization, with how many factories it converted and refused, how many secrets it created and how many knowledge sources it moved. A knowledge source it can't move gets its own `knowledge source not moved over` line with the reason. Running it again changes nothing for a factory it converted.

A factory Goal cannot convert keeps its version and runs on connections. Needs you shows a conversion entry for it that names each problem, for example "connection aws-prod uses the aws kind, which Goal no longer converts: remove it from the factory". Goal refuses connections of the `aws` and `bedrock` kinds, and custom kinds that start a command with no fields. A converted version that fails its test is refused the same way, and its entry names what to fix: the repository and whether the token can't read it or can't push to it, with the host's reason, or the environment file, the MCP server and its error. No problem shows a secret's value. Each time goald starts, it tries a refused factory again, so fixing the token's access or the server is enough: the next start publishes the converted version once. The entry goes away once the factory publishes a version with environment files.
