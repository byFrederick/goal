-- The knowledge base (00054), under row-level security: every statement sees the transaction's
-- organization only. Embeddings travel as pgvector's text form ('[0.1,0.2,…]'), '' for none.

-- name: KnowledgeSources :many
-- The organization's sources, or the one named or with the given id.
SELECT s.id, s.name, s.title, s.kind, c.name AS connection, s.config,
       extract(epoch FROM s.sync_every)::bigint AS sync_every_seconds, s.next_sync_at, s.lease_until, s.status,
       s.last_synced_at, s.last_error, s.embed_model, s.created_by, s.created_at, s.updated_at,
       (SELECT count(*) FROM knowledge_documents d WHERE d.org_id = s.org_id AND d.source_id = s.id) AS documents
FROM knowledge_sources s
LEFT JOIN connections c ON c.org_id = s.org_id AND c.id = s.connection_id
WHERE (sqlc.narg(name)::text IS NULL OR s.name = sqlc.narg(name)::text)
  AND (sqlc.narg(id)::uuid IS NULL OR s.id = sqlc.narg(id)::uuid)
ORDER BY s.name;

-- name: KnowledgeConnection :one
-- A connection a source may read: its id and its kind's sync adapter ('' when it cannot sync).
SELECT c.id, coalesce(k.spec->'spec'->>'sync', '')::text AS sync
FROM connections c JOIN connection_kinds k ON k.id = c.kind_id
WHERE c.name = sqlc.arg(name);

-- name: InsertKnowledgeSource :exec
-- org_id comes from its default, goal_org(). A live source never syncs.
INSERT INTO knowledge_sources (id, name, title, kind, connection_id, config, sync_every, next_sync_at, created_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(title), sqlc.arg(kind), sqlc.narg(connection_id), sqlc.arg(config),
        make_interval(secs => sqlc.arg(sync_every_seconds)::bigint),
        CASE WHEN sqlc.arg(kind)::text = 'live' THEN NULL ELSE now() END, sqlc.arg(created_by));

-- name: UpdateKnowledgeSource :one
UPDATE knowledge_sources
SET title = coalesce(sqlc.narg(title)::text, title), config = coalesce(sqlc.narg(config)::jsonb, config),
    sync_every = CASE WHEN sqlc.narg(sync_every_seconds)::bigint IS NULL THEN sync_every
                      ELSE make_interval(secs => sqlc.narg(sync_every_seconds)::bigint) END,
    updated_at = now()
WHERE name = sqlc.arg(name)
RETURNING id;

-- name: LockKnowledgeSource :one
SELECT id, kind FROM knowledge_sources WHERE name = sqlc.arg(name) FOR UPDATE;

-- name: SourceBlobs :many
-- The uploads' content hashes of a source's documents.
SELECT DISTINCT blob_sha256::text AS blob FROM knowledge_documents
WHERE source_id = sqlc.arg(source_id) AND blob_sha256 IS NOT NULL;

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
-- The source most overdue: not live, its lease ended, and due or, with a model, of another
-- model's chunks unless its last sync failed. Leased until until, as syncing.
UPDATE knowledge_sources s SET lease_until = sqlc.arg(until)::timestamptz, status = 'syncing'
WHERE s.id = (
  SELECT k.id FROM knowledge_sources k
  WHERE k.kind <> 'live' AND (k.lease_until IS NULL OR k.lease_until <= sqlc.arg(now)::timestamptz)
    AND (k.next_sync_at <= sqlc.arg(now)::timestamptz
         OR (sqlc.arg(model)::text <> '' AND k.embed_model IS DISTINCT FROM sqlc.arg(model)::text AND k.status <> 'failed'))
  ORDER BY k.next_sync_at, k.id
  LIMIT 1
  FOR UPDATE SKIP LOCKED)
RETURNING s.id;

-- name: RenewKnowledgeSource :execrows
UPDATE knowledge_sources SET lease_until = sqlc.arg(until)::timestamptz
WHERE id = sqlc.arg(id) AND status = 'syncing' AND lease_until > sqlc.arg(now)::timestamptz;

-- name: FinishKnowledgeSync :execrows
-- The next sync is at plus the interval unless the source was made due since its claim (due).
UPDATE knowledge_sources
SET status = CASE WHEN sqlc.arg(error)::text = '' THEN 'idle' ELSE 'failed' END,
    last_error = nullif(sqlc.arg(error)::text, ''),
    last_synced_at = CASE WHEN sqlc.arg(error)::text = '' THEN sqlc.arg(at)::timestamptz ELSE last_synced_at END,
    embed_model = CASE WHEN sqlc.arg(error)::text = '' THEN nullif(sqlc.arg(model)::text, '') ELSE embed_model END,
    lease_until = NULL,
    next_sync_at = CASE WHEN next_sync_at IS DISTINCT FROM sqlc.arg(due)::timestamptz THEN next_sync_at
                        ELSE sqlc.arg(at)::timestamptz + sync_every END,
    updated_at = now()
WHERE id = sqlc.arg(id) AND kind <> 'live';

-- name: KnowledgeVersions :many
SELECT external_id, version FROM knowledge_documents
WHERE source_id = sqlc.arg(source_id) AND status <> 'pending';

-- name: UpsertKnowledgeDocument :one
-- org_id comes from its default, goal_org(); an upload's hash stays when a sync rewrites it.
INSERT INTO knowledge_documents AS d (id, source_id, external_id, version, title, url, mime, bytes, blob_sha256, status, error,
                                      seen_at, indexed_at, embed_model, created_by)
