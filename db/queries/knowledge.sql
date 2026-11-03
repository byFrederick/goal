-- The knowledge base (00054), under row-level security: every statement sees the transaction's
-- organization only.

-- name: KnowledgeSources :many
-- The organization's sources, or the one named or with the given id.
SELECT s.id, s.name, s.title, s.kind, c.name AS connection, sc.name AS secret, s.config, s.read_when, s.current_snapshot_id,
       extract(epoch FROM s.sync_every)::bigint AS sync_every_seconds, s.next_sync_at, s.lease_until, s.status,
       s.last_synced_at, s.last_error, s.sync_failures, s.created_by, s.created_at, s.updated_at,
       (SELECT count(*) FROM knowledge_documents d WHERE d.org_id = s.org_id AND d.source_id = s.id) AS documents
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
UPDATE knowledge_sources
SET title = coalesce(sqlc.narg(title)::text, title), config = coalesce(sqlc.narg(config)::jsonb, config),
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
SELECT DISTINCT blob_sha256::text AS blob FROM knowledge_documents
WHERE source_id = sqlc.arg(source_id) AND blob_sha256 IS NOT NULL
ORDER BY 1;

-- name: LockKnowledgeBlobs :exec
-- Holds the organization's upload contents of the given hashes until the transaction ends, in
-- order: an upload of one waits for a deletion that drops its object, and the other way round.
SELECT pg_advisory_xact_lock(hashtextextended('goal-knowledge-blob/' || goal_org()::text || '/' || b.blob, 0))
FROM (SELECT DISTINCT x AS blob FROM unnest(sqlc.arg(blobs)::text[]) x ORDER BY 1) b;

-- name: DeleteKnowledgeSource :exec
DELETE FROM knowledge_sources WHERE id = sqlc.arg(id);

-- name: UnheldBlobs :many
-- Of the given content hashes, those no document of the organization holds.
SELECT b::text AS blob FROM unnest(sqlc.arg(blobs)::text[]) b
WHERE NOT EXISTS (SELECT 1 FROM knowledge_documents d WHERE d.org_id = goal_org() AND d.blob_sha256 = b)
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

-- name: KnowledgeVersions :many
SELECT external_id, version FROM knowledge_documents
WHERE source_id = sqlc.arg(source_id) AND status <> 'pending';

-- name: UpsertKnowledgeDocument :one
-- org_id comes from its default, goal_org(); an upload's hash stays when a sync rewrites it, and
-- its seen time never goes back (a sync that stalled past its lease writes an older one).
INSERT INTO knowledge_documents AS d (id, source_id, external_id, version, title, url, mime, bytes, blob_sha256, status, error,
                                      seen_at, indexed_at, created_by)
VALUES (sqlc.arg(id), sqlc.arg(source_id), sqlc.arg(external_id), sqlc.arg(version), sqlc.arg(title), sqlc.narg(url),
        sqlc.arg(mime), sqlc.arg(bytes), sqlc.narg(blob_sha256), sqlc.arg(status), sqlc.narg(error), sqlc.arg(seen_at),
        sqlc.narg(indexed_at), sqlc.arg(created_by))
ON CONFLICT (source_id, external_id) DO UPDATE
SET version = EXCLUDED.version, title = EXCLUDED.title, url = EXCLUDED.url, mime = EXCLUDED.mime, bytes = EXCLUDED.bytes,
    blob_sha256 = coalesce(EXCLUDED.blob_sha256, d.blob_sha256), status = EXCLUDED.status, error = EXCLUDED.error,
    seen_at = greatest(d.seen_at, EXCLUDED.seen_at), indexed_at = EXCLUDED.indexed_at
RETURNING id;

-- name: KnowledgeSourceKind :one
SELECT kind FROM knowledge_sources WHERE id = sqlc.arg(id);

-- name: UpdateKnowledgeUpload :one
-- An upload's document as its sync read it; none when it was deleted meanwhile, which a waiting
-- update sees once the deletion commits.
UPDATE knowledge_documents
SET version = sqlc.arg(version), title = sqlc.arg(title), mime = sqlc.arg(mime), bytes = sqlc.arg(bytes),
    status = sqlc.arg(status), error = sqlc.narg(error), seen_at = greatest(seen_at, sqlc.arg(seen_at)),
    indexed_at = sqlc.narg(indexed_at)
WHERE source_id = sqlc.arg(source_id) AND external_id = sqlc.arg(external_id)
RETURNING id;

-- name: DeleteDocumentChunks :exec
DELETE FROM knowledge_chunks WHERE document_id = sqlc.arg(document_id);

-- name: InsertKnowledgeChunks :exec
-- A document's chunks in one statement.
INSERT INTO knowledge_chunks (source_id, document_id, ord, heading, text)
SELECT sqlc.arg(source_id), sqlc.arg(document_id), (sqlc.arg(ords)::int[])[i], (sqlc.arg(headings)::text[])[i],
       (sqlc.arg(texts)::text[])[i]
FROM generate_series(1, cardinality(sqlc.arg(ords)::int[])) AS i;

