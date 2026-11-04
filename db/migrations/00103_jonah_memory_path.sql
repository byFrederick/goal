-- +goose Up
-- Jonah may propose facts for the factory's memory (ADR 0039 decision 7): memory.yaml, at the
-- definition's root, joins the paths a proposal may edit. goald writes that file itself, only from
-- facts that quote a person's note, answer or send-back reason, and drops a definition edit of it.
-- domain.ProposalPathAllowed checks the same. 00101's function is replaced; the CHECK of
-- jonah_proposals.paths calls it by name, and every stored path still passes it. Definers pin
-- their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_jonah_paths_allowed(p_paths text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
  SELECT cardinality(p_paths) > 0
     AND cardinality(p_paths) = (SELECT count(DISTINCT p) FROM unnest(p_paths) AS p)
     AND NOT EXISTS (
       SELECT 1 FROM unnest(p_paths) AS p
       WHERE p IS NULL
          OR p !~ '^(factory\.yaml|memory\.yaml|(agents|skills|rules|contracts)/.+\.(md|yaml|json))$'
          OR p ~ '(^|/)\.\.?(/|$)'
          OR p ~ '[[:cntrl:]]')
$$;
-- +goose StatementEnd