VALUES (sqlc.arg(id), sqlc.arg(source_id), sqlc.arg(external_id), sqlc.arg(version), sqlc.arg(title), sqlc.narg(url),
        sqlc.arg(mime), sqlc.arg(bytes), sqlc.narg(blob_sha256), sqlc.arg(status), sqlc.narg(error), sqlc.arg(seen_at),
        sqlc.narg(indexed_at), sqlc.narg(embed_model), sqlc.arg(created_by))
ON CONFLICT (source_id, external_id) DO UPDATE
SET version = EXCLUDED.version, title = EXCLUDED.title, url = EXCLUDED.url, mime = EXCLUDED.mime, bytes = EXCLUDED.bytes,
    blob_sha256 = coalesce(EXCLUDED.blob_sha256, d.blob_sha256), status = EXCLUDED.status, error = EXCLUDED.error,
    seen_at = EXCLUDED.seen_at, indexed_at = EXCLUDED.indexed_at, embed_model = EXCLUDED.embed_model
RETURNING id;

-- name: DeleteDocumentChunks :exec
DELETE FROM knowledge_chunks WHERE document_id = sqlc.arg(document_id);

-- name: InsertKnowledgeChunks :exec
-- A document's chunks in one statement; an empty embedding is none.
INSERT INTO knowledge_chunks (source_id, document_id, ord, heading, text, embedding, embed_model)
SELECT sqlc.arg(source_id), sqlc.arg(document_id), (sqlc.arg(ords)::int[])[i], (sqlc.arg(headings)::text[])[i],
       (sqlc.arg(texts)::text[])[i], nullif((sqlc.arg(embeddings)::text[])[i], '')::halfvec(1024),
       CASE WHEN (sqlc.arg(embeddings)::text[])[i] = '' THEN NULL ELSE sqlc.arg(model)::text END
FROM generate_series(1, cardinality(sqlc.arg(ords)::int[])) AS i;

-- name: MarkKnowledgeSeen :exec
UPDATE knowledge_documents SET seen_at = sqlc.arg(at)
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

-- name: MakeKnowledgeSourceDue :exec
UPDATE knowledge_sources SET next_sync_at = now() WHERE id = sqlc.arg(id) AND kind <> 'live';

-- name: KnowledgeDocuments :many
-- A page of a source's documents in id order, or the document of an external id, or with an id.
SELECT d.id, d.source_id, d.external_id, d.version, d.title, d.url, d.mime, d.bytes, d.blob_sha256, d.status, d.error,
       d.seen_at, d.indexed_at, d.embed_model, d.created_by, s.kind AS source_kind
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

-- name: SetSearchScan :exec
-- The nearest-neighbour scan goes on past chunks the filters drop (pgvector's iterative scan),
-- so a search still finds its candidates when other organizations' or sources' chunks are nearer.
SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true), set_config('hnsw.ef_search', '100', true);

-- name: SearchKnowledge :many
-- Two candidate lists over the named sources only, the nearest chunks of the model by cosine
-- distance (with an embedding) and the best by keywords (any of the query's words, ts_rank on
-- the simple tsvector divided by 1 + the log of the chunk's length, so long chunks full of
-- common words do not win), fused by reciprocal rank (k = 60; domain.FuseRanks).
WITH src AS (
  SELECT id, name FROM knowledge_sources WHERE name = ANY (sqlc.arg(sources)::text[])
),
query AS (
  SELECT nullif(replace(plainto_tsquery('simple', sqlc.arg(query)::text)::text, ' & ', ' | '), '')::tsquery AS q
),
near AS (
  SELECT n.id, row_number() OVER (ORDER BY n.distance, n.id) AS r FROM (
    SELECT c.id, c.embedding <=> nullif(sqlc.arg(embedding)::text, '')::halfvec(1024) AS distance
    FROM knowledge_chunks c
    WHERE sqlc.arg(embedding)::text <> '' AND c.embed_model = sqlc.arg(model)::text AND c.source_id IN (SELECT id FROM src)
    ORDER BY c.embedding <=> nullif(sqlc.arg(embedding)::text, '')::halfvec(1024)
    LIMIT 50
  ) n
),
words AS (
  SELECT w.id, row_number() OVER (ORDER BY w.rank DESC, w.id) AS r FROM (
    SELECT c.id, ts_rank(c.tsv, query.q, 1) AS rank
    FROM knowledge_chunks c, query
    WHERE c.tsv @@ query.q AND c.source_id IN (SELECT id FROM src)
    ORDER BY rank DESC, c.id
    LIMIT 50
  ) w
),
fused AS (
  SELECT f.id, sum(1.0 / (60 + f.r))::float8 AS score FROM (SELECT id, r FROM near UNION ALL SELECT id, r FROM words) f
  GROUP BY f.id
)
SELECT c.id, c.document_id, src.name AS source, d.title, d.url, c.heading, c.text, fused.score
FROM fused
JOIN knowledge_chunks c ON c.id = fused.id
JOIN knowledge_documents d ON d.org_id = c.org_id AND d.source_id = c.source_id AND d.id = c.document_id
JOIN src ON src.id = c.source_id
ORDER BY fused.score DESC, c.id
LIMIT sqlc.arg(max_hits);
