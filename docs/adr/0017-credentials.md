# ADR 0017 — Credentials: organization-owned sealed secrets, scoped to factories and agents, that connections refer to as {{name}}

Status: accepted · 2026-10-27 · Builds on [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (sealed connection secrets under the platform master key, and why the Cloudflare Secrets Store was not used; this ADR extends its decision 5, see its [amendment](0008-portable-runs-connections-and-knowledge.md#amendment--2026-10-27-connection-secret-fields-may-refer-to-a-credential)) and [ADR 0016](0016-ui-revamp.md) (the `credentials` and `credential-new` screens are binding)

## Context

Today a secret lives only inside a connection. A connection is an organization row whose secret field values are sealed as one JSON object with AES-256-GCM under the platform master key; the associated data binds organization, connection and key id, and no column holds a secret in the clear ([00051_connections.sql](../../db/migrations/00051_connections.sql)). goald refuses to start unless it holds every master key id that some connection is sealed under (`goal_connection_key_ids`). At run start the engine resolves the run's connections, renders each kind's env, audits each use as `connection.used` and puts every secret value on the run's scrub list (`runEnv` in [internal/core/engine/connections.go](../../internal/core/engine/connections.go)).

That leaves three gaps:

- The same token used by two connections is pasted twice and replaced twice.
- An agent that needs a key without a connection kind around it (an SSH key for a deploy, a cloud role) has nowhere to get it.
- Nothing records when a key expires or when it should be rotated.

ADR 0016 makes the `credentials` and `credential-new` screens in [docs/design/screens/connections/](../design/screens/connections/) binding. They show organization-wide credentials with a type, a factory scope, the agents that use them, the last use, an expiry and a rotation reminder, a `{{name}}` form for using one in a connection, and an import from `.env` or YAML. The [screens index](../design/screens/README.md) leaves to the spec how credentials relate to connections. This ADR decides it, so the parts of assembly "Credentials" (domain, store, connection references, engine, API, CLI and the two screens) build against one record.

Two `{{…}}` syntaxes would now exist, and they must not be confused:

- A **kind template**, `{{field}}` or `{{minted.<name>}}`, lives in the strings of a connection kind's `KindSpec` (env values, MCP headers, git settings; `refs` and `templates` in [internal/core/domain/connection.go](../../internal/core/domain/connection.go)). It is expanded when a kind is rendered.
- A **credential reference**, `{{name}}`, is the whole value an admin types into a secret field of a connection. It is resolved to the credential's value before the kind is rendered, and it never takes part in kind template expansion.

## Decision

1. **A credential is an organization-owned row with a name, a type, a sealed value, a description, a factory scope, an agent list, an expiry and a rotation reminder.** The name follows the connection name pattern `^[a-z][a-z0-9-]{1,38}[a-z0-9]$` and is unique within the organization. The description is optional and at most 500 characters. The expiry is an optional timestamp; the rotation reminder is an optional number of days before the expiry, and it requires an expiry. The row also records who created and last changed it, when, and `last_used_at`. It is under row-level security like every organization table, and it lives in `internal/core/domain` behind a `Credentials` port.

2. **The types are `api_key`, `oauth_token`, `ssh_key`, `cloud_role` and `app_key`, and in this assembly every type reaches a run as one env value.** The UI shows them as API key, OAuth token, SSH key, Cloud role and App key, and the Credentials list's filter tabs are the types. The type is a label: the engine treats every type the same way. Delivery that differs by type (an SSH key written as a file, a cloud role minted per run, an OAuth token that refreshes itself, as the screen shows) is later work with its own decision.

3. **The value is sealed with AES-256-GCM under the platform master key, exactly as connection secrets are, and nothing ever returns it.** A credential stores `key_id`, a 12-byte `nonce` and the `ciphertext`; the associated data binds organization, credential id and key id, so a ciphertext copied to another row does not open. No API answer, CLI output or log line contains a value. Replacing the value is the only write to it; there is no read. goald's start-up check covers credentials too: `goal_connection_key_ids` (or the function that replaces it) returns the key ids of connections and credentials together, and goald refuses to start without each of them.

4. **A credential's factory scope is either all factories, including ones created later, or a list of factory ids; its agent list is kept per factory.** Factory ids are `metadata.id` UUIDs, never names ([AGENTS.md](../../AGENTS.md) rule 2). The agent list maps a factory id to agent names from that factory's definition. The names are stored as data, so a definition that drops an agent leaves a name that matches nothing; it is not an error. A credential with no factories reaches no run.

5. **A connection's secret field whose whole value is `{{name}}` stores a reference to credential `name` instead of a sealed value, and the engine opens it at run start only when the credential allows the run's factory.** The reference is stored in the clear with the connection, beside its sealed values. A secret value that holds `{{…}}` anywhere but as its whole value (`Bearer {{token}}`, `{{a}}{{b}}`) is refused when the connection is written. Non-secret fields, URLs and command lines never take a credential: `{{name}}` there is refused, or is a kind template where a kind allows one. At run start the engine replaces each reference with the credential's value before the kind is rendered. When the credential does not exist, or its scope does not include the run's factory, the run fails with an error that names the connection and the credential and never shows a value. A connection that names a credential gets it whatever the credential's agent list says: the agent list governs only direct use (decision 6).

6. **An agent on a credential's agent list gets it in its run's box as one env variable named after the credential.** The variable is the name upper-cased with each `-` turned into `_`: `cloudflare-token-2027` becomes `CLOUDFLARE_TOKEN_2027`. The factory scope applies too: the run's factory must be in it. A name whose variable is reserved for the platform (`domain.ReservedRunEnv`: `HOME`, `PATH`, `USER`, `SHELL`, the proxy variables, `CLAUDE_CONFIG_DIR` and the prefixes `GOAL_`, `LD_`, `MISE_`) is refused when the credential is created, renamed or imported. When the variable is one that a connection of the same run also sets, the run fails with an error that names the credential, the connection and the variable, never a value; neither one silently wins.

7. **Every opened value joins the run's scrub list, and each use is recorded.** A value opened through a reference or for direct use is added to `runEnv.secrets`, so it is scrubbed from everything the run's commands report, as connection secrets are. Each run that opens a credential updates its `last_used_at` and writes one audit event `credential.used` with the credential and the run, never the value; the engine logs `credential used` with the run and the names.

8. **A credential that a connection refers to cannot be deleted, and a replaced value applies to runs that start afterwards.** Deleting it is refused with 409, and the answer names the connections that refer to it. A run that already started keeps the value it opened; the next run opens the new one.

9. **Credentials import from `.env` or YAML, with a dry run first and a replace-or-skip choice on conflict.** A `.env` file is `KEY=value` lines; each key becomes a name by lower-casing it and turning `_` into `-` (`SENTRY_TOKEN` becomes `sentry-token`). A YAML file has a top-level `credentials:` map of name to value. A dry run returns each name as new or already existing and, for an existing one, how many connections use it, and stores nothing. On a conflict the caller chooses replace (the value is replaced, decision 8) or skip. Every name is checked as in decisions 1 and 6. Imported credentials start with type `api_key` and no factories, so they reach no run until someone scopes them.

10. **Permissions mirror connections: `credential:read` and `credential:manage`.** `credential:read` is for owner, admin and member, and shows names and metadata only (type, description, scope, agents, the connections that use it, last use, expiry, reminder). `credential:manage` is for owner and admin, needs a fresh session, and covers create, change, replace, delete and import. Both are declared in [contracts/api/permissions.yaml](../../contracts/api/permissions.yaml) beside `connection:read` and `connection:manage`, with the same keys and tokens settings.

11. **Rejected alternatives.**
    - *Keep secrets only inside connections.* The same token is then pasted into each connection, rotated in each, and an agent that needs a key without a kind has no place to get it.
    - *Cloudflare Secrets Store.* It holds at most 100 secrets and they are readable only through Worker bindings, so goald cannot open them per run and the platform would not run the same way on Docker Compose ([ADR 0008](0008-portable-runs-connections-and-knowledge.md)).
    - *Encryption in the browser.* The value would be sealed before it reaches goald. It is deferred: the Credentials screen shows its "Encrypt in your browser instead" link as "Not available yet".

## Consequences

- A token used by several connections is stored and replaced once, and the list shows which connections and agents use it and when it expires.
- An agent can get a key without a connection kind, limited by factory and agent.
- The engine's run start gains a step: open the referenced and directly used credentials, check their scope, fail with a named error, and add the values to the scrub list. The engine part builds it with the in-memory fakes in `internal/ports/porttest`.
- A connection's stored shape changes: a secret field is either sealed or a reference. The connection-refs part changes the domain check and the store; existing connections are all sealed and need no migration of their values.
- The master key check now spans two tables; the store part changes it in a new migration and never edits 00051.
- The screens' footer says a credential can be written into "a connection or MCP server setting"; under decision 5 it goes only into a connection's secret field. The credentials-ui part words the copy to match this ADR and lists the difference in its pull request.
- Each later part links this ADR from the docs, rules and change-map entries it changes.
