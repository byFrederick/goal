-- The knowledge base (00054), under row-level security: every statement sees the transaction's
-- organization only.

-- name: KnowledgeSources :many
-- The organization's sources, or the one named or with the given id.
SELECT s.id, s.name, s.title, s.kind, c.name AS connection, sc.name AS secret, s.config, s.read_when, s.current_snapshot_id,
       extract(epoch FROM s.sync_every)::bigint AS sync_every_seconds, s.next_sync_at, s.lease_until, s.status,
       s.last_synced_at, s.last_error, s.sync_failures, s.created_by, s.created_at, s.updated_at
FROM knowledge_sources s
LEFT JOIN connections c ON c.org_id = s.org_id AND c.id = s.connection_id
LEFT JOIN secrets sc ON sc.org_id = s.org_id AND sc.id = s.secret_id
WHERE (sqlc.narg(name)::text IS NULL OR s.name = sqlc.narg(name)::text)
  AND (sqlc.narg(id)::uuid IS NULL OR s.id = sqlc.narg(id)::uuid)
ORDER BY s.name;

-- name: KnowledgeConnection :one
-- A connection a source may read: its id and its kind's sync adapter ('' when it cannot sync).
SELECT c.id, coalesce(k.spec->'spec'->>'sync', '')::text AS sync
FROM connections c JOIN connection_kinds k ON k.id = c.kind_id
WHERE c.name = sqlc.arg(name);

-- name: KnowledgeSecret :one
-- The id of a secret a source may read with.
SELECT id FROM secrets WHERE name = sqlc.arg(name);

-- name: SecretKnowledgeSources :many
-- The sources that read with secret name, by name: a secret's deletion is refused while one does.
SELECT s.name FROM knowledge_sources s JOIN secrets sc ON sc.org_id = s.org_id AND sc.id = s.secret_id
WHERE sc.name = sqlc.arg(name)
ORDER BY s.name;

-- name: InsertKnowledgeSource :exec
-- org_id comes from its default, goal_org(). A live source never syncs.
INSERT INTO knowledge_sources (id, name, title, kind, connection_id, secret_id, config, read_when, sync_every, next_sync_at, created_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(title), sqlc.arg(kind), sqlc.narg(connection_id), sqlc.narg(secret_id), sqlc.arg(config),
        sqlc.arg(read_when),
        make_interval(secs => sqlc.arg(sync_every_seconds)::bigint),
        CASE WHEN sqlc.arg(kind)::text = 'live' THEN NULL ELSE now() END, sqlc.arg(created_by));

-- name: UpdateKnowledgeSource :one
-- A new read-when line makes a source that writes snapshots due at once, so its next snapshot
-- carries the line; read_when on the right is the row's old line.
UPDATE knowledge_sources
SET next_sync_at = CASE WHEN sqlc.narg(read_when)::text IS NOT NULL AND read_when IS DISTINCT FROM sqlc.narg(read_when)::text
                             AND kind IN ('upload', 'repository', 'confluence') THEN now()
                        ELSE next_sync_at END,
    title = coalesce(sqlc.narg(title)::text, title), config = coalesce(sqlc.narg(config)::jsonb, config),
    secret_id = coalesce(sqlc.narg(secret_id)::uuid, secret_id), read_when = coalesce(sqlc.narg(read_when)::text, read_when),
    sync_every = CASE WHEN sqlc.narg(sync_every_seconds)::bigint IS NULL THEN sync_every
                      ELSE make_interval(secs => sqlc.narg(sync_every_seconds)::bigint) END,
    updated_at = now()
WHERE name = sqlc.arg(name)
RETURNING id;

-- name: ConvertKnowledgeSource :exec
-- Moves a synced source over to its kind (ports.Knowledge.ConvertSource): off its connection,
-- with the config it keeps, due at once.
UPDATE knowledge_sources
SET kind = sqlc.arg(kind), connection_id = NULL, config = sqlc.arg(config)::jsonb, next_sync_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND kind = 'synced' AND secret_id IS NOT NULL;

-- name: LockKnowledgeSource :one
SELECT id, kind FROM knowledge_sources WHERE name = sqlc.arg(name) FOR UPDATE;

-- name: SourceBlobs :many
-- The uploads' content hashes of a source's documents, in order (LockKnowledgeBlobs).
SELECT DISTINCT blob_sha256::text AS blob FROM knowledge_uploads
WHERE source_id = sqlc.arg(source_id)
ORDER BY 1;

-- name: LockKnowledgeBlobs :exec
-- Holds the organization's upload contents of the given hashes until the transaction ends, in
-- order: an upload of one waits for a deletion that drops its object, and the other way round.
SELECT pg_advisory_xact_lock(hashtextextended('goal-knowledge-blob/' || goal_org()::text || '/' || b.blob, 0))
FROM (SELECT DISTINCT x AS blob FROM unnest(sqlc.arg(blobs)::text[]) x ORDER BY 1) b;

-- name: DeleteKnowledgeSource :exec
DELETE FROM knowledge_sources WHERE id = sqlc.arg(id);

