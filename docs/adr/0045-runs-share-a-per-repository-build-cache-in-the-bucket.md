# ADR 0045 — Runs share a per-repository build cache in the bucket

Status: accepted · 2026-11-08 · Builds on [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (run boxes, the bucket and PostgreSQL as the only state) and [ADR 0005](0005-factory-definitions-in-object-storage.md) (the bucket's content-addressed blobs under each organization's prefix)

## Context

Every run box starts from a fresh clone. It downloads Go modules and the pnpm store and builds from nothing before the agent can test anything. A week of production run data (software#828) put cold installs and builds at roughly 28% to 55% of run CPU.

Go's module and build caches and the pnpm store depend on a repository's lockfiles and toolchain versions, not on the commit. Runs of one repository could restore them instead of rebuilding them, but a box keeps nothing after it ends, so the cache has to live outside it.

The bucket holds only immutable content-addressed blobs ([architecture rule](../../.claude/rules/architecture.md), "State"): a blob's key is the hash of its bytes, so it can never change and never needs versioning, which R2 does not offer. A build cache cannot be keyed that way: a box must look the cache up before it has built it, so the key can only be known from the inputs. Two builds of the same inputs also produce different bytes (timestamps, file order).

goald keeps every piece of state another instance needs in PostgreSQL ([architecture rule](../../.claude/rules/architecture.md), "State"); the bucket is never listed to find what exists.

## Decision

1. **A cache object is keyed by a hash of its inputs, not of its content.** The cache key is a SHA-256 (64 lowercase hex characters) of the repository's lockfiles and toolchain versions. Two runs with the same inputs look up the same key.
2. **Each object is written once, by the box that reserved its key, and is never rewritten.** A box reserves the key in the index before it uploads and commits it after the upload. A reservation older than one hour (`domain.BuildCacheStaleAfter`) is taken to be dead and may be reserved again by another box, with a new object key; a committed (ready) key is never reserved again. Commit names the object key it reserved with, so a box whose reservation was taken over commits nothing.
3. **Objects are deleted only by pruning.** Pruning keeps the newest three ready caches of a repository (`domain.BuildCacheKeep`), deletes the other index rows and the stale reservations, and returns their object keys for the caller to delete from the bucket. Nothing else deletes or replaces a cache object, except the organization purge, which removes everything under the organization's prefix (`domain.OrgPrefix`, `internal/retention`). Cache objects must therefore live under that prefix.
4. **These objects are the one exception to "store only immutable content-addressed blobs".** They are immutable (decision 2) but keyed by their inputs. Within this ADR's rules no object is overwritten, so the reason for the architecture rule, that R2 has no object versioning, still holds.
5. **The index lives in PostgreSQL.** Table `build_caches` (migration 00143) has one row per organization, repository key and cache key, in state `saving` or `ready`, with its object key, size and times, under row-level security like every organization's table. `ports.BuildCaches` (`internal/core/ports/buildcaches.go`) reads and writes it; a reserve is one `INSERT ... ON CONFLICT DO UPDATE`, so concurrent reserves of a key have one winner.
6. **The cache is shared within one organization, per repository.** The repository key is a hash of the repository's clone URL, not a factory id, so a cache one run saves reaches later runs of any of the organization's factories on that repository. Caches never cross organizations. A factory's definition can turn the build cache off for its runs.

## Consequences

- Runs whose inputs match a saved cache skip the cold download and build. The first run after a lockfile or toolchain change still pays for it, and saves the new cache.
- A cache holds build output from one factory's run that another factory of the organization restores. That is the cost of decision 6: a factory that does not want to share, or to trust what others saved, turns the cache off.
- Bucket use grows by at most three ready caches per repository, plus reservations in flight. Pruning is the caller's job: whoever commits a cache prunes the repository afterwards and deletes the returned objects.
- The gateway (presigned uploads and downloads, the object key layout under `domain.OrgPrefix`), the run box's restore and save, the definition's switch and the runs page's hit or miss are built on this index by later changes; this ADR binds each of them.
- The architecture rule on the bucket names this ADR as its exception.
