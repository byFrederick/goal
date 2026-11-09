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
- a scope: all factories, including ones created later, a list of factories, or none, the default.

The Secrets page lists them with where each is used, when it was last used and what expires soon. **New secret** opens a side sheet, and **Import** reads a `.env` or YAML file and checks it before anything is saved. A secret's page shows its scope, its expiry and its uses, and has Replace value and Delete. Replace value applies to runs that start afterwards, and then tests again each MCP server whose headers or env name the secret, stdio servers included, in every factory's current published version and open draft, so the Environments section shows the new results. It also tests again each kept repository test whose token is the secret ([Testing a repository's token](#testing-a-repositorys-token)). A test that fails keeps that failure as its result and doesn't stop the replace. Delete is disabled, with the reason, while something uses the secret: a factory's current published version that refers to it as a repository token, its model key, a trigger's model-agrees key or from an environment file, a knowledge source that reads with it, an organization webhook it signs, or the organization's Slack notification setting. An archived factory doesn't count. Changing a secret needs an owner or admin who has signed in within the last 10 minutes. Everyone in the organization can read the list, never a value. The [Organization policy](/docs/concepts/organization-policy/) page covers the organization's switch for **Use secrets**.

Values come in only by pasting them, through standard input or from an import file, never on the command line, and no command prints one.

## Environment files

An environment is a file ([reference](/docs/reference/environment-file/)) in the factory's definition, `environments/<name>.yaml`. It says what an agent's box installs and what it can reach. Every factory has `environments/default.yaml`. An agent names another with `environment: <name>` in its definition, and an agent without one gets `default`. On an agent's page, the Environment select does that.

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

The `{{name}}` placeholder names a secret. It is valid in an environment file's `env`, `files` and `mcp`, in `runtime.repository.token`, in a `runtime.repositories` entry's `token`, in `runtime.modelKey` and in a trigger's `modelAgrees.key`, and nowhere else. Publishing checks that each secret exists, signs no webhook and is open to the factory. Each MCP server's name is 3 to 40 lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit, so `playwright` publishes and `play_wright` does not. Each MCP server's tools are `allow`, `ask` or `off`; a tool with no mode is `allow`, and the most specific match wins. See [skills and rules](/docs/concepts/skills-and-rules/#tools-that-ask-first).

An environment can set `image` to run its box from an image of your own. A box on Cloudflare runs only the Worker's own images, so a run there fails on it. Publishing shows a warning at `image` and still publishes, since a box on Docker Compose runs it.

In the Definition, the **Environments** section has a tab per environment, and **Add a tool** adds an MCP server, a command-line tool or an API from a catalog, or a custom one, to the draft. You pick a secret or create one in place, and the page shows the lines it adds before you add them. The same file is under `environments/` in the YAML view. MCP servers are tested with the draft's secrets, and the result is shown, never required to publish. A stdio server is tested only when its command is exactly one the catalog ships, such as `github-mcp-server stdio`; goald never runs any other command, and reports such a server as untested. Each URL in its env, including one a secret holds, must also be on a host listed in **Code hosts**; otherwise the server is reported as untested and the error names the host, or the variable when a secret holds the URL.

The catalog has two GitHub templates. Each adds `gh`, the token as `GH_TOKEN`, GitHub's MCP server as `github` and a test of the token against `https://api.github.com/user`:

- **`github-read`** is for agents that only read. It runs the MCP server in its read-only mode (the `X-MCP-Readonly: true` header) and suggests a read-only fine-grained token, `github-read-token`.
- **`github`** keeps every tool of the MCP server, with `merge_pull_request` and `delete_*` set to `ask`, so a merge asks first. It suggests `github-token`.

Both take a `host` parameter, `github.com` by default. A GitHub Enterprise Server host, such as `ghe.example.com`, swaps the lines: `GH_HOST` is the host and the token is `GH_ENTERPRISE_TOKEN` instead of `GH_TOKEN`; the MCP server is the local `github-mcp-server stdio` (with `--read-only` for `github-read`) with `GITHUB_HOST=https://ghe.example.com`; and the test asks `https://ghe.example.com/api/v3/user`. A host takes only letters, digits, dots, hyphens and a `:port`. `GET /v1/catalog` lists each template's parameters, and an environment test of a template takes their values as `parameters`. Add a tool writes the `github.com` lines for now. The environment test starts the local server inside goald to list its tools, but only when the host is one of your organization's code hosts; otherwise it reports the server as untested and says to add the host.

Publishing warns when an agent whose `code` is `read`, or any check's agent, can call GitHub MCP tools that write. Goal judges each tool by what the server said about it in the environment's last passing test: a tool the server didn't mark read-only (`readOnlyHint`), and that the environment's `tools` set to `allow`, is named in a `github-tools-not-read-only` warning with the test's date. Setting those tools to `ask` or `off`, or using `github-read`, clears it. A GitHub server that was never tested, or whose last test failed, gets `github-tools-unknown`: its tools' access is unknown until you test the environment. Goal recognizes GitHub's server by its address, `api.githubcopilot.com`, or its command, `github-mcp-server`; a server it doesn't recognize gets no such warning. The warnings read the environment file only, not the organization policy, and say nothing about `gh` or a token in `env`.

A token an environment file names, such as `GH_TOKEN`, is the environment's: every run of an agent in that environment gets it, and Goal can't narrow what the token may do. The repository token is separate. It reaches git's credential store only, and never the agent's environment unless an environment file names it.

## The repository and the model

Two kinds of secret are named in the factory's runtime, not in an environment file:

- `runtime.repository` is `{url, token}`. The token clones, pushes and reads and merges pull requests. There is no app to install: the token's own scope is the limit, so a fine-grained token for one repository is the right choice. The host picks the code host: `github.com` is GitHub and `gitlab.com` is GitLab. An entry of `runtime.repositories` has no `forge`: the organization's code hosts give each host its kind. Only the shorthand keeps `forge`, and on any other host it needs `forge: github` or `forge: gitlab`; publishing refuses a version without it, and so does a merge.
- `runtime.repositories` is the list form, described below. Each entry's `token` is a secret.
- `runtime.modelKey` is the secret that gives every run its model. A value that starts with `sk-ant-oat` is a Claude subscription token and a run gets it as `CLAUDE_CODE_OAUTH_TOKEN`. Any other value is an Anthropic API key and becomes `ANTHROPIC_API_KEY`.

None of them reaches the agent's environment unless an environment file names it, for example as `GH_TOKEN` for `gh`. The shorthand and the model key are set on the factory's Settings page; Settings › Code lists, adds, removes and tests the `runtime.repositories` entries (see [Factory settings](/docs/guides/factory-settings/)).

The repository token signs git in to the repository's host for three things: the clone, the push of a new base branch (an assembly's integration branch), and the agent's own `git push`. Git signs in as `x-access-token` on GitHub and as `oauth2` on GitLab, with the token as the password. Goal keeps the token in git's credential store inside the run's box, outside the workspace, so the agent's push works without the token in its environment. The box and its store are thrown away after the run. The agent's prompt says that git signs in with the repository token. A public repository needs no token: it clones without a login.

goald reads and merges pull requests with the same token: a person's merge, a gate's merge on approval, the landing pull request of an integration branch, and the reads of a part's pull request base and of the default branch's head and history that an item's base commit and its "changed since your plan" note come from. A pull request in a repository the entry doesn't cover is refused before the token is opened, and so is a merge for a repository with no token. Each of these opens the token and writes one `secret.used` audit entry for the factory, with no run. With several entries, each repository uses its own entry's token, as the next section says.

### Repositories and what agents may write

To set this up step by step, follow [Connect code to a factory](/docs/guides/connect-code/).

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

For each repository, the most specific entry that covers it gives the token: one repository before an owner pattern. An agent that only reads takes a `read` entry's token whenever one covers the repository. Publishing warns with `reader-write-token` when an agent that only reads would get a `write` entry's token because no `read` entry covers it.

goald never pushes, creates branches, opens pull requests or merges in a repository whose entry only reads, or for an agent that only reads. A merge takes each pull request in its own repository with the token of the most specific entry that covers it. When that entry is a `read` entry, or no entry covers the repository, the merge is refused before goald updates a branch, merges or deletes one, and the reason names the repository and says an entry with `access: write` must cover it. Reads, such as an item's base commits and its "changed since your plan" note, take a `read` entry's token first.

### Testing a repository's token

A repository test tells you what a token can do for one of the factory's entries, and Goal keeps the answer so pages show it without asking the host again. `POST /v1/orgs/{org}/factories/{factory}/repositories/test` takes the entry's `url` (one repository, or an owner pattern such as `https://github.com/acme/*`), its secret and its `access`. It needs `definition:edit`. Goal opens the secret for that factory, so a secret limited to other factories is refused with `secret-not-allowed`, and asks the host's API, never git. A host the organization doesn't list is refused before the secret is opened.

The answers:

- **A `read` entry** answers "Can read". For an owner pattern it counts the repositories the token can read: "Can read 48 repositories in acme", or "at least 1000" when there are more than Goal counts. When the token can also push, the answer warns: "Goal won't push, but agents could. Use a read-only token."
- **A `write` entry** answers "Can read and push", or says which of the two fails with the host's reason. The host reports no pull-request permission, so Goal can't check it, and the answer notes that. For a classic GitHub token, push reflects the account's role rather than the token's scopes; a fine-grained token answers its own grant.
- **A host that doesn't answer** within 10 seconds gives "couldn't test".

Each answer is kept per factory and URL, the newest replacing the one before, and `GET /v1/orgs/{org}/factories/{factory}/repositories/tests` lists them for anyone who can read the factory. A test runs again when someone clicks Test, and when the secret's value is replaced. No answer holds the token.

From the CLI, `goal repository test <url> --secret <name> --factory <factory> [--access read|write]` runs the same test and keeps the answer; `--access` defaults to `write`, the access of the `runtime.repository` shorthand. It prints the answer, its warning or note, and the read and push checks, and exits 1 when the token can't do what the access asks. `goal factory show <factory>` lists the repositories of the factory's current version, each with its access, the default marked `*` and its kept answer or "not tested", then the kept answers that match no repository. It only reads kept answers and never tests.

## Webhook secrets

An organization webhook is signed by one of the organization's secrets, which no run can hold. Creating a webhook (`POST /v1/orgs/{org}/webhooks`) either creates a secret named `webhook-<name>`, cut to 40 characters without a trailing hyphen, with a generated value shown once, or uses a secret that exists, for a sender that issues its own value, such as PagerDuty or Sentry. A generated value is 64 hexadecimal characters, or `whsec_` and base64 for a Standard Webhooks sender. A created secret is open to no factory.

The webhook refers to the secret by id, so the webhook stays signed by it whatever the secret is called. A secret is not renamed by the API, the CLI or the UI. The secret can't be deleted while a webhook uses it, and its uses list the webhook.

Publishing refuses a version whose environment file, repository token, `runtime.modelKey` or trigger key names a secret that signs a webhook (`secret-signs-webhook`), even when the secret is open to the factory. A run that held the value could sign deliveries of its own.

Factory webhook endpoints are gone. A factory that had one must recreate it as an organization webhook, open to the factory, and change any trigger that named the endpoint so its `on` names the webhook. goald converts no endpoint, and its migrations refuse a database that still has one.

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
7. The agent gets the environment's MCP servers. The prompt has a **Your box** section that lists the box's command-line tools, the names of its variables, its file paths and its MCP servers, never a value.
8. Each call to an MCP server's tool follows the environment's mode for it: `allow` runs, `ask` waits for a person, and `off` is refused. The worker checks the agent's other tool calls against its rules (see [skills and rules](/docs/concepts/skills-and-rules/)).

The organization policy also stores an MCP tool mode cap, `mcp_tool_modes` in `PUT /v1/orgs/{org}/policy`: a tool name or glob (`*`, `?`) mapped to `allow`, `ask` or `off`, such as `{"*": "ask"}` for "every MCP tool at least asks". Where the environment's mode and the cap disagree, the stricter one wins in every run: under `{"*": "ask"}` a tool the environment sets to `allow` asks, and one set to `off` stays off. Always allow does not lift an `ask` that comes from the cap. Publishing shows a warning (`mcp-tool-mode-capped`) at each tool whose mode the cap makes stricter, naming the mode written and the one runs use, and still publishes. A save that leaves the field out keeps the stored cap, and a mode or pattern that is not valid is refused with a 400. The [organization policy](/docs/concepts/organization-policy/) page covers the rest of the policy.

An agent holds the secrets of its environment for the whole run, and it has network access from the box. A prompt-injected agent can misuse what its tokens reach. The control for that is a narrow token, with only the repositories and permissions the agent needs. The [organization policy](/docs/concepts/organization-policy/)'s **Use secrets** can switch an agent's environment off: it then gets no `env`, `files` or MCP servers, while the repository and model keys still reach the run. ADR 0008 and ADR 0037 (`docs/adr/` in the repository) record these decisions and their accepted risks.

## Access

Access is read only. It shows every factory's environments, the tools each reaches, the secret each uses, the last test and what is about to expire. Switch the view between By what they reach, By factory and By secret. A row opens the environment in the factory's definition, where you change it. It is the page to answer "what can this agent reach?" and "where does this key go?".

A row is an MCP server, a variable, a file or Slack notifications (`mcp`, `env`, `file`, `slack`). Repository tokens and `runtime.modelKey` never appear in Access; only the secret's own page lists those uses.

Access also shows the secret the organization's Slack notifications use, when they use one. It is one row, Slack notifications, with no factory: it appears under By what they reach and under its secret in By secret, never under By factory or when Access shows one factory. Its status comes from the secret alone: missing when the secret is gone, expired or expiring by the secret's expiry and reminder, otherwise ok.

## Publish checks for keys in an environment file

Two checks keep a key out of an environment file's plain text. Publishing shows them at the `env` entry, in the YAML view and in the Environments section.

- **`reserved-env`** is an error. The entry sets a variable the platform keeps for itself (`GOAL_*`, `HOME`, `PATH`, the proxy variables). Name it otherwise.
- **`plain-key`** says a plain value looks like a key. It is an error when the value starts with a known token prefix, and a warning when only the name ends in `_TOKEN`, `_KEY`, `_SECRET` or `_PASSWORD`.

The `plain-key` fix, **Move to Secrets**, replaces the value with `{{name}}`, the variable's name in lowercase with dashes (`GH_TOKEN` becomes `{{gh-token}}`). Create the secret with that name, or pick another. A value that already has `{{` in it is never flagged.

## Box network and sizes

A box on Docker reaches the network only through `goal-egress`. It allows a destination whose resolved addresses are all public, and goald's gateway paths (the MCP gateway, the run's bundle and its tool approvals). It refuses loopback, private and link-local addresses, such as the cloud metadata service, even when a name resolves to one. A box on Cloudflare has the open internet.

