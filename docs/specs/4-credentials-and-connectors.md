# Phase 4 — Credentials, connectors and MCP gateway

Status: draft for review, **to be redesigned before building** · follows phase 2.2 (needs 2.2 A–E) · part 4.1 Vault and credentials, part 4.2 Connectors and MCP gateway · OpenAPI tags `credentials` (4.1, was `vault`) and `connectors` (4.2) · migrations `00050–00059` (4.1 `00050–00051`, 4.2 `00052–00054`)

(restructure) This spec merges the former phase C (vault and credentials) and phase G (connectors and MCP gateway). Credentials exist for model access, repositories and connectors, so they are designed and built together. (phase 4) It was 2.2's last phase, F, then phase 3; it is now phase 4, built after phase 3, identity and access (file `4-credentials-and-connectors.md`, was `3-credentials-and-connectors.md` and before that `2.2-f-credentials-and-connectors.md`); parts F1 and F2 became 3.1 and 3.2 and are now 4.1 and 4.2, PRs `4.1-n`/`4.2-n`, migrations `00050–00054` (were `00040–00044`; phase 3 took `00040–00049`). (identity) It builds on phase 3's people, roles and permission table: see "Identity in phase 4" below. Part 4.1 is the old C content and part 4.2 the old G content, with their detail kept. References name the part: `4.1 §6.6`, `4.2 §4.3`; pull requests are `4.1-1…4.1-9` and `4.2-1…4.2-26`.

## Design pass before building

This phase comes after 2.2 so it can be designed well. Before any phase 4 pull request, a dedicated design pass re-examines the points below and may rewrite this spec; what follows is the starting point, not a decision.