-- name: UnheldBlobs :many
-- Of the given content hashes, those no upload of the organization holds.
SELECT b::text AS blob FROM unnest(sqlc.arg(blobs)::text[]) b
WHERE NOT EXISTS (SELECT 1 FROM knowledge_uploads d WHERE d.org_id = goal_org() AND d.blob_sha256 = b)
ORDER BY b;

-- name: SyncKnowledgeSourceNow :one
UPDATE knowledge_sources SET next_sync_at = CASE WHEN kind = 'live' THEN NULL ELSE now() END
WHERE name = sqlc.arg(name)
RETURNING kind;

-- name: ClaimKnowledgeSource :one
-- The source due first in any live organization, leased under a new token (00055), in no
-- organization: the caller binds the transaction to the one returned.
SELECT c.org_id::uuid AS org_id, c.id::uuid AS id, c.lease_token::uuid AS lease_token
FROM goal_claim_knowledge_source(sqlc.arg(now)::timestamptz, sqlc.arg(until)::timestamptz) c;

-- name: RenewKnowledgeSource :execrows
UPDATE knowledge_sources SET lease_until = sqlc.arg(until)::timestamptz
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token)::uuid AND status = 'syncing'
  AND lease_until > sqlc.arg(now)::timestamptz;

-- name: HoldKnowledgeLease :one
-- The lease's source, locked against a claim until the transaction ends, while the token is its
-- current one: a seen mark or a deletion under it applies, or none does.
SELECT true FROM knowledge_sources WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token)::uuid FOR SHARE;

-- name: FinishKnowledgeSync :execrows
-- The next sync is at plus the interval unless the source was made due since its claim (due).
-- Nothing when another claim took the source (its token).
UPDATE knowledge_sources
SET status = CASE WHEN sqlc.arg(error)::text = '' THEN 'idle' ELSE 'failed' END,
    last_error = nullif(sqlc.arg(error)::text, ''),
    last_synced_at = CASE WHEN sqlc.arg(error)::text = '' THEN sqlc.arg(at)::timestamptz ELSE last_synced_at END,
    sync_failures = CASE WHEN sqlc.arg(error)::text = '' THEN 0 ELSE sync_failures + 1 END,
    lease_until = NULL, lease_token = NULL,
    next_sync_at = CASE WHEN next_sync_at IS DISTINCT FROM sqlc.arg(due)::timestamptz THEN next_sync_at
                        ELSE sqlc.arg(at)::timestamptz + sync_every END,
    updated_at = now()
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token)::uuid AND kind <> 'live';

-- name: NotifyKnowledgeSync :exec
-- The organization's stream event knowledge on goal_org_events, sent when the claim's or the
-- finish's transaction commits; postgres.Notices reads it back from the source's row.
SELECT pg_notify('goal_org_events', json_build_object(
  'org_id', goal_org(), 'event', 'knowledge',
  'data', json_build_object('source_id', id, 'name', name, 'status', status))::text)
FROM knowledge_sources WHERE id = sqlc.arg(id);

-- name: InsertKnowledgeSnapshot :one
-- org_id comes from its default, goal_org(); zip_key is generated from the ids.
INSERT INTO knowledge_snapshots (id, source_id, files, failed, bytes, zip_bytes, changes, read_when, created_at)
VALUES (sqlc.arg(id), sqlc.arg(source_id), sqlc.arg(files), sqlc.arg(failed), sqlc.arg(bytes), sqlc.arg(zip_bytes),
        sqlc.arg(changes), sqlc.arg(read_when), sqlc.arg(created_at))
RETURNING zip_key;

-- name: DeleteKnowledgeFiles :exec
DELETE FROM knowledge_files WHERE source_id = sqlc.arg(source_id);

-- name: InsertKnowledgeFiles :exec
-- A snapshot's files, one row per element of the arrays.
INSERT INTO knowledge_files (source_id, path, title, bytes, version)
SELECT sqlc.arg(source_id), (sqlc.arg(paths)::text[])[i], (sqlc.arg(titles)::text[])[i], (sqlc.arg(sizes)::bigint[])[i],
       (sqlc.arg(versions)::text[])[i]
FROM generate_series(1, cardinality(sqlc.arg(paths)::text[])) AS i;

-- name: SetKnowledgeSnapshot :exec
UPDATE knowledge_sources SET current_snapshot_id = sqlc.arg(snapshot_id) WHERE id = sqlc.arg(id);

-- name: KnowledgeSnapshots :many
-- A source's snapshots, newest first.
SELECT id, source_id, files, failed, bytes, zip_bytes, changes, read_when, zip_key, created_at
FROM knowledge_snapshots
WHERE source_id = sqlc.arg(source_id)
ORDER BY created_at DESC, id DESC;

