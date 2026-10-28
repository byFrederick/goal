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
FROM goal_claim_knowledge_source(sqlc.arg(now)::timestamptz, sqlc.arg(until)::timestamptz, sqlc.arg(model)::text) c;

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
    embed_model = CASE WHEN sqlc.arg(error)::text = '' THEN nullif(sqlc.arg(model)::text, '') ELSE embed_model END,
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
                                      seen_at, indexed_at, embed_model, created_by)
VALUES (sqlc.arg(id), sqlc.arg(source_id), sqlc.arg(external_id), sqlc.arg(version), sqlc.arg(title), sqlc.narg(url),
        sqlc.arg(mime), sqlc.arg(bytes), sqlc.narg(blob_sha256), sqlc.arg(status), sqlc.narg(error), sqlc.arg(seen_at),
        sqlc.narg(indexed_at), sqlc.narg(embed_model), sqlc.arg(created_by))
ON CONFLICT (source_id, external_id) DO UPDATE
SET version = EXCLUDED.version, title = EXCLUDED.title, url = EXCLUDED.url, mime = EXCLUDED.mime, bytes = EXCLUDED.bytes,
    blob_sha256 = coalesce(EXCLUDED.blob_sha256, d.blob_sha256), status = EXCLUDED.status, error = EXCLUDED.error,
    seen_at = greatest(d.seen_at, EXCLUDED.seen_at), indexed_at = EXCLUDED.indexed_at, embed_model = EXCLUDED.embed_model
RETURNING id;

-- name: KnowledgeSourceKind :one
SELECT kind FROM knowledge_sources WHERE id = sqlc.arg(id);

-- name: UpdateKnowledgeUpload :one
-- An upload's document as its sync read it; none when it was deleted meanwhile, which a waiting
-- update sees once the deletion commits.
UPDATE knowledge_documents
SET version = sqlc.arg(version), title = sqlc.arg(title), mime = sqlc.arg(mime), bytes = sqlc.arg(bytes),
    status = sqlc.arg(status), error = sqlc.narg(error), seen_at = greatest(seen_at, sqlc.arg(seen_at)),
    indexed_at = sqlc.narg(indexed_at), embed_model = sqlc.narg(embed_model)
WHERE source_id = sqlc.arg(source_id) AND external_id = sqlc.arg(external_id)
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
-- The nearest-neighbour scan over the HNSW index goes on past chunks the filters drop
-- (pgvector's iterative scan), so a search over large sources still finds its candidates when
-- other organizations' or sources' chunks are nearer. Small sources are searched exactly instead
-- (SearchKnowledge).
SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true), set_config('hnsw.ef_search', '100', true);

-- name: SearchKnowledge :many
-- Two candidate lists over the named sources only, the nearest chunks of the model by cosine
-- distance (with an embedding) and the best by keywords (any of the query's words, ts_rank on
-- the simple tsvector divided by 1 + the log of the chunk's length, so long chunks full of
-- common words do not win), fused by reciprocal rank (k = 60; domain.FuseRanks).
--
-- The nearest are exact when the sources hold at most 20,000 chunks: their distances, computed
-- behind a fence (OFFSET 0) that keeps the HNSW index out, and sorted. The index holds every
-- organization's chunks and the filters apply after it, so walking it for sources that are a
-- small share of the table finds too few of theirs within pgvector's scan budget, or none
-- (review 4 SQL M2). Larger sources walk the index; partitioning by organization (spec 4 §6) is
-- the step after this if installs outgrow that.
WITH src AS (
  SELECT id, name FROM knowledge_sources WHERE name = ANY (sqlc.arg(sources)::text[])
),
query AS (
  SELECT nullif(replace(plainto_tsquery('simple', sqlc.arg(query)::text)::text, ' & ', ' | '), '')::tsquery AS q
),
small AS (
  SELECT count(*) <= 20000 AS exact
  FROM (SELECT 1 FROM knowledge_chunks c WHERE c.source_id IN (SELECT id FROM src) LIMIT 20001) n
),
near AS (
  SELECT n.id, row_number() OVER (ORDER BY n.distance, n.id) AS r FROM (
    (SELECT e.id, e.distance FROM (
       SELECT c.id, c.embedding <=> nullif(sqlc.arg(embedding)::text, '')::halfvec(1024) AS distance
       FROM knowledge_chunks c
       WHERE (SELECT exact FROM small) AND sqlc.arg(embedding)::text <> '' AND c.embed_model = sqlc.arg(model)::text
         AND c.source_id IN (SELECT id FROM src)
       OFFSET 0
     ) e
     ORDER BY e.distance, e.id
     LIMIT 50)
    UNION ALL
    (SELECT c.id, c.embedding <=> nullif(sqlc.arg(embedding)::text, '')::halfvec(1024) AS distance
     FROM knowledge_chunks c
     WHERE NOT (SELECT exact FROM small) AND sqlc.arg(embedding)::text <> '' AND c.embed_model = sqlc.arg(model)::text
       AND c.source_id IN (SELECT id FROM src)
     ORDER BY c.embedding <=> nullif(sqlc.arg(embedding)::text, '')::halfvec(1024)
     LIMIT 50)
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
