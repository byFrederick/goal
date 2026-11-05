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

The Secrets page lists them with where each is used, when it was last used and what expires soon. **New secret** opens a side sheet, and **Import** reads a `.env` or YAML file and checks it before anything is saved. A secret's page shows its scope, its expiry and its uses, and has Replace value and Delete. Replace value applies to runs that start afterwards, and then tests again each remote MCP server whose headers use the secret, in every factory's current published version and open draft, so the Environments section shows the new results. A server that fails its test keeps that failure as its result and doesn't stop the replace. Delete is disabled, with the reason, while something uses the secret: a factory's current published version that refers to it as a repository token, its model key, a trigger's model-agrees key or from an environment file, a knowledge source that reads with it, an organization webhook it signs, or a connection that refers to it. An archived factory doesn't count. Changing a secret needs an owner or admin who has signed in within the last 10 minutes. Everyone in the organization can read the list, never a value.

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

The `{{name}}` placeholder names a secret. It is valid in an environment file's `env`, `files` and `mcp`, in `runtime.repository.token`, in a `runtime.repositories` entry's `token`, in `runtime.modelKey` and in a trigger's `modelAgrees.key`, and nowhere else. Publishing checks that each secret exists, signs no webhook and is open to the factory. Each MCP server's name is 3 to 40 lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit, so `playwright` publishes and `play_wright` does not. Each MCP server's tools are `allow`, `ask` or `off`; a tool with no mode is `allow`, and the most specific match wins. See [skills and rules](../skills-and-rules/#tools-that-ask-first).

An environment can set `image` to run its box from an image of your own. A box on Cloudflare runs only the Worker's own images, so a run there fails on it. Publishing shows a warning at `image` and still publishes, since a box on Docker Compose runs it.

In the Definition, the **Environments** section has a tab per environment, and **Add a tool** adds an MCP server, a command-line tool or an API from a catalog, or a custom one, to the draft. You pick a secret or create one in place, and the page shows the lines it adds before you add them. The same file is under `environments/` in the YAML view. Remote MCP servers are tested with the draft's secrets, and the result is shown, never required to publish.

The catalog has two GitHub templates. Each adds `gh`, the token as `GH_TOKEN`, GitHub's MCP server as `github` and a test of the token against `https://api.github.com/user`:

- **`github-read`** is for agents that only read. It runs the MCP server in its read-only mode (the `X-MCP-Readonly: true` header) and suggests a read-only fine-grained token, `github-read-token`.
- **`github`** keeps every tool of the MCP server, with `merge_pull_request` and `delete_*` set to `ask`, so a merge asks first. It suggests `github-token`.

Both take a `host` parameter, `github.com` by default. A GitHub Enterprise Server host, such as `ghe.example.com`, swaps the lines: `GH_HOST` is the host and the token is `GH_ENTERPRISE_TOKEN` instead of `GH_TOKEN`; the MCP server is the local `github-mcp-server stdio` (with `--read-only` for `github-read`) with `GITHUB_HOST=https://ghe.example.com`; and the test asks `https://ghe.example.com/api/v3/user`. A host takes only letters, digits, dots, hyphens and a `:port`. `GET /v1/catalog` lists each template's parameters, and an environment test of a template takes their values as `parameters`. Add a tool writes the `github.com` lines for now. The environment test doesn't run stdio servers yet, so it reports the local server as untested.

Publishing warns when an agent whose `code` is `read`, or any check's agent, can call GitHub MCP tools that write. Goal judges each tool by what the server said about it in the environment's last passing test: a tool the server didn't mark read-only (`readOnlyHint`), and that the environment's `tools` set to `allow`, is named in a `github-tools-not-read-only` warning with the test's date. Setting those tools to `ask` or `off`, or using `github-read`, clears it. A GitHub server that was never tested, or whose last test failed, gets `github-tools-unknown`: its tools' access is unknown until you test the environment. Goal recognizes GitHub's server by its address, `api.githubcopilot.com`, or its command, `github-mcp-server`; a server it doesn't recognize gets no such warning. The warnings read the environment file only, not the organization policy, and say nothing about `gh` or a token in `env`.