An environment's `size` is `small`, `medium` or `large`, and `medium` is the default:

| Size | Docker | Cloudflare |
| --- | --- | --- |
| `small` | 1 vCPU, 2 GiB | 1 vCPU, 6 GiB |
| `medium` | 2 vCPU, 4 GiB | 2 vCPU, 8 GiB |
| `large` | 4 vCPU, 8 GiB | 4 vCPU, 12 GiB |

The organization policy page shows the sizes of the backend your install runs; see [Organization policy](/docs/concepts/organization-policy/).

## API keys and personal tokens

An API key or a personal token can carry `secret:read`, `access:read` and `standing_rule:read`: it lists secrets and where each is used, reads Access and lists standing rules, and never reads a value. It can never carry `secret:manage` or `standing_rule:manage`. Those need an owner or admin signed in within the last 10 minutes, in the browser or the CLI session.

## From the command line

- `goal secret list`, `show`, `create`, `replace`, `delete` and `import` manage the organization's secrets. A value comes only from stdin or the import file and is never shown.
- `goal access [--factory <f>] [--group reach|factory|secret]` shows what agents reach, as the Access page does.
- `goal env test <factory> [<env>]` calls `tools/list` on the environment's remote MCP servers from goald, with their secrets opened there, and exits 1 when one fails.
- `goal repository test <url> --secret <name> --factory <factory>` tests a token for one of the factory's entries and keeps the answer ([Testing a repository's token](#testing-a-repositorys-token)). Without `--factory` it says the flag is required and exits 1 without calling goald.

Flags and output are in the [CLI reference](/docs/reference/cli/).

## Moving over from connections

Connections are gone. Organizations that used them were moved to secrets and environment files by a conversion at startup ([ADR 0037](https://github.com/byFrederick/goal/blob/main/docs/adr/0037-secrets-and-environment-files.md), decision 19). goald no longer runs it, and an install made since starts on secrets and environment files.

A factory the conversion refused kept its version. Needs you shows a conversion entry for it that names each problem, and no problem shows a secret's value. The entry goes away once the factory publishes a version with environment files.