-- name: ExpiredKnowledgeSnapshots :many
-- The source's snapshots retention deletes, oldest first: not its current one, pinned by no
-- running agent run (its knowledge_pinned event names the snapshot), and replaced, its next
-- snapshot made, at or before before.
SELECT s.id, s.source_id, s.files, s.failed, s.bytes, s.zip_bytes, s.changes, s.read_when, s.zip_key, s.created_at
FROM (
  SELECT ks.*, lead(ks.created_at) OVER (ORDER BY ks.created_at, ks.id) AS replaced_at
  FROM knowledge_snapshots ks
  WHERE ks.source_id = sqlc.arg(source_id)
) s
JOIN knowledge_sources src ON src.id = s.source_id
WHERE s.id IS DISTINCT FROM src.current_snapshot_id
  AND s.replaced_at <= sqlc.arg(before)::timestamptz
  AND NOT EXISTS (
    SELECT 1 FROM agent_runs r JOIN item_events e ON e.run_id = r.id
    WHERE r.status = 'running' AND e.kind = 'knowledge_pinned'
      AND e.data -> 'snapshots' @> jsonb_build_array(jsonb_build_object('snapshot', s.id::text)))
ORDER BY s.created_at, s.id;

-- name: DeleteKnowledgeSnapshots :exec
-- The source's snapshots ids, but never its current one.
DELETE FROM knowledge_snapshots s
USING knowledge_sources src
WHERE s.source_id = sqlc.arg(source_id) AND s.id = ANY (sqlc.arg(ids)::uuid[])
  AND src.id = s.source_id AND s.id IS DISTINCT FROM src.current_snapshot_id;

-- name: KnowledgeFiles :many
-- One page of a source's current files in path order (byte order: path is COLLATE "C"), after a
-- path, and with a prefix those whose path, or title in any case, starts with it.
SELECT source_id, path, title, bytes, version
FROM knowledge_files
WHERE source_id = sqlc.arg(source_id) AND path > sqlc.arg(after)::text
  AND (sqlc.arg(prefix)::text = '' OR starts_with(path, sqlc.arg(prefix)::text)
       OR starts_with(lower(title), lower(sqlc.arg(prefix)::text)))
ORDER BY path
LIMIT sqlc.arg(max_rows);

-- name: AddKnowledgeReads :exec
-- Adds each element of the arrays to its source, path, factory and day's counts (days as
-- YYYY-MM-DD). The caller merges counts of one key first: a statement updates a row once.
INSERT INTO knowledge_reads (source_id, path, factory_id, day, reads, searches)
SELECT (sqlc.arg(source_ids)::uuid[])[i], (sqlc.arg(paths)::text[])[i], (sqlc.arg(factory_ids)::uuid[])[i],
       ((sqlc.arg(days)::text[])[i])::date, (sqlc.arg(reads)::int[])[i], (sqlc.arg(searches)::int[])[i]
FROM generate_series(1, cardinality(sqlc.arg(source_ids)::uuid[])) AS i
ON CONFLICT (org_id, source_id, path, factory_id, day)
DO UPDATE SET reads = knowledge_reads.reads + excluded.reads, searches = knowledge_reads.searches + excluded.searches;

-- name: KnowledgeFileReads :many
-- A source's counts of the days from since on (YYYY-MM-DD), by day, path and factory.
SELECT source_id, path, factory_id, day::text AS day, reads, searches
FROM knowledge_reads
WHERE source_id = sqlc.arg(source_id) AND day >= (sqlc.arg(since)::text)::date
ORDER BY day, path, factory_id;

-- name: KnowledgeSourceKind :one
SELECT kind FROM knowledge_sources WHERE id = sqlc.arg(id);

-- name: KnowledgeUploads :many
-- An upload source's files, by name.
SELECT u.id, u.source_id, u.name, u.mime, u.bytes, u.blob_sha256, u.created_by, u.created_at
FROM knowledge_uploads u
WHERE u.source_id = sqlc.arg(source_id)
ORDER BY u.name;

-- name: KnowledgeUploadNamed :one
-- The file named name of a source, locked for its replacement or deletion.
SELECT u.id, u.blob_sha256 FROM knowledge_uploads u WHERE u.source_id = sqlc.arg(source_id) AND u.name = sqlc.arg(name) FOR UPDATE;

-- name: UpsertKnowledgeUpload :many
-- A file of an upload source, replacing the one of its name; nothing when it holds that content
-- already. org_id comes from its default, goal_org().
INSERT INTO knowledge_uploads AS u (id, source_id, name, mime, bytes, blob_sha256, created_by)
VALUES (sqlc.arg(id), sqlc.arg(source_id), sqlc.arg(name), sqlc.arg(mime), sqlc.arg(bytes), sqlc.arg(blob_sha256)::text, sqlc.arg(created_by))
ON CONFLICT (source_id, name) DO UPDATE
SET mime = EXCLUDED.mime, bytes = EXCLUDED.bytes, blob_sha256 = EXCLUDED.blob_sha256
WHERE u.blob_sha256 IS DISTINCT FROM EXCLUDED.blob_sha256
RETURNING id;

-- name: DeleteKnowledgeUpload :exec
DELETE FROM knowledge_uploads WHERE id = sqlc.arg(id);