- **Threat model.** Who can read, use or exfiltrate a secret: people (members with phase 3's org and factory roles; editors change definitions, which choose what a run gets), agents (prompt injection through tool results), the gateway and its upstreams, sidecars, the database, backups and the bucket; multi-org installs; what a compromised goald may reach.
- **Credential lifecycle.** Create, grant, use, rotate, revoke and expire as one model: who may do each, what a running item keeps, what is audited, how a person learns of expiry. It settles the open rotation decision (erase the previous ciphertext at rotation, or keep it 24 h with an undo; review decision 4 recommends erasing).
- **OAuth and MCP authorization.** The current MCP authorization spec revision (protected resource metadata, client ID metadata documents, dynamic registration, resource indicators) and what changed since 4.2 was written; laptop redirect URIs; refresh-token rotation; per-org clients.
- **Secret storage options.** PostgreSQL envelope encryption with a key file or KMS (4.1 as written), AWS Secrets Manager, 1Password; one backend per org or per credential, and how an org moves between them.
- **Per-run minting.** Short-lived tokens minted per run (GitHub App installation tokens, AWS STS, Bedrock), their lifetime against long tasks, and whether model access can be minted too.
- **Approval-gated tools.** Held calls (keep the run versus suspend and resume), built-in tools ("Ask me"), what an approver sees, and expiry.
- **Access control.** Phase 3's roles and permission table apply; this pass confirms the permissions proposed in "Identity in phase 4" and settles the `claude-subscription` rule for multi-user installs.
- **Connector packaging.** Curated versus org-private connectors, MCP Registry import, pinned images and CLI sidecars, how a connector update reaches a factory, and who operates the hosted catalog.

## Identity in phase 4 (from phase 3)

Phase 3 ([spec](3-identity-and-access.md)) ships sign-in, organization and factory roles, API keys, the permission table `api/permissions.yaml` and the fresh-session rule. Phase 4 uses them as follows; the design pass confirms the details.
- **Permissions.** Every phase 4 operation carries an `x-goal-scope` from `api/permissions.yaml`, added in the PR that adds the operation. Proposed: `credential:read` (owners and admins; members see only what their factories' screens show), `credential:manage` and `credential:grant` (owners and admins, `fresh: true`), `vault:manage` (owners, `fresh: true`), `connector:manage` (owners and admins), `connection:test` (`factory_role: editor`), `approval:decide` (`factory_role: approver`, `fresh: true`). None of them is `keys: true` unless the design pass decides otherwise.
- **Who approves** tool calls: the factory's approvers (and editors, owners and admins, who include the approver's rights), the same people who decide items.
- **OAuth flows.** `oauth_states.requested_by` is the signed-in user who started the flow (`Principal.Actor().Ref()`); the callback refuses a state finished by anyone else.
- **One public origin.** `GOAL_PUBLIC_URL` (phase 3) is the base of the OAuth redirect and the client metadata document, and equals Better Auth's `BETTER_AUTH_URL`.
- **Platform operator.** Whoever runs `goal catalog push` or other install-level operations is a platform admin (phase 3's `platform_admins`).
- **Fresh session.** Creating, rotating, revoking, granting and ungranting credentials, changing the vault and deciding tool approvals need a sign-in within 10 minutes (403 `reauth-required`).
- **`claude-subscription` on a multi-user install** (for the design pass). ADR 0003's rule ("only while the install has one person") becomes: a subscription credential is owned by the user who adds it, and resolves only for runs of items that user submitted; it is never used for another person's work. Phase 4 adds the owner column and the check; `LocalMode` below is replaced by it.
- Actors in `*_by` columns and the audit log are `Principal.Actor()` refs (`user:<id>`, `service:<key id>`); phase 4 decides which operations API keys may perform.

**What 2.2 (A–E) leaves for phase 4 (the interim rule, README).** Until phase 4, model access (`GOAL_TASK_ENV`, `CLAUDE_CODE_OAUTH_TOKEN`) and repository access (`env:GOAL_GITHUB_TOKEN` in definitions) are platform secrets of the install resolved through today's `ports.Secrets` env adapter; B validates `env:` references for existence only; C shows Model access and Vault as "Arrives with credentials" and has no Credentials dot; D shows how Basics signs in read-only; E shows block reasons as plain text. Phase 4 replaces each of these: 4.1-5 wires `authoring.VaultCredentials` in place of B's `EnvReferences`, 4.1-6 adds `credentials_needing_attention` to C's `/attention`, 4.1-8 fills C's sections, the sidebar dot and D's `CredentialSelect` (setting `capabilities.credentials`), and 4.1-9 removes the interim exception.

## Part 4.1: Vault and credentials

(restructure) Formerly phase C, "Vault and credentials (core)". It needs A (tenancy, audit, the request actor and `Authorizer` seam) and plugs into B (validation), C (org settings, sidebar), D (Basics) and E (item block reasons), all built before it.

### 1. Summary

Part 4.1 replaces factory secrets in `.env` with an organization-owned vault: credentials are stored in PostgreSQL under envelope encryption (AES-256-GCM per version, data key wrapped by a key-encryption key from a local key file; AWS KMS designed, built with the AWS deployment), granted to factories, and resolved on the host only, per run, with an audit trail. Model access (Claude subscription token, Anthropic API key; Bedrock designed), repository access (GitHub token) and generic connection secrets become credentials referenced as `credential:<name>`; `env:` stays only for platform bootstrap, and `GOAL_TASK_ENV`, `CLAUDE_CODE_OAUTH_TOKEN` and `GOAL_GITHUB_TOKEN` disappear. It ships the `Vault`, `KeyWrapper`, `ModelAuth` and `CredentialProbe` ports, the owner/admin credentials API, CLI, the Credentials and New credential screens (core subset), the Vault and Model access sections of organization settings, and the credential picker the builder's Basics section uses.

### 2. Scope

**In**
- `ports.Vault` (create, rotate, update metadata, revoke, grant, ungrant, list, get, resolve, check references, vault settings), values write-only.
- PostgreSQL envelope-encryption vault in `internal/adapters/postgres`; `ports.KeyWrapper` with the local key-file adapter `internal/adapters/keyfile`; the AWS KMS adapter's design.
- Tables `credentials`, `credential_versions`, `credential_grants`, `credential_uses`, `vault_settings`, `vault_keys`, with RLS.
- Credential kinds needed before connectors: `claude-subscription`, `anthropic-api-key`, `github-token`, `secret` (generic named fields); `bedrock-role` reserved (stored shape defined, not creatable until its adapter exists).
- `ports.Secrets` takes an explicit scope; `credential:` references resolved through the vault; `env:` refused in factory scope after cutover.
- Engine model access through `ports.ModelAuth` (replaces `engine.Config.TaskEnv`); gitworkspace, CodeHost (`github`) and gateway header/env/command references through factory-granted credentials.
- Key rotation (`goald vault add-key | rewrap | remove-key | status`), credential rotation, revocation, expiry and rotation-due status.
- Audit entries through A's `audit_log`; per-run usage records.
- One-time import of the goal factory's `.env` values (`goal credential import-env`), then removal of those variables.
- API (`credentials` tag, was `vault`), CLI (`goal credential …`), web (Credentials, New credential, rotate/revoke dialogs, Vault and Model access settings sections, `CredentialSelect` for D).
- (restructure) Replacing the interim pieces A–E ship (README interim rule): B's `EnvReferences` check by `authoring.VaultCredentials`; C's "Arrives with credentials" placeholders by `ModelAccessSection` and `VaultSection`, plus the Credentials sidebar dot and `credentials_needing_attention` on C's `/attention`; D's read-only Basics lines by `CredentialSelect` (`capabilities.credentials`); E's plain-text block reason by a link to the credential.
- Factory schema fields `spec.runtime.modelCredential`, `spec.runtime.repository.credential`, the `credential:` reference form; deprecation of `spec.runtime.auth`, `spec.runtime.repository.token`, `spec.secrets`.

**Out**
- Connector credentials (OAuth 2.1 with refresh, GitHub App installations, Datadog/Azure/AWS/connection-string kinds, `connector_id`, `oauth_states`, "Sign in with X"): part 4.2, fitting the same tables (section 6.6).
- The `awskms` and `awssecrets` adapters and moving an organization between vault backends: designed here, built with the AWS deployment (not phase 4).
- 1Password: later (unscheduled; AGENTS.md rule 7 names it the last secrets provider).
- Model slots ("6 slots shared by every factory", per-factory slot limits) in Org settings → Model access: not 4.1 (open question 1).
- Notifications for expiring credentials (status is shown; no email or push).

### 3. Canvas boards covered

| Board | Element | Delivered by |
| --- | --- | --- |
| `Credentials.dc.html` | Page header, description, "New credential" | 4.1 |
| | Table header "N credentials in Acme" and "Vault: PostgreSQL with envelope encryption" | 4.1 (`GET /credentials` + `GET /settings/vault`) |
| | Columns Name, Connector, How it signs in, Granted to (chips), Expiry and rotation, Last used, row menu | 4.1, for core kinds: "Connector" shows the system (Claude, GitHub, Other); "How it signs in" the kind label. Rows `acme-github-app`, `jira-oauth`, `aws-readonly`, `datadog-keys` are 4.2 kinds |
| | Expired row (warning icon, "Expired Oct 21"), selected-row accent | 4.1 (status) |
| | Detail panel: name, expired alert, public metadata rows, "Token Hidden, set <date> by <user>", "Paste a new token", "Revoke" | 4.1 |
| | Detail "Granted to" checkboxes with "1 connection" | 4.1 for checkboxes and model/repository use counts; "N connection(s)" counts need 4.2's `connections` table (4.1 shows references found in the factory's current definition, see 10.2) |
| | "Factories without a grant can't see that this credential exists." | 4.1 (API returns only granted credentials to factory scope) |
| | History ("Calls refused, expired Oct 21 to now, 37", "Granted to sre", "Created, 90 day token") | 4.1 (`credential_uses` + audit) |
| | Org sidebar "Credentials" with expired dot, rail key icon | C renders the entry; 4.1 adds the dot (restructure: C ships without it) from `credentials_needing_attention`, which 4.1 adds to C's `/attention`, and `summary.expired` / `summary.attention` in `GET /credentials` |
| `Credential-New.dc.html` | Breadcrumb, title, description | 4.1 |
| | Step 1 "Connector" grid | 4.1 shows core systems: **Claude** (subscription token, API key; Bedrock disabled "Needs the AWS deployment"), **GitHub** (token; "GitHub App" disabled "Coming with connectors"), **Other secret**. 4.2 replaces the grid with catalog connectors (GitHub, Jira, Datadog, Azure, AWS, PostgreSQL) and keeps Claude |
| | Step 2 "Sign in to X": method radios | 4.1: one method per core kind ("Paste a token/key"); "Sign in with Datadog" (OAuth) is 4.2 |
| | Name ("How factories and logs refer to it"), kind-specific public fields (Datadog site → e.g. GitHub account filled by test) | 4.1 for core kinds |
| | Secret inputs with Show toggle, lock note "Keys are encrypted as soon as you save…" | 4.1 |
| | "Remind me to rotate" (30, 90 days, never) | 4.1 (`rotate_every_days`); plus "Expires on" date, 4.1 addition (needed for subscription tokens) |
| | Step 3 "Grant to factories" with per-factory notes | 4.1: notes "Uses it for model access / its repository" or "Not used yet"; "Can use it in its Datadog connection" is 4.2 |
| | "Granting doesn't change a running factory. Each factory picks the credential in its Connections." | 4.1 with core wording: "…Each factory picks the credential in its Definition." 4.2 restores "Connections" |
| | Cancel, "Test calls … before saving", "Test and save" | 4.1: test for `github-token` and `anthropic-api-key`; other kinds show "Save" |
| `Org-Settings.dc.html` | Vault section: PostgreSQL with envelope encryption (selected), AWS Secrets Manager, 1Password "Coming in a later phase", note "Changing the vault moves all N credentials…" | 4.1 (`VaultSection`, `GET/PUT /settings/vault`). AWS Secrets Manager is rendered disabled "Available on AWS deployments" until its adapter exists; the move itself is not built |
| | Model access: radio cards per model credential ("Claude subscription, Max plan · Signed in by F. Cruz", "Anthropic API key · Billed per token…") | 4.1 (`ModelAccessSection`) |
| | "Most slots each factory may use" | Not 4.1 (open question 1) |
| | General, Danger zone, "Save changes" | C (page) and A (API); 4.1 replaces C's two placeholders and makes C's Save also call `PUT /settings/vault` for them (restructure) |
| `New-Basics.dc.html`, `Define-Basics.dc.html` | Repository select "Through the GitHub connection" | D renders (read-only access line until 4.1, restructure); 4.1 supplies `CredentialSelect` and `GET /factories/{factory}/credentials`. In 4.1/D the field is repository URL + credential select; 4.2 turns it into the GitHub connection picker |
| | Model access (not on the board) | D adds a "Model access" select under Basics ("Organization default" + granted model credentials) using 4.1's `CredentialSelect` (open question 2) |
| `Define-YAML.dc.html` | `credential: acme-github-app` under `connections` | 4.2 (connections); 4.1 defines the name form and `credential:<name>` references |

### 4. Domain and engine changes

#### 4.1 Domain (`internal/domain/credential.go`, new)

```go
type CredentialID string // UUIDv7

// CredentialKind is how a credential signs in. Kinds are platform code (how goald turns a
// secret into a header, an env var or a clone), not factory behaviour.
type CredentialKind string

const (
    KindClaudeSubscription CredentialKind = "claude-subscription"
    KindAnthropicAPIKey    CredentialKind = "anthropic-api-key"
    KindBedrockRole        CredentialKind = "bedrock-role" // reserved: not creatable in phase 4
    KindGitHubToken        CredentialKind = "github-token"
    KindSecret             CredentialKind = "secret"       // generic named fields
)

// CredentialPurpose is what a resolution is for; a kind serves some purposes only.
type CredentialPurpose string

const (
    PurposeModel      CredentialPurpose = "model"
    PurposeRepository CredentialPurpose = "repository"
    PurposeConnection CredentialPurpose = "connection" // gateway headers, sidecar env and command
    PurposePlatform   CredentialPurpose = "platform"   // env: bootstrap only, never a credential
)

type CredentialField struct {
    Name     string // snake_case
    Label    string
    Secret   bool   // secret fields are encrypted; public ones live in credentials.metadata
    Required bool
}

type CredentialKindSpec struct {
    Kind      CredentialKind
    Label     string // "Claude subscription", "Anthropic API key", "GitHub token", "Secret"
    System    string // "Claude", "GitHub", "Other": the table's Connector column
    Fields    []CredentialField
    Purposes  []CredentialPurpose
    Default   string // field a bare credential:<name> resolves to
    Available bool   // false: listed, not creatable (bedrock-role)
    OwnerOnly bool   // claude-subscription: owned by one user, resolves only for that user's own items ("Identity in phase 4"; design pass)
}

func CredentialKinds() []CredentialKindSpec
func KindSpec(k CredentialKind) (CredentialKindSpec, bool)

type CredentialStatus string // active | expiring | rotation_due | expired | revoked

// StatusAt: revoked > expired (expires_at <= now) > expiring (expires_at within 14 days) >
// rotation_due (current version older than rotate_every) > active.
func CredentialStatusAt(expiresAt *time.Time, rotateDueAt *time.Time, revokedAt *time.Time, now time.Time) CredentialStatus

// SecretRef is a parsed reference: a literal, env:NAME, or credential:<name|uuid>[#field].
type SecretRef struct {
    Scheme SecretScheme // literal | env | credential
    Name   string       // variable name, or credential name or id
    Field  string       // credential field; empty = the kind's Default
}

func ParseSecretRef(s string) (SecretRef, error) // error only for a malformed credential:/env: ref

// ModelEnv maps a resolved model credential to the agent task's environment.
func ModelEnv(kind CredentialKind, fields map[string]string) (map[string]string, error)
```

Kind registry (core):

| Kind | System / label | Secret fields | Public fields (`metadata`) | Purposes | Default field | Probe |
| --- | --- | --- | --- | --- | --- | --- |
| `claude-subscription` | Claude / Claude subscription | `token` | `plan` (free text, "Max plan"), `account` | model | `token` | none (would spend quota) |
| `anthropic-api-key` | Claude / Anthropic API key | `api_key` | `workspace` (free text) | model, connection | `api_key` | `GET https://api.anthropic.com/v1/models` |
| `bedrock-role` | Claude / Amazon Bedrock | none | `region`, `role_arn`, `external_id` | model | — | later |
| `github-token` | GitHub / GitHub token | `token` | `account` (login, set by the probe), `api_url` (reserved, GHES) | repository, connection | `token` | `GET /user` on `GOAL_GITHUB_API_URL`; reads `github-authentication-token-expiration` into `expires_at` |
| `secret` | Other / Secret | 1–8 author-named fields, default one field `value` | `description`, `url` | connection | `value` (or the only field) | none |

`ModelEnv`: `claude-subscription` → `{CLAUDE_CODE_OAUTH_TOKEN, GOAL_MODEL_AUTH=claude-subscription}`; `anthropic-api-key` → `{ANTHROPIC_API_KEY, GOAL_MODEL_AUTH=anthropic-api-key}`; `bedrock-role` → error "Bedrock model access is not available yet".

Credential names: the factory slug pattern `^[a-z][a-z0-9-]{1,38}[a-z0-9]$`, unique among an organization's non-revoked credentials, **immutable** (definitions and logs refer to them; rename = create new, re-point, revoke old).

`domain.Runtime` gains `ModelCredential string` (a credential name; empty = the organization's model access). `domain.Repository.AuthRef` now holds `credential:<name>` (or a legacy `env:` ref until cutover).

**Credential host binding (keeps 2.2's repository host binding).** A credential is sent only to the host(s) of its code host or service, never to a URL a definition names: a `github-token` clones and merges only on its GitHub's web host (`GOAL_GITHUB_WEB_URL`'s, or the credential's own once `api_url` is used) and reaches its API only, so both the credentials check (`repository-host-not-allowed`) and `gitworkspace`'s clone-time refusal before resolving keep applying to `credential:` references, as they do to `env:GOAL_GITHUB_TOKEN` today (architecture.md, `Workspaces`). The same holds in the gateway: every credential carries its host bindings (the hosts a connection may send it to; a `github-token`'s include its MCP server, `api.githubcopilot.com`), and the gateway checks them, scheme https and host, before it resolves a `credential:` header word for an upstream request, and never gives a host-bound credential to a sidecar, as it checks 2.2's `domain.SecretPolicy` for `env:` references today (architecture.md, MCP gateway, "Secret policy"); a refused credential makes the connection unavailable without being resolved. Binding is checked at run time, not only at validation, so a version published before a binding changed cannot send the credential elsewhere.

#### 4.2 Engine

- `engine.Deps` gains `ModelAuth ports.ModelAuth` (required). `engine.Config.TaskEnv` is removed at cutover (4.1-9); until then it is the fallback when `ModelAuth` returns `ErrNoModelAccess` (logged as deprecated once per factory).
- Per run, **before** `Workspaces.Prepare`: `env, err := ModelAuth.TaskEnv(ctx, scope{Org, Factory, Run, PurposeModel}, line.Runtime.ModelCredential)`. An error wrapping `ports.ErrCredentialUnavailable` blocks the item (existing `block` path, no retry, rework count unchanged) with event data `{reason: "model access: credential claude-acme expired", reason_code: "credential_unavailable", credential: "claude-acme"}`; any other error blocks as today.
- `TaskSpec.Env` carries exactly `ModelEnv` output; nothing else secret.
- `Workspaces.Prepare` receives `WorkspaceSpec.OrgID` (scope built by the adapter from the spec with `PurposeRepository`).
- `engine.Merge` passes `ports.RepoRef{Repository, Scope{Org, Factory, Purpose: repository, Actor: by}}` to `CodeHost`.
- Gateway task token signing keeps `GatewayKeyRef` (platform scope, `env:`).

#### 4.3 Runner and worker

- `docker.taskEnv`: no unconditional `ANTHROPIC_API_KEY` removal. It keeps only the model variables matching `GOAL_MODEL_AUTH` (`claude-subscription` → `CLAUDE_CODE_OAUTH_TOKEN`; `anthropic-api-key` → `ANTHROPIC_API_KEY`), drops the other model variables, `GOAL_GATEWAY_TOKEN` handling unchanged.
- `worker/src/agent.ts`: `hiddenEnv` derives from `GOAL_MODEL_AUTH` the same way (an API key never silently takes over billing: it is passed only when the factory's model credential is an API key). `worker/README.md` updated.

### 5. Ports and adapters

#### 5.1 `ports.Secrets` (changed)

```go
// SecretScope says who resolves a reference and why. FactoryID empty = platform scope.
type SecretScope struct {
    OrgID     domain.OrgID              // from A
    FactoryID domain.FactoryID
    RunID     domain.RunID              // empty when not for an agent run (a merge, a test)
    Purpose   domain.CredentialPurpose
    Actor     domain.Actor              // from A; who acts when RunID is empty
}

type Secrets interface {
    // Resolve returns the value ref points to: a literal as is, env:NAME in platform scope only
    // (after cutover), credential:<name|id>[#field] through the vault for scope. Errors wrap
    // ErrSecretNotFound or ErrCredentialUnavailable and never contain values.
    Resolve(ctx context.Context, scope SecretScope, ref string) (string, error)
}
```

Callers: engine (gateway key: platform scope), `gitworkspace` (repository), `github` (repository), gateway headers, sidecar env and command words (connection). `envsecrets.Env` ignores scope and rejects `credential:` like `op://`.

#### 5.2 `ports.Vault` (new)

```go
var (
    ErrCredentialUnavailable = errors.New("credential unavailable") // umbrella for CredentialError
    ErrCredentialNameTaken   = errors.New("credential name taken")
    ErrNoModelAccess         = errors.New("no model access configured")
)

type UnavailableReason string // not_found | not_granted | expired | revoked | wrong_purpose | not_available

// CredentialError explains a refused resolution; errors.Is(err, ErrCredentialUnavailable).
type CredentialError struct{ Credential string; Reason UnavailableReason }

// (review) Per the README convention every method reads the org from ctx (A's domain.OrgFrom);
// the explicit `org` parameters and `OrgID` fields below are dropped when the port is written.
type Vault interface {
    Create(ctx context.Context, c NewCredential) (Credential, error)
    // Rotate stores a new version, makes it current and destroys the previous version's
    // ciphertext in the same transaction.
    Rotate(ctx context.Context, r Rotation) (Credential, error)
    // Update changes public metadata, expires_at and rotate_every; never secrets or the name.
    Update(ctx context.Context, u CredentialUpdate) (Credential, error)
    // Revoke destroys every version's ciphertext; the row stays for history; the name is freed.
    Revoke(ctx context.Context, org domain.OrgID, id domain.CredentialID, by domain.Actor) error
    Grant(ctx context.Context, org domain.OrgID, id domain.CredentialID, f domain.FactoryID, by domain.Actor) error
    Ungrant(ctx context.Context, org domain.OrgID, id domain.CredentialID, f domain.FactoryID, by domain.Actor) error

    // Credential returns metadata by id or name (ErrNotFound).
    Credential(ctx context.Context, org domain.OrgID, ref string) (Credential, error)
    List(ctx context.Context, q CredentialQuery) ([]Credential, error)
    History(ctx context.Context, org domain.OrgID, id domain.CredentialID, after string, limit int) ([]CredentialHistoryEntry, string, error)

    // Resolve decrypts the current version for scope after checking org, grant (or the org's
    // model access for PurposeModel), status and purpose; it records the use. PurposeTest
    // resolutions (probes) skip the grant check and record nothing.
    Resolve(ctx context.Context, scope SecretScope, ref domain.SecretRef) (Secret, error)
    // Check reports every reference a factory's definition makes that would not resolve
    // (B's publish validation, D's problems list). It decrypts nothing.
    Check(ctx context.Context, org domain.OrgID, f domain.FactoryID, uses []CredentialUse) ([]CredentialProblem, error)

    Settings(ctx context.Context, org domain.OrgID) (VaultSettings, error)
    SetSettings(ctx context.Context, org domain.OrgID, s VaultSettings, by domain.Actor) (VaultSettings, error)
}

type NewCredential struct {
    OrgID       domain.OrgID
    Name        string
    Kind        domain.CredentialKind
    Metadata    map[string]string
    Fields      map[string]string // secret fields; zeroed by the caller after Create returns
    ExpiresAt   *time.Time
    RotateEvery time.Duration     // 0 = no reminder
    Grants      []domain.FactoryID
    By          domain.Actor
}

type Rotation struct {
    OrgID     domain.OrgID
    ID        domain.CredentialID
    Fields    map[string]string // every secret field of the kind
    ExpiresAt *time.Time        // replaces the previous expiry (nil clears it)
    By        domain.Actor
}

type CredentialUpdate struct {
    OrgID       domain.OrgID
    ID          domain.CredentialID
    Metadata    map[string]string // nil = unchanged
    ExpiresAt   **time.Time       // nil = unchanged; *nil = clear
    RotateEvery *time.Duration
    By          domain.Actor
}

type Credential struct {
    ID           domain.CredentialID
    OrgID        domain.OrgID
    Name         string
    Kind         domain.CredentialKind
    Metadata     map[string]string
    Version      int
    VersionAt    time.Time
    VersionBy    domain.Actor
    ExpiresAt    *time.Time
    RotateEvery  time.Duration
    RotateDueAt  *time.Time
    LastUsedAt   *time.Time
    Grants       []domain.FactoryID
    ModelDefault bool // the organization's model access
    CreatedAt    time.Time
    CreatedBy    domain.Actor
    RevokedAt    *time.Time
    Status       domain.CredentialStatus
}

type CredentialQuery struct {
    OrgID     domain.OrgID
    Factory   domain.FactoryID         // when set: only credentials usable by it (granted, or model access)
    Purpose   domain.CredentialPurpose // when set: only kinds serving it
    Revoked   bool                     // include revoked
    After     string                   // name cursor
    Limit     int                      // default 100, max 500
}

type Secret struct {
    CredentialID domain.CredentialID
    Name         string
    Kind         domain.CredentialKind
    Version      int
    Fields       map[string]string
}

func (s Secret) Value(field string) (string, error) // field "" = the kind's Default

type CredentialUse struct {
    Ref     domain.SecretRef
    Purpose domain.CredentialPurpose
    Path    string // where in the definition: "spec.runtime.repository.credential"
}

type CredentialProblem struct {
    CredentialUse
    Reason UnavailableReason
    Detail string // "github-goal is not granted to goal"
}

type CredentialHistoryEntry struct {
    Cursor  string
    Kind    string // created | rotated | updated | granted | ungranted | revoked | used | refused
    At      time.Time
    Until   *time.Time   // uses: last_at
    Count   int64        // uses: how many
    Actor   domain.Actor
    Factory domain.FactoryID
    Run     domain.RunID
    Reason  UnavailableReason
    Data    map[string]any // version, expires_at, rotate_every_days: never secrets
}

type VaultSettings struct {
    Backend         string               // postgres (only accepted value in phase 4)
    ModelCredential *domain.CredentialID // the organization's model access
    UpdatedAt       time.Time
    UpdatedBy       domain.Actor
}
```

`domain.PurposeTest` (internal value, never in a definition) is added for probes.

#### 5.3 `ports.KeyWrapper` (new sub-port)

```go
// KeyWrapper encrypts data keys under a key-encryption key (KEK) that never enters the
// database: a local key file on a laptop, AWS KMS in the cloud.
type KeyWrapper interface {
    // ActiveKeyID names the KEK Wrap uses.
    ActiveKeyID() string
    // Wrap encrypts dek under the active KEK, bound to aad.
    Wrap(ctx context.Context, dek, aad []byte) (WrappedKey, error)
    // Unwrap returns the data key; ErrUnknownKey when w.KeyID is not loaded, an error when
    // aad or the wrapped bytes do not authenticate.
    Unwrap(ctx context.Context, w WrappedKey, aad []byte) ([]byte, error)
}

type WrappedKey struct {
    KeyID      string
    Ciphertext []byte
}

var ErrUnknownKey = errors.New("unknown key-encryption key")
```

**`internal/adapters/keyfile`** (built in 4.1).
- `keyfile.Load(path string) (*Wrapper, error)`, `keyfile.Generate(path string) error`, `keyfile.AddKey(path string) (id string, err error)`, `keyfile.RemoveKey(path, id string) error`.
- File: UTF-8 text, `#` comments, one base64 (std) 32-byte key per line; the first key is active, the others unwrap only. Written atomically (temp file + rename) with mode `0600`.
- `Load` refuses a file that is not a regular file, has any group/other permission bit (`mode&0o077 != 0`), is a symlink, or holds a key that is not 32 bytes, naming the path and never the content.
- Key id: `local:` + first 16 hex chars of `SHA-256("goal-kek-id/v1" || key)`; the id identifies the key without revealing it.
- Wrap: AES-256-GCM with the KEK, random 12-byte nonce, `Ciphertext = nonce || sealed`, AAD = caller's aad.
- Default path `GOAL_VAULT_KEY_FILE`, else `<os.UserConfigDir()>/goal/vault.key` (outside the repository; `.gitignore` gets `vault.key` too as a guard).

**`internal/adapters/awskms`** (designed, built with the AWS deployment).
- Key id = the KMS key ARN (`GOAL_VAULT_KMS_KEY_ARN`; when set, goald uses KMS instead of the key file).
- Wrap = `kms:Encrypt` (KeyId, Plaintext = DEK, `EncryptionContext{"goal:aad": base64(SHA-256(aad))}`); Unwrap = `kms:Decrypt` with the same context.
- KMS automatic key rotation keeps the ARN, so no rewrap; changing keys uses `goald vault rewrap`.
- Caches unwrapped **data keys** (not values) in memory for at most 5 minutes, bounded to 1,024 entries keyed by SHA-256 of the wrapped bytes; a revoked or rotated credential is still refused at once because ciphertext and `revoked_at` are checked before unwrapping. The cache is optional state another instance never needs.
- IAM (Terraform, later): the goald task role may `kms:Encrypt`/`kms:Decrypt` on that key only, with `kms:EncryptionContextKeys` = `goal:aad`.

#### 5.4 `ports.ModelAuth` (new; named in architecture.md as "later")

```go
// ModelAuth gives one agent task model access: the environment the worker needs.
type ModelAuth interface {
    // TaskEnv resolves the factory's model credential (name; "" = the organization's model
    // access) for scope and returns the task variables (domain.ModelEnv). ErrNoModelAccess
    // when neither is set; CredentialError when the credential cannot be used.
    TaskEnv(ctx context.Context, scope SecretScope, credential string) (map[string]string, error)
}
```

#### 5.5 `ports.CredentialProbe` (new)

```go
// CredentialProbe checks a secret against its system before it is saved ("Test and save").
type CredentialProbe interface {
    Probe(ctx context.Context, kind domain.CredentialKind, metadata, fields map[string]string) (ProbeResult, error)
}

type ProbeResult struct {
    OK        bool
    Detail    string            // "Signed in as acme-bot", or the system's refusal, values scrubbed
    Metadata  map[string]string // filled public fields (github-token: account)
    ExpiresAt *time.Time        // when the system reports one
}
```

Implemented by `internal/adapters/github` (`github-token`; `GET /user`, no redirects, token only in `Authorization`, scrubbed from errors, as `call` does today) and a new `internal/adapters/anthropic` (`anthropic-api-key`; `GET /v1/models?limit=1` with `x-api-key` and `anthropic-version`, 10 s timeout, no redirects). `cmd/goald` wires a `map[domain.CredentialKind]ports.CredentialProbe` into the API.

#### 5.6 `internal/secrets` (new core package)

`secrets.Router` implements `ports.Secrets` and `ports.ModelAuth` over `ports.Vault` (credentials) and a platform `ports.Secrets` (`envsecrets`):

```go
func NewRouter(v ports.Vault, platform ports.Secrets, cfg RouterConfig, log *slog.Logger) *Router

type RouterConfig struct {
    // AllowFactoryEnv keeps env: references working in factory scope during the migration
    // (logged once per factory and reference). Removed in 4.1-9 with the field itself.
    AllowFactoryEnv bool
    // (identity) 2.2's LocalMode is gone: a claude-subscription credential resolves only for runs
    // of items its owner submitted ("Identity in phase 4"; design pass).
}
```

- Literal → returned as is. `env:` → platform resolver when `scope.FactoryID == ""`, else allowed only with `AllowFactoryEnv`, otherwise a `CredentialError{Reason: wrong_purpose}` with detail "env: references are for platform settings; use credential:<name>".
- `credential:` → `Vault.Resolve`, then `Secret.Value(field)`.
- `TaskEnv`: name → `Vault.Resolve(PurposeModel)`; empty name → `Vault.Settings(org).ModelCredential` → resolve; none → `ErrNoModelAccess`; then `domain.ModelEnv`. `claude-subscription` for an item its owner did not submit → `CredentialError{not_available}` (identity; design pass).

#### 5.7 Other adapter changes

| Package | Change |
| --- | --- |
| `internal/adapters/postgres` | `postgres.NewVault(pool, wrapper ports.KeyWrapper, clock ports.Clock) *Vault` implementing `ports.Vault`; `envelope.go` (seal/open); `(*Vault).Rewrap(ctx, org, limit) (int, error)`, `(*Vault).EnsureKeys(ctx, wrapperIDs []string, active string) error` for goald's start and `vault` subcommands |
| `internal/authoring` (B's package) | (restructure) `VaultCredentials{Vault ports.Vault}` implements B's `authoring.Credentials`: converts `config.SecretUse` to `ports.CredentialUse`, calls `Vault.Check` and maps reasons to B's codes (B §4.7); goald wires it instead of `EnvReferences` |
| `internal/adapters/gitworkspace` | `Secrets.Resolve` with `{OrgID, FactoryID, RunID, PurposeRepository}` from `WorkspaceSpec` |
| `internal/adapters/github` | `CodeHost` methods take `ports.RepoRef{Repository domain.Repository; Scope ports.SecretScope}` instead of `domain.Repository`; implements `CredentialProbe` |
| `internal/adapters/envsecrets` | new signature; `credential:` added to foreign prefixes |
| `internal/adapters/docker` | `taskEnv` per 4.3 |
| `internal/gateway` | `refPrefixes` gains `credential:`; `session.resolve` passes `{OrgID, FactoryID, RunID, PurposeConnection}` from the task claims (A adds `OrgID` to `TaskClaims`) and the target url (`""` for a sidecar), checked against the credential's host bindings before resolving, as `domain.SecretPolicy` is today; a `CredentialError` makes the connection unavailable for that request with the reason in the `goal/connections` `_meta` ("credential grafana-acme expired") and is logged without values |

#### 5.8 depguard

```yaml
secrets:
  files: ["**/internal/secrets/**"]
  deny:
    - pkg: github.com/byFrederick/goal/internal/adapters
      desc: the router reaches the vault and the environment through ports
    - pkg: github.com/byFrederick/goal/internal/engine
      desc: secrets resolution never drives the engine
    - pkg: github.com/byFrederick/goal/internal/config
      desc: the router receives references, not definitions
    - pkg: github.com/aws
      desc: AWS access belongs in internal/adapters
    - pkg: github.com/jackc/pgx
      desc: database access belongs in internal/adapters/postgres
```

The existing `adapters` rule already keeps `keyfile`, `anthropic` and `awskms` away from engine, config and api; architecture.md's import table gains the `internal/secrets` row.

### 6. Data model

Migrations `00050` and `00051` (range 00050–00059). They assume A's `organizations(id)`, `factories.org_id` with `UNIQUE (org_id, id)`, `agent_runs.org_id`, `audit_log`, roles `goal_owner`/`goal_app`.

#### 6.1 `00050_vault.sql`

```sql
-- +goose Up
-- Credentials: an organization's secrets, envelope-encrypted. Metadata here; ciphertext in
-- credential_versions. Nothing in this file stores a secret in clear.

-- Platform table (no org): the key-encryption keys that have wrapped data keys, by id only.
CREATE TABLE vault_keys (
  id          text PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 2048),   -- local:<16 hex> or a KMS key ARN
  created_at  timestamptz NOT NULL DEFAULT now(),
  retired_at  timestamptz                                               -- set by rewrap when nothing uses it
);

-- (review) A's tenancy kit for every org table below: org_id NOT NULL DEFAULT goal_org(),
-- credentials.org_id REFERENCES organizations (id) ON DELETE CASCADE, child FKs ON DELETE CASCADE,
-- policies named org_isolation with (SELECT goal_org()); credential_uses.run_id becomes the
-- composite (org_id, run_id) REFERENCES agent_runs (org_id, id).
CREATE TABLE credentials (
  id            uuid PRIMARY KEY,
  org_id        uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  name          text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  kind          text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),   -- validated in Go (domain.KindSpec)
  metadata      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object' AND pg_column_size(metadata) <= 8192),
  current_version integer NOT NULL DEFAULT 1 CHECK (current_version >= 1),
  rotate_every  interval CHECK (rotate_every IS NULL OR rotate_every BETWEEN interval '1 day' AND interval '730 days'),
  expires_at    timestamptz,
  last_used_at  timestamptz,
  created_by    text,                                -- A's Actor.Ref() (no-auth); NULL: system
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  revoked_at    timestamptz,
  revoked_by    text,
  UNIQUE (org_id, id)
);
-- Names are unique among live credentials; a revoked name can be reused.
CREATE UNIQUE INDEX credentials_org_name_key ON credentials (org_id, name) WHERE revoked_at IS NULL;
CREATE INDEX credentials_org_expiry_idx ON credentials (org_id, expires_at) WHERE revoked_at IS NULL AND expires_at IS NOT NULL;

CREATE TABLE credential_versions (
  id             uuid PRIMARY KEY,
  org_id         uuid NOT NULL,
  credential_id  uuid NOT NULL,
  version        integer NOT NULL CHECK (version >= 1),
  alg            text NOT NULL DEFAULT 'A256GCM' CHECK (alg = 'A256GCM'),
  nonce          bytea CHECK (octet_length(nonce) = 12),
  ciphertext     bytea CHECK (octet_length(ciphertext) BETWEEN 17 AND 65552),   -- ≤ 64 KiB payload + tag
  wrapped_dek    bytea CHECK (octet_length(wrapped_dek) <= 4096),
  kek_id         text REFERENCES vault_keys (id),
  created_by     text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  destroyed_at   timestamptz,                                                 -- rotated away or revoked: ciphertext erased
  FOREIGN KEY (org_id, credential_id) REFERENCES credentials (org_id, id),
  UNIQUE (credential_id, version),
  -- A live version has all four; a destroyed one has none.
  CHECK ((destroyed_at IS NULL AND nonce IS NOT NULL AND ciphertext IS NOT NULL AND wrapped_dek IS NOT NULL AND kek_id IS NOT NULL)
      OR (destroyed_at IS NOT NULL AND nonce IS NULL AND ciphertext IS NULL AND wrapped_dek IS NULL AND kek_id IS NULL))
);
-- Rewrap finds live versions not under the active key.
CREATE INDEX credential_versions_kek_idx ON credential_versions (kek_id) WHERE destroyed_at IS NULL;

CREATE TABLE credential_grants (
  org_id         uuid NOT NULL,
  credential_id  uuid NOT NULL,
  factory_id     uuid NOT NULL,
  granted_by     text,
  granted_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (credential_id, factory_id),
  FOREIGN KEY (org_id, credential_id) REFERENCES credentials (org_id, id),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);
CREATE INDEX credential_grants_factory_idx ON credential_grants (factory_id);

-- One row per (credential, factory, run, purpose, outcome): "used by run" and "calls refused".
CREATE TABLE credential_uses (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id         uuid NOT NULL,
  credential_id  uuid NOT NULL,
  factory_id     uuid NOT NULL,
  run_id         uuid REFERENCES agent_runs (id),     -- NULL: not by an agent run (a merge by a person)
  purpose        text NOT NULL CHECK (purpose IN ('model', 'repository', 'connection')),
  outcome        text NOT NULL CHECK (outcome IN ('ok', 'not_granted', 'expired', 'revoked', 'wrong_purpose', 'not_available')),
  version        integer,                              -- the version used (ok only)
  first_at       timestamptz NOT NULL DEFAULT now(),
  last_at        timestamptz NOT NULL DEFAULT now(),
  uses           bigint NOT NULL DEFAULT 1 CHECK (uses >= 1),
  FOREIGN KEY (org_id, credential_id) REFERENCES credentials (org_id, id),
  UNIQUE NULLS NOT DISTINCT (credential_id, factory_id, run_id, purpose, outcome)
);
CREATE INDEX credential_uses_history_idx ON credential_uses (credential_id, last_at DESC);

ALTER TABLE credentials         ENABLE ROW LEVEL SECURITY;
ALTER TABLE credential_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE credential_grants   ENABLE ROW LEVEL SECURITY;
ALTER TABLE credential_uses     ENABLE ROW LEVEL SECURITY;

CREATE POLICY org_isolation ON credentials
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
CREATE POLICY org_isolation ON credential_versions
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
CREATE POLICY org_isolation ON credential_grants
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
CREATE POLICY org_isolation ON credential_uses
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

-- History is kept: the app may not delete credentials, versions or uses; grants can be removed.
GRANT SELECT, INSERT, UPDATE ON credentials, credential_versions, credential_uses TO goal_app;
GRANT SELECT, INSERT, DELETE ON credential_grants TO goal_app;
GRANT SELECT, INSERT, UPDATE ON vault_keys TO goal_app;
REVOKE DELETE ON credentials, credential_versions, credential_uses, vault_keys FROM goal_app;
```

(`REVOKE` guards against default privileges A may set; harmless otherwise.)

#### 6.2 `00051_vault_settings.sql`

```sql
-- +goose Up
-- Per-organization vault choices. A missing row means the defaults (postgres, no model access).
CREATE TABLE vault_settings (
  org_id               uuid PRIMARY KEY DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,   -- (review)
  backend              text NOT NULL DEFAULT 'postgres' CHECK (backend IN ('postgres', 'aws-secrets-manager', '1password')),
  model_credential_id  uuid,
  updated_by           text,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (org_id, model_credential_id) REFERENCES credentials (org_id, id)
);
ALTER TABLE vault_settings ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON vault_settings
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));   -- (review)
GRANT SELECT, INSERT, UPDATE ON vault_settings TO goal_app;
```

No backfill: both migrations create empty tables; data arrives through the import (section 14).

#### 6.3 Envelope encryption (`internal/adapters/postgres/envelope.go`)

Per version:
1. `dek := 32 random bytes` (`crypto/rand`).
2. `payload := JSON {"f": {field: value…}}` (only the kind's secret fields; ≤ 64 KiB).
3. `aad := "goal/credential/v1|" + org_id + "|" + credential_id + "|" + version + "|" + kind`.
4. `nonce := 12 random bytes`; `ciphertext := AES-256-GCM(dek).Seal(payload, nonce, aad)`.
5. `wrapped := KeyWrapper.Wrap(dek, "goal/dek/v1|" + org_id + "|" + credential_id + "|" + version)`.
6. Store `nonce, ciphertext, wrapped.Ciphertext, wrapped.KeyID`; zero `dek` and `payload` byte slices (best effort; Go strings returned to callers cannot be zeroed).

Binding org, credential, version and kind into both AADs means a row copied to another credential, organization or version, or a changed kind, fails to decrypt, even for someone with write access to the database. A fresh data key per version means rotation never reuses a key.

#### 6.4 Behaviour of the PostgreSQL vault

- Every method is one transaction that starts with `SET LOCAL goal.org_id = $org` (A's helper) and writes its audit row in the same transaction.
- `Create`: validate kind (available, local-only rule), name, fields (required present, unknown rejected, each ≤ 16 KiB, total ≤ 64 KiB), grants (factories in the org); insert credential, version 1, grants; `ErrCredentialNameTaken` on the unique index.
- `Rotate`: `SELECT … FOR UPDATE` the credential (not revoked); insert version n+1; set `current_version`, `expires_at`; erase version n (`nonce, ciphertext, wrapped_dek, kek_id = NULL, destroyed_at = now()`). Nothing resolves an old version: runs keep what they resolved in memory (a running container keeps its model token until it ends), and the gateway resolves per call, so a rotation takes effect on the next call.
- `Revoke`: set `revoked_at/by`; erase every live version; keep grants (history shows them) but resolution refuses `revoked`; if it is the org's model access, clear `vault_settings.model_credential_id` in the same transaction.
- `Resolve`: one query joins credential (by id or live name), current version, grant for `scope.FactoryID` and `vault_settings` (model access); checks in order: found → purpose allowed for kind → granted (or model access for `PurposeModel`) → not revoked → not expired; then unwraps and opens. It upserts `credential_uses` (`ON CONFLICT … DO UPDATE SET uses = uses + 1, last_at = now()`), sets `last_used_at` only when older than one minute, and appends an audit `credential.used` / `credential.refused` only when the upsert inserted (first use per run and outcome; `xmax = 0`).
- `Check`: the same checks without decrypting or recording.
- `Rewrap(org, limit)`: under `pg_try_advisory_xact_lock(hashtext('goal.vault.rewrap'))`, selects up to `limit` live versions with `kek_id <> active` `FOR UPDATE SKIP LOCKED`, unwraps with the old KEK, wraps with the active one, updates `wrapped_dek, kek_id`. Ciphertext and data keys do not change.
- `EnsureKeys` at goald start (platform table, no org): inserts the active key id into `vault_keys`; refuses start when a non-retired `vault_keys` id is not loaded ("vault key local:3f… is missing from <path>; restore the key file from backup"), naming ids only.

#### 6.5 sqlc queries (`queries/vault.sql`)

`CreateCredential`, `CreateCredentialVersion`, `CredentialByID`, `CredentialByName`, `ListCredentials` (with grant arrays and model-access flag, keyset by name), `LockCredential`, `SetCurrentVersion`, `DestroyVersion`, `DestroyAllVersions`, `RevokeCredential`, `UpdateCredentialMeta`, `InsertGrant` (`ON CONFLICT DO NOTHING`), `DeleteGrant`, `ResolveCredential` (join for 6.4), `UpsertCredentialUse` (returns `inserted bool`), `TouchLastUsed`, `CredentialUses` (history page), `VaultSettings`, `UpsertVaultSettings`, `LiveVersionsNotUnder` (rewrap batch), `RewrapVersion`, `VaultKeys`, `InsertVaultKey`, `RetireVaultKey`, `CountLiveVersionsUnder`.

#### 6.6 How part 4.2 fits the same tables

| 4.2 needs | Fits as |
| --- | --- |
| the connector a credential signs in to | 4.2's migration adds `credentials.connector text` and `method text` (by name, so a credential spans connector versions; 4.2 §16), not a `connector_id` FK; core kinds keep NULL (review) |
| "needs reconnect" (OAuth `invalid_grant`) | 4.2's migration adds `credentials.needs_reconnect_at timestamptz`; `CredentialStatus` gains `needs_reconnect` (after `revoked`, before `expired`); 4.2 adds `Vault.MarkNeedsReconnect(ctx, id, by)` (review) |
| OAuth 2.1 (access + refresh token, expiry) | kind `oauth2`; secret fields `access_token`, `refresh_token`; public `scopes`, `consented_at`; refresh = `Vault.Rotate` by actor `system` under 4.2's advisory-lock job; `expires_at` = refresh token expiry ("Renews itself, consent Oct 2") |
| GitHub App ("Tokens last 1 h, minted per run") | kind `github-app`: secret `private_key`; public `app_id`, `installation_id`; the minted installation token is never stored |
| Datadog keys, Azure SP, AWS role, connection string | connector-declared kinds (fields from the connector spec), stored exactly like `secret` |
| `oauth_states` with encrypted PKCE verifier | 4.2's table; the postgres adapter seals it with `envelope.go` (AAD `goal/oauth-state/v1|…`) inside `ConnectorCatalog.SaveOAuthState`, so `Vault` needs no `Seal`/`Open` (review) |
| 4.2's material for the gateway | `Vault.Resolve(ctx, SecretScope{…, PurposeConnection}, SecretRef)` → `Secret.Fields`; it records the use, so no `MarkUsed`. OAuth refresh = `Vault.Rotate` with `By` = system actor (4.2's `AddVersion`) (review) |
| Probes for connector kinds | more `CredentialProbe` entries |

`kind` is a validated text column, so new kinds need no schema change.

#### 6.7 Conformance suite

`internal/ports/porttest/vaulttest` (run by `porttest.Vault`, the in-memory fake, and by the PostgreSQL adapter under `integration`):

- create then resolve returns the fields; a bare reference returns the default field; `#field` returns that field; an unknown field errors.
- metadata never contains secret fields; `Credential`/`List` never expose fields.
- not granted → `CredentialError{not_granted}`; granted after `Grant` → resolves; `Ungrant` → refused again.
- `PurposeModel` resolves the org's model access without a grant; a `github-token` for `PurposeModel` → `wrong_purpose`.
- expired (`expires_at <= now` via the fake clock) → `expired`; revoked → `revoked`; the name can be reused after revoke.
- rotate → next resolve returns the new value; the previous version has `destroyed_at` and no ciphertext.
- `Check` reports every problem and records no use.
- uses: two resolves in one run → one row with `uses = 2`; a refused resolve adds a row with that outcome; history lists created, granted, used, refused.
- name taken → `ErrCredentialNameTaken`.

The fake (`porttest.Vault`) stores fields in memory without encryption; `porttest.KeyWrapper` is a reversible fake with an id.

### 7. Bucket layout changes

None. No credential, wrapped key or key-encryption key ever goes to the bucket.

### 8. API

Tag `credentials` (was `vault`; 4.2's endpoints are tagged `connectors`, restructure). Every path is under `/v1/orgs/{org}`. Problems are RFC 9457 with an added `code` (shared `Problem` schema; see contracts); codes below are written in kebab-case in the contract (`credential-name-taken`, `probe-failed`) (review). (identity) Each operation names its permission in `x-goal-scope` ("Identity in phase 4"); create, rotate, revoke, grant and ungrant need a fresh session. Secret values appear only in request bodies; no response, problem, log or event contains one. Request bodies with secrets are capped at 128 KiB and never logged.

| Method and path | Request | Response | Errors |
| --- | --- | --- | --- |
| `GET /credentials?after=&limit=&revoked=` | — | `200 CredentialList {credentials: Credential[], next_after, summary: {total, expired, attention}}` (`attention` = expiring + rotation_due) | — |
| `POST /credentials` | `NewCredential {name, kind, metadata?, secret: {field: value}, expires_at?, rotate_every_days?, grants?: [factory id or name], test?: bool}` | `201 Credential` (+ `Location`) | 400 `invalid`, 409 `credential_name_taken`, 422 `kind_not_available` (bedrock-role, github-app), 422 `local_only` (subscription outside local mode), 422 `probe_failed` (detail = scrubbed system answer; nothing saved), 404 unknown factory in grants |
| `GET /credentials/{credential}` | — | `200 CredentialDetail` = Credential + `versions: [{version, created_at, created_by, destroyed_at}]`, `uses: {factory: {model, repository, connection}}` (references in each granted factory's current definition, from B) | 404 |
| `PATCH /credentials/{credential}` | `{metadata?, expires_at? (null clears), rotate_every_days? (null clears)}` | `200 Credential` | 400, 404, 409 `credential_revoked` |
| `POST /credentials/{credential}/rotate` | `{secret: {…}, expires_at?, test?}` | `200 Credential` | 400, 404, 409 `credential_revoked`, 422 `probe_failed` |
| `POST /credentials/{credential}/test` | — | `200 ProbeResult {ok, detail, expires_at?}` | 404, 409 revoked, 422 `not_testable` |
| `POST /credentials/{credential}/revoke` | `{confirm: <name>}` | `204` | 400 confirm mismatch, 404, 409 already revoked |
| `PUT /credentials/{credential}/grants/{factory}` | — | `204` (idempotent) | 404, 409 revoked |
| `DELETE /credentials/{credential}/grants/{factory}` | — | `204` (idempotent) | 404 |
| `GET /credentials/{credential}/history?after=&limit=` | — | `200 {entries: CredentialHistoryEntry[], next_after}` newest first, limit ≤ 100 | 404 |
| `GET /credential-kinds` | — | `200 {kinds: CredentialKindSpec[]}` (labels, fields with `secret`/`required`, purposes, `available`, `testable`, `local_only`) | — |
| `GET /settings/vault` | — | `200 VaultSettings {backend, model_credential: Credential?, backends: [{id, label, available, note}]}` | — |
| `PUT /settings/vault` | `{backend, model_credential: id or name or null}` | `200 VaultSettings` | 422 `backend_not_available` (anything but postgres in phase 4), 422 `wrong_purpose` (not a model kind), 404 |
| `GET /factories/{factory}/credentials?purpose=` | — | `200 {credentials: FactoryCredential[], model_access: FactoryCredential?}`; `FactoryCredential = {id, name, kind, label, system, status, expires_at}`; only credentials granted to the factory plus the org's model access | 404 (also when the caller has no role: the factory's existence is not revealed) |

`{credential}` is the id or the live name. Schemas: `Credential`, `CredentialDetail`, `CredentialList`, `NewCredential`, `CredentialRotation`, `CredentialPatch`, `CredentialHistoryEntry`, `CredentialKindSpec`, `ProbeResult`, `VaultSettings`, `FactoryCredential`. `secret` properties are `writeOnly: true` with `format: password`.

`Credential` = `{id, name, kind, label, system, metadata, version, version_at, version_by, expires_at, rotate_every_days, rotate_due_at, last_used_at, status, grants: [{factory_id, factory_name}], model_access: bool, created_at, created_by, revoked_at}`.

**SSE.** On `/v1/orgs/{org}/stream`: `event: credential`, `data: {credential_id, action: created|rotated|updated|granted|ungranted|revoked|settings}`; published with `pg_notify('goal_org_events', {org_id, event: "credential", data})` in the same transaction (A's channel, review). No event on use. Clients invalidate the credentials keys (section 10).

### 9. CLI changes

`goal` (API client; `--org` per A, default the only organization):

```
goal credential list [--revoked]
goal credential show <name>
goal credential create <name> --kind K [--meta k=v ...] [--expires 2027-10-01] [--rotate-every 90d]
                       [--grant FACTORY ...] [--test] (--from-env VAR | --from-file PATH | stdin)
goal credential rotate <name> [--expires DATE] [--test] (--from-env VAR | --from-file PATH | stdin)
goal credential grant <name> <factory>     goal credential ungrant <name> <factory>
goal credential revoke <name>              (asks to type the name; --yes for scripts)
goal credential import-env --factory F [--env-file .env] [--dry-run]
goal vault settings [--model-access NAME]
```

- Secrets are never flag values (no shell history): `--from-env`, `--from-file` (must be `0600`), or stdin; on a terminal stdin is read without echo (`golang.org/x/term`, a new direct dependency of `cmd/goal` only). Multi-field kinds read `--field name=env:VAR` pairs.
- `import-env --factory goal` reads the factory's current definition, finds every `env:NAME` reference (repository token, connection headers, env, command words) plus the model variable `CLAUDE_CODE_OAUTH_TOKEN`, and for each set variable creates a credential (named by the rules in section 14, e.g. `GOAL_GITHUB_TOKEN` → `github-goal`), kind from where it is used (`github-token` for the repository token and GitHub MCP headers; `claude-subscription` for the model token; `secret` otherwise), grants it to the factory, sets the model credential as the organization's model access when none is set, and prints the reference rewrite table (`env:GOAL_GITHUB_TOKEN → credential:github-goal`). `--dry-run` prints without creating. Values never print. Idempotent: an existing credential with the planned name is reported and skipped.

`goald` gains subcommands (same environment as the daemon; they connect as `goal_app` and set each organization in turn):

```
goald vault status          key ids in the file (active first), vault_keys rows, live versions per key
goald vault add-key         prepend a new active key to the key file (old keys stay for unwrapping)
goald vault rewrap          re-wrap every live version under the active key, org by org, in batches of 100;
                            retire keys with no live versions left
goald vault remove-key <id> drop a retired key from the file (refuses while any live version uses it)
```

`goald` (daemon) start: load the key file; when it is missing and `vault_keys` is empty, generate it (log `vault key created at <path>; back it up`); when missing and keys exist, exit 2. `GOAL_TASK_ENV` set after 4.1-9 → exit 2 with "GOAL_TASK_ENV was removed: model access is a credential (goal credential import-env)".

### 10. Web UI

#### 10.1 Routes

| Route | Screen | Board |
| --- | --- | --- |
| `/o/:org/credentials` (`?c=<name>` selects the detail panel, `?revoked=1`) | Credentials | `Credentials.dc.html` |
| `/o/:org/credentials/new` (`?kind=&grant=<factory>` preselect) | New credential | `Credential-New.dc.html` |
| `/o/:org/settings` | C's page; 4.1's `VaultSection`, `ModelAccessSection` | `Org-Settings.dc.html` |

New route `/o/:org/credentials/new` is added to the registry (contracts). Files: `web/src/routes/o.$org._org.credentials.index.tsx`, `o.$org._org.credentials.new.tsx` (under C's org-sidebar layout, review); screens in `web/src/features/credentials/` (`list.tsx`, `detail-panel.tsx`, `new-credential.tsx`, `rotate-dialog.tsx`, `revoke-dialog.tsx`, `secret-input.tsx`); shared pieces other features use in `web/src/shared/`: `credential-select.tsx` (`CredentialSelect`), `vault-settings.tsx` (`VaultSection`, `ModelAccessSection`, controlled: `value`/`onChange`, C's page saves).

#### 10.2 Behaviour

**Credentials** (identity: owners and admins manage; controls follow `can`).
- Table per board; Connector column shows `system`, "How it signs in" the kind `label`; Granted to as chips (factory names); "Expiry and rotation" text: "Expires Oct 1", "Expired Oct 21" (warning icon, `--rust` per MASTER.md), "Rotate every 90 days, next Nov 4", "No expiry"; Last used relative (`Intl.RelativeTimeFormat`); row menu: Rotate, Test (testable kinds), Copy reference (`credential:<name>`), Revoke.
- Detail panel under the table: name (mono), status alert when expired ("Expired on Oct 21. sre's model access is blocked until you paste a new token." built from `uses`), public metadata rows, "Token Hidden, set <version_at> by <version_by>" with a lock icon, "Paste a new token" (rotate dialog) and "Revoke"; Granted to: one checkbox per factory of the org (PUT/DELETE grant on toggle, optimistic, rolled back on error) with the use note ("model access", "repository", "2 references" from `uses`), the "can't see" note; History list from `/history` with "Show more".
- States: loading skeleton rows; empty ("No credentials yet. Add model access and a GitHub token so factories can run." + New credential); error with retry.

**New credential.**
- Step 1 grid from `GET /credential-kinds` grouped by `system` (Claude, GitHub, Other); unavailable kinds disabled with their note.
- Step 2 fields from the kind spec: Name (slug validation, "How factories and logs refer to it"), public fields, secret fields as `SecretInput` (`type=password`, Show toggle with `aria-pressed`, `autocomplete="off"`, `spellcheck=false`), the lock note, Expires on (date; for `claude-subscription` the hint "Tokens from `claude setup-token` last about a year"), Remind me to rotate (30, 90 days, never).
- Step 3 grant checkboxes (all factories of the org; `?grant=` prechecked) with the core note.
- Footer: Cancel; "Test and save" for testable kinds (with the "Test calls GitHub's /user before saving" line), "Save" otherwise. A failed test shows the scrubbed detail inline and keeps the form. On success → `/o/:org/credentials?c=<name>`; secret inputs are cleared from state before navigation.
- The secret mutation uses `gcTime: 0` and no `useMutationState`, so values do not stay in TanStack Query's mutation cache; secrets never go into the URL, `localStorage` or logs.

**Rotate dialog**: secret fields, Expires on, Test; text "Running items keep the old token until they end. Revoke the old token at <system> once they finish." **Revoke dialog**: lists granted factories and what stops ("goal's model access", "sre's repository"), asks to type the name.

**Org settings sections** (they replace C's "Arrives with credentials" placeholders, restructure). `ModelAccessSection`: radio cards for each live model-capable credential (label, `metadata.plan`, "Signed in by <created_by>" for subscriptions, "Billed per token…" for API keys), plus "Add model access" → `/credentials/new?kind=claude-subscription`. `VaultSection`: three radio cards per board; only PostgreSQL enabled; the note "Changing the vault moves all N credentials" is shown only when another backend is enabled (not in 2.2).

**`CredentialSelect`** (for D's Basics): props `{org, factory, purpose: 'model' | 'repository' | 'connection', value, onChange, allowDefault?}`; options from `GET /factories/{factory}/credentials?purpose=`; model purpose shows "Organization default · <name>" first; each option shows status (expired in `--rust`); footer link "Grant a credential" to `/credentials/new?kind=…&grant=<factory>`.

#### 10.3 Data fetching

```ts
keys.credentials      = (org) => ['org', org, 'credentials'] as const
keys.credentialList   = (org, revoked: boolean) => ['org', org, 'credentials', 'list', { revoked }] as const
keys.credential       = (org, name) => ['org', org, 'credentials', 'item', name] as const
keys.credentialHistory= (org, name) => ['org', org, 'credentials', 'item', name, 'history'] as const
keys.credentialKinds  = (org) => ['org', org, 'credential-kinds'] as const          // staleTime: Infinity
keys.vaultSettings    = (org) => ['org', org, 'settings', 'vault'] as const
keys.factoryCredentials = (org, factory, purpose?) => ['org', org, 'factory', factory, 'credentials', { purpose }] as const
```

SSE `credential` → invalidate `['org', org, 'credentials']`, `keys.vaultSettings(org)` (action `settings`) and `['org', org, 'factory']` credential lists; `resync` invalidates all. Mutations invalidate the same keys on settle.

#### 10.4 Phone and accessibility

Below 768 px the table becomes a list of cards (name, system · kind, status line, granted chips, menu), the detail panel opens full width under the selected card (`?c=`), steps stack; all targets 44 px. Table semantics with `scope="col"`; status conveyed by text, not colour alone; dialogs focus-trapped (Radix); grant checkboxes labelled with the factory name; Playwright axe at 375, 768, 1440. Mock API (`web/mock`) gains credentials, kinds and vault settings fixtures (acme: `claude-acme`, `github-goal`, an expired `grafana-acme` of kind `secret`).

### 11. Security and tenancy

- **Isolation.** Every vault query runs inside a transaction with `SET LOCAL goal.org_id`; RLS on all org tables; composite FKs `(org_id, credential_id)` and `(org_id, factory_id)` prevent cross-org grants even from code bugs; AADs bind ciphertext to org, credential, version and kind.
- **Who may use a credential.** Resolution requires the scope's factory to hold a grant (or, for `PurposeModel`, the credential to be the org's model access), the kind to serve the purpose, and the credential to be live and unexpired. The factory-scoped API lists only those, so "factories without a grant can't see that this credential exists".
- **Access.** (identity) Owners and admins manage credentials and read history, with a fresh session for every change. Nobody reads secret values through any API (write-only).
- **Values.** Secret only in: the request body (TLS once hosted; loopback today), goald memory during create/rotate/resolve, the agent container's env (model credential only), git's environment for the clone, the `Authorization` header to GitHub/Anthropic, gateway upstream headers and sidecar env. Never in: responses, problems, logs, events, audit, `credential_uses`, the bucket, TanStack caches, URLs. `gosec` and a test assert the API's JSON never contains a created value.
- **Keys.** The KEK never enters PostgreSQL; `vault_keys` stores ids only. A database dump without the key file (or KMS access) reveals no secret. Key file mode `0600` enforced at load. Losing the key file loses every credential (documented; `goald vault status` shows the path to back up).
- **Erasure.** Rotation and revocation erase ciphertext and wrapped keys in the same transaction (crypto-shredding of that version).
- **Subscription tokens.** `claude-subscription` is owned by the user who adds it and resolves only for runs of items that user submitted, never for another person's work (ADR 0003's one-person rule restated for multi-user installs; settled in the design pass).
- **Audit** (A's `audit_log`, same transaction, actor = user, `system` or the run's agent): `credential.created` {name, kind, version, grants, expires_at, rotate_every_days}, `credential.rotated` {version}, `credential.updated` {fields changed, never values}, `credential.granted` / `credential.ungranted` {factory}, `credential.revoked`, `credential.used` {factory, run, purpose, version} (first per run), `credential.refused` {factory, run, purpose, reason} (first per run and reason), `credential.imported` {from: "env", variable name}, `vault.settings_changed` {backend, model_credential}, `vault.rewrapped` {org, versions, to_key} and `vault.key_added` / `vault.key_retired` {key id} (system).

### 12. Observability

- Logs (slog JSON): `vault.resolve` at debug (credential name and id, kind, version, purpose, factory, run, outcome, duration); refused resolutions at warn; create/rotate/revoke/grant at info; key load, generation, rewrap progress at info. Never a value, a key or ciphertext.
- Events: `credential` on the org stream (section 8); a blocked item's event carries `reason_code: credential_unavailable` and the credential name; gateway `gateway_tool_call` events unchanged (outcome `unavailable` when a credential is refused).
- Metrics: none yet (goald has no metrics pipeline); when OTel metrics land: `goal_vault_resolutions_total{purpose,outcome}`, `goal_vault_resolve_seconds`, `goal_vault_credentials{status}`.

### 13. Testing

| Layer | Tests (behaviour protected) |
| --- | --- |
| `internal/domain` | `ParseSecretRef` (literal, env, credential with name, uuid and field; malformed rejected); `CredentialStatusAt` precedence; `ModelEnv` per kind |
| `internal/adapters/postgres` (unit) | envelope: seal/open round-trip; a changed AAD part (org, credential, version, kind), nonce, ciphertext or wrapped key fails; a different KEK fails; two seals of one payload differ |
| `internal/adapters/keyfile` | load refuses group/other-readable files, symlinks and bad lengths; first key wraps, others unwrap; ids are stable and differ per key; add/remove keep the file atomic and `0600` |
| `porttest/vaulttest` | section 6.7, against the fake (unit) and PostgreSQL (integration) |
| `internal/adapters/postgres` (integration) | RLS: as `goal_app` with org B set, org A's credentials, versions, grants and uses are invisible and inserts with org A fail; no column of any table contains a created plaintext (scan `bytea`/`text`/`jsonb`); rewrap moves every live version to the new key and resolution still works; `EnsureKeys` refuses a missing key; concurrent rotations of one credential end with consecutive versions |
| `internal/secrets` | env refused in factory scope (allowed in platform scope; allowed with `AllowFactoryEnv`); `TaskEnv` uses the factory's credential over the org default; `ErrNoModelAccess` when neither; subscription refused for an item its owner did not submit |
| `internal/engine` | unavailable model credential blocks the item before a workspace is prepared, with `reason_code`; task env is exactly the model env; merge uses repository scope with the approver as actor |
| `internal/adapters/docker` | model variables kept only for `GOAL_MODEL_AUTH`'s kind |
| `internal/adapters/gitworkspace`, `github` | existing tests on the new signatures; probe reads the expiry header; token absent from errors |
| `internal/gateway` | `credential:` header words resolve with the claims' scope; a refused credential makes the connection unavailable and its reason reaches `_meta` without values |
| `internal/api` | factory credential lists show only granted credentials; unknown org → 404; create/rotate validation; 409 name taken; probe failure saves nothing; no response body contains the secret; revoke confirm |
| `cmd/goal` | `import-env` plan from a fixture definition (names, kinds, rewrite table); secrets never accepted as flag values |
| `cmd/goald` | `GOAL_TASK_ENV` refused after 4.1-9; key file missing with keys recorded → exit 2 |
| Worker (Vitest) | `hiddenEnv` per `GOAL_MODEL_AUTH` |
| Playwright (mock, 375/768/1440, axe) | create a GitHub token credential with a grant and see it in the list; rotate it; revoke with typed confirmation; an expired credential shows its alert; choose model access in settings |

### 14. Rollout and migration

From today (single tenant; secrets in `.env`; `GOAL_TASK_ENV`; `env:` in definitions):

(restructure) "Today" here is the state phases A–E leave: organizations, versioned definitions and the interim rule's platform secrets.

1. 4.1-1–4.1-5 land with no behaviour change for existing factories: `AllowFactoryEnv = true`, engine falls back to `TaskEnv` while the org has no model access; B's `EnvReferences` check stays wired until 4.1-5 swaps in `VaultCredentials`, which still accepts `env:` references while `AllowFactoryEnv` is on.
2. goald's first start after 4.1-3 creates `vault.key` (logged with its path) and records its id.
3. Migrate the goal factory (runbook in `docs/engineering/vault.md`, written in 4.1-6):
   ```
   task up                                          # goald with the vault
   goal credential import-env --factory goal --dry-run
   goal credential import-env --factory goal        # claude-acme (claude-subscription, org model access),
                                                    # github-goal (github-token, granted to goal), postgres-dev (secret)
   goal credential test github-goal
   ```
   Then rewrite the definition's references (`spec.runtime.repository.credential: github-goal`, GitHub MCP header `Bearer credential:github-goal`, `credential:postgres-dev`) through B's draft and publish (or `goal factory pull/push`), run one item end to end, and check `goal credential list` shows Last used for each.
   The same for sre and university when they are in use.
4. 4.1-9 (cutover): remove `engine.Config.TaskEnv`, `GOAL_TASK_ENV` (exit 2 when set), `AllowFactoryEnv`; `env:` in a factory definition becomes a validation error ("use credential:<name>"); remove `CLAUDE_CODE_OAUTH_TOKEN`, `GOAL_GITHUB_TOKEN`, `GOAL_DEV_DATABASE_URL`, `GOAL_SRE_*`, `GOAL_UNIVERSITY_*` from `.env.example`; add `GOAL_VAULT_KEY_FILE`. The person deletes those lines from their `.env` after step 3 succeeded. Platform variables stay: database URL, bucket, listen, gateway key, `GOAL_GITHUB_API_URL`, vault key file.

Import naming: `CLAUDE_CODE_OAUTH_TOKEN` → `claude-<org slug>`; a variable used as a repository token or GitHub header → `github-<factory>`; others → the variable lowercased without `GOAL_` and the factory prefix (`GOAL_DEV_DATABASE_URL` → `dev-database-url`, `GOAL_SRE_GRAFANA_TOKEN` → `grafana-token`); collisions get `-<factory>`. Names can be overridden with `--name VAR=name`.

Nothing is lost: values move from `.env` into the vault before any variable is removed, and until 4.1-9 every `env:` reference still resolves, so reverting a definition is a rollback. Migrations 00050/00051 only add tables.

### 15. Work breakdown

| PR | Content | Acceptance criteria and verification |
| --- | --- | --- |
| 4.1-1 refactor: secret scope | `ports.SecretScope`; `Secrets.Resolve(ctx, scope, ref)`; `CodeHost` takes `RepoRef`; `WorkspaceSpec.OrgID`; callers build scopes; ADR "Vault and credentials" (next free number) | No behaviour change: all existing tests pass on new signatures; `task check` |
| 4.1-2 domain and keys | `domain/credential.go` (kinds, purposes, status, `SecretRef`, `ModelEnv`); `ports.Vault`, `KeyWrapper`, `ModelAuth`, `CredentialProbe`; `porttest.Vault`, `porttest.KeyWrapper`, `vaulttest` against the fake; `internal/adapters/keyfile`; `envelope.go` | Domain, keyfile and envelope tests (section 13) pass; `task check` |
| 4.1-3 PostgreSQL vault | migrations 00050, 00051; `queries/vault.sql`; `postgres.Vault` incl. uses, audit, `Rewrap`, `EnsureKeys` | `vaulttest` + RLS + plaintext-scan + rewrap integration tests pass (`task test-integration`); `task migrate-check` |
| 4.1-4 schema and config | `factory.schema.json`: `runtime.modelCredential`, `runtime.repository.credential`, `credential:` in `secretRef`; `auth`, `repository.token`, `secrets` deprecated (warnings); B's `config.Factory.SecretUses()` gains the credential fields and purpose `model` (restructure: was `CredentialUses()`); `FactoryModel` `basics.repository.credential`, `basics.model_credential`; `domain.Runtime.ModelCredential` | Config tests: uses listed with paths and purposes; deprecated fields warn, still load; fixtures in `internal/config/testdata` updated |
| 4.1-5 resolution | `internal/secrets.Router`; engine `ModelAuth` with `TaskEnv` fallback; docker and worker `GOAL_MODEL_AUTH`; gateway `credential:` words; `authoring.VaultCredentials` replacing B's `EnvReferences` in goald's wiring (restructure); goald wiring (key file load/generate, `EnsureKeys`); depguard rule | Engine, router, docker, gateway, worker tests; manual: an item of the goal factory runs with a vault model credential and a vault GitHub token on a laptop |
| 4.1-6 API and probes | OpenAPI `credentials` tag, regenerated clients; handlers; `credentials_needing_attention` on C's `/attention` (restructure); `anthropic` adapter; github probe; org-event publication; `docs/engineering/vault.md` | API tests (section 13); `task api:gen` clean; probe tests against `httptest` |
| 4.1-7 CLI | `goal credential …`, `import-env`, `goald vault …` | CLI tests; manual import of the goal factory's `.env` in `--dry-run` |
| 4.1-8 web | Credentials, New credential, dialogs, `CredentialSelect`, `VaultSection`, `ModelAccessSection`, mock fixtures; (restructure) the sections replace C's placeholders, the Credentials dot and the delete text's credential count go into C's shell, `CredentialSelect` replaces D's read-only Basics lines (`capabilities.credentials` on), E's Item header links a `credential_unavailable` block to the credential; flips Credentials in `screens.ts` | Playwright flows at three widths with axe; screenshots attached; ui-ux-pro-max checklist |
| 4.1-9 cutover | remove `TaskEnv`, `GOAL_TASK_ENV`, `AllowFactoryEnv`; `env:` in factory scope is an error; `.env.example`, architecture.md (ports, wiring, deployment table), AGENTS.md rule 7 wording | After the goal factory migration (section 14): goald starts without the removed variables, an item runs to done, `grep -r CLAUDE_CODE_OAUTH_TOKEN .env.example cmd internal` finds only the worker/runner model mapping; `task check` |

### 16. Cross-spec contracts

**4.1 provides**
- Ports `Vault`, `KeyWrapper`, `ModelAuth`, `CredentialProbe` (new names: `ModelAuth` was already planned in architecture.md; `KeyWrapper` and `CredentialProbe` are needed for the key file/KMS split and "Test and save"); changed `Secrets.Resolve(ctx, SecretScope, ref)`; `ports.RepoRef`; errors `ErrCredentialUnavailable`, `CredentialError`, `ErrNoModelAccess`, `ErrCredentialNameTaken`.
- `authoring.VaultCredentials` over `Vault.Check(org, factory, []CredentialUse)` and the credential fields of `config.Factory.SecretUses()` (4.1-4) → plugs into **B**'s `authoring.Credentials` extension point, replacing B's interim `EnvReferences` ("every connection's credential granted to this factory") (restructure).
- Tables `credentials`, `credential_versions`, `credential_grants`, `credential_uses`, `vault_settings`, `vault_keys` (migrations 00050, 00051).
- Endpoints under `/v1/orgs/{org}`: `/credentials…`, `/credential-kinds`, `/settings/vault`, `/factories/{factory}/credentials`; SSE `event: credential`.
- Web: route `/o/:org/credentials/new` (registry addition); `CredentialSelect`, `VaultSection`, `ModelAccessSection` in `web/src/shared/`; query keys in 10.3. `credentials_needing_attention` on **C**'s `/attention` and the sidebar dot, the Model access and Vault sections in C's settings page (restructure).
- Factory schema: `spec.runtime.modelCredential`, `spec.runtime.repository.credential`, `credential:<name>[#field]` references → **D**'s Basics writes them once 4.1 sets `capabilities.credentials` and adds the `FactoryModel` fields (restructure); **4.2** reuses the reference form and the name rules.
- Engine block event `reason_code: credential_unavailable` + `credential` → 4.1 links it from **E**'s Item header and `blocked` inbox entries (restructure).
- For **4.2**: the extension table in 6.6 (`connector_id`, `oauth2`, `github-app`, connector kinds, `envelope.go` reuse, `Rotate` by `system` for refresh).

**4.1 needs**
- **A**: `domain.OrgID`, `domain.Actor` (actor refs in `*_by` columns); `organizations(id)`, `factories.org_id` with `UNIQUE (org_id, id)`, `agent_runs.org_id`; the `SET LOCAL goal.org_id` transaction helper in the postgres adapter; `audit_log` insertable in the caller's transaction; the request actor and `Authorizer` seam, filled by phase 3 (principal, permission table, fresh-session rule); `OrgID` in `domain.Line`, `TaskClaims` and engine scope; a way for platform jobs (`goald vault rewrap`) to list every organization id as `goal_app`; org-event NOTIFY for the org stream; the shared `Problem` schema gaining `code`; role grants convention for `goal_app`.
- **B**: draft/publish API to rewrite the goal factory's references during the migration (fallback: `goal factory pull/push`); the `authoring.Credentials` extension point and `config.Factory.SecretUses()` (restructure); and the "used by" map for `CredentialDetail.uses` (which granted factories' current definitions reference a credential, from `config.Factory.SecretUses()` over current versions).
- **C**: Org settings page with the Model access and Vault placeholders 4.1 replaces (its Save gains `PUT /settings/vault`); org sidebar entry, to which 4.1 adds the dot; `/attention`; shell around 4.1's pages (restructure).
- **D**: Basics' read-only access lines and `capabilities.credentials`, which 4.1 turns into `CredentialSelect` for model access and the repository credential (restructure).
- **E**: the Item header and `blocked` inbox entries that show the block reason (restructure).
- **4.2**: adds connector kinds and `connector_id` without changing 4.1's tables otherwise; Credential-New's connector grid replaces 4.1's core grid.

### 17. Open questions

1. **Model slots.** Org settings → Model access shows "6 slots shared by every factory" and per-factory slot limits. That is engine concurrency, not vault; which phase owns it (A's org settings or E's flow work)? 4.1 stores only which credential is the model access. (Answered in review: A7 owns slot caps.)
2. **Model access per factory.** The canvas puts model access in Org settings only; ADR 0003 says per factory. 4.1 supports both (org default, optional `runtime.modelCredential`). Should D show the per-factory override in Basics, or keep it YAML-only?
3. **Repository via the GitHub connection.** Basics on the canvas says "Through the GitHub connection". After 4.2, should `runtime.repository.credential` be dropped in favour of the GitHub connection's credential (one source), or kept for repositories without a GitHub connection?
4. **Previous versions.** The architecture page says rotation keeps old versions "until no run uses them"; this spec erases the previous ciphertext at rotation because nothing re-reads an old version (runs hold what they resolved). Confirm, or keep a 24-hour undo window with a rollback endpoint. (restructure: decided in the design pass, credential lifecycle.)
5. **Bedrock.** Model access through Bedrock needs goald to mint AWS session credentials per run (`sts:AssumeRole`, at most 1 hour under role chaining, shorter than the 2-hour task timeout). Defer to the AWS deployment with the KMS adapter, or design a refresh path for long runs now?
6. **`GOAL_GITHUB_API_URL`.** Stays a platform setting; GitHub Enterprise per credential (`metadata.api_url`) is reserved but unused. Enough for phase 4?
7. **Default vault backend move.** Org settings promises "Changing the vault moves all N credentials". With only PostgreSQL available in phase 4 the option is disabled; confirm that showing AWS Secrets Manager as disabled (not hidden) is the intended state.

## Part 4.2: Connectors and MCP gateway

(restructure) Formerly phase G. It needs 4.1 (vault), D (factory builder), E (work screens) and, through them, A, B, C.

### 1. Summary

Organizations connect their factories to outside systems through a **connector catalog**: curated entries Goal publishes in the bucket, plus org-private ones, each saying how to reach a system (remote MCP server, MCP server image or CLI tool), how to sign in (OAuth 2.1 with PKCE, keys, service principal, GitHub App, IAM role) and which tools read, write or need approval. A factory's definition binds connectors to its agents as **connections** (connector + a granted vault credential + per-tool rules), validated and resolved at publish, so the gateway serves each agent exactly the tools and credentials its published version allows. The gateway learns OAuth tokens (refreshed by goald), per-run minted tokens (GitHub App, AWS STS), CLI tools as allowlisted sidecars, **approval-gated tool calls** that pause the agent until an approver decides; routing across several goald instances is deferred to the AWS deployment (review).

### 2. Scope

**In**
- Connector file format (`kind: Connector`), its JSON Schema `schema/connector.schema.json`, the `internal/connectors` package (parse, validate, effective tool policy, injection templates, scopes, CLI command grammar).
- Curated catalog in the bucket (`catalog/connectors/<name>/<version>.yaml`), org-private connectors (blobs under the org), the `connectors` table as index, catalog sync, import from the official MCP Registry.
- Connector-form connections in `factory.yaml`, their validation problems and quick fixes inside B's validate/publish, the `connections` table (binding + health), connection tests.
- OAuth client in goald: discovery, client ID metadata documents and dynamic client registration, authorization code + PKCE, callback, refresh job, revocation; tokens stored through 4.1's `Vault`.
- Connector credential methods and their fields; credential test before save; GitHub App installation tokens and AWS STS sessions minted per run (`Minter`).
- CLI tools as exec sidecars exposed as one MCP tool `<connection>__run`.
- Approval-gated tools: `tool_approvals`, gateway hold/release, expiry, sweeper, API, SSE, CLI, audit.
- Per-connector smoke tests and CI fakes.
- UI: Connector catalog, factory Connections page (read-only, operational), the connector part of Credentials and New credential, Approval, the full Connections section of the wizard and the Definition editor (Visual and YAML problems), the Connections card of the Agent page.

**Out** (later or other phases)
- ECS sidecars (an ECS `Sidecars` adapter), per-connection egress allowlists for sidecars, kubectl and other CLIs beyond `aws` and `az`. Egress filtering itself is in place since [ADR 0009](../adr/0009-agent-network-isolation.md): a sidecar with `network: true` (and an exec sidecar of `spec.cli`, which the Docker adapter starts the same way) runs on the egress tools' network and reaches only public hosts, through the egress proxy, never the host, a private network or a metadata service. A connection's own host list (from its connector's `spec.image` or `spec.cli`) becomes one more policy of that proxy.
- Gateway multi-instance routing (the `g` owner-instance claim, `GOAL_PEER_URL`, forwarding; old 4.2-16): Goal runs one goald until the AWS deployment, no board needs it, and architecture.md already documents that sidecar and held-approval traffic must reach the instance that owns the run. It comes with the AWS deployment (review).
- The `knowledge` builtin connection (later, with the knowledge base); it keeps being reported unavailable and shows as "Not served yet" in the UI.
- Editing a held call's arguments, auto-approve rules, approval delegation, push notifications for approvals.
- Suspending an agent run across a long approval (the run stays alive while it waits; see 4.6).
- The vault itself, credential list/rotation/grants/revoke (4.1), definition drafts/versions/publish (B), wizard and editor shells (D), Waiting-on-you inbox and item trace (E), sidebar and org shell (C).

### 3. Canvas boards covered

| Board (`project/canvas.json`) | 4.2 delivers | Not 4.2 |
| --- | --- | --- |
| `Catalog.dc.html` Connector catalog | All content: Import from MCP Registry, Add connector, search, kind filter with counts, cards (tile, name, kind, endpoint/image/allowlist line, sign-in chips, policy line, used by) | Org sidebar, rail (C) |
| `Connections.dc.html` Connections: sre | All content: blocked-connection banner with "Renew the token", Browse catalog, Add in Definition, table with All/Connected/Blocked filter, row menu, tools panel (access, agents, needs approval, this week), CLI allowlist panel, Edit in Definition / Edit allowlist links | Factory sidebar and its Connections dot (C, from 4.2's `summary.blocked`) |
| `Credentials.dc.html` | Connector column, "How it signs in", expiry/rotation text for OAuth, GitHub App and STS, non-secret fields in the detail panel, "1 connection" per grant, "Calls refused, expired" history row | List, grants, paste new value, revoke, vault label (4.1) |
| `Credential-New.dc.html` | Step 1 connector picker, step 2 sign-in methods (OAuth button, connector-defined fields such as Datadog site), per-factory grant hints, "Test and save" | Grant checkboxes, rotate reminder, the save itself (4.1) |
| `Approval.dc.html` | Whole page: exact call, connection, signed-in-as, why it waits, args viewer, rendered preview, decision panel with keyboard A/R, why the agent asked | "SRE-149 so far" timeline component (E, 4.2 adds the "Paused for this approval" entry kind), Items badge (E) |
| `New-Connections.dc.html` wizard step 5 | "Added to …" list with Tested/Blocked, connector panel (credential select, status/test, tools table with agents and needs-approval switches, "Show all"), Remove…, Add from the catalog | Step bar, Visual/YAML toggle, draft saving, Back/Continue (D, B) |
| `Define-Connections.dc.html` | Same panel in the editor plus "New" tool badge, "Preview only" scope note, inline problem with quick fixes (Require approval, Limit to the preview build), Health link | Section tabs and change counts (D/B; 4.2 supplies problem counts) |
| `Define-YAML.dc.html` | Connection schema for Monaco, 4.2's problems and their quick fixes ("Add approval: required", "Add scope: preview") | Editor, diff gutter (D/B) |
| `Define-Publish.dc.html`, `New-Review.dc.html` | Checks: connections resolve, every connection uses a vault credential or needs none, every write tool is scoped to named agents / needs approval, no credentials reach an agent, blocked connection warning with "Add a credential" | Versions, diff, publish (B/D) |
| `Define.dc.html` (Line) | Line check "Merge uses the GitHub connection, which allows merges only through Goal" | Line editor (D) |
| `Agent.dc.html`, `Mobile-Agent.dc.html` | Connections card (name, kind and credential line, Ready/Blocked) | Stats, skills, tool policy, review reasons (D/E) |
| `Main.dc.html`, `Phone.dc.html`, `Board.dc.html`, `Item.dc.html` | Data only: pending approvals for "3 things wait for your OK" and "A Jira comment from investigator-1", sidebar "Connections, 1 blocked" | The panels (E, C) |
| `New-Start.dc.html` | "Connects to" chips come from template connections' connectors | Templates (D) |

Tool names on the boards (`jira__add_comment`) are illustrative; the gateway uses `<connection>__<upstream tool name>` (`jira__addCommentToJiraIssue`), and the UI shows them as served.

### 4. Domain and engine changes

#### 4.1 Connector file (`kind: Connector`)

One YAML file per connector version, immutable once indexed. Curated: `catalog/connectors/<name>/<version>.yaml`; org-private: content-addressed blob (section 7).

| Field | Meaning |
| --- | --- |
| `apiVersion: goal/v1`, `kind: Connector` | |
| `metadata.name` | slug `^[a-z][a-z0-9-]{1,38}[a-z0-9]$`, no `__` |
| `metadata.version` | integer ≥ 1, equals the file name |
| `metadata.title`, `description`, `tile` (2 letters), `vendor`, `homepage` | catalog card |
| `metadata.registry` | `{name, version}` when imported from the MCP Registry |
| `spec.kind` | `remote_mcp` · `mcp_image` · `cli` |
| `spec.remote` | `url` (https; may hold `{{…}}` placeholders), `transport: streamable_http` (default) or `sse` |
| `spec.image` | `ref` (must be pinned, `config.Pinned`), `command` (args), `env` (templates), `network` (bool) |
| `spec.cli` | `image` (pinned), `entrypoint` (default `["sleep","infinity"]`), `binary`, `network`, `env`, `setup` (argv lists run once at start, output discarded), `timeout` (default 60s, max 5m), `maxOutputBytes` (default 256 KiB, max 4 MiB), `denyArgs`, `scrub` (regexes), `commands` (ordered `{pattern, access, default}`) |
| `spec.settings` | JSON Schema (object) for connection-level settings (`repos`, `site`, `origins`, `access`); validated at publish |
| `spec.scopes[]` | `{name, label, description, always, args, env, token, arguments}`: connection-wide restrictions (4.4) |
| `spec.signIn[]` | ordered methods: `method` (`oauth`, `api_token`, `api_keys`, `token`, `github_app`, `aws_role`, `access_keys`, `service_principal`, `connection_string`, `service_account_token`, `none`), `label`, `fields[]` (`name`, `label`, `kind: text|secret|select|textarea|pem`, `options`, `pattern`, `help`, `optional`), `oauth` (`resource`, `issuer`, `scopes`, `registration: [metadata_document, dynamic, static]`, `clientId` for static), `mint` (`kind: github_app|aws_role`, `permissions`, `duration`), `inject` (`headers`, `env`, `args`), `test` (`listTools: true`, or `tool: {name, arguments}`, or `http: {method, url, headers, expectStatus}`) |
| `spec.tools` | `unknown: {access, default}` (default `{write, off}`) and `entries[]`: `{name, access: read|write, default: on|off|approval|blocked, reason, approval: {title, why, preview}}` |
| `spec.summary` | the catalog card's policy sentence when the counts are not enough |

`preview` renders the held call on the Approval page: `{format: markdown|json, title, body, by}` where `body` is a JSON pointer into the arguments and `title`/`by` are templates over `args.<pointer>` and `credential.<non-secret field>`.

Injection templates are a closed grammar, not a template engine: `{{name}}` (a credential field, then a minted field), `{{settings.name}}`, `{{basic a b}}` (base64 `a:b`), `{{join name sep}}`. Anything else is a schema error. Values are inserted verbatim into one header, env value or argv word, never parsed further.

Example (curated `jira/1.yaml`, abridged):

```yaml
apiVersion: goal/v1
kind: Connector
metadata: { name: jira, version: 1, title: Jira (Atlassian Rovo MCP), tile: ji, vendor: Atlassian }
spec:
  kind: remote_mcp
  remote: { url: https://mcp.atlassian.com/v1/mcp }
  signIn:
    - method: oauth
      label: OAuth 2.1
      oauth: { resource: https://mcp.atlassian.com/v1/mcp, registration: [metadata_document, dynamic] }
      inject: { headers: { Authorization: "Bearer {{access_token}}" } }
    - method: api_token
      label: API token
      fields:
        - { name: email, label: Atlassian account email, kind: text }
        - { name: api_token, label: API token, kind: secret }
      inject: { headers: { Authorization: "Basic {{basic email api_token}}" } }
      test: { listTools: true }
  tools:
    unknown: { access: write, default: off }
    entries:
      - { name: searchJiraIssuesUsingJql, access: read, default: on }
      - { name: getJiraIssue, access: read, default: on }
      - name: addCommentToJiraIssue
        access: write
        default: approval
        approval:
          title: Approve a comment on Jira
          why: comments on Jira need approval
          preview: { format: markdown, title: "How it will look on {{args./issueIdOrKey}}", body: /commentBody }
      - { name: transitionJiraIssue, access: write, default: approval }
      - { name: createJiraIssue, access: write, default: approval }
  summary: Comments and transitions need approval.
```

Curated v1 set (content work item 4.2-25): `github` (remote, `github_app` + `token`, scope `repos` always on, `merge_pull_request` blocked "Merges only through Goal"), `jira`, `datadog` (URL by `site`, `oauth` + `api_keys` with `DD-API-KEY`/`DD-APPLICATION-KEY` and an `http` test against `https://api.{{site}}/api/v1/validate`), `grafana` (image, `GRAFANA_URL` + service account token), `azure-mcp` (image, `service_principal` → `AZURE_TENANT_ID/CLIENT_ID/CLIENT_SECRET/SUBSCRIPTION_ID`, `--read-only`), `az-cli` (cli, `setup: az login --service-principal …; az account set`), `aws-cli` (cli, `aws_role` minted 1 h or `access_keys`, `denyArgs: [--endpoint-url, --profile, --ca-bundle, --no-verify-ssl]`), `postgres` (image, `connection_string` → `DATABASE_URI`, scope `read` → `--access-mode=restricted`, `execute_sql` write/approval), `playwright` (image, sign-in `none`, scope `preview` → `--allowed-origins {{join settings.origins ;}}`, form and click tools write), `aws-docs` (image, `none`). Endpoints and tool names are verified by the smoke tests before an entry ships.

#### 4.2 Connections in `factory.yaml`

`spec.connections[]` gains the connector form. The legacy inline form (`type: mcp|builtin|aws`) stays valid and served as today; publish warns on inline `type: mcp` and offers a quick fix when a catalog entry matches its URL or image repository.

```yaml
connections:
  - connector: jira            # name, or name@version; resolved at publish (latest active)
    name: jira                 # optional, default the connector name; slug without "__"
    credential: jira-oauth     # org credential name; omit for sign-in "none" or while not chosen (blocked)
    agents: [triager, investigator]
    access: read               # optional: serve read tools only
    approvalTimeout: 30m       # optional, default GOAL_APPROVAL_TIMEOUT
    tools:
      jira__addCommentToJiraIssue: { agents: [investigator], approval: required }
      jira__createJiraIssue: { off: true }
  - connector: github
    credential: acme-github-app
    agents: [backend-engineer, reviewer]
    repos: [acme/goal]         # connector setting (validated against spec.settings)
  - connector: playwright
    name: browser
    agents: [frontend-engineer, reviewer]
    origins: [https://preview.acme.example]
    tools:
      browser__browser_click: { access: write, scope: preview }
  - connector: aws-cli
    name: aws
    credential: aws-readonly
    agents: [investigator]
    commands: ["aws logs filter-log-events *", "aws ecs describe-* *"]   # subset of the connector's
```

Tool entry keys are exact served names `<connection>__<tool>`; fields `agents`, `approval: required|none`, `scope`, `access` (classifies a tool the catalog does not know, or raises read to write; never lowers), `off`.

**Effective policy** (`connectors.Policy`, pure, used by validation, the gateway and the UI). For connection C, upstream tool T, agent A:
1. Rule source: catalog entry for T, else `tools.unknown`. Access = max(catalog, entry.access).
2. `default: blocked` → never served (only an org-private connector version can change it).
3. `C.access: read` and access write → not served.
4. Entry `off` → not served.
5. Agents: entry `agents` if set; else catalog default `on` → C's agents; `off` or `approval` → nobody.
6. Approval: entry `approval` if set; else required when the catalog default is `approval`.
7. The agent's legacy `tools.allow`/`tools.deny` `<conn>__<pattern>` entries still filter on top (deny wins).
8. CLI connections serve one tool, `run`; the rule of each command is the first matching pattern's (access, default), and `approval: required` on the `run` entry or `default: approval` on the pattern holds the call.

Agents of a connection = `C.agents` ∪ agents whose legacy `connections` list names C (warning: "move to the connection's agents"). `domain.AgentSpec.Connections` is computed this way in `config.(*Factory).Line()`, so the engine's task token keeps naming the agent's connections with no engine change.

#### 4.3 Validation (hooks in B's pipeline)

`connectors.Checker` implements B's `authoring.Check` (problems) and `authoring.Resolver` (the `Resolution`) and runs in B's validate and publish (review). Problems carry B's shape (severity `error|warning|info`, section `connections`, JSON pointer, message, quick fixes as RFC 6902 patches). Codes are kebab-case in the contract (README): `connector-unknown`, …; three codes merge with B's names: `credential_not_granted` → `credential-not-granted` (emitted once, by B's `credentials` check over 4.1), `credential_missing` → `connection-blocked`, `write_tool_no_rule` → `write-tool-unruled` (review).

| Code | Severity | Rule |
| --- | --- | --- |
| `connector_unknown` | error | `connector` names no active curated or org-private connector (or that version) |
| `credential_unknown` | error | `credential` names no credential of the org |
| `credential_connector` | error | the credential was made for another connector |
| `credential_not_granted` | error | not granted to this factory (fix: link to the credential) |
| `credential_missing` | warning (B has no `blocked` severity; the connection status is Blocked) (review) | connector needs sign-in and `credential` is absent ("Azure is blocked: it has no credential yet. You can publish…") |
| `credential_expired` | warning | expired or needs reconnect (publish allowed; connection blocked) |
| `setting_invalid` | error | settings fail the connector's `spec.settings` schema |
| `scope_unsatisfied` | error | a scope is used but its settings are missing (`preview` without `origins`) |
| `tool_unknown_key` | error | a tool key not prefixed by this connection's name |
| `tool_blocked` | error | an entry enables a `blocked` tool |
| `agent_unknown` | error | `agents` names an agent the factory does not have |
| `write_tool_no_rule` | error | a write tool served to ≥ 1 agent with no `approval: required` and no scope (an `always` scope counts). Fixes: "Require approval" (`approval: required`), "Limit to <scope label>" for each scope that applies |
| `write_tool_no_agents` | info | write tools off for everyone (the "Every write tool is scoped to named agents" check passes) |
| `command_outside_allowlist` | error | a CLI `commands` entry is not within the connector's allowlist (equal to an entry, or an entry with its trailing `*` replaced by more literal words and an optional trailing `*`) |
| `inline_connection` | warning | legacy inline `type: mcp`; fix "Use the <name> connector" when URL/image matches |
| `merge_needs_github` | error | a station with `action: merge` and no connection whose connector is `github` (the line check on Define) |

Summary checks for Review and publish come from the same run: counts of connections that resolve, credentials from the vault or none needed, write tools scoped/needing approval, and "No credentials reach an agent" (true for every served connection; false only when a legacy inline connection carries a literal value, which is then an error).

`Resolution` (written by B in the publish transaction as `definition_versions.resolved` and projected into `connections` rows from `Publish.Bindings`, B §5.1) (review): per connection `{name, connector_id, connector_name, connector_version, credential_id|null, agents, policy, settings}`. Connector versions are pinned at publish; a new connector version reaches a factory only when it publishes again.

#### 4.4 Scopes

A scope is a connection-wide restriction the connector enforces; selecting it on any tool turns it on for the whole connection (the editor says so). Enforcement kinds, all on the host:
- `args` / `env`: added to the sidecar's command or environment (`--allowed-origins`, `--access-mode=restricted`).
- `token`: restricts minted credentials (`repositories: settings.repos` for a GitHub App token).
- `arguments`: the gateway refuses a call whose arguments fall outside the settings (`{owner: /owner, repo: /repo, in: settings.repos}`), before any approval or upstream call.

#### 4.5 Domain additions (`internal/domain`)

```go
type ConnectorKind string // "remote_mcp" | "mcp_image" | "cli"

type Connector struct {
	ID           uuid.UUID
	OrgID        OrgID // zero = curated
	Name         string
	Version      int
	Kind         ConnectorKind
	Title        string
	Status       string // active | deprecated
	Spec         json.RawMessage
	SourceKey    string
	SHA256       string
	RegistryName string
	CreatedAt    time.Time
}

type ConnectionBinding struct {
	OrgID               OrgID
	FactoryID           FactoryID
	Name                string
	DefinitionVersionID uuid.UUID
	ConnectorID         *uuid.UUID // nil: legacy inline connection
	ConnectorName       string
	ConnectorVersion    int
	CredentialID        *uuid.UUID
	Agents              []string
	Policy              map[string]ToolRule // served tool name → effective rule
	Settings            json.RawMessage
}

type ToolRule struct {
	Access   string   // read | write
	Agents   []string
	Approval bool
	Scope    string
	Off      bool
}

type ApprovalState string // pending, approved, rejected, expired, abandoned, executed, failed

type ToolApproval struct {
	ID                               uuid.UUID
	OrgID                            OrgID
	FactoryID                        FactoryID
	ItemID                           ItemID
	RunID                            RunID
	Station, Agent, Connection, Tool string
	Arguments                        json.RawMessage
	Preview                          json.RawMessage
	Why                              string
	State                            ApprovalState
	RequestedAt, ExpiresAt           time.Time
	DecidedBy                        *Actor // A's actor (no-auth)
	DecidedAt                        *time.Time
	DecisionReason                   string
	ExecutedAt                       *time.Time
	Outcome                          string // ok | tool_error | failed
	Holder                           string // owner gateway URL
}
```

New event kinds (item_events, kinds are text): `tool_approval_requested` (`approval_id, connection, tool`), `tool_approval_decided` (`approval_id, decision, by`), `tool_approval_settled` (`approval_id, state, outcome`). `gateway_tool_call` data gains `connector`, `connector_version`, `credential` (name), `approval_id`, `approved_by`, and outcomes `credential_expired`, `approval_rejected`, `approval_expired`, `command_refused`, `scope_refused`.

`TaskClaims` keeps A's `o` and B's `v`; the gateway resolves bindings of the version in `v`. The `g` owner-instance claim is deferred with forwarding (review).

#### 4.6 Approval-gated calls

```
agent ─tools/call jira__addCommentToJiraIssue─▶ gateway (owner instance)
  policy: approval required → ToolApprovals.Request (row pending + item event, one tx; NOTIFY goal_approvals)
  hold the HTTP request: MCP progress notification every 15 s (when the client sent a progressToken)
  wait on the instance's approval feed (LISTEN goal_approvals, Resync re-reads; also re-read every 30 s)
    approved  → Settle(approved→executed|failed) after forwarding the STORED arguments exactly once
    rejected  → tool error "Rejected by <name>: <reason>"; Settle not needed (terminal)
    deadline  → Settle(pending→expired); tool error "Not approved within 30m"
    run ended / agent disconnected → Settle(pending→abandoned)
```
- Deadline = min(`approvalTimeout`, token expiry − 1 min). The worker sets Claude Code's MCP tool timeout to the task timeout (`MCP_TOOL_TIMEOUT`, from `TaskSpec.Timeout`), so a held call never times out on the agent side first.
- The run keeps its lease and WIP slot while paused (the work is in progress and waits on a person); the engine is not changed. An item has a pending approval when a `pending` row exists for its running run; E shows it under Waiting on you.
- Double execution is impossible: only the holder forwards, and only after a CAS `approved → executed|failed` claim (`Settle` returns `ErrConflict` otherwise).
- Sweeper (`goal.approvals.sweep`, every 30 s under an advisory lock): pending rows past `expires_at` + 1 min, or whose run is not `running`, become `abandoned`/`expired` (covers a dead holder).

#### 4.7 Engine

- (review) `Config.GatewayPeerURL` / `GOAL_PEER_URL` are deferred with forwarding.
- `TaskSpec` unchanged; `worker/` reads `timeout` (already in spec.json) to set `MCP_TOOL_TIMEOUT`.
- Nothing else: approvals, connection resolution and credentials live in the gateway and `internal/connectors`.

### 5. Ports and adapters

#### 5.1 New ports (`internal/ports`)

```go
// ConnectorCatalog persists the connector index, factories' connection bindings and health,
// connection tests, and OAuth clients and handshakes. Implemented by internal/adapters/postgres.
// (review) Per the README convention the org comes from ctx; the explicit org parameters below
// (here and in ToolApprovals) are dropped. Curated writes run with goal.scope = 'platform'.
// SaveOAuthState/SaveOAuthClient take plaintext verifier and secrets and seal them inside the
// adapter with 4.1's envelope.go.
type ConnectorCatalog interface {
	// Connector returns org's connector by name: the org-private one when it exists (it shadows a
	// curated one of the same name), else the curated one. version 0 = latest active.
	Connector(ctx context.Context, org domain.OrgID, name string, version int) (domain.Connector, error)
	ConnectorByID(ctx context.Context, org domain.OrgID, id uuid.UUID) (domain.Connector, error)
	ListConnectors(ctx context.Context, org domain.OrgID, q ConnectorQuery) ([]ConnectorListing, error)
	// SaveConnector inserts a new version (curated when org is zero, platform scope only). The
	// (org, name, version) is unique; an existing one is ErrConflict.
	SaveConnector(ctx context.Context, org domain.OrgID, c domain.Connector) (domain.Connector, error)
	SetConnectorStatus(ctx context.Context, org domain.OrgID, id uuid.UUID, status string) error

	Connections(ctx context.Context, org domain.OrgID, factory domain.FactoryID) ([]ConnectionState, error)
	ConnectionsByCredential(ctx context.Context, org domain.OrgID, credential uuid.UUID) ([]ConnectionState, error)
	// TouchConnection records a call's health; callers throttle to once a minute per connection.
	TouchConnection(ctx context.Context, org domain.OrgID, factory domain.FactoryID, name string, h ConnectionHealth) error
	SaveConnectionTest(ctx context.Context, t ConnectionTest) error
	LatestConnectionTests(ctx context.Context, org domain.OrgID, factory domain.FactoryID) ([]ConnectionTest, error)
	ToolCallStats(ctx context.Context, org domain.OrgID, factory domain.FactoryID, since time.Time) ([]ToolCallStat, error)

	OAuthClient(ctx context.Context, org domain.OrgID, issuer, redirectURI string) (OAuthClientRecord, error)
	SaveOAuthClient(ctx context.Context, c OAuthClientRecord) (OAuthClientRecord, error)
	SaveOAuthState(ctx context.Context, s OAuthState) error
	// TakeOAuthState deletes and returns an unexpired state: ErrNotFound when missing, used or expired.
	TakeOAuthState(ctx context.Context, org domain.OrgID, stateHash []byte, now time.Time) (OAuthState, error)
	DeleteExpiredOAuthStates(ctx context.Context, now time.Time) (int, error)
}

// OAuth speaks OAuth 2.1 to authorization servers on the goald host. Implemented by
// internal/adapters/oauthclient. Tokens never appear in errors.
type OAuth interface {
	// Discover finds the authorization server of a protected resource (RFC 9728, from the
	// resource's WWW-Authenticate or well-known document), else of issuer (RFC 8414, then OIDC).
	Discover(ctx context.Context, resource, issuer string) (OAuthServer, error)
	Register(ctx context.Context, srv OAuthServer, r ClientRegistration) (OAuthClientCredentials, error) // RFC 7591
	Exchange(ctx context.Context, srv OAuthServer, c OAuthClientCredentials, code, verifier, redirectURI, resource string) (OAuthTokens, error)
	// Refresh wraps ErrOAuthGrant when the server answers invalid_grant (reconnect needed).
	Refresh(ctx context.Context, srv OAuthServer, c OAuthClientCredentials, refreshToken, resource string) (OAuthTokens, error)
	Revoke(ctx context.Context, srv OAuthServer, c OAuthClientCredentials, token, hint string) error // RFC 7009, best effort
}

// ToolApprovals holds write calls waiting for a person. Implemented by internal/adapters/postgres.
type ToolApprovals interface {
	// Request inserts a pending approval and its tool_approval_requested event in one transaction.
	Request(ctx context.Context, a domain.ToolApproval) (domain.ToolApproval, error)
	ToolApproval(ctx context.Context, org domain.OrgID, id uuid.UUID) (domain.ToolApproval, error)
	ListToolApprovals(ctx context.Context, q ToolApprovalQuery) ([]domain.ToolApproval, error)
	// Decide moves pending → approved|rejected (CAS) with its event; ErrConflict when not pending.
	Decide(ctx context.Context, d ToolApprovalDecision) (domain.ToolApproval, error)
	// Settle moves from → to (CAS) with its event; ErrConflict when the row is not in from.
	Settle(ctx context.Context, org domain.OrgID, id uuid.UUID, from, to domain.ApprovalState, outcome string, at time.Time) error
	// SweepStale settles pending approvals past their deadline or whose run is no longer running.
	SweepStale(ctx context.Context, now time.Time, limit int) (int, error)
	// Listen delivers a notice for every committed state change (Resync first, as ItemNotices).
	Listen(ctx context.Context, deliver func(ToolApprovalNotice)) error
}

// Minter derives a short-lived credential from a long-lived one. Implemented by
// internal/adapters/github (kind github_app) and internal/adapters/awssts (kind aws_role).
type Minter interface {
	Mint(ctx context.Context, r MintRequest) (Minted, error)
}

type MintRequest struct {
	Kind         string
	Fields       map[string]string // resolved credential fields; never logged
	Repositories []string          // github_app: owner/name, all on one installation
	Permissions  map[string]string // github_app
	SessionName  string            // aws_role: goal-<run id>
	Duration     time.Duration
}

type Minted struct {
	Fields    map[string]string // github_app: token; aws_role: access_key_id, secret_access_key, session_token
	ExpiresAt time.Time
}

// MCPRegistry reads the official MCP Registry. Implemented by internal/adapters/mcpregistry.
type MCPRegistry interface {
	Search(ctx context.Context, q, cursor string, limit int) (RegistryPage, error)
	// Server returns server.json; OCI packages carry their resolved sha256 digest.
	Server(ctx context.Context, name, version string) (RegistryServer, error)
}
```

`Sidecars` is extended for CLI tools (adapter `docker`):

```go
type SidecarSpec struct {
	// ... existing fields
	Entrypoint []string // replaces the image's entrypoint when set (exec sidecars)
}

type Sidecars interface {
	Start(ctx context.Context, s SidecarSpec) (Sidecar, error)
	// StartExec starts a sidecar that runs Entrypoint and serves Exec calls (docker exec).
	StartExec(ctx context.Context, s SidecarSpec) (ExecSidecar, error)
}

type ExecSidecar interface {
	// Exec runs argv (no shell) with stdin closed, killing it at timeout; output beyond max
	// bytes is dropped and reported as truncated. A command that ran and failed is a result.
	Exec(ctx context.Context, argv []string, env map[string]string, timeout time.Duration, max int64) (ExecResult, error)
	Close() error
}

type ExecResult struct {
	ExitCode         int
	Stdout, Stderr   []byte
	Truncated, Timed bool
	Duration         time.Duration
}
```
Exec wraps argv as `timeout --signal=KILL <n>s argv…` when the connector says the image has `timeout` (default true); otherwise a timed-out exec closes the sidecar, which restarts lazily.

Needed from 4.1: `Vault` as 4.1 §5.2 defines it (section 16) (review).

#### 5.2 New package `internal/connectors`

- `Parse([]byte) (*Spec, error)`: YAML → schema (`schema.Connector`, embedded) → cross checks (tool names unique, scopes referenced exist, templates parse, images pinned, `commands` patterns parse).
- `Policy(spec, conn config.Connection, factory) Policy`; `Checker` (4.3); `Template` rendering; `cli.Parse(command) ([]string, error)` and `cli.Match(patterns, argv)`.
- `Service` (wired in `cmd/goald`): `SyncCurated(ctx)`, `CreatePrivate(ctx, org, yaml, by)`, `ImportPreview(ctx, org, name, version)`, `OAuthStart`, `OAuthCallback`, `RefreshDue(ctx)`, `Material(ctx, org, factory, run, binding) (Material, error)` (fresh OAuth access, minted tokens, plain fields; implements `gateway.Credentials`), `TestCredential`, `Revoked(ctx, org, credential)` hook.
- Imports `domain`, `ports`, `config`, `authoring` (for the `Check`/`Resolver` types), `schema`, yaml and jsonschema only (review).

#### 5.3 Gateway changes (`internal/gateway`)

- `Deps` gains `Credentials` (`Material` above), `Approvals ports.ToolApprovals`, `ApprovalFeed` (instance fan-out over `ToolApprovals.Listen`, started in main), `Bindings` (the run's definition version `Resolution`, via B's definitions cache), `Peers http.RoundTripper` (forwarding); `Config` gains `ApprovalTimeout` (30m), `PeerURL`, `CLITimeoutMax` (5m).
- Session `prepare` for a connector connection: render URL/headers/env/args from the connector spec over `Material` fields; apply scopes; credentials expired/revoked/not granted → unavailable with a value-free reason ("its credential grafana-acme expired on Oct 21") and a denied event outcome `credential_expired` per refused call. GitHub App tokens are minted on first use per (run, connection), cached in memory until 5 min before expiry, and revoked at `EndRun` (best effort).
- `policy` is replaced by `connectors.Policy` (legacy allow/deny folded in).
- CLI connections: an exec sidecar per (run, connection) in the existing pool (same idle/max/close rules); the gateway serves the synthetic tool `run` (`{command: string}`, description lists the allowed patterns) itself; `setup` commands run once after start.
- Approval hold (4.6) before forwarding; held requests are counted in the pool's per-run accounting so `EndRun` settles them `abandoned`.
- Forwarding: deferred (review; see section 2).
- Health: `TouchConnection` (last_used_at, last_ok_at or last_error/at), throttled per instance; credential use is recorded by `Vault.Resolve` itself (review).
- Tester (used by the API via an interface): `TestConnection(ctx, org, factory, binding or draft connection, by)` opens a session as a synthetic run `test-<uuid>` (sidecars started and stopped within the call), lists tools, runs the method's `test`, returns tools with their effective rules; `TestCredential(ctx, org, connector, method, fields)` the same without a factory.

#### 5.4 Adapters

| Adapter | Implements | Notes |
| --- | --- | --- |
| `postgres` | `ConnectorCatalog`, `ToolApprovals` | sqlc queries; approvals Listen on its own connection (like Notices) |
| `oauthclient` (new) | `OAuth` | stdlib HTTP; `GOAL_OAUTH_HTTP_TIMEOUT` 15s; no redirects followed on token endpoints; https only except loopback; response size cap 1 MiB; `iss` (RFC 9207) checked when advertised |
| `github` | `Minter` (`github_app`) | RS256 JWT (iat−60s, exp+9m), `POST /app/installations/{id}/access_tokens` with `repositories` and `permissions`; `DELETE /installation/token` on revoke; `GOAL_GITHUB_API_URL` |
| `awssts` (new) | `Minter` (`aws_role`) | AWS SDK v2 STS `AssumeRole` with goald's standard credential chain, `ExternalId`, `DurationSeconds` |
| `mcpregistry` (new) | `MCPRegistry` | `GOAL_MCP_REGISTRY_URL` (default `https://registry.modelcontextprotocol.io`), `/v0/servers`; digest via OCI distribution `HEAD /v2/<repo>/manifests/<tag>` with anonymous bearer tokens |
| `docker` | `Sidecars.StartExec` | same hardening as sidecars; entrypoint override; exec with `AttachStdout/Stderr`, no TTY, user = sidecar user, `WorkingDir /tmp` |

`gitworkspace` and `github` (`CodeHost`) accept a repository credential of method `github_app`: the token is minted per use through `Minter` for that one repository (4.1 moves `Repository.AuthRef` to a credential reference; 4.2 adds the minting path).

#### 5.5 depguard

- `internal/connectors`: allow `domain`, `ports`, `config`, `authoring`, `schema`, `sigs.k8s.io/yaml`, `santhosh-tekuri/jsonschema`; deny `adapters`, `engine`, `api`, `gateway`, `definitions`, `factorystore`, cloud SDKs (review).
- `internal/gateway`: add `internal/connectors` to its allowed imports.
- `internal/adapters/oauthclient`, `mcpregistry`, `awssts`: the usual adapter row (own SDK; `awssts` may import `aws-sdk-go-v2/service/sts`).
- `internal/api`: add a deny for `internal/connectors`; it reaches the service, tester and approvals through small interfaces it declares (review).

### 6. Data model

Migrations `00052`–`00054` (goose, forward-only). (review) A's `goal_org()` stays strict. `00052` adds `goal_org_or_null()` = `nullif(current_setting('goal.org_id', true), '')::uuid` and uses it only in the `connectors` policies, so curated reads work without an org. Every other table below follows A's kit: `org_id NOT NULL DEFAULT goal_org()` with `ON DELETE CASCADE`, a policy named `org_isolation` using `(SELECT goal_org())`, and composite parent keys (`(org_id, factory_id) REFERENCES factories (org_id, id)`, likewise for items, runs and credentials). `00052` also adds `credentials.connector text`, `credentials.method text` and `credentials.needs_reconnect_at timestamptz` (4.1 §6.6). The binding columns of `connections` are the projection of the current version's `definition_versions.resolved` (B), written by B's publish transaction.

#### 00052_connectors.sql

```sql
CREATE TABLE connectors (
  id            uuid PRIMARY KEY,
  org_id        uuid REFERENCES organizations (id),          -- NULL = curated
  name          text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$' AND name NOT LIKE '%--%'),
  version       integer NOT NULL CHECK (version > 0),
  kind          text NOT NULL CHECK (kind IN ('remote_mcp', 'mcp_image', 'cli')),
  title         text NOT NULL CHECK (length(title) <= 120),
  spec          jsonb NOT NULL,
  source_key    text NOT NULL,
  sha256        text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deprecated')),
  registry_name text,
  created_by    text,                                       -- Actor.Ref() (no-auth)
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX connectors_curated_key ON connectors (name, version) WHERE org_id IS NULL;
CREATE UNIQUE INDEX connectors_private_key ON connectors (org_id, name, version) WHERE org_id IS NOT NULL;
CREATE INDEX connectors_latest_idx ON connectors (org_id, name, version DESC) WHERE status = 'active';
ALTER TABLE connectors ENABLE ROW LEVEL SECURITY;
CREATE POLICY connectors_read ON connectors FOR SELECT
  USING (org_id IS NULL OR org_id = goal_org());
CREATE POLICY connectors_org_insert ON connectors FOR INSERT WITH CHECK (org_id = goal_org());
CREATE POLICY connectors_org_update ON connectors FOR UPDATE
  USING (org_id = goal_org()) WITH CHECK (org_id = goal_org());
-- Curated rows are written only by the catalog sync job, which sets goal.scope = 'platform' in
-- its transaction. A guard against request paths, not a boundary against goald itself.
CREATE POLICY connectors_curated_write ON connectors FOR ALL
  USING (org_id IS NULL AND current_setting('goal.scope', true) = 'platform')
  WITH CHECK (org_id IS NULL AND current_setting('goal.scope', true) = 'platform');

-- The published binding of each current connection, written by B's publish transaction
-- (rows of removed connections are deleted there), and its health, written by the gateway.
CREATE TABLE connections (
  id                    uuid PRIMARY KEY,
  org_id                uuid NOT NULL REFERENCES organizations (id),
  factory_id            uuid NOT NULL REFERENCES factories (id),
  name                  text NOT NULL,
  definition_version_id uuid NOT NULL REFERENCES definition_versions (id),
  connector_id          uuid REFERENCES connectors (id),     -- NULL: legacy inline connection
  credential_id         uuid REFERENCES credentials (id) ON DELETE SET NULL,
  agents                text[] NOT NULL DEFAULT '{}',
  policy                jsonb NOT NULL DEFAULT '{}',          -- served tool → effective rule
  settings              jsonb NOT NULL DEFAULT '{}',          -- non-secret settings only
  last_used_at          timestamptz,
  last_ok_at            timestamptz,
  last_error_at         timestamptz,
  last_error            text CHECK (length(last_error) <= 500), -- scrubbed
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  UNIQUE (factory_id, name)
);
CREATE INDEX connections_credential_idx ON connections (credential_id) WHERE credential_id IS NOT NULL;
CREATE INDEX connections_connector_idx ON connections (org_id, connector_id);
ALTER TABLE connections ENABLE ROW LEVEL SECURITY;
CREATE POLICY connections_org ON connections USING (org_id = goal_org()) WITH CHECK (org_id = goal_org());

CREATE TABLE connection_tests (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL REFERENCES organizations (id),
  factory_id       uuid REFERENCES factories (id),             -- NULL: a credential test
  connection       text,
  connector_id     uuid NOT NULL REFERENCES connectors (id),
  credential_id    uuid REFERENCES credentials (id) ON DELETE SET NULL,
  ok               boolean NOT NULL,
  tools            jsonb NOT NULL DEFAULT '[]',                -- [{name, description, access, known}]
  error            text CHECK (length(error) <= 500),
  duration_ms      integer NOT NULL,
  tested_by        text,                                       -- Actor.Ref()
  tested_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connection_tests_latest_idx ON connection_tests (factory_id, connection, tested_at DESC);
ALTER TABLE connection_tests ENABLE ROW LEVEL SECURITY;
CREATE POLICY connection_tests_org ON connection_tests USING (org_id = goal_org()) WITH CHECK (org_id = goal_org());

-- "This week" per tool on the Connections page.
CREATE INDEX item_events_tool_calls_idx ON item_events (factory_id, at) WHERE kind = 'gateway_tool_call';
```
Status is derived at read time, never stored: `blocked` (connector needs sign-in and no credential; credential expired, needs reconnect, revoked or no longer granted), `failing` (last_error_at > last_ok_at within 15 min), `connected` (a call or test succeeded), `untested`.

#### 00053_oauth.sql

```sql
CREATE TABLE oauth_clients (
  id                  uuid PRIMARY KEY,
  org_id              uuid NOT NULL REFERENCES organizations (id),
  issuer              text NOT NULL,
  redirect_uri        text NOT NULL,
  registration        text NOT NULL CHECK (registration IN ('metadata_document', 'dynamic', 'static')),
  client_id           text NOT NULL,
  client_secret_sealed bytea,           -- Vault.Seal; NULL for public clients
  registration_token_sealed bytea,      -- RFC 7592 access token, when issued
  server              jsonb NOT NULL,   -- discovered metadata: endpoints and features
  created_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, issuer, redirect_uri)
);
ALTER TABLE oauth_clients ENABLE ROW LEVEL SECURITY;
CREATE POLICY oauth_clients_org ON oauth_clients USING (org_id = goal_org()) WITH CHECK (org_id = goal_org());

CREATE TABLE oauth_states (
  state_hash           bytea PRIMARY KEY,           -- sha256 of the whole state string
  org_id               uuid NOT NULL REFERENCES organizations (id),
  connector_id         uuid NOT NULL REFERENCES connectors (id),
  method               text NOT NULL,
  client_id            uuid NOT NULL REFERENCES oauth_clients (id),
  pkce_verifier_sealed bytea NOT NULL,              -- Vault.Seal
  resource             text NOT NULL,
  scopes               text[] NOT NULL DEFAULT '{}',
  credential_id        uuid REFERENCES credentials (id),   -- reconnecting this credential
  credential_name      text,                              -- or creating one
  grants               uuid[] NOT NULL DEFAULT '{}',
  return_to            text NOT NULL CHECK (return_to ~ '^/o/[^/]+/'),
  requested_by         text NOT NULL,                     -- Actor.Ref()
  created_at           timestamptz NOT NULL DEFAULT now(),
  expires_at           timestamptz NOT NULL,
  CHECK ((credential_id IS NULL) <> (credential_name IS NULL))
);
CREATE INDEX oauth_states_expires_idx ON oauth_states (expires_at);
ALTER TABLE oauth_states ENABLE ROW LEVEL SECURITY;
CREATE POLICY oauth_states_org ON oauth_states USING (org_id = goal_org()) WITH CHECK (org_id = goal_org());
```
The state string is `<org id, base64url>.<32 random bytes, base64url>`: the callback reads the org from it, sets `goal.org_id`, then takes the row by hash under RLS, so a state cannot be redeemed in another org and the database never holds a usable state.

#### 00054_tool_approvals.sql

```sql
CREATE TABLE tool_approvals (
  id              uuid PRIMARY KEY,
  org_id          uuid NOT NULL REFERENCES organizations (id),
  factory_id      uuid NOT NULL REFERENCES factories (id),
  item_id         uuid NOT NULL REFERENCES work_items (id),
  run_id          uuid NOT NULL REFERENCES agent_runs (id),
  station         text NOT NULL,
  agent           text NOT NULL,
  connection      text NOT NULL,
  tool            text NOT NULL,
  arguments       jsonb NOT NULL CHECK (octet_length(arguments::text) <= 65536),
  arguments_sha256 text NOT NULL,
  preview         jsonb,
  why             text NOT NULL,
  state           text NOT NULL DEFAULT 'pending'
                  CHECK (state IN ('pending', 'approved', 'rejected', 'expired', 'abandoned', 'executed', 'failed')),
  requested_at    timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  decided_by      text,                                       -- Actor.Ref()
  decided_at      timestamptz,
  decision_reason text CHECK (length(decision_reason) <= 2000),
  executed_at     timestamptz,
  outcome         text CHECK (outcome IN ('ok', 'tool_error', 'failed')),
  holder          text NOT NULL,
  CHECK ((state IN ('approved', 'rejected', 'executed', 'failed')) = (decided_by IS NOT NULL))
);
CREATE INDEX tool_approvals_pending_idx ON tool_approvals (org_id, factory_id, requested_at) WHERE state = 'pending';
CREATE INDEX tool_approvals_item_idx ON tool_approvals (item_id, requested_at DESC);
CREATE INDEX tool_approvals_run_idx ON tool_approvals (run_id) WHERE state IN ('pending', 'approved');
CREATE INDEX tool_approvals_stats_idx ON tool_approvals (factory_id, connection, tool, requested_at);
ALTER TABLE tool_approvals ENABLE ROW LEVEL SECURITY;
CREATE POLICY tool_approvals_org ON tool_approvals USING (org_id = goal_org()) WITH CHECK (org_id = goal_org());

-- +goose StatementBegin
CREATE FUNCTION notify_tool_approval() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' OR NEW.state IS DISTINCT FROM OLD.state THEN
    PERFORM pg_notify('goal_approvals', json_build_object(
      'id', NEW.id, 'org_id', NEW.org_id, 'factory_id', NEW.factory_id, 'item_id', NEW.item_id,
      'run_id', NEW.run_id, 'state', NEW.state, 'at', now())::text);
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER tool_approvals_notify AFTER INSERT OR UPDATE OF state ON tool_approvals
  FOR EACH ROW EXECUTE FUNCTION notify_tool_approval();
```

Backfill: none (new tables). Retention: `connection_tests` keep the newest 20 per connection (pruned by the sweep job); `oauth_states` deleted when taken or expired; `tool_approvals` kept (audit).

#### sqlc queries (`queries/connectors.sql`, `connections.sql`, `oauth.sql`, `tool_approvals.sql`)

`GetConnector` (org-private first: `ORDER BY org_id NULLS LAST, version DESC LIMIT 1`), `GetConnectorByID`, `ListConnectors` (with kind/q filters and `used_by` = distinct factory names from `connections`), `InsertConnector`, `SetConnectorStatus`, `ListConnections` (joined with credentials for status inputs and with the latest test), `ConnectionsByCredential`, `TouchConnection`, `InsertConnectionTest`, `LatestConnectionTests` (`DISTINCT ON (connection)`), `PruneConnectionTests`, `ToolCallStats` (`item_events` 7 days grouped by `data->>'connection', data->>'tool'` plus approved/rejected counts from `tool_approvals`), `GetOAuthClient`, `UpsertOAuthClient`, `InsertOAuthState`, `TakeOAuthState` (`DELETE … WHERE state_hash = $1 AND expires_at > $2 RETURNING *`), `DeleteExpiredOAuthStates`, `InsertToolApproval` (+ `InsertItemEvent` in the same tx), `GetToolApproval`, `ListToolApprovals` (keyset on `(requested_at, id)`), `DecideToolApproval` / `SettleToolApproval` (`UPDATE … WHERE id = $1 AND state = $2 RETURNING *`), `SweepStaleApprovals` (`FOR UPDATE SKIP LOCKED`, joined to `agent_runs.status`).

#### Conformance suites

- `porttest/approvaltest`: request → decide → settle paths; deciding twice is `ErrConflict`; concurrent decide (20 goroutines) has exactly one winner; settle CAS; sweep abandons pending rows of finished runs and expires overdue ones; `Listen` delivers Resync then every change. Run by the fake (unit) and postgres (integration).
- `porttest/catalogtest`: org-private shadows curated; version 0 = latest active; deprecated excluded from latest but readable by version; duplicate version is `ErrConflict`; `TakeOAuthState` is one-time and refuses expired.
- Postgres-only (RLS): org B cannot read org A's private connector, connections, tests, clients, states or approvals even by id; curated rows are visible to both; a request transaction (no `goal.scope`) cannot insert or update curated rows.

### 7. Bucket layout

```
catalog/connectors/<name>/<version>.yaml     curated entries (operator-written, immutable)
orgs/<org-id>/blobs/<sha256>                 org-private connector files (B's blob store)
```
Nothing else. The sync job (`goal.catalog.sync`, every `GOAL_REFRESH_EVERY`, advisory lock) lists `catalog/connectors/`, parses new keys and inserts index rows; a key whose content differs from its indexed `sha256` is logged as an error and ignored (versions are immutable); a parse failure is logged and skipped, never half-indexed.

### 8. API

Tag `connectors`. Errors are RFC 9457 problems; an unknown org is 404 (A). (identity) Each operation names its permission: managing connectors `connector:manage` (owners and admins), testing a connection `connection:test` (editors), deciding approvals `approval:decide` (approvers, fresh session). Lists use `?cursor=&limit=` (default 50, max 200) and return `next_cursor`.

#### Catalog

| Method, path | Request → response | Errors |
| --- | --- | --- |
| `GET /v1/orgs/{org}/connectors?kind=&q=&deprecated=` | → `{connectors: ConnectorCard[], counts: {all, remote_mcp, mcp_image, cli}}` | |
| `GET /v1/orgs/{org}/connectors/{connector}?version=` | → `ConnectorDetail` (card + `spec`, `versions[]`, `sign_in[]` with field definitions, `tools[]`, `used_by[]`, `private`) | 404 |
| `POST /v1/orgs/{org}/connectors` | `{spec_yaml}` → 201 `ConnectorDetail` (version 1) | 400 `connector_invalid` (problems with line/column), 409 name taken (curated or private) |
| `POST /v1/orgs/{org}/connectors/{connector}/versions` | `{spec_yaml}` → 201 (version = latest + 1) | 400, 404, 409 curated (fork it under another name) |
| `POST /v1/orgs/{org}/connectors/{connector}/deprecate` | → 200 | 404, 409 curated |
| `GET /v1/orgs/{org}/registry/servers?q=&cursor=` | → `{servers: RegistryServer[], next_cursor}` | 502 registry unreachable |
| `POST /v1/orgs/{org}/connectors/import-preview` | `{registry_name, version}` → `{spec_yaml, warnings[]}` (not saved; npm/pypi-only servers → 422 "only remote servers and OCI images can be imported") | 404, 422, 502 |

`ConnectorCard`: `{name, version, title, tile, kind, where, sign_in: [{method, label}], policy: {read, write, approval, blocked, summary}, used_by: [factory name], private, status}`.

#### Connections

| Method, path | Request → response |
| --- | --- |
| `GET /v1/orgs/{org}/factories/{factory}/connections?agent=` | → `{connections: Connection[], summary: {total, connected, blocked, failing}}` |
| `GET /v1/orgs/{org}/factories/{factory}/connections/{connection}` | → `ConnectionDetail` |
| `POST /v1/orgs/{org}/factories/{factory}/connection-tests` | `{name}` (published) or `{connection}` (a draft connection object in factory.yaml form) → `ConnectionTest` |

`Connection`: `{name, connector: {name, version, title, tile, kind}, kind_label, subtitle, credential: {id, name, method_label, detail} | null, agents[], status, status_reason, last_used_at, definition_version}`; `ConnectionDetail` adds `tools: [{name, access, agents, approval, scope, off, known, calls_7d, approved_7d, rejected_7d}]`, `commands: {allowed: [pattern], connector_total}` for CLI, `latest_test`. Test errors: 400 invalid draft, 404, 422 credential not granted; the test itself failing is `ok: false`, not an error. Timeout 60 s (sidecar pulls continue in the background; result says "image still pulling, try again").

#### Credentials (connector part; 4.1 owns `/credentials` CRUD)

| Method, path | Request → response |
| --- | --- |
| `POST /v1/orgs/{org}/credential-tests` | `{connector, method, fields}` → `{ok, detail}` (fields never echoed) |
| `POST /v1/orgs/{org}/oauth/start` | `{connector, method?, name? \| credential?, grants[], return_to}` → `{authorize_url}` |
| `GET /v1/oauth/callback?code=&state=&iss=` / `?error=&state=` | → 303 to `return_to` with `?connected=<credential>` or `?oauth_error=<code>` |
| `GET /oauth/client-metadata.json` | the client ID metadata document (only served when `GOAL_PUBLIC_URL` is https) |

4.1's `POST /credentials` and rotation call 4.2's hook to validate `fields` against the connector method (unknown or missing fields → 400 with field names). 4.1's revoke calls `Service.Revoked` (OAuth revocation, best effort; audit).

#### Approvals

| Method, path | Request → response | Errors |
| --- | --- | --- |
| `GET /v1/orgs/{org}/approvals?state=pending&factory=&item=` | → `{approvals: ToolApproval[], next_cursor}` | |
| `GET /v1/orgs/{org}/approvals/{approval}` | → `ToolApprovalDetail` | 404 |
| `POST /v1/orgs/{org}/approvals/{approval}/approve` | `{}` → `ToolApproval` | 404, 409 not pending |
| `POST /v1/orgs/{org}/approvals/{approval}/reject` | `{reason}` (1–2000 chars) → `ToolApproval` | 400, 404, 409 |

`ToolApproval`: `{id, item: {id, ref, title}, factory, station, agent, agent_label ("investigator-1": agent + lease slot + 1), connection, tool, access, state, requested_at, expires_at, decided_by: {id, name} | null, decided_at, decision_reason, outcome}`. `ToolApprovalDetail` adds `arguments`, `preview: {title, format, body, by}`, `why`, `policy_link` (definition section + connection + tool), `signed_in_as: {credential, method_label, consented_by, consented_at}`, `rationale` (text of the run's last `agent_message` event before the request, ≤ 1000 chars).

#### SSE (on A/E's `/v1/orgs/{org}/stream`)

- `event: approval` `{approval_id, factory_id, item_id, ref, state, at}` from `goal_approvals` (org-filtered).
- Item events `tool_approval_*` flow as `event: item` as today.
- `event: connection` `{factory_id, connection, status}` when a derived status changes (credential expired, revoked or refreshed; test result; first failure), published by goald through `pg_notify('goal_org_events', {org_id, event: "connection", …})` (A's channel) in the transaction that causes it (review).

### 9. CLI

- `goal catalog validate <dir|file>`: parse and validate connector files locally (operator and private authors).
- `goal catalog push <dir>`: operator command, writes curated files to the bucket directly with the bucket credentials (like today's factory push); refuses to overwrite an existing `<name>/<version>.yaml` with different content.
- `goal connectors [--kind]`, `goal connections <factory>`: API reads, table output.
- `goal approvals [--factory] [--all]`, `goal approval <id>` (prints call and preview), `goal approval approve <id>`, `goal approval reject <id> --reason …`.
- `goal follow` prints `waiting on you: jira__addCommentToJiraIssue (approval 0193…) — goal approval <id>` once per pending approval.

### 10. Web UI

Data in `web/src/api` (client functions + `queries.ts`); features `features/catalog`, `features/connections`, `features/approvals`; the connector panel for the builder in `shared/connections-editor` (used by D's wizard and Definition section routes).

| Route | Board | Screen |
| --- | --- | --- |
| `/o/:org/catalog` | Catalog | grid of `ConnectorCard`; `?kind=&q=` in the URL |
| `/o/:org/catalog/new` | (Add connector) | YAML editor (Monaco + `connector.schema.json`) with server problems; "Import from MCP Registry" opens a search dialog that fills it via import-preview |
| `/o/:org/catalog/:connector` | (card click) | detail: versions, sign-in methods, tools with access/default, used by |
| `/o/:org/credentials/new?connector=` | Credential-New | steps 1–2 by 4.2, step 3 and save by 4.1 |
| `/o/:org/f/:factory/connections?connection=&status=` | Connections | read-only operational page |
| `/o/:org/f/:factory/items/:ref/approvals/:approval` | Approval | decision page |
| `/o/:org/f/:factory/definition/connections?connection=` and `/o/:org/new/connections?connection=` | Define-Connections, New-Connections | 4.2's panel inside D's section shell |
| `/o/:org/f/:factory/agents/:agent` | Agent | 4.2's Connections card |

TanStack Query keys (under C's `['org', <org id>]` root, review): `['connectors', {kind, q}]`, `['connector', name, version]`, `['registry', q]`, `['factory', factory, 'connections', {agent}]`, `['factory', factory, 'connection', name]`, `['approvals', {state, factory, item}]`, `['approval', id]`. SSE: `approval` → invalidate `approvals*`, `approval(id)`, the item's queries; `connection` → the factory's connections queries; `resync` → all of them.

Screen behaviour:
- **Catalog**: kind segmented control with counts; search filters client-side over the loaded list; card "Used by" "No factory yet" in muted text; Add/Import shown with `connector:manage` (identity). Empty search → "No connector matches …" with a clear button.
- **Connections** (read-only; edits go to Definition): banner for the first blocked connection with its reason and a button to its credential (4.1's detail, "Paste a new token" / reconnect for OAuth via `oauth/start` with `credential`); row menu: Open credential, Edit in Definition, Test now (shows the result inline, does not change the definition). Selecting a row shows its tools panel (`?connection=`); a CLI connection shows the allowlist panel ("N of M shown", "No write commands" when none is write, Edit allowlist → Definition). Status uses icon + text, never colour alone. Phone: rows become cards; the tools table becomes a list per tool with agent chips.
- **Credential-New**: connectors with at least one sign-in method other than `none`; methods as radio cards; OAuth method shows "Sign in with <vendor>" and starts `oauth/start` (grants and name are collected first, carried in the state); field kinds render as inputs with Show toggles for secrets (`autocomplete=off`, never prefilled, cleared on unmount); "Test and save" runs `credential-tests` then 4.1's create; a failed test shows the detail and offers "Save anyway". Grant hints per factory from its connections ("Can use it in its Datadog connection" / "No Datadog connection yet").
- **Connections panel** (wizard and Definition): left list of the draft's connections with derived status (Tested, Connected, Blocked, N problems) and "Add from the catalog" (search + Add appends a connector-form connection with defaults to the draft via D's draft patch); right panel: title, description, Remove… (confirm dialog naming agents that lose tools), credential select (only credentials for this connector granted to this factory; "Add a credential" link to Credential-New with `connector=` and return), status line from the latest test with "Test" button, tools table (Access chip, agent checkboxes in a group labelled "Agents allowed to use <tool>", needs-approval `role=switch` labelled "<tool> needs approval", "New" badge for tools a test listed that the catalog and the previous version do not know, "Preview only" under a tool whose scope applies), problems inline under their row with quick-fix buttons applying the problem's patch, "N more read tools are off. Show all". Every change is a draft patch; the YAML view reflects it.
- **Approval**: "Approve and run" primary; reason textarea; "Reject with this reason" disabled until a reason is typed; keys A (approve) and R (focus reason) when focus is not in a text field; after a decision the page shows the outcome and links back to the item; a decided, expired or abandoned approval renders read-only with who/when. Arguments in a read-only code viewer (Fira Code, line numbers, `aria-label="Call arguments, JSON"`); the markdown preview is rendered without raw HTML. Phone: decision panel as a sticky bottom area with 44 px buttons.
- **Agent Connections card**: `connections?agent=` rows with subtitle "<kind label>, credential: <name>[, read only][. Comments need approval]" and Ready/Blocked status; builtin `knowledge` shows "Not served yet".

States: skeletons for loading; errors as inline problems with retry; empty catalog filter, no connections ("This factory connects to nothing yet. Add connections in Definition."), no pending approvals. Follow MASTER.md tokens; Playwright with axe at 375, 768, 1440 px.

### 11. Security and tenancy

- Every table has `org_id` and RLS; every request and job transaction sets the org. The OAuth callback derives the org from the state and redeems the one-time state (identity: the person finishing the flow must be `requested_by`, the signed-in user who started it, or the state is refused); `return_to` must be an `/o/<this org>/…` path (open-redirect guard).
- Credentials reach only the goald host: headers on upstream requests, a sidecar's environment/arguments, an exec's environment. Never the agent's container, item events, logs, API responses or the audit log. Resolved values (≥ 6 chars), minted tokens and their JSON-escaped forms are scrubbed from results, errors and logs (existing mechanism), plus CLI `scrub` regexes and generic patterns (AWS key ids, PEM blocks, JWT-shaped tokens).
- The gateway checks on every call: token, agent still declared in the run's definition version, connection granted to the agent, tool permitted by the effective policy, credential granted to the factory and live, scope arguments, CLI command grammar and allowlist, then approval.
- CLI commands never pass through a shell: unquoted `; & | < > $ \` ( ) { }` and newlines are refused; `denyArgs` refused in either `--flag value` or `--flag=value` form; command ≤ 4 KiB; output capped; per-call timeout ≤ 5 min.
- OAuth: PKCE S256 only; state 32 random bytes, 10-minute expiry, one-time; verifier sealed; `iss` checked when advertised; token endpoints over https (loopback http for tests); no redirects on credentialed requests; refresh-token rotation honoured; `invalid_grant` marks the credential needs-reconnect.
- Approvals: the factory's approvers decide (`approval:decide`, fresh session; identity); the stored arguments are what runs; arguments are capped at 64 KiB and shown verbatim (escaped) to the approver.
- Forwarding between instances carries the original task token; the target verifies it; `Goal-Forwarded` stops loops; peer URLs come only from signed claims.
- Audit (A's `Audit`): `connector.created`, `connector.version_added`, `connector.deprecated`, `credential.oauth_connected`, `credential.oauth_reconnect_needed`, `credential.oauth_revoked`, `connection.tested`, `tool_approval.decided` (approval id, tool, decision, by, reason length only), `tool_call.approved_executed` (approval id, outcome). Never values or arguments.

### 12. Observability

- Logs (slog JSON): approval requested/decided/settled (ids, tool, wait seconds), OAuth discovery/registration/refresh outcomes (issuer, client id, never tokens), catalog sync (indexed, skipped with reason), exec refused (pattern miss reason, not the full command when it contains a refused metacharacter), forwarding failures.
- Metrics (OTel names, emitted when goald's metrics land; logs carry the same facts until then): `goal_gateway_tool_calls_total{connector,outcome}`, `goal_gateway_tool_call_seconds{connector}`, `goal_tool_approvals_pending`, `goal_tool_approval_wait_seconds{decision}`, `goal_oauth_refresh_total{result}`, `goal_sidecars_running{kind}`, `goal_gateway_forwarded_total{result}`.
- Events: item events above; SSE `approval` and `connection`.

### 13. Testing

- **Unit, `internal/connectors`**: every curated file in `internal/connectors/testdata` (acme examples) parses; schema errors per field; effective policy table (blocked, read-only connection, defaults on/off/approval, entry overrides, legacy allow/deny, CLI pattern rules); each validation problem and its fix patch (applying the fix removes the problem); templates (closed grammar, unknown function refused, `basic`, `join`); CLI tokenizer (quotes, escapes, every refused metacharacter), pattern matching (glob words, trailing `*`, order), `denyArgs` both forms, `command_outside_allowlist` containment.
- **Gateway with port fakes** (in-process MCP servers from the SDK, `porttest` fakes for Vault, Minter, ToolApprovals, Sidecars): connector connection served with injected headers; expired credential → unavailable + `credential_expired` event per refused call; scope `arguments` refusal; GitHub App token minted once per run and re-minted near expiry; OAuth access refreshed once when two calls race; approval approve → upstream called once with the stored bytes; reject → tool error with reason; deadline → expired; `EndRun` → abandoned; progress notifications while held; forwarding to the owner, no double forward (421), owner down → 503; CLI `run` allowed, refused, timed out, truncated, scrubbed.
- **Conformance**: `approvaltest`, `catalogtest` (fake + postgres); RLS isolation tests in `postgres` integration.
- **Adapters**: `oauthclient` against an httptest authorization server (RFC 9728 → 8414 → OIDC fallbacks, DCR, PKCE verified by the fake, refresh rotation, `invalid_grant`, `iss` mismatch, redirects not followed, tokens absent from errors); `github` minter against httptest (JWT claims and signature, repositories and permissions sent); `awssts` against a stub endpoint; `mcpregistry` against recorded fixtures and an OCI HEAD stub; `docker` exec sidecar integration with a pinned busybox (allowed echo, timeout kill, output cap, no network without `network`).
- **Service**: OAuth start/callback with fakes (one-time state, expired, wrong org prefix, reconnect adds a vault version); refresh job (due selection, needs-reconnect on `invalid_grant`) and the advisory lock in integration (two instances, one refresh); catalog sync (new key indexed once, changed content refused, invalid file skipped).
- **API handlers**: unknown org → 404, 409 on second decision, problem shapes, callback redirects, connector create problems with lines.
- **Smoke** (`//go:build smoke`, `task test-smoke CONNECTOR=<name>`; credentials from `GOAL_SMOKE_<CONNECTOR>_<FIELD>` env vars, skipped when absent; run by hand before a connector version ships, results quoted in its PR): list tools, assert every catalog `entries` name exists upstream (drift) and report upstream tools the catalog lacks, call one read tool, assert no credential text in the result. Jira (read `getJiraIssue` on a sandbox project), Datadog (validate + one read), Azure MCP and az CLI (`az group list`), GitHub (App token minted for one repo, `get_file_contents`), Postgres (compose database, `list_schemas`), Playwright (snapshot of a local httptest page via `host.docker.internal`), AWS docs (search).
- **Fakes for CI**: `internal/connectors/connectortest`: fake remote MCP server (jira-like read and write tools, header assertions), fake authorization server, fake GitHub App endpoint, fake registry; `web/mock` gains connectors, connections, credential tests, OAuth start (returns a mock consent page that redirects back) and approvals with stream events.
- **Playwright**: catalog filter and search; Datadog keys credential with test and grants; OAuth connect round trip on the mock; wizard: add Jira, pick credential, give investigator `addComment` with approval, publish blocked by `write_tool_no_rule` then fixed with the quick fix; Connections page shows an expired credential banner; approve with key A; reject requires a reason; 375 px approval.

### 14. Rollout and migration

1. Ship tables, ports and the catalog sync with an empty catalog; nothing changes for existing factories (legacy inline connections keep working through the gateway and 4.1's credential references).
2. Operator pushes the curated v1 catalog (`goal catalog push`); goald indexes it.
3. The goal factory's inline connections (GitHub remote MCP with a token reference, sidecars) get the `inline_connection` warning with a quick fix to the connector form; the editor applies it as a draft; publishing resolves credentials (4.1 has moved the GitHub token into the vault as a `github` `token` credential). Switching the repository and the GitHub connection to a GitHub App credential is a separate draft.
4. Approvals, OAuth and CLI connections are opt-in by definition; nothing is gated until a published version asks for it.
5. Multi-instance forwarding is deferred; one goald runs until the AWS deployment (review).
6. Docs: `docs/engineering/architecture.md` gateway section and ports table updated with each PR that changes them; ADR 0008 "Connectors, connections and approval-gated tools" with 4.2-1.

New platform settings (`cmd/goald/settings.go`): `GOAL_PUBLIC_URL` (defined by phase 3 and shared with `BETTER_AUTH_URL`; here also the OAuth redirect and client metadata base), `GOAL_APPROVAL_TIMEOUT` (30m) (review), `GOAL_MCP_REGISTRY_URL`, `GOAL_OAUTH_HTTP_TIMEOUT` (15s). No factory value is an environment variable.

### 15. Work breakdown

Each PR: tests named in it pass, `task check` (and `task test-integration` when SQL or an adapter changes) green.

| # | PR | Acceptance criteria · verification |
| --- | --- | --- |
| 4.2-1 | `schema/connector.schema.json`, `internal/connectors` parse/validate, templates, testdata, `goal catalog validate`, ADR 0008 | Example connectors parse; each invalid fixture reports its field · unit tests |
| 4.2-2 | Migration 00052, `ConnectorCatalog` (connector + connection methods), postgres, `catalogtest`, RLS tests | Suites pass on fake and postgres; isolation tests · integration |
| 4.2-3 | Catalog sync job, `goal catalog push`, bucket key | Pushed file indexed once; changed content refused; bad file skipped · unit (in-memory bucket) + integration |
| 4.2-4 | Catalog API (list, detail, create private, versions, deprecate) + OpenAPI + `goal connectors` | Handler tests incl. 409 name taken · unit |
| 4.2-5 | Connector-form connections in `factory.schema.json`, effective policy, `Checker` problems + fixes, `Resolution` into B's publish (bindings row write), `Line()` agent connections | Each problem in 4.3 raised and fixed; publish writes `connections` rows in the same transaction · unit + integration |
| 4.2-6 | Gateway: connector connections with Vault material, scopes, policy, health, audit fields, value-free unavailability | Gateway tests listed in 13 for remote and image connections · unit |
| 4.2-7 | `Minter` port, GitHub App minting (github adapter), gateway per-run cache, workspaces/CodeHost minting | Token restricted to repos; minted once per run · unit (httptest) |
| 4.2-8 | `awssts` minter | AssumeRole called with external id and session name · unit (stub endpoint) |
| 4.2-9 | `Sidecars.StartExec`, docker exec, CLI `run` tool, grammar | Allowed/refused/timeout/truncation/scrub · unit + docker integration |
| 4.2-10 | `OAuth` port, `oauthclient` adapter, client metadata document route | Fake AS tests in 13 · unit |
| 4.2-11 | Migration 00053, OAuth service (start, callback, refresh job, revocation hook), endpoints | One-time state, wrong org, refresh lock, needs-reconnect · unit + integration |
| 4.2-12 | Credential tests endpoint, field validation hook for 4.1's create/rotate | Datadog `http` test against fake; missing field → 400 · unit |
| 4.2-13 | Migration 00054, `ToolApprovals` port, postgres, `approvaltest`, NOTIFY | Conformance on fake and postgres · integration |
| 4.2-14 | Gateway hold/release/expiry, approval feed, sweeper job, worker `MCP_TOOL_TIMEOUT` | Approval tests in 13; worker Vitest on the timeout env · unit |
| 4.2-15 | Approvals API, SSE `approval`/`connection`, CLI approvals, `follow` line | Handler tests (409), stream test · unit |
| 4.2-16 | Deferred to the AWS deployment (review): `Gateway` claim, `GOAL_PEER_URL`, forwarding | — |
| 4.2-17 | `MCPRegistry` port, `mcpregistry` adapter, import-preview | Remote and OCI servers map to specs; npm-only → 422 · unit |
| 4.2-18 | Connection tests endpoint (gateway Tester), `ToolCallStats` | Draft and published tests; stats over 7 days · unit + integration |
| 4.2-19 | Web: Catalog, connector detail, Add/Import | Playwright catalog flows, axe · e2e at 3 widths |
| 4.2-20 | Web: Credential-New steps 1–2, OAuth connect, Credentials connector columns/detail rows | Playwright credential flows · e2e |
| 4.2-21 | Web: Connections page | Expired banner, filter, tools and allowlist panels · e2e |
| 4.2-22 | Web: Connections panel for wizard and Definition, YAML quick fixes | Wizard flow with publish blocked then fixed · e2e |
| 4.2-23 | Web: Approval page (+ phone), item hooks for E | Approve with A, reject with reason · e2e |
| 4.2-24 | Web: Agent Connections card | Statuses per connection · e2e |
| 4.2-25 | Curated catalog v1 content + smoke tests (one PR per family: remote, images, CLI) | Smoke results in the PR; drift none · `task test-smoke` |
| 4.2-26 | Legacy-to-connector quick fix, architecture.md gateway section | Goal factory converts with no runtime change · unit + manual run |

### 16. Cross-spec contracts

**4.2 provides**
- Ports `ConnectorCatalog`, `OAuth`, `ToolApprovals` (registry names) and two new ones, `Minter` and `MCPRegistry` (the registry did not foresee per-run minting or the registry import; both are outbound systems, so each needs its own port). `Sidecars` gains `StartExec`/`ExecSidecar`, `SidecarSpec.Entrypoint`.
- Package `internal/connectors` (`Checker`, `Policy`, `Service`).
- Tables `connectors`, `connections`, `connection_tests`, `oauth_clients`, `oauth_states`, `tool_approvals`; NOTIFY channel `goal_approvals`; `connection` events go on A's `goal_org_events` (review).
- To **B**: `connectors.Checker` as `authoring.Check` (problems in B's shape, severities `error|warning|info`, fixes as RFC 6902 patches) and `authoring.Resolver` (`resolved` + `Bindings`); `factory.schema.json` connector form (review).
- To **4.1**: credential field validation hook (`connectors.ValidateFields(connector, method, fields)`), `Service.Revoked` hook, OAuth-created credentials through `Vault`, Credential-New steps 1–2 and the connector columns.
- To **C**: `GET …/connections` `summary.blocked` for the sidebar dot and Floor/Board/Item "Connections, 1 blocked"; routes `/o/:org/catalog`, `/o/:org/f/:factory/connections`.
- To **D**: the connections panel component (`shared/connections-editor`) taking the draft and a patch callback; "Connects to" chips from template connections; problems with fixes for the YAML view and Review checks.
- To **C/E**: a `tool_approval` `InboxSource` on C's `/inbox` and `waiting_on_you` counts on C's `/attention`; `GET /v1/orgs/{org}/approvals?state=pending` and SSE `event: approval`; item event kinds `tool_approval_requested|decided|settled` for the timeline ("Paused for this approval"); `ItemCard.pending_approval: {id, tool, requested_at} | null` (accepted by E) (review).

**4.2 needs**
- **A**: `domain.OrgID`, `domain.Actor`, the org transaction helper (`SET LOCAL goal.org_id`; 4.2's curated policies use 4.2's own `goal_org_or_null()`), the request actor and `Authorizer` seam (filled by phase 3), `Audit`, `TaskClaims.OrgID`, `Tenancy.ActiveOrgs` for cross-org jobs, and the `goal_org_events` channel. No `JobLocks` port: each job's store method takes its own `pg_try_advisory_xact_lock`, as 4.1's `Rewrap` does (review).
- **B**: `definition_versions` (FK target) with `resolved jsonb` (B §6.1, accepted); validation pipeline that runs the `Checker` and blocks publish on errors but not on warnings; publish transaction that writes the `connections` rows from `Publish.Bindings` (delete removed names, upsert present) and stores `resolved`; the definitions cache returns a run's version (`v` claim) so the gateway resolves bindings of the version the run started on (review).
- **4.1** (mapped onto 4.1 §5.2, review): `Vault.Credential(ctx, ref)` (metadata incl. `connector`, `method`, status); `Vault.Resolve(ctx, SecretScope{…, PurposeConnection}, SecretRef)` → `Secret.Fields` with `CredentialError` reasons (records the use, so no `MarkUsed`); `Vault.Rotate` with `By` = system actor for OAuth refresh (no `AddVersion`); `NewCredential` gains `Connector`, `Method` (field secrecy from the connector's sign-in fields); `Vault.MarkNeedsReconnect` and status `needs_reconnect` (added by 4.2, 4.1 §6.6); no `Seal`/`Open` (the postgres adapter seals OAuth state with `envelope.go`); `credentials.connector`/`method` text columns (4.2's migration); repository credentials usable by `Minter` (`github_app`).
- **D**: the Connections section shell in the wizard and editor with draft patching and the Visual/YAML toggle; Review and publish rendering 4.2's checks.
- **C/E**: C's stream union carrying `event: approval` and `event: connection` (A's feed reads `goal_approvals` and `goal_org_events`); C's `InboxSource` registry; E's item timeline component for the Approval page (review).

### 17. Open questions

1. Held calls keep the agent container and WIP slot for up to 30 minutes. Is that the right default, or should long approvals suspend the run (checkpoint the session and requeue the item on decision)? That needs a worker resume and an engine state; out of 4.2 as written.
2. Laptop OAuth: `GOAL_PUBLIC_URL` is `http://127.0.0.1:8080`, so client ID metadata documents are unavailable and DCR must accept a loopback http redirect. Do Atlassian's and Datadog's servers accept that? If not, laptops need a static client per org (an `oauth` method field set) or a tunnel; the smoke tests will tell.
3. The canvas shows Atlassian at `/v1/sse` (legacy SSE transport). 4.2 supports `sse` through the SDK, but the curated entry targets `/v1/mcp`; confirm by smoke test.
4. Should curated connector updates ever reach a factory without a publish (security fixes to an image digest)? As written, never; an operator would deprecate and factories would see a warning.
5. `connections` holds only the current version's bindings. Runs still on an older version resolve from B's stored `Resolution`; is B willing to own `definition_versions.resolved`, or should 4.2 keep a `connection_bindings(definition_version_id, …)` table instead?
6. Exec sidecars use `timeout` and `sleep` from the vendor image. For distroless CLI images we would need a Goal-built image; acceptable to limit v1 to images that ship coreutils (aws-cli, azure-cli do)?
7. **Decided (phase 3):** the platform operator who runs `goal catalog push` is a platform admin. Whether the push goes through an API with `platform:manage` instead of bucket credentials stays for the design pass.