-- name: MarkKnowledgeSeen :exec
UPDATE knowledge_documents SET seen_at = greatest(seen_at, sqlc.arg(at))
WHERE source_id = sqlc.arg(source_id) AND external_id = ANY (sqlc.arg(external_ids)::text[]);

-- name: DeleteUnseenDocuments :execrows
DELETE FROM knowledge_documents WHERE source_id = sqlc.arg(source_id) AND seen_at < sqlc.arg(before);

-- name: InsertKnowledgeUpload :many
-- An upload, pending; nothing when the source has a document of that content already.
INSERT INTO knowledge_documents (id, source_id, external_id, version, title, mime, bytes, blob_sha256, status, seen_at, created_by)
VALUES (sqlc.arg(id), sqlc.arg(source_id), sqlc.arg(blob_sha256)::text, sqlc.arg(blob_sha256)::text, sqlc.arg(title), sqlc.arg(mime),
        sqlc.arg(bytes), sqlc.arg(blob_sha256)::text, 'pending', now(), sqlc.arg(created_by))
ON CONFLICT (source_id, external_id) DO NOTHING
RETURNING id;

-- name: KnowledgeDocuments :many
-- A page of a source's documents in id order, or the document of an external id, or with an id.
SELECT d.id, d.source_id, d.external_id, d.version, d.title, d.url, d.mime, d.bytes, d.blob_sha256, d.status, d.error,
       d.seen_at, d.indexed_at, d.created_by, s.kind AS source_kind
FROM knowledge_documents d JOIN knowledge_sources s ON s.org_id = d.org_id AND s.id = d.source_id
WHERE (sqlc.narg(source_id)::uuid IS NULL OR d.source_id = sqlc.narg(source_id)::uuid)
  AND (sqlc.narg(external_id)::text IS NULL OR d.external_id = sqlc.narg(external_id)::text)
  AND (sqlc.narg(id)::uuid IS NULL OR d.id = sqlc.narg(id)::uuid)
  AND (sqlc.narg(after)::uuid IS NULL OR d.id > sqlc.narg(after)::uuid)
ORDER BY d.id
LIMIT sqlc.arg(max_rows);

-- name: LockKnowledgeDocument :one
SELECT d.id, d.blob_sha256, d.title, s.kind AS source_kind, s.name AS source_name
FROM knowledge_documents d JOIN knowledge_sources s ON s.org_id = d.org_id AND s.id = d.source_id
WHERE d.id = sqlc.arg(id)
FOR UPDATE OF d;

-- name: DeleteKnowledgeDocument :exec
DELETE FROM knowledge_documents WHERE id = sqlc.arg(id);

-- name: DocumentText :one
SELECT coalesce(string_agg(text, E'\n\n' ORDER BY ord), '')::text AS text FROM knowledge_chunks WHERE document_id = sqlc.arg(document_id);

-- name: SearchKnowledge :many
-- The best chunks of the named sources only by keywords: any of the query's words, ts_rank on
-- the simple tsvector divided by 1 + the log of the chunk's length, so long chunks full of
-- common words do not win; a chunk scores 1/(60 + its rank), as reciprocal rank fusion would.
WITH src AS (
  SELECT id, name FROM knowledge_sources WHERE name = ANY (sqlc.arg(sources)::text[])
),
query AS (
  SELECT nullif(replace(plainto_tsquery('simple', sqlc.arg(query)::text)::text, ' & ', ' | '), '')::tsquery AS q
),
words AS (
  SELECT w.id, row_number() OVER (ORDER BY w.rank DESC, w.id) AS r FROM (
    SELECT c.id, ts_rank(c.tsv, query.q, 1) AS rank
    FROM knowledge_chunks c, query
    WHERE c.tsv @@ query.q AND c.source_id IN (SELECT id FROM src)
    ORDER BY rank DESC, c.id
    LIMIT 50
  ) w
)
SELECT c.id, c.document_id, src.name AS source, d.title, d.url, c.heading, c.text, (1.0 / (60 + words.r))::float8 AS score
FROM words
JOIN knowledge_chunks c ON c.id = words.id
JOIN knowledge_documents d ON d.org_id = c.org_id AND d.source_id = c.source_id AND d.id = c.document_id
JOIN src ON src.id = c.source_id
ORDER BY words.r, c.id
LIMIT sqlc.arg(max_hits);

-- Knowledge use and verdicts (00072, ADR 0027).

-- name: UpsertKnowledgeUse :execrows
-- One use per run, document and kind: a second merges its chunk ids into the first's, sorted
-- and distinct, and keeps the earlier time. A document of another source or organization fails
-- the document key, or changes no row when the use exists (ports.ErrNotFound); a merge past
-- 1,000 chunks fails its check.
INSERT INTO knowledge_uses AS u (factory_id, item_id, run_id, source_id, document_id, kind, chunk_ids, at)
VALUES (sqlc.arg(factory_id), sqlc.arg(item_id), sqlc.arg(run_id), sqlc.arg(source_id), sqlc.arg(document_id), sqlc.arg(kind),
        ARRAY(SELECT DISTINCT c FROM unnest(sqlc.arg(chunk_ids)::bigint[]) c ORDER BY c), sqlc.arg(at))
