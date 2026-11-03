---
title: Connections
description: How an organization signs in to outside systems, how its secrets are kept, and how a run's box gets only what its agents need.
sidebar:
  order: 22
---

A connection is an organization's sign-in to an outside system, such as a code host, a model provider or an MCP server. It is made from a connection kind, which says which fields the connection has, which of them are secret and what a run's box receives. Secret values are write-only and sealed in goald. They reach a run only as environment variables in that run's box, never in a prompt, a log, an API answer or a commit.

## Kinds and connections

A kind is a template, a YAML file of the shape in `contracts/definitions/connection-kind.schema.json`. Its `spec` declares:

- `fields`: the values an admin fills in. A field marked `secret` is write-only.
- `env`: the environment variables a run's box gets, with each one's value built from the fields.
- `mcp`, `cli`, `git`: an MCP server the agent calls, a tool installed at prepare, or a git login over https.
- `test`: the request that tells whether a connection works.
- `docs`: a short hint that is given to agents. It never holds a secret.

goald ships the platform's kinds in `internal/connkind/kinds/`, among them `github`, `gitlab`, `anthropic`, `aws` and `confluence`. An organization can add its own with `goal connection kind push`. A connection is an instance of a kind with its own values. Its name is what a factory's agents list.

Templates inside a kind are limited. `{{field}}` names one of the kind's fields. `${NAME}` in an MCP header is left for Claude Code to expand inside the box, so the secret is not in the run's bundle.

## Credentials

A secret field can hold its value directly, or refer to a credential. A credential is an organization's named secret, sealed in goald and never shown again. A connection's field refers to it with `{{name}}`, and the reference is stored in the clear. A credential cannot be deleted while a connection refers to it.

On the Credentials screen, the **New credential** menu in the top bar either creates one credential or imports many from a `.env` or YAML file. The import is checked before anything is saved. The search field next to it filters by name and description, and keeps its text in the page's address (`?q=`), so a link opens the same list.

Values come in only through standard input or an import file, never on the command line, and no command prints one. Changing a credential needs an owner or admin who has signed in within the last 10 minutes.

## What a run receives

1. A run's agents name the connections they may use. The factory's definition decides which.
2. When the box is built, goald resolves each secret and sets the kind's `env` variables in the box's environment.
3. The agent can use those variables from the box, for example through `gh` or an API client. Its MCP servers read their headers from the same environment.
4. The worker checks every connection tool call against the agent's rules before it runs (see [skills and rules](../skills-and-rules/)).

An agent holds its connections' credentials for the whole run, and it has network access from the box. A prompt-injected agent can misuse what its tokens reach. The control for that is a narrow token, with only the repositories and permissions the agent needs. ADR 0008 (`docs/adr/0008-portable-runs-connections-and-knowledge.md` in the repository) records this decision and its accepted risks.

## Tool modes

Each tool of an MCP connection is `allow`, `ask` or `off`. A tool with no mode is `allow`. `ask` pauses the call for a person, and `off` never runs it. The modes are set on the organization connection, and the stricter of the connection's mode and the factory's rule applies. See [skills and rules](../skills-and-rules/#tools-that-ask-first).

## Testing and managing connections

- `goal connection` moved: whatever follows it, it prints where connections went (Access with `goal access`, Secrets with `goal secret`, and the factory's environment files) and exits 2.
- `goal secret list`, `show`, `create`, `replace`, `delete` and `import` manage the organization's secrets, which a connection refers to as `{{name}}`. A value comes only from stdin or the import file and is never shown. `goal credential` moved to `goal secret`: it prints where and exits 2.

Flags and output are in the [CLI reference](../../reference/).

## Moving to secrets and environment files

Goal moves every organization from connections to secrets and environment files once, when goald starts, with no person needed ([ADR 0037](https://github.com/byFrederick/goal/blob/main/docs/adr/0037-secrets-and-environment-files.md), decision 19). For each organization it:

1. Makes each sealed connection field a secret open to every factory. The secret is named after the connection, or `<connection>-<field>` when the kind has several secret fields. A field that referred to a credential uses that credential, which is a secret already. A secret that already exists by name is left as it is.
2. Converts each factory's current version into a new version, published by Goal with the message "Moved connections to environments (ADR 0037)":
   - the factory's connections become `environments/default.yaml`, rendered from their kinds: the kind's variables, its command-line tool and its MCP server, with each secret field as a `{{secret}}` reference;
   - an agent whose connections differ from the factory's gets `environments/<agent>.yaml` and `environment: <agent>`;
   - `runtime.size` and `runtime.tools` move into every environment file;
   - `runtime.repository.connection` becomes `runtime.repository.token`, `runtime.modelConnection` becomes `runtime.modelKey`, and a trigger's `modelAgrees.connection` becomes `modelAgrees.key`;
   - each tool mode set on a connection, other than `allow`, becomes that server's `tools:` entry.
3. Reopens a draft that was open on the new version, converted, for its author. A draft that does not convert is reopened as it was, and publishing it shows what to fix.

Running items keep the version they started on. goald logs one `moved over` line per organization, with how many factories it converted and refused and how many secrets it created. Running it again changes nothing.

A factory Goal cannot convert keeps its version and runs on connections. Needs you shows a conversion entry for it that names each problem, for example "connection aws-prod uses the aws kind, which Goal no longer converts: remove it from the factory". Goal refuses connections of the `aws` and `bedrock` kinds, and custom kinds that start a command with no fields. The entry goes away once the factory publishes a version with environment files.

## Where you meet connections

The connections screen lists each connection with its kind, its status and the factories that use it. A connection that works shows a green dot and “Connected”; one that failed its test or lacks a secret shows a red dot and says so. Under Used by, each factory appears as its letter badge, and knowledge sources keep their name. Search connections, in the top bar, narrows the list by name, description or kind; the screen has no kind or factory filter. The factory builder picks connections for an agent from this list.

A connection's page has a Test button in the top bar. It runs the connection's kind's test, and runs it again whenever you press it. The result shows in the page, under the connection's name. Each credential type filter on the credentials screen shows how many credentials it holds, and All shows the total.
