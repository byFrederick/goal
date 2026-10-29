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

Values come in only through standard input or an import file, never on the command line, and no command prints one. Changing a credential needs an owner or admin who has signed in within the last 10 minutes.

## What a run receives

1. A run's agents name the connections they may use. The factory's definition decides which.
2. When the box is built, goald resolves each secret and sets the kind's `env` variables in the box's environment.
3. The agent can use those variables from the box, for example through `gh` or an API client. Its MCP servers read their headers from the same environment.
4. The worker checks every connection tool call against the agent's rules before it runs (see [skills and rules](../skills-and-rules/)).

An agent holds its connections' credentials for the whole run, and it has network access from the box. A prompt-injected agent can misuse what its tokens reach. The control for that is a narrow token, with only the repositories and permissions the agent needs. ADR 0008 (`docs/adr/0008-portable-runs-connections-and-knowledge.md` in the repository) records this decision and its accepted risks.

Here is a connection for an `acme` repository, created from the `github` kind. The token is read from standard input, so it never appears in the shell history:

```sh
goal connection create acme-github --kind github --field host=github.com --secret token=-
```

## Tool modes

Each tool of an MCP connection is `allow`, `ask` or `off`. A tool with no mode is `allow`. `ask` pauses the call for a person, and `off` never runs it. The modes are set on the organization connection, and the stricter of the connection's mode and the factory's rule applies. See [skills and rules](../skills-and-rules/#tools-that-ask-first).

## Testing and managing connections

- `goal connection test <name>` runs the kind's test from goald and exits 1 when it fails. The connection records when it was last tested and the outcome.
- `goal connection list`, `show`, `update` and `delete` manage a connection. Deleting is refused while a factory's current version uses the connection.
- `goal connection kinds` lists the kinds, the platform's and the organization's.
- `goal credential list`, `create`, `replace`, `rm` and `import` manage credentials.

Flags and output are in the [CLI reference](../../reference/).

## Where you meet connections

The connections screen lists each connection with its kind, status and the factories that use it. The factory builder picks connections for an agent from this list.