A token an environment file names, such as `GH_TOKEN`, is the environment's: every run of an agent in that environment gets it, and Goal can't narrow what the token may do. The repository token is separate. It reaches git's credential store only, and never the agent's environment unless an environment file names it.

## The repository and the model

Two kinds of secret are named in the factory's runtime, not in an environment file:

- `runtime.repository` is `{url, token}`. The token clones, pushes and reads and merges pull requests. There is no app to install: the token's own scope is the limit, so a fine-grained token for one repository is the right choice. The host picks the code host: `github.com` is GitHub and `gitlab.com` is GitLab. Any other host needs `forge: github` or `forge: gitlab`; publishing refuses a version without it, and so does a merge.
- `runtime.repositories` is the list form, described below. Each entry's `token` is a secret.
- `runtime.modelKey` is the secret that gives every run its model. A value that starts with `sk-ant-oat` is a Claude subscription token and a run gets it as `CLAUDE_CODE_OAUTH_TOKEN`. Any other value is an Anthropic API key and becomes `ANTHROPIC_API_KEY`.

None of them reaches the agent's environment unless an environment file names it, for example as `GH_TOKEN` for `gh`. The shorthand and the model key are set on the factory's Settings page; `runtime.repositories` is edited in the YAML view (see [Factory settings](../../guides/factory-settings/)).

The repository token signs git in to the repository's host for three things: the clone, the push of a new base branch (an assembly's integration branch), and the agent's own `git push`. Git signs in as `x-access-token` on GitHub and as `oauth2` on GitLab, with the token as the password. Goal keeps the token in git's credential store inside the run's box, outside the workspace, so the agent's push works without the token in its environment. The box and its store are thrown away after the run. The agent's prompt says that git signs in with the repository token. A public repository needs no token: it clones without a login.

goald reads and merges pull requests with the same token: a person's merge, a gate's merge on approval, the landing pull request of an integration branch, and the reads of a part's pull request base and of the default branch's head and history that an item's base commit and its "changed since your plan" note come from. A pull request on a host other than the repository's is refused before the token is opened, and so is a merge for a repository with no token. Each of these opens the token and writes one `secret.used` audit entry for the factory, with no run.

### Repositories and what agents may write

`runtime.repositories` lists the repositories a factory's runs may open. A factory with none has no code.

```yaml
runtime:
  repositories:
    - name: app
      url: https://github.com/acme/app
      token: "{{github-acme}}"
      access: write
```

- `name` is 3 to 40 lowercase letters, digits and hyphens, unique in the factory.
- `url` is one repository, or an owner's every repository written as `https://github.com/acme/*`.
- `access` is `read` or `write`. A `write` entry needs a `token`.
- `default: true` marks the entry an item uses when it names none. Only one entry may have it, and it must name one repository. The only entry is the default when it names one repository.
- The URL's host must be `github.com`, `gitlab.com` or one of the organization's code hosts. Publishing refuses any other host, so no token goes to a host nobody listed.

`runtime.repository` is the shorthand for one `write` entry that is the default, named after the URL's last segment (`repo` when that isn't a valid name). A definition has one form or the other, never both.

An agent's `code` says whether it writes: `write` or `read`, next to its `builtins`. An agent without `code` writes when the factory has a `write` entry, so existing factories keep working. A line that merges, with a merge station or integration branches, needs a `write` entry and an agent that writes.

For now, publishing refuses a second entry, `access: read` and `code: read` with "not supported yet": runs start and merge in one repository that writes until Goal can work in several.
## Webhook secrets

An organization webhook is signed by one of the organization's secrets, which no run can hold. Creating a webhook (`POST /v1/orgs/{org}/webhooks`) either creates a secret named `webhook-<name>`, cut to 40 characters without a trailing hyphen, with a generated value shown once, or uses a secret that exists, for a sender that issues its own value, such as PagerDuty or Sentry. A generated value is 64 hexadecimal characters, or `whsec_` and base64 for a Standard Webhooks sender. A created secret is open to no factory.

The webhook refers to the secret by id, so renaming the secret keeps the webhook signed by it. The secret can't be deleted while a webhook uses it, and its uses list the webhook.

