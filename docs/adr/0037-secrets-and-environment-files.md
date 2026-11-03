# ADR 0037 — Secrets hold the keys, environment files say what agents reach

Status: accepted · 2026-11-02 · Amended by [ADR 0039](0039-jonah-learns-from-the-line.md) (decision 5: a try's agent gets none of its environment's `env`, `files` or MCP servers) and [ADR 0040](0040-one-door-for-every-source.md) (decision 19 step 5: webhook endpoints name a secret, which keeps its previous value for 24 hours after a change) and [ADR 0041](0041-code-and-github-without-an-app.md) (decisions 6–7: a list of repositories with their access, on code hosts the organization lists)

- **Replaces:**
  - [ADR 0017](0017-credentials.md), credentials.
  - Decision 5 of [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (connections carry credentials) and its 2026-10-27 amendment.
- **Amends:**
  - [ADR 0031](0031-tool-calls-ask-first-and-standing-rules.md): where tool modes live.
  - [ADR 0024](0024-organization-policy.md): Use credentials.
  - [ADR 0032](0032-self-serve-onboarding.md): the Code step.
  - [ADR 0023](0023-notifications-and-sign-up-by-domain.md): Slack.
  - [ADR 0028](0028-trigger-try-and-model-check.md) and [ADR 0029](0029-bake-offs-are-eval-runs-of-one-agent-across-models.md): the model connection.
- **Screens:** the canvas pages "Access, secrets and knowledge" and "Build a factory", exported to [docs/design/screens/](../design/screens/README.md). They are binding ([ADR 0016](0016-ui-revamp.md), [ADR 0036](0036-design-system-v3-plain-and-motion-binding.md)).

## Context

Giving an agent one tool today takes three ideas:

- **A credential** holds a key ([ADR 0017](0017-credentials.md)).
- **A connection kind** is a template that says how a tool is used: fields, env, an MCP server, a CLI and a test ([internal/connkind/kinds/](../../internal/connkind/kinds/)).
- **A connection** is an organization row that joins a kind to keys. Those keys are either sealed in the row or point at a credential.

The owner found this hard to use. Five problems stand out:

1. **Keys live in two places.** A connection seals its own key or refers to a credential as `{{name}}`. To rotate a key, you first have to find where it lives.
2. **There are three placeholder syntaxes.** `{{name}}` refers to a credential, `{{field}}` lives inside a kind, and `${VAR}` is expanded by Claude Code in the box. Each follows different rules.
3. **A custom tool needs a kind file first.** A CLI or API that Goal does not ship needs a ConnectionKind YAML before it can be added. CLIs install either from a kind or from `runtime.tools`, so there are two ways to do the same thing.
4. **What an agent can reach is spread out.** It depends on the factory's `connections` list, each agent's `connections`, each credential's agent list, the organization's tool modes and the definition's `toolRule`. None of these is versioned with the definition except the two lists.
5. **The screens assume a GitHub App** ("As the Goal app, on byFrederick", "Give the Goal app access"). Goal has no app. Nobody will install one now, and someone who wants to use a token instead shouldn't need one.

On 2026-11-02 the owner compared three options on the canvas and chose option B:

- **A: an organization Tools registry with per-agent lists.** Simple to click through, but tools sit outside the versioned definition, and every agent has a list to keep in step.
- **B: Secrets plus environment files in the definition.** This is the one chosen.
- **C: B with keys kept outside the box by an egress proxy.** This is kept for later; see decision 13.

The owner's constraints:
- One model for every tool, not centred on GitHub.
- No GitHub App.
- Simplicity, scale and clean code.
- No steps the user doesn't need.

Research of comparable products (Warp, Devin, Codex, Copilot coding agent, Cursor, Anthropic's Managed Agents, Composio, Arcade, Nango, and the MCP authorization spec) points the same way:
- **Environment:** what is installed in the box; every product defines CLIs here.
- **Secrets:** the values, stored once per organization and granted by name.
- **Tool config:** refers to secrets by name.

A CLI or a plain API is "environment plus secret", not a connection.

## Decision

### Secrets

1. **A secret is the only place a value lives.** It is an organization row with:
   - a name: the credential name pattern `^[a-z][a-z0-9-]{1,38}[a-z0-9]$`, unique in the organization;
   - a source (decision 2);
   - a sealed value;
   - an optional description (at most 500 characters);
   - a factory scope: all factories (including future ones), or a list of factory ids;
   - an optional expiry, and a reminder of 1 to 90 days before it, which requires an expiry;
   - who created it, who last changed it, when, and `last_used_at`.

   Sealing works exactly as for credentials today:
   - AES-256-GCM under the platform master key, with a key id.
   - The associated data binds organization, secret id and key id.
   - Nothing returns the value. Replacing it is the only write.
   - goald refuses to start without every key id a secret is sealed under.

   ADR 0017's agent list goes away: an environment file grants a secret (decision 5). ADR 0017's type label also goes away: a secret is what it is used as.

2. **A secret's value is pasted or imported, and the run gets it as it is.** The row records its `source` so that later sources fit without changing anything that uses a secret. Each later source gets its own ADR, and the UI shows no source choice until there is a second one:
   - `sign-in`: an OAuth grant a person makes once in the browser. Goal keeps it and refreshes it, which is how MCP servers that only take OAuth will work.
   - `1password`: an `op://vault/item/field` reference, resolved at run start (the platform's last phase, as decided on 2026-10-23).

   Goal does not run on AWS and mints no cloud credentials. The `aws` and `bedrock` connection kinds go away with the other kinds (decision 19). An agent that needs a cloud CLI gets its key as a variable or a file, like any other tool.

3. **Secrets import from `.env` or YAML,** exactly as ADR 0017 decision 9 says:
   - a dry run first;
   - replace or skip on conflict;
   - imported secrets start open to no factory.

4. **Permissions.** `secret:read` and `secret:manage` replace `credential:*` and `connection:*`, with the same roles:
   - `read`: owner, admin and member. It shows metadata and where each secret is used, never a value.
   - `manage`: owner and admin, with a fresh session.

   A secret that a published definition, a knowledge source, a webhook or the organization's Slack setting uses cannot be deleted. The 409 names each use.

### Environment files

5. **An environment is a file in the factory definition, `environments/<name>.yaml`. It says what an agent's box installs and what it can reach.** Every factory has `environments/default.yaml`. An agent names its environment in its file with `environment: <name>`, and an agent without one gets `default`. Judges and eval runs also take `environment:`, and default to `default`.

   ```yaml
   apiVersion: goal/v1
   kind: Environment
   metadata: {name: default}
   spec:
     size: medium                     # small | medium | large
     image: ghcr.io/acme/box:2026.11  # optional; Goal's box when unset
     tools: [go@1.26, node@22, gh@2]  # mise specs, after the repository's own mise.toml
     setup:                           # run in order after clone and tools
       - pnpm install --frozen-lockfile
     env:                             # what CLIs and API calls read
       GH_TOKEN: "{{github-token}}"
       SENTRY_ORG: byfrederick
     files:                           # written before setup, mode 0600
       ~/.kube/config: "{{kube-staging}}"
     mcp:
       linear:
         url: https://mcp.linear.app/mcp
         headers: {Authorization: "Bearer {{linear-key}}"}
         tools: {create_comment: ask, "delete_*": off}
       playwright:
         command: [npx, "@playwright/mcp@0.0.41"]
   ```

   The rules for each key:
   - **`env`** holds plain values and `{{secret}}` references. A reference may be the whole value or part of it, as in `postgres://app:{{db-pass}}@db/app`. Names reserved for the platform (`domain.ReservedRunEnv`) are refused.
   - **`files`** keys are absolute paths or start with `~/`. The values are references.
   - **`mcp`** entries are either remote (`url`, optional `headers`) or stdio (`command`, optional `env`), never both. `tools` maps a tool name or glob to `allow`, `ask` or `off`. The most specific match wins, and an unmatched tool is `allow`.
   - **`size`, `image`, `tools` and `setup`** replace `runtime.size` and `runtime.tools`, which move here.

   Each `setup` command must finish in 30 minutes, and a non-zero exit fails the run with the command and its last 50 lines. Setup output goes through the scrub list like everything else.

6. **There is one placeholder, `{{secret}}`, and it is valid in exactly four places:**
   - an environment file's `env`, `files` and `mcp` (`headers`, and a stdio server's `env`);
   - `runtime.repository.token`;
   - `runtime.modelKey`;
   - a trigger's `modelAgrees.key`.

   Anywhere else it is a check error. The secret must exist and be open to the factory. Publish checks this (codes `secret-missing` and `secret-not-in-scope`, located at the key), and so does run start, which fails naming the secret and never a value.

   A value with literal `{{` is not supported.

7. **The repository and the model take secrets directly.**
   - `runtime.repository` becomes `{url, token}`.
     - The token clones, pushes and reads and merges pull requests.
     - The URL's host picks the code host adapter: `github.com` is GitHub, `gitlab.com` is GitLab, and any other host needs `forge: github|gitlab`.
     - There is no app: a fine-grained token's own scope is the limit, as ADR 0008 already said.
     - The repository token is not in the agent's environment unless an environment file names it, for example as `GH_TOKEN` for `gh`.
   - `runtime.modelConnection` becomes `runtime.modelKey`, a `{{secret}}` reference:
     - a `value` that starts with `sk-ant-oat` is a Claude subscription token and becomes `CLAUDE_CODE_OAUTH_TOKEN`;
     - any other `value` is an Anthropic API key and becomes `ANTHROPIC_API_KEY`;

     Judges, evals, bake-offs and Jonah use the same key, as they use the model connection today.

8. **Tool modes live in the environment file. Standing rules still lift an `ask`.**
   - ADR 0031's per-connection modes and the definition's `toolRule` become `mcp.<server>.tools`, so the factory's author sets them and they are versioned and reviewed.
   - A run's effective mode is the stricter of the file's mode and the organization policy (decision 12).
   - A standing rule, Always allow, is still an organization record. Its key changes from connection plus tool to factory, server, tool and agent role.
   - A standing rule turns an `ask` from the file into `allow` for its agent role. It never changes `off`.
   - The Environments form shows each standing rule under its tool, with Revoke.
   - Asking, approving and Needs you are unchanged.

9. **Testing.**
   - **Remote MCP servers:** Goal tests each one by calling `tools/list` with its headers resolved from the draft's secrets. The tool names it returns feed the modes UI ("9 found").
   - **Catalog templates:** a template can carry an HTTP test request for its variables, for example Cloudflare's token verify.
   - **CLIs, files and setup** are not tested ahead of a run. A failed setup names the failing command (decision 5).
   - **When tests run:** when a tool is added, when a referenced secret is replaced, and on Test in the form. Results are kept per factory, environment and server. They are shown, never required to publish.

### What the box gets

10. **At run start the engine resolves the agent's environment, from the version the run is pinned to.** In order, it:
    1. checks every reference's scope;
    2. opens each secret;
    3. puts each `env` value in the box's environment, and writes `files` (mode 0600);
    4. renders the MCP config;
    5. adds every opened value to the run's scrub list;
    6. writes the audit event `secret.used`, one per secret per run, with secret and run but never a value, and updates `last_used_at`.

    A header that names a secret is rendered as `${GOAL_SECRET_<NAME>}`, with the value in that env variable. Claude Code expands it in the box, so the run bundle never holds a value, as ADR 0008 required.

    A replaced value applies to runs that start afterwards.

11. **The prompt tells agents what their box has.** Goal adds one short section listing:
    - the CLIs;
    - the variable names, never their values;
    - the files' paths;
    - the MCP servers.

    That list replaces the kinds' `docs` hints.

12. **The organization policy's "Use credentials" becomes "Use secrets".**
    - `environment` (the default): an agent gets what its environment names.
    - `off`: an agent gets no `env`, `files` or `mcp` from its environment. The repository and model keys still reach the run.

    The policy may also cap MCP tool modes for the whole organization ("write tools: at least ask"), which decision 8's "stricter wins" applies.

13. **Keys outside the box come later, as a switch on a secret, not as a change to the files.** A later ADR may add "Keep out of the box" with allowed hosts to a `value` secret:
    - The box then gets a placeholder.
    - The egress proxy swaps in the value only for those hosts, as Anthropic's vaults do.
    - Environment files don't change.

    Credential brokers (Composio, Arcade, Nango) are not used: they don't help `gh`, `wrangler` or `kubectl`, and they add a vendor to every call.

### Where it shows

14. **Screens.**
    - **Organization:**
      - The sidebar's Connections and Credentials become **Access** and **Secrets**.
      - **Access** is read only. It shows every factory's environments, the tools each reaches, the secret each uses, the last test and what is about to expire, by tool, by factory or by secret. Each row opens the environment in the factory's definition.
      - **Secrets** has the list, New secret (a side sheet), Import and the secret page. The secret page shows where the secret is used, its scope, its expiry, Replace value, and Delete, which is disabled with the reason while it is in use.
    - **Factory:**
      - The Definition's Form view gains an **Environments** section, replacing Connections. It has a tab per environment with what uses it, then:
        - The box: size, image and setup.
        - Command-line tools.
        - Variables, where each value is plain text or a secret picked from a list.
        - Files.
        - MCP servers, with their tool modes, standing rules and tests.
        - Checks.
      - In the YAML view the same file sits under `environments/`.
      - **Add a tool** is one page: a catalog of MCP servers, CLIs and APIs, plus Custom, which writes the lines into the draft. You pick an existing secret or create one in place, and the page shows the lines it adds before Add to draft.
      - The agent page shows an **Environment** select instead of Connections.
      - A check flags a plain `env` value that looks like a key, with a one-click "Move to Secrets".
    - **Onboarding step 2 ("Code"):** a repository URL and a token. The token is saved as a secret, and Goal tests clone and push access.
    - **Knowledge:** a synced source (repository docs, Confluence) picks a secret.
    - **Every screen works at 1440, 1280, 1024, 768 and 390 pixels:** the phone and tablet versions are on the canvas.

15. **The catalog is today's kinds, rewritten as templates shipped with goald** (`internal/catalog/*.yaml`). A template holds:
    - a title and a one-line description;
    - what it adds: tools, `env` with secret slots, `files`, `mcp` with suggested modes;
    - a suggested secret name;
    - an optional HTTP test.

    Applying a template only writes into the draft. Nothing in the organization changes except a secret you created in the same step.

    "Define a kind" goes away. Custom is a form with three types:
    - **MCP server:** a URL and headers, or a command.
    - **Command-line tool:** a mise spec, variables and files.
    - **API:** variables, typically a base URL and a key.

### API, CLI and data

16. **API:**
    - **Secrets:** `/v1/orgs/{org}/secrets` covers list, create, get, patch, replace value (`PUT …/value`), delete and import (dry run, then apply). The secrets API replaces `/credentials`.
    - **Access:** `/v1/orgs/{org}/access` is the read model behind the Access page, computed from published definitions and test results. It replaces `/connections` and `/connection-kinds`.
    - **Catalog:** `GET /v1/catalog` lists the templates.
    - **Tests:** `POST /v1/orgs/{org}/factories/{factory}/environments/{env}/test` tests a draft or a version, optionally one server. It needs `definition:edit`, and the secrets it opens never leave goald.

    `permissions.yaml` declares `secret:read`, `secret:manage` and `access:read` (owner, admin, member).

17. **CLI:**
    - `goal secret list|show|create|replace|delete|import`;
    - `goal env test <factory> [<env>] [--server <name>]`;
    - `goal access [--factory <f>]`.

    `goal connection …` and `goal credential …` print where they moved and exit 2 for one release, then go away.

18. **Data:**
    - A new migration renames `credentials` to `secrets`, adds `source` (`value` for every row) and drops the type label and the agent list.
    - A connection's sealed values and references are read once by the migration in decision 19, then the `connections`, `connection_kinds` and connection-test tables are dropped in a later migration, after the release that converts them.
    - The key-id check (`goal_connection_key_ids`, or its replacement) covers `secrets` only.
    - Standing rules gain `factory_id`, `environment` and `server`, and are re-keyed by the migration.

### Moving over

19. **Goal converts every organization in one release, with no person needed, and each step is visible.**
    1. Every credential becomes a secret with the same name and value.
    2. Each sealed connection field becomes a secret named after the connection, or `<connection>-<field>` when the kind has several secret fields.
    3. For each factory, the latest published version is converted into a new version, published by Goal, with the changelog "Moved connections to environments (ADR 0037)":
       - The factory's connections become entries in `environments/default.yaml`, rendered from their kinds.
       - An agent whose `connections` differed from the factory's gets an environment named after the agent, holding its set.
       - `runtime.size` and `runtime.tools` move into every environment.
       - `repository.connection` becomes `repository.token`.
       - `modelConnection` becomes `modelKey`.
       - Organization tool modes become `tools:` in each file that uses the server.
    4. Standing rules are re-keyed.
    5. Knowledge sources, webhooks and Slack settings point at the new secrets.

    Running items keep their pinned version. The Versions tab shows the diff. A draft open during the conversion is rebased onto the new version, and its conflicts show as in any rebase.

    The migration is idempotent and logs one line per organization. Connections of the `aws` and `bedrock` kinds are not converted, because Goal mints no cloud credentials (decision 2); no organization uses them today. A factory it cannot convert, for example because it names one of those or a custom kind with a stdio command and no fields, keeps its version. That factory gets a Needs you entry naming what to fix, and its runs keep working on the old path until it publishes. The old path is removed one release later.

## Rejected alternatives

- **Organization Tools registry (option A).** Tools would live outside the definition, so they wouldn't be versioned or reviewed with it, and a factory couldn't change its own. Every agent would carry a tool list. Its best part, a catalog with create-or-pick for secrets, is kept in decision 15.
- **Keys outside the box now (option C).** It needs TLS interception in the egress proxy and a certificate the box trusts. It doesn't cover requests signed with the key, and it breaks CLIs that check a key's format at start. It stays as decision 13's later switch.
- **A GitHub App or any per-forge app.** Nobody will install one now, and a token already does the job. A forge app can come back as a secret source (`sign-in`) without changing files.
- **Keeping connection kinds as organization data.** It is the third idea this ADR removes. Templates cover the shipped kinds, and Custom covers the rest.
- **Secrets as env variables named after the secret (ADR 0017, decision 6).** That hides which variable a CLI reads. Naming the variable in the file is one line and is explicit.
- **Per-agent secret lists (Warp).** Agents share environments instead. Two agents that need different access get two environments, which scales with the number of distinct boxes, not the number of agents.

## Consequences

- **What an agent reaches is one file, reviewed and published like the rest of the factory,** and the factory can change it itself. Access shows the whole organization in one place.
- **One value store and one placeholder.** Rotating a key is Replace value on one secret, and the next runs use it.
- **The engine's run start changes** (decision 10), and connection resolution, kinds, the kind renderer and the connection tests go away. `internal/connkind` becomes `internal/catalog` (templates only).
- **The definition schema changes:**
  - It gains `environments/*.yaml`, the agent's `environment`, `runtime.repository.token`, `runtime.modelKey` and `modelAgrees.key`.
  - It loses `spec.connections`, the agent's `connections`, `toolRule`, `runtime.modelConnection`, `runtime.size` and `runtime.tools`.

  Publish refuses the removed keys with a quick fix that points at the conversion.
- **ADR 0031** keeps asking, approvals and standing rules. Only where modes live changes.
- **ADR 0023's Slack notifications** use a secret (`slack-bot` by default) set in the organization's notification settings, and Slack intake uses a webhook.
- **ADR 0032's onboarding:** step 2 is done when the first factory has a repository and token that pass the test, and "a Slack connection exists" becomes "Slack notifications are set".
- **ADR 0028 and ADR 0029** read `runtime.modelKey` wherever they read the model connection.
- **The docs site and the change map are updated** in the parts that change them:
  - `docs/engineering/factory-definition.md` gains an "Environment files" section;
  - `docs/engineering/change-map/` gets entries for secrets, environments, access and the catalog.
- **Accepted risk:** the box still holds the values its environment names, so a prompt-injected agent can misuse what its tokens reach. Narrow tokens remain the control, as ADR 0008 said, and decision 13 is the way out.

## The build plan

The owner approved this ADR and its screens on 2026-11-02. Each row is one pull request in one assembly. The design branch `design/agent-access` holds this ADR, the exported screens and the screens index, and is merged first. Every UI row follows [MASTER.md](../design/MASTER.md) v3 and [MOTION.md](../design/MOTION.md). It must match its screens at 1440, 1280, 1024, 768 and 390 pixels in light and dark: nothing past the viewport edge, no clipped or spilling text, 44 px tap targets on phones. Its routes join the layout matrix from software#484 and #495.

| # | Pull request | Lane | What it changes | Merges after |
|---|---|---|---|---|
| 1 | **Design docs.** Merge `design/agent-access`. | docs | This ADR, the ADR index, ADR 0017, 0008 and 0031's status lines, `docs/design/screens/**` and its README | — |
| 2 | **Secrets in the domain and the store.** Decisions 1–4 and 18. | backend | A migration that renames `credentials` to `secrets`, adds `source` and drops the type label and the agent list. `domain.Secret` and the `Secrets` port in `internal/core`, with fakes in `porttest`. The postgres store. The master key id check over `secrets`. | 1 |
| 3 | **Secrets API and CLI.** Decisions 4, 16 and 17. | backend | `/v1/orgs/{org}/secrets` (list, create, get, patch, `PUT …/value`, delete, import with a dry run) in `contracts/api/openapi.yaml`. `secret:read` and `secret:manage` in `permissions.yaml`. Handlers replace `internal/api/credential*.go`. `goal secret …` replaces `goal credential …`, which prints where it moved for one release. | 2 |
| 4 | **Environment files in the definition.** Decisions 5–8. | backend | `contracts/definitions/environment.schema.json`. The factory schema: `environments/*.yaml`, the agent's `environment`, `runtime.repository.token`, `runtime.modelKey`, `modelAgrees.key`. Removed: `spec.connections`, the agent's `connections`, `toolRule`, `modelConnection`, `runtime.size` and `runtime.tools`. `internal/config` loads environment files and refuses the removed keys with a quick fix. `internal/authoring` adds the checks `secret-missing`, `secret-not-in-scope`, `reserved-env` and `plain-key` (a plain `env` value that looks like a key). `internal/factorymodel` adds the Environments section for the Form view. | 2 |
| 5 | **Catalog and tests.** Decisions 9 and 15. | backend | `internal/connkind` becomes `internal/catalog`: templates from today's kinds, without `aws` and `bedrock`. `GET /v1/catalog`. `POST …/factories/{factory}/environments/{env}/test` calls `tools/list` for MCP servers and runs a template's HTTP test, and keeps the results per factory, environment and server. `goal env test`. | 4 |
| 6 | **Run start.** Decisions 7, 8, 10, 11 and 12. | backend | The engine (`internal/core/engine/connections.go`, `runtask.go`) resolves the agent's environment from the pinned version. It opens secrets after checking scope, writes `env` and `files` (mode 0600), and renders MCP headers as `${GOAL_SECRET_<NAME>}`. It adds values to the scrub list and writes `secret.used` and `last_used_at`. The model key maps to `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY`. The repository token logs git in, and the URL's host picks the code host adapter. Tool modes come from the file, capped by the organization policy, and standing rules are re-keyed (decision 8). The prompt gains a section that lists what the box has. The policy's "Use credentials" becomes "Use secrets". | 4 |
| 7 | **Access read model.** Decision 16. | backend | `GET /v1/orgs/{org}/access`, computed from published versions, test results and secret expiry, grouped by what agents reach, by factory or by secret. `access:read`. `goal access`. | 5 |
| 8 | **Moving over.** Decision 19. | backend | A one-time, idempotent conversion. Credentials become secrets, and sealed connection fields become secrets. Each factory gets a new version, published by Goal with environment files rendered from its kinds and the changelog "Moved connections to environments (ADR 0037)". Standing rules are re-keyed, and knowledge sources, webhooks and Slack settings point at secrets. A factory that can't be converted gets a Needs you entry. A later migration drops `connections`, `connection_kinds` and the connection tests. | 6, 7 |
| 9 | **Secrets pages.** | frontend | `features/credentials` becomes `features/secrets`: the list with filters and the New secret side sheet; the secret page (where used, activity, Replace value, settings, Delete disabled while used); Import. Screens: `connections/secrets`, `secrets-768`, `secret`, `secrets-import`, and `mobile/secrets` and `mobile/secret-new`. | 3 |
| 10 | **Access page.** | frontend | `features/connections` becomes `features/access`: the stat strip, the alert, By what they reach / By factory / By secret, the matrix, and a list below 1000 px. Screens: `connections/access`, `access-768`, `mobile/access`. The sidebar, the phone drawer and every link say Access and Secrets. | 7 |
| 11 | **Environments in the definition editor.** | frontend | `features/factory-editor`: the Form view's Environments section, the YAML view's `environments/` folder, Add a tool (the catalog, pick or create a secret in place, the server's answer, what agents may do, the lines added, Add to draft) and Custom (MCP server, command-line tool, API). Screens: `build/definition-environments`, `-768`, `definition-environment-yaml`, `add-tool`, `add-tool-768`, `add-tool-custom`, `mobile/environments`, `mobile/add-tool`. | 4, 5 |
| 12 | **Everything else that named connections.** | frontend | The agent page's Environment select (`build/agent`), the agents defaults (`build/agents`), factory settings (repository token, model key, environments, Slack via a secret), and onboarding step 2 (`get-started/repository`: URL and token, tested, saved as a secret, no app). Also the knowledge source's secret, and the words in Needs you, Models, Members, Spend, Webhooks, Intake and New factory, as the exported screens show them. | 6, 9 |
| 13 | **Docs.** | docs | `docs/engineering/factory-definition.md` gets an "Environment files" section. Change-map entries for secrets, environments, the catalog, access and the conversion. The docs site replaces `concepts/connections.md` with Access and secrets, and updates the guides that add a connection. The glossary gains Secret, Environment and Access, and drops Connection kind. `frontend.md` and `architecture.md` change, then `task docs:ref:gen`. | 8, 12 |