ON CONFLICT (org_id, run_id, document_id, kind) DO UPDATE
SET chunk_ids = ARRAY(SELECT DISTINCT c FROM unnest(u.chunk_ids || excluded.chunk_ids) c ORDER BY c),
    at = least(u.at, excluded.at)
WHERE u.source_id = excluded.source_id;

-- name: UpsertKnowledgeVerdict :one
-- One verdict per actor, document and run (a verdict without a run counting as its own:
-- NULLS NOT DISTINCT); a later one replaces its verdict, note, item and time. The source is
-- the document's; no row when the document is not the organization's (ports.ErrNotFound).
INSERT INTO knowledge_verdicts AS v (source_id, document_id, run_id, item_id, verdict, note, actor, at)
SELECT d.source_id, d.id, sqlc.narg(run_id), sqlc.narg(item_id), sqlc.arg(verdict), sqlc.arg(note), sqlc.arg(actor),
       coalesce(sqlc.narg(at)::timestamptz, now())
FROM knowledge_documents d WHERE d.id = sqlc.arg(document_id)
ON CONFLICT ON CONSTRAINT knowledge_verdicts_actor_key DO UPDATE
SET verdict = excluded.verdict, note = excluded.note, item_id = excluded.item_id, at = excluded.at
RETURNING v.source_id, v.at;

-- name: KnowledgeSourceUsage :many
-- Per document of a source with a use or a verdict in the last days days: the distinct runs with a use
-- and the verdicts given, agents' and people's apart; the most used first, then by id.
WITH uses AS (
  SELECT u.document_id, count(DISTINCT u.run_id) AS runs
  FROM knowledge_uses u
  WHERE u.source_id = sqlc.arg(source_id) AND u.at >= now() - make_interval(days => sqlc.arg(days)::int)
  GROUP BY u.document_id
), verdicts AS (
  SELECT v.document_id,
         count(*) FILTER (WHERE v.actor LIKE 'agent:%' AND v.verdict = 'helped') AS agents_helped,
         count(*) FILTER (WHERE v.actor LIKE 'agent:%' AND v.verdict = 'misled') AS agents_misled,
         count(*) FILTER (WHERE v.actor LIKE 'user:%' AND v.verdict = 'helped') AS people_helped,
         count(*) FILTER (WHERE v.actor LIKE 'user:%' AND v.verdict = 'misled') AS people_misled
  FROM knowledge_verdicts v
  WHERE v.source_id = sqlc.arg(source_id) AND v.at >= now() - make_interval(days => sqlc.arg(days)::int)
  GROUP BY v.document_id
)
SELECT d.id, d.title, coalesce(u.runs, 0)::bigint AS runs,
       coalesce(v.agents_helped, 0)::bigint AS agents_helped, coalesce(v.agents_misled, 0)::bigint AS agents_misled,
       coalesce(v.people_helped, 0)::bigint AS people_helped, coalesce(v.people_misled, 0)::bigint AS people_misled
FROM uses u
FULL JOIN verdicts v ON v.document_id = u.document_id
JOIN knowledge_documents d ON d.id = coalesce(u.document_id, v.document_id)
ORDER BY runs DESC, d.id
LIMIT sqlc.arg(max_rows);

-- name: KnowledgeDocumentUses :many
-- A page of the runs that used a document, by their first use newest first (ties by run id
-- descending), after the run after: those before its first use of the document.
SELECT u.run_id, u.item_id, u.factory_id, array_agg(u.kind ORDER BY u.kind)::text[] AS kinds, min(u.at)::timestamptz AS at
FROM knowledge_uses u
WHERE u.document_id = sqlc.arg(document_id)
GROUP BY u.run_id, u.item_id, u.factory_id
HAVING sqlc.narg(after)::uuid IS NULL
    OR (min(u.at), u.run_id) < ((SELECT min(a.at) FROM knowledge_uses a
                                 WHERE a.document_id = sqlc.arg(document_id) AND a.run_id = sqlc.narg(after)::uuid),
                                sqlc.narg(after)::uuid)
ORDER BY min(u.at) DESC, u.run_id DESC
LIMIT sqlc.arg(max_rows);

-- name: KnowledgeRunUses :many
SELECT factory_id, item_id, run_id, source_id, document_id, kind, chunk_ids, at
FROM knowledge_uses
WHERE run_id = sqlc.arg(run_id)
ORDER BY at, document_id, kind;

-- name: KnowledgeRunVerdicts :many
-- The verdicts given for a run, by time, document and actor.
SELECT source_id, document_id, run_id, item_id, verdict, note, actor, at
FROM knowledge_verdicts
WHERE run_id = sqlc.arg(run_id)
ORDER BY at, document_id, actor;

-- Knowledge as files (00090): a source's snapshots, its current files and the daily read counts.

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
