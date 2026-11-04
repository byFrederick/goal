-- +goose Up
-- Jonah proposals as exact replacements (ADR 0039 decisions 5 and 6). A proposal's edits replace
-- its whole files: each element of jonah_proposals.edits is {path, replacements: [{old, new}]},
-- applied again to the file at Approve and Edit, or {path, content} for a new file. A proposal
-- stored before this migration keeps its whole files as {path, content, base_sha256}: content
-- replaces the file while it still has base_sha256, and an empty base_sha256 is a new file, so
-- earlier review and undo proposals stay applicable.
--
-- The path allowlist becomes positive: factory.yaml, whose paths goald checks apart, and the .md,
-- .yaml and .json files under agents/, skills/, rules/ and contracts/. No script or other file.
-- The allowlist is replaced after the rows are converted, which passed the old one. A row with a
-- path outside the new one could not be decided, as an update checks it again; ADR 0039 decision
-- 21 found no organization with a proposal, and the watch part's undo proposals edit what their
-- review proposals did.
--
-- Observations persist across reviews: a later review updates an open (watching or proposed) one,
-- so goal_app may update its text, items, count, state and proposal, and updated_at says when. A
-- cause that is not a definition problem is not_jonahs. Definers pin their search_path as 00036
-- explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- edits is a non-empty array whose elements are {path, replacements} with at least one {old, new}
-- of a non-empty old, or {path, content} with an optional base_sha256 of 64 lowercase hex; their
-- text totals at most 256 KiB; and the paths, in order, are p_paths.
-- +goose StatementBegin
CREATE FUNCTION goal_jonah_edits_valid(p_edits jsonb, p_paths text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
  SELECT CASE WHEN jsonb_typeof(p_edits) IS DISTINCT FROM 'array' OR jsonb_array_length(p_edits) = 0 THEN false
  ELSE (
    SELECT bool_and(coalesce(jsonb_typeof(e) = 'object'
                             AND jsonb_typeof(e -> 'path') = 'string'
                             AND CASE
                               WHEN e ? 'replacements' THEN
                                 NOT e ? 'content' AND NOT e ? 'base_sha256'
                                 AND jsonb_typeof(e -> 'replacements') = 'array'
                                 AND jsonb_array_length(e -> 'replacements') > 0
                                 AND NOT EXISTS (
                                   SELECT 1 FROM jsonb_array_elements(e -> 'replacements') AS r
                                   WHERE jsonb_typeof(r) IS DISTINCT FROM 'object'
                                      OR jsonb_typeof(r -> 'old') IS DISTINCT FROM 'string'
                                      OR jsonb_typeof(r -> 'new') IS DISTINCT FROM 'string'
                                      OR r ->> 'old' = '')
                               ELSE
                                 jsonb_typeof(e -> 'content') = 'string'
                                 AND (NOT e ? 'base_sha256' OR (jsonb_typeof(e -> 'base_sha256') = 'string'
                                                                AND e ->> 'base_sha256' ~ '^([0-9a-f]{64})?$'))
                             END, false))
       AND coalesce(sum(coalesce(octet_length(e ->> 'content'), 0)
                        + coalesce((SELECT sum(octet_length(r ->> 'old') + octet_length(r ->> 'new'))
                                    FROM jsonb_array_elements(CASE WHEN jsonb_typeof(e -> 'replacements') = 'array'
                                                                   THEN e -> 'replacements' ELSE '[]' END) AS r), 0)), 0) <= 262144
       AND array_agg(e ->> 'path' ORDER BY o) = p_paths
    FROM jsonb_array_elements(p_edits) WITH ORDINALITY AS x (e, o))
  END
$$;
-- +goose StatementEnd

-- Each file becomes a whole-content edit with its base_sha256, an empty one dropped.
ALTER TABLE jonah_proposals ADD COLUMN edits jsonb;
UPDATE jonah_proposals SET edits = (
  SELECT jsonb_agg(CASE WHEN f ->> 'base_sha256' = '' THEN jsonb_build_object('path', f -> 'path', 'content', f -> 'content')
                        ELSE jsonb_build_object('path', f -> 'path', 'content', f -> 'content', 'base_sha256', f -> 'base_sha256')
                   END ORDER BY o)
  FROM jsonb_array_elements(files) WITH ORDINALITY AS x (f, o));
ALTER TABLE jonah_proposals ALTER COLUMN edits SET NOT NULL;
ALTER TABLE jonah_proposals ADD CONSTRAINT jonah_proposals_edits_check CHECK (goal_jonah_edits_valid(edits, paths));
-- Dropping files drops its CHECK (goal_jonah_files_valid), which nothing else uses.
ALTER TABLE jonah_proposals DROP COLUMN files;
DROP FUNCTION goal_jonah_files_valid(jsonb, text[]);

-- Every path is one a proposal may edit (decision 6): factory.yaml or a .md, .yaml or .json file
-- under agents/, skills/, rules/ or contracts/, without a . or .. segment or a control
-- character, each once, and at least one. domain.ProposalPathAllowed checks the same.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_jonah_paths_allowed(p_paths text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
  SELECT cardinality(p_paths) > 0
     AND cardinality(p_paths) = (SELECT count(DISTINCT p) FROM unnest(p_paths) AS p)
     AND NOT EXISTS (
       SELECT 1 FROM unnest(p_paths) AS p
       WHERE p IS NULL
          OR p !~ '^(factory\.yaml|(agents|skills|rules|contracts)/.+\.(md|yaml|json))$'
          OR p ~ '(^|/)\.\.?(/|$)'
          OR p ~ '[[:cntrl:]]')
$$;
-- +goose StatementEnd

-- updated_at is created_at for the observations made so far.
ALTER TABLE jonah_observations ADD COLUMN updated_at timestamptz;
UPDATE jonah_observations SET updated_at = created_at;
ALTER TABLE jonah_observations ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE jonah_observations DROP CONSTRAINT jonah_observations_state_check;
ALTER TABLE jonah_observations ADD CONSTRAINT jonah_observations_state_check
  CHECK (state IN ('watching', 'proposed', 'not_jonahs'));
-- A factory's open observations, which a review reads and updates.
CREATE INDEX jonah_observations_open_idx ON jonah_observations (org_id, factory_id, updated_at DESC)
  WHERE state IN ('watching', 'proposed');

GRANT UPDATE (summary, detail, item_ids, count, state, proposal_id, updated_at) ON jonah_observations TO goal_app;
