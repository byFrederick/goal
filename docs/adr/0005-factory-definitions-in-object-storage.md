# ADR 0005 — Factory definitions in object storage

Status: accepted · 2026-10-23 · Amends [ADR 0001](0001-technology-stack.md) and [ADR 0002](0002-jonah-evals.md)

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