Publishing refuses a version whose environment file, repository token, `runtime.modelKey` or trigger key names a secret that signs a webhook (`secret-signs-webhook`), even when the secret is open to the factory. A run that held the value could sign deliveries of its own.

A factory's webhook endpoint made before organization webhooks is converted once when goald starts. Its signing secret becomes an organization secret named `webhook-<name>`, open to no factory, holding the value the endpoint showed when it was made. The endpoint becomes the organization's webhook with the same address, the `github` sender for a `github` endpoint and `standard` for a `generic` one, and is open to its factory for every repository. An endpoint keeps its name unless the name is reserved (a sender name, `schedule` or `webhook`) or an older endpoint of another factory has it; then it takes the first free suffix, such as `github-2`, cut to fit 40 characters. A secret name that is taken gets a suffix the same way. goald logs each rename as `webhook renamed` with the factory and both names: a published trigger that named the endpoint by its old name needs its `on` changed.

Replacing the value of a secret that signs a webhook keeps the previous value for 24 hours, sealed apart from the new one, so a delivery signed with either passes while you update the sender. Checking a signature opens the secret without a `secret.used` audit entry and leaves its last use as it was. `goal db rekey` reseals the previous value too and drops it once its 24 hours are over.

### Receiving deliveries

A sender posts each delivery to the webhook's address, `POST /hooks/{webhook id}`, with no session or API key. goald reads at most 1 MiB and checks the signature against the raw body before it reads anything in it. The current value of the secret passes, and so does the previous one for 24 hours after a replace.

How each sender signs:

| Sender | Signature | Delivery id | Event |
| --- | --- | --- | --- |
| `github` | `X-Hub-Signature-256`: `sha256=` and the hex HMAC-SHA256 of the body | `X-GitHub-Delivery` | `X-GitHub-Event` |
| `gitlab` | `X-Gitlab-Token`: the secret's value as it is | `X-Gitlab-Event-UUID` | the body's `object_kind` |
| `standard` | `webhook-signature`: one or more space-separated `v1,` values, each the base64 HMAC-SHA256 of `<webhook-id>.<webhook-timestamp>.<body>` under the base64 key after `whsec_`. A `webhook-timestamp` more than 5 minutes off is refused | `webhook-id` | the body's `type`, else `event` |
| `pagerduty` | `X-PagerDuty-Signature`: one or more comma-separated `v1=` values, each the hex HMAC-SHA256 of the body | the body's `event.id` | the body's `event.event_type` |
| `sentry` | `Sentry-Hook-Signature`: the hex HMAC-SHA256 of the body | `Request-ID` | `Sentry-Hook-Resource` |
| `token` | `Authorization: Bearer <value>`, or the value alone in the webhook's own header | the webhook's id path in the body | the webhook's event path in the body |

When a sender names no delivery id, the id is the hex SHA-256 of the body. An id over 200 characters is replaced by its hex SHA-256.

The answers:

| Status | When |
| --- | --- |
| 202 | Accepted. The body is `{"id": "<delivery id>", "receipt": "accepted"}`. |
| 200 | The webhook already took this delivery id. Nothing is created, and the body says `"receipt": "duplicate"`. |
| 401 | The signature, token or timestamp is missing or wrong. |
| 404 | No enabled organization webhook has this id. |
| 405 | A method other than `POST`. |
| 413 | The body is over 1 MiB. |
| 429 | Past 30 refused deliveries a minute to one webhook, or past 60 requests a minute from one address to unknown webhooks. `Retry-After` says when to try again. Each goald counts in its own memory. A signed delivery is never limited. |

Each delivery gets a receipt: Accepted, Duplicate or Refused. What goald keeps:

- **The ledger.** Every accepted delivery id goes into the webhook's ledger, without its body, for a year. A delivery replayed with the same id within that year is a duplicate, even after its row is gone.
- **Deliveries a trigger names** keep their row, headers and body for 30 days. A trigger names a delivery when it is enabled, in a factory the webhook is open to, and its `on` names the webhook or the sender and event, such as `github.check_run`.
- **GitHub's ping**, sent when you add the webhook in GitHub, keeps no row of its own: only its ledger id and its daily count. Goald records when it accepted the ping on the webhook (`pinged_at`), which the webhook shows as "Received GitHub's ping". A duplicate ping leaves that time as it is.
- **Deliveries no trigger names** keep only their ledger id and a count per day.
- **Refused deliveries** keep their headers and the reason, with no body, for 30 days.

