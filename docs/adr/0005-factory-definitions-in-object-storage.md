# ADR 0005 — Factory definitions in object storage

Status: accepted · 2026-10-23 · Amends [ADR 0001](0001-technology-stack.md) and [ADR 0002](0002-jonah-evals.md) · Amended in 2.2: decisions 1, 2, 4, 5 and 7 are replaced by the [amendment](#amendment--22-versions-in-postgresql-content-as-blobs) at the end

## Context

A factory's behavior is its definition: `factory.yaml`, agent prompts, skills, contracts and eval suites. Until now those files sat in this repository under `factories/<name>/`. That made every prompt tweak a code release, tied Jonah's remediation to pull requests on Goal's own code, mixed domain content with the platform's source, and meant `goald` on AWS would need a checkout to know what to run. Definitions change on a different cadence, by different people (and by Jonah), and need rollback in seconds, not a revert and a deploy.

## Decision

1. **Definitions are content, not code.** They live in one S3-compatible bucket, one prefix per factory keyed by its immutable `metadata.id`:

   ```
   s3://<bucket>/factories/<factory-id>/
     factory.yaml
     agents/  skills/  contracts/
     evals/        evals.yaml, cases/, judges/, fixtures/
     knowledge/<collection>/   the knowledge base
   ```

   This repository keeps only the JSON Schemas (`schema/`) and small, neutral test fixtures (`internal/config/testdata`, sample org `acme`). No factory definition is committed here.
2. **Versioning is on, and it is the rollback mechanism.** Every write creates a new object version. A push reports the version of every file it wrote; a reader loads a factory at the versions it listed, so it never mixes two pushes and can record exactly what it ran. Rolling back means restoring (pinning) the previous object versions. Old versions expire only after the lifecycle window.
3. **One port, two backends.** `ports.ObjectStore` (`Get`, `Put`, `List`, `Delete`, with object version ids) is implemented by `internal/adapters/s3store` on the AWS SDK for Go v2: Amazon S3 on AWS through the standard credential chain, and locally **RustFS** (`rustfs/rustfs`, Apache-2.0, pinned to an exact tag in `compose.yaml`) through a custom endpoint with path-style addressing. MinIO no longer publishes images, so it is not used.
4. **`internal/config` loads from an `fs.FS`.** The same loader and cross-reference validation read a local working copy (`LoadDir`) or a bucket prefix (`internal/factorystore` snapshot). A path in `factory.yaml` resolves inside the factory's tree only.
5. **Editing is pull → edit → validate → push.** `goal factory pull <id|name> <dir>` writes a factory into an empty folder; `goal factory validate <dir>` checks it; `goal factory push <dir>` validates again, uploads changed files (`factory.yaml` last), deletes removed ones and never touches `knowledge/`. `goal storage init` creates the bucket with versioning; `goal factory list` validates everything in the bucket. The working copy lives outside this repository and is not under git.
6. **`knowledge/` is reserved, not used, in phase 1.** Nothing in phase 1 reads, writes or ingests it; knowledge-base provisioning (Terraform, Bedrock) is phase 3.
7. **Jonah ships definition changes, not pull requests.** An approved remediation becomes new object versions with the previous versions recorded as its rollback pin ([ADR 0002](0002-jonah-evals.md), decision 7). The `goal` factory still opens pull requests for Goal's own code.

## Rejected

- **Definitions in git (this repository or a separate one).** Pull requests give review and history, but every prompt change becomes a release, `goald` on AWS would need a checkout or a sync job, rollback is a revert plus a deploy, and Jonah would need git credentials. Object versions give history and instant rollback; validation on push gives the gate.
- **Definitions in PostgreSQL.** One datastore is attractive, but definitions are file trees that humans edit with schema autocompletion and that Bedrock and agents read as files; rows would need an export/import layer for both.
- **MinIO locally.** No longer publishes images.
- **A bucket per factory.** More IAM and more buckets to version and encrypt, for isolation that prefix-scoped IAM already gives.

## Consequences

- `task check` validates the test fixtures; real definitions are validated on every push and by `goal factory list`.
- Local development needs RustFS (`task storage:up`, `task storage:init`); `task test-integration` runs the adapter against it.
- Factory content can no longer be reviewed in this repository's pull requests. Review happens in the working copy before a push, and every push is attributable and reversible by object version.

## Amendment — 2.2: versions in PostgreSQL, content as blobs

Status: accepted · 2026-10-23 · amends decisions 1, 2, 4, 5 and 7. Specified in [2.2-B](../specs/2.2-b-definitions.md); built in its pull requests B2–B10.

**Context.** A version was "whatever object versions a listing returned". A bucket has no transaction across objects, so a refresh that lists a prefix halfway through a push can load new prompts with the old `factory.yaml`; pushing `factory.yaml` last narrows that window but does not close it. The builder adds shared drafts, publishing by people in the UI, rollback, per-run version pinning and questions such as "which factories use this credential", all of which need atomic writes, foreign keys and queries. Organizations (2.2-A) also make the tenant part of every key.

**Decision.**

1. The bucket stays the home of definition **content**: every file is stored once per organization under its SHA-256, `orgs/<org-id>/blobs/<sha256>` (lowercase hex, no extension), written with a conditional put (`PutIfAbsent`) and never overwritten. Readers verify the hash of every blob they fetch.
2. PostgreSQL is the **index of versions**: `definition_versions` holds each draft and published version of a factory as a manifest (slash path relative to the factory root → sha256) with its number, author and publisher; `factories.current_version_id` names the version that runs. A factory has at most one open draft. Publishing (and restoring) is one transaction that also writes the audit row; a trigger NOTIFYs `goal_definitions` and every goald reloads. Published versions are immutable; rollback (restore) moves the pointer, and numbers are never reused.
3. Agent runs record the version they started on (`agent_runs.definition_version_id`) and finish on it; the next step of an item uses the current version. Items are not pinned to versions.
4. `internal/config` still loads a definition from an `fs.FS`; the tree now comes from a manifest and its blobs.
5. Editing happens through goald's API, and only there: the builder (Visual and YAML) and the CLI both write the factory's one draft, and definition files written to the bucket directly change nothing. `goal factory pull` writes a version's files into an empty folder with `.goal/pulled.json` (which version it was); `goal factory push` makes the folder the draft's whole tree at the revision it read, refusing an open draft with changes the folder was not pulled from and a version published after the pull, and `--publish` (or `goal factory publish`) publishes it expecting the version it saw running; `validate`, `list`, `versions` and `restore` call the API too. Only the operator commands (`goal storage init`, `goal catalog skills|templates`) use the bucket. Validation runs on save and before publish and includes secret references (credential grants once phase 4 lands).
6. Jonah's remediations become drafts published like any other; their rollback pin is the previous version id.
7. Templates are definitions saved as data under `catalog/templates/<name>/<version>/`. The other two curated catalogs are definition content too: vendorable skills under `catalog/skills/` (phase D) and connector specs under `catalog/connectors/` (phase 4). Nothing else goes in the bucket.

The layout after this amendment:

```
s3://<bucket>/
  orgs/<org-id>/blobs/<sha256>             one definition file, written once, never overwritten
  catalog/templates/<name>/<version>/      template.yaml + factory/ (a definition tree)
  catalog/skills/                          vendorable skills (phase D)
  catalog/connectors/                      connector specs (phase 4)
```

**Rejected.**

- Keeping versions implicit in object versions: no atomicity across files, no foreign keys, no queries.
- Moving file content into PostgreSQL: definitions are file trees that agents and Bedrock read as files; the bucket keeps them cheap and readable.
- Per-factory prefixes of mutable files: the blob store deduplicates across versions and clones and makes every version reproducible.

**Consequences.**

- A version is reproducible from its manifest alone. Paths that ADRs 0001 and 0002 write as `factories/<id>/…` (evals, cases) now name files of a version's tree, not bucket keys.
- The legacy prefix (`factories/<id>/`, which 2.2-A moves to the interim `orgs/<org-id>/factories/<id>/`) is imported once as v1 of each factory and then ignored; its objects stay in place for rollback until removed by hand.
- Unreferenced blobs (abandoned uploads, discarded drafts) accumulate until a garbage collector is added.
- The bucket's object versioning stays on as protection, no longer as the version mechanism.
- Where knowledge documents live in the bucket is decided with the knowledge base (later, unscheduled); decision 6 is unchanged.

