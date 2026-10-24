-- +goose Up
-- Phase 3 fixes (review L1): an invitation belongs to a live organization. Deleting an
-- organization cancels its pending invitations (goal_retire_org), but Better Auth's rows stay
-- until the purge, so a lingering owner or admin could still invite, opening install sign-up to
-- any address and mailing from Goal's address for the whole retention window. Now an invitation
-- is refused for an organization that is not live, atomically (a trigger, with the organization
-- row share-locked so a deletion waits and then cancels it), the auth service asks whether an
-- organization is live before inviting (goal_org_live) and counts only invitations of live
-- organizations as invitees (goal_is_invitee), and the invitations such owners made before this
-- migration are canceled. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- goal_org_live is whether p_org is an organization that has not been deleted.
CREATE FUNCTION goal_org_live(p_org uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT EXISTS (SELECT 1 FROM organizations WHERE id = p_org AND deleted_at IS NULL)
$$;

-- goal_is_invitee is whether p_email (case and surrounding spaces ignored) has a pending
-- invitation, unexpired at p_now, to a live organization: the sign-up gate's invitee (spec 3 §8.7).
CREATE FUNCTION goal_is_invitee(p_email text, p_now timestamptz) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT EXISTS (
    SELECT 1 FROM auth.invitation i
    JOIN organizations o ON o.id = i."organizationId" AND o.deleted_at IS NULL
    WHERE lower(i.email) = lower(btrim(p_email)) AND i.status = 'pending' AND i."expiresAt" > p_now)
$$;

-- goal_invite_live_org refuses an invitation to an organization that is not live
-- (check_violation goal_invitation_live_org). The organization row is share-locked, so a deletion
-- running at the same time waits for the invitation and its retire trigger then cancels it.
-- +goose StatementBegin
CREATE FUNCTION goal_invite_live_org() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  PERFORM 1 FROM organizations WHERE id = NEW."organizationId" AND deleted_at IS NULL FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'invitation to an organization that is not live' USING
      ERRCODE = 'check_violation', CONSTRAINT = 'goal_invitation_live_org';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER invitation_live_org BEFORE INSERT ON auth.invitation
  FOR EACH ROW EXECUTE FUNCTION goal_invite_live_org();

-- The pending invitations of organizations deleted before now, made by a lingering member after
-- the deletion canceled the rest: canceled as goal_retire_org would have (00043 audits them).
UPDATE auth.invitation i SET status = 'canceled', "changedBy" = 'system:org-deleted', "changedAt" = clock_timestamp()
FROM organizations o
WHERE o.id = i."organizationId" AND o.deleted_at IS NOT NULL AND i.status = 'pending';

REVOKE ALL ON FUNCTION goal_org_live(uuid), goal_is_invitee(text, timestamptz), goal_invite_live_org() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_org_live(uuid), goal_is_invitee(text, timestamptz) TO goal_auth;