No delivery keeps a signature or token header.

## What a run receives

1. The run starts the agent's environment from the version the run is pinned to. A secret you replace applies to runs that start afterwards, and the tests of the MCP servers that use it run again.
2. goald checks that every referenced secret is open to the factory, then opens each one.
3. It puts each `env` value, each file's content and each MCP secret in the box's environment, and renders the MCP configuration. A header that names a secret reaches Claude Code as a variable the box expands, so the run's bundle never holds a value.
4. Every opened value goes on the run's scrub list, and one `secret.used` audit entry is written per secret per run, without a value.
5. The box clones the repository and installs its tools, then the environment's `tools`.
6. Before the agent starts, the box writes each of the `files` at its path, `~/` being the agent's home, with mode 0600. Then it runs the `setup` commands in order in the checkout, each with a 30-minute limit. A command that exits non-zero or runs past its limit fails the run, and the run's error names the command and shows its last 50 lines of output with secret values removed. The files are written again at every attempt, so a replaced value reaches the next one. Setup runs once per box: a retry in the same box skips it after it succeeded, and runs it again after it failed. A new box runs it again, so write commands that are safe to repeat.
7. The agent gets the environment's MCP servers. The prompt has a **Your box** section that lists the box's command-line tools, the names of its variables, its file paths and its MCP servers, never a value. It takes the place of the Connections section that runs on connections get.
8. Each call to an MCP server's tool follows the environment's mode for it: `allow` runs, `ask` waits for a person, and `off` is refused. The worker checks the agent's other tool calls against its rules (see [skills and rules](../skills-and-rules/)).

The organization policy also stores an MCP tool mode cap, `mcp_tool_modes` in `PUT /v1/orgs/{org}/policy`: a tool name or glob (`*`, `?`) mapped to `allow`, `ask` or `off`, such as `{"*": "ask"}` for "every MCP tool at least asks". Where the environment's mode and the cap disagree, the stricter one wins in every run: under `{"*": "ask"}` a tool the environment sets to `allow` asks, and one set to `off` stays off. Always allow does not lift an `ask` that comes from the cap. Publishing shows a warning (`mcp-tool-mode-capped`) at each tool whose mode the cap makes stricter, naming the mode written and the one runs use, and still publishes. A save that leaves the field out keeps the stored cap, and a mode or pattern that is not valid is refused with a 400.

An agent holds the secrets of its environment for the whole run, and it has network access from the box. A prompt-injected agent can misuse what its tokens reach. The control for that is a narrow token, with only the repositories and permissions the agent needs. The organization policy's **Use secrets** can switch an agent's environment off: it then gets no `env`, `files` or MCP servers, while the repository and model keys still reach the run. ADR 0008 and ADR 0037 (`docs/adr/` in the repository) record these decisions and their accepted risks.

## Access

Access is read only. It shows every factory's environments, the tools each reaches, the secret each uses, the last test and what is about to expire. Switch the view between By what they reach, By factory and By secret. A row opens the environment in the factory's definition, where you change it. It is the page to answer "what can this agent reach?" and "where does this key go?".

Access also shows the secret the organization's Slack notifications use, when they use one. It is one row, Slack notifications, with no factory: it appears under By what they reach and under its secret in By secret, never under By factory or when Access shows one factory. Its status comes from the secret alone: missing when the secret is gone, expired or expiring by the secret's expiry and reminder, otherwise ok.

## From the command line

