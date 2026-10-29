-- +goose Up
-- Draft requests (ADR 0021; ports.DraftRequests): an asked change of a factory's definition, from
-- the Definition screen's Ask bar or the Agent screen's Ask Jonah for a fix (agent set). A request
-- is queued, then running under a lease, then done or failed with its result. A factory has at
-- most one queued or running request (draft_requests_one_open_idx; the adapter maps its violation
-- to domain.ErrDraftRequestOpen). requester is an Actor.Ref(). Rows go with the factory or the
-- organization.
--
-- Leases are fenced as 00055's knowledge leases are: each claim draws a new lease_holder, and a
-- heartbeat or finish names it, so a goald that stalled past its lease changes nothing once
-- another claim took the request. goal_claim_draft_request claims the oldest queued request of
-- every live organization's in one statement and returns the organization the run then works
-- under; goal_requeue_draft_requests puts every running request whose lease ended back in the
-- queue. Both read across organizations, so they run as goal_owner with their search_path pinned
-- (00036).
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE draft_requests (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id       uuid NOT NULL,
  requester        text NOT NULL CHECK (requester ~ '^(user|service|agent|system):.+$'),  -- Actor.Ref()
  text             text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 2000),
  agent            text CHECK (agent ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),               -- NULL for the Ask bar
  status           text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
  summary          text NOT NULL DEFAULT '' CHECK (char_length(summary) <= 4000),
  changed_paths    text[] NOT NULL DEFAULT '{}',
  cost_usd         numeric(12, 4) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
  turns            integer NOT NULL DEFAULT 0 CHECK (turns >= 0),
  error            text NOT NULL DEFAULT '' CHECK (char_length(error) <= 2000),
  created_at       timestamptz NOT NULL DEFAULT now(),
  started_at       timestamptz,
  finished_at      timestamptz,
  lease_holder     uuid,
  lease_expires_at timestamptz,
  -- Queued holds no start, lease or end; running a start and a lease; done and failed an end and
  -- no lease.
  CONSTRAINT draft_requests_status_times CHECK (
    CASE status
      WHEN 'queued' THEN started_at IS NULL AND finished_at IS NULL AND lease_holder IS NULL AND lease_expires_at IS NULL
      WHEN 'running' THEN started_at IS NOT NULL AND finished_at IS NULL AND lease_holder IS NOT NULL AND lease_expires_at IS NOT NULL
      ELSE finished_at IS NOT NULL AND lease_holder IS NULL AND lease_expires_at IS NULL END),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);

-- One queued or running request per factory (ADR 0021, decision 4).
CREATE UNIQUE INDEX draft_requests_one_open_idx ON draft_requests (factory_id) WHERE status IN ('queued', 'running');
-- A factory's requests newest first; also the purge's delete by organization and the cascade from
-- factories.
CREATE INDEX draft_requests_org_factory_idx ON draft_requests (org_id, factory_id, created_at DESC, id DESC);
-- What the claim scans, across organizations: the queued, oldest first.
CREATE INDEX draft_requests_queued_idx ON draft_requests (created_at, id) WHERE status = 'queued';
-- What the requeue scans: the running, by the end of their lease.
CREATE INDEX draft_requests_running_idx ON draft_requests (lease_expires_at) WHERE status = 'running';

ALTER TABLE draft_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON draft_requests
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON draft_requests TO goal_app;
GRANT UPDATE (status, summary, changed_paths, cost_usd, turns, error, finished_at, lease_holder, lease_expires_at)
  ON draft_requests TO goal_app;

-- The oldest queued request of a live organization, running from p_now and leased until p_until
-- under a new holder.
CREATE FUNCTION goal_claim_draft_request(p_now timestamptz, p_until timestamptz)
RETURNS TABLE (org_id uuid, id uuid, lease_holder uuid)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  UPDATE draft_requests r
  SET status = 'running', started_at = p_now, lease_holder = gen_random_uuid(), lease_expires_at = p_until
  WHERE r.id = (
    SELECT q.id FROM draft_requests q
    WHERE q.status = 'queued'
      AND EXISTS (SELECT 1 FROM organizations o WHERE o.id = q.org_id AND o.deleted_at IS NULL)
    ORDER BY q.created_at, q.id
    LIMIT 1
    FOR UPDATE OF q SKIP LOCKED)
  RETURNING r.org_id, r.id, r.lease_holder
$$;

-- Every running request whose lease ended by p_now, queued again with no start or lease; how many.
CREATE FUNCTION goal_requeue_draft_requests(p_now timestamptz)
RETURNS integer
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  WITH requeued AS (
    UPDATE draft_requests
    SET status = 'queued', started_at = NULL, lease_holder = NULL, lease_expires_at = NULL
    WHERE status = 'running' AND lease_expires_at <= p_now
    RETURNING 1)
  SELECT count(*)::integer FROM requeued
$$;

REVOKE ALL ON FUNCTION goal_claim_draft_request(timestamptz, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_requeue_draft_requests(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_claim_draft_request(timestamptz, timestamptz) TO goal_app;
GRANT EXECUTE ON FUNCTION goal_requeue_draft_requests(timestamptz) TO goal_app;