- `goal secret list`, `show`, `create`, `replace`, `delete` and `import` manage the organization's secrets. A value comes only from stdin or the import file and is never shown. `goal credential` and `goal connection` moved: each prints where its commands went and exits 2.
- `goal repository test <url> --secret <name>` checks, before any factory exists, whether the secret's token can read a repository on one of the organization's [code hosts](/docs/getting-started/organizations/#code-hosts) and push to it. A repository on any other host is refused (`repository-host-unknown`) before the secret is opened. goald asks the code host's API, never git, and prints each check with the host's reason. A host that gives no answer within 10 seconds answers "couldn't test". For a classic GitHub token, push ok reflects the account's role rather than the token's scopes; a fine-grained token answers its own grant. Owners and admins can run it, and the secret must be open to all factories. The API route is `POST /v1/orgs/{org}/repositories/test`.

Flags and output are in the [CLI reference](../../reference/).

## Moving over from connections

Organizations that used connections, kinds and credentials were moved to secrets and environment files once, when goald started, with no person needed ([ADR 0037](https://github.com/byFrederick/goal/blob/main/docs/adr/0037-secrets-and-environment-files.md), decision 19). For each organization it:

1. Makes each sealed connection field a secret open to every factory. The secret is named after the connection, or `<connection>-<field>` when the kind has several secret fields. A field that referred to a credential uses that credential, which is a secret already. A secret that already has the name is reused when it is open to every factory and holds the connection's value. Otherwise that secret is left as it is, and the connection's secret takes the next free name with a number: `github-2`, then `github-3`. The new name is the one the converted factory and its knowledge sources refer to, and goald logs a `secret renamed` line naming the connection, the old name and the new one, never a value.
2. Converts each factory's current version into a new version, published by Goal with the message "Moved connections to environments (ADR 0037)":
   - the factory's connections become `environments/default.yaml`, rendered from their kinds: the kind's variables, its command-line tool and its MCP server, with each secret field as a `{{secret}}` reference;
   - an agent whose connections differ from the factory's gets `environments/<agent>.yaml` and `environment: <agent>`;
   - `runtime.size` and `runtime.tools` move into every environment file;
   - `runtime.repository.connection` becomes `runtime.repository.token`, `runtime.modelConnection` becomes `runtime.modelKey`, and a trigger's `modelAgrees.connection` becomes `modelAgrees.key`;
   - each tool mode set on a connection, other than `allow`, becomes that server's `tools:` entry.
3. Tests the converted version before it publishes it. The repository token must be able to read `runtime.repository.url` and push to it: goald asks the code host's API, as `goal repository test` does, since it runs no git. Each remote MCP server in each environment file must answer `tools/list` with its headers' secrets filled in, as the environment test does. A check that fails, or gets no answer within 10 seconds, publishes nothing.
4. Reopens a draft that was open on the new version, converted, for its author. A draft that does not convert is reopened as it was, and publishing it shows what to fix.

It also points each synced knowledge source still on a connection at the secret its connection's token became, and copies the reader and the connection's other values into the source's config ([Knowledge](../knowledge/)). A source stays on its connection when that secret is open to some factories only, since a knowledge source syncs only with a secret open to all.

When the organization's Slack notification setting names no secret and it has a Slack connection, it points the setting at the secret that connection's token became. With several Slack connections it uses the oldest, the one Slack notifications were sent with. A setting that already names a secret is left as it is. A Slack connection whose secret does not exist gets a `slack setting not moved over` line with the connection's name and the reason, and Slack stays off until an admin picks a secret.

Running items keep the version they started on. goald logs one `moved over` line per organization, with how many factories it converted and refused, how many secrets it created, how many knowledge sources it moved, whether it set the Slack notification setting (`slack`) and how many code hosts it added to the organization's list (`hosts`, [Code hosts](/docs/getting-started/organizations/#code-hosts)). A knowledge source it can't move gets its own `knowledge source not moved over` line with the reason. Running it again changes nothing for a factory it converted.

A factory Goal cannot convert keeps its version and runs on connections. Needs you shows a conversion entry for it that names each problem, for example "connection aws-prod uses the aws kind, which Goal no longer converts: remove it from the factory". Goal refuses connections of the `aws` and `bedrock` kinds, and custom kinds that start a command with no fields. A converted version that fails its test is refused the same way, and its entry names what to fix: the repository and whether the token can't read it or can't push to it, with the host's reason, or the environment file, the MCP server and its error. No problem shows a secret's value. Each time goald starts, it tries a refused factory again, so fixing the token's access or the server is enough: the next start publishes the converted version once. The entry goes away once the factory publishes a version with environment files.
