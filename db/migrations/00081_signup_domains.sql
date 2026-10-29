-- +goose Up
-- Sign-up by domain (ADR 0023, decision 7): an owner lets anyone with a verified email at the
-- organization's own domain sign up without an invitation, joining as a member with a factory
-- role (join_role) on every factory that exists then.
--
-- org_signup_domains holds at most one domain per organization, under the tenancy kit. goal_app
-- reads it; only goal_set_signup_domain writes it. A domain is lower case and matched exactly
-- (no subdomains), and belongs to at most one organization: a deleted organization's row stays
-- until the purge but admits nobody, and a live organization's claim takes it over.
-- public_email_domains is the list of webmail domains no organization may claim, kept here and
-- nowhere else (decision 7): changing it takes a new migration. The auth service reads both
-- through goal_signup_domain_org and joins people through goal_apply_signup_domain. Definers pin
-- their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE org_signup_domains (
  org_id    uuid PRIMARY KEY DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  domain    text NOT NULL CHECK (length(domain) <= 253
                                 AND domain ~ '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
  join_role text NOT NULL DEFAULT 'viewer' CHECK (join_role IN ('viewer', 'approver', 'editor')),
  set_by    text NOT NULL CHECK (set_by ~ '^(user|service|agent|system):.+$'),  -- Actor.Ref()
  set_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT org_signup_domains_domain_key UNIQUE (domain)
);
ALTER TABLE org_signup_domains ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON org_signup_domains
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT ON org_signup_domains TO goal_app;

CREATE TABLE public_email_domains (
  domain text PRIMARY KEY CHECK (domain = lower(domain))
);
INSERT INTO public_email_domains (domain) VALUES
  ('gmail.com'), ('googlemail.com'),
  ('outlook.com'), ('hotmail.com'), ('hotmail.co.uk'), ('hotmail.fr'), ('hotmail.de'), ('live.com'), ('msn.com'),
  ('yahoo.com'), ('yahoo.co.uk'), ('yahoo.fr'), ('yahoo.de'), ('yahoo.co.jp'), ('ymail.com'), ('rocketmail.com'),
  ('icloud.com'), ('me.com'), ('mac.com'),
  ('aol.com'), ('aim.com'),
  ('proton.me'), ('protonmail.com'), ('protonmail.ch'), ('pm.me'),
  ('gmx.com'), ('gmx.de'), ('gmx.net'), ('web.de'), ('t-online.de'),
  ('mail.com'), ('email.com'), ('zoho.com'), ('zohomail.com'), ('fastmail.com'), ('fastmail.fm'), ('hey.com'),
  ('tutanota.com'), ('tutanota.de'), ('tuta.io'), ('tuta.com'),
  ('yandex.com'), ('yandex.ru'), ('mail.ru'), ('inbox.ru'), ('bk.ru'), ('list.ru'), ('rambler.ru'),
  ('qq.com'), ('163.com'), ('126.com'), ('sina.com'), ('naver.com'), ('daum.net'), ('hanmail.net'),
  ('orange.fr'), ('wanadoo.fr'), ('free.fr'), ('laposte.net'), ('libero.it'), ('virgilio.it'),
  ('comcast.net'), ('verizon.net'), ('att.net'), ('sbcglobal.net'), ('cox.net'), ('btinternet.com');

-- goal_email_domain is the domain of an email: after its last @, lower case, surrounding spaces
-- ignored; null when there is none.
CREATE FUNCTION goal_email_domain(p_email text) RETURNS text
LANGUAGE sql IMMUTABLE SET search_path FROM CURRENT
AS $$
  SELECT nullif(substring(lower(btrim(p_email)) FROM '@([^@]*)$'), '')
$$;

-- goal_set_signup_domain sets (p_domain) or clears (null) the sign-up domain of the transaction's
-- organization, for the owner p_user, stamped p_by and audited as org.signup_domain_set {domain,
-- join_role, from} or org.signup_domain_cleared {domain}; nothing to change audits nothing. It
-- refuses, as check_violation with the constraint named: a p_user who is not an owner of it
-- (goal_signup_domain_owner), a domain p_user's own sign-in email is not at
-- (goal_signup_domain_not_yours), a public webmail domain (goal_signup_domain_public), and a domain
-- another live organization holds (goal_signup_domain_taken). A deleted organization's row of the
-- domain is taken over. Two organizations claiming at once: the unique key refuses the second.
-- +goose StatementBegin
CREATE FUNCTION goal_set_signup_domain(p_user uuid, p_domain text, p_role text, p_by text, p_request_id text) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_org    uuid := goal_org();
  v_domain text := lower(btrim(p_domain));
  v_old    org_signup_domains%ROWTYPE;
BEGIN
  PERFORM 1 FROM organizations WHERE id = v_org AND deleted_at IS NULL FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'organization % does not exist', v_org USING ERRCODE = 'no_data_found';
  END IF;
  PERFORM 1 FROM auth.member WHERE "organizationId" = v_org AND "userId" = p_user AND role = 'owner';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'user % is not an owner of organization %', p_user, v_org
      USING ERRCODE = 'check_violation', CONSTRAINT = 'goal_signup_domain_owner';
  END IF;
  SELECT * INTO v_old FROM org_signup_domains WHERE org_id = v_org FOR UPDATE;

  IF v_domain IS NULL THEN
    IF v_old.org_id IS NOT NULL THEN
      DELETE FROM org_signup_domains WHERE org_id = v_org;
      PERFORM goal_append_audit(v_org, p_by, 'org.signup_domain_cleared', 'org', v_org::text,
                                jsonb_build_object('domain', v_old.domain), p_request_id);
    END IF;
    RETURN;
  END IF;

  IF goal_email_domain((SELECT email FROM auth."user" WHERE id = p_user)) IS DISTINCT FROM v_domain THEN
    RAISE EXCEPTION 'the domain is not that of the owner''s own email'
      USING ERRCODE = 'check_violation', CONSTRAINT = 'goal_signup_domain_not_yours';
  END IF;
  IF EXISTS (SELECT 1 FROM public_email_domains WHERE domain = v_domain) THEN
    RAISE EXCEPTION 'a public email domain admits anyone'
      USING ERRCODE = 'check_violation', CONSTRAINT = 'goal_signup_domain_public';
  END IF;
  IF EXISTS (SELECT 1 FROM org_signup_domains s JOIN organizations o ON o.id = s.org_id AND o.deleted_at IS NULL
             WHERE s.domain = v_domain AND s.org_id <> v_org) THEN
    RAISE EXCEPTION 'another organization holds the domain'
      USING ERRCODE = 'check_violation', CONSTRAINT = 'goal_signup_domain_taken';
  END IF;
  IF v_old.domain IS NOT DISTINCT FROM v_domain AND v_old.join_role IS NOT DISTINCT FROM p_role THEN
    RETURN;
  END IF;

  DELETE FROM org_signup_domains s USING organizations o
  WHERE s.domain = v_domain AND s.org_id <> v_org AND o.id = s.org_id AND o.deleted_at IS NOT NULL;
  INSERT INTO org_signup_domains (org_id, domain, join_role, set_by, set_at)
  VALUES (v_org, v_domain, p_role, p_by, now())
  ON CONFLICT (org_id) DO UPDATE SET domain = EXCLUDED.domain, join_role = EXCLUDED.join_role,
                                     set_by = EXCLUDED.set_by, set_at = EXCLUDED.set_at;
  PERFORM goal_append_audit(v_org, p_by, 'org.signup_domain_set', 'org', v_org::text,
                            jsonb_build_object('domain', v_domain, 'join_role', p_role,
                                               'from', CASE WHEN v_old.org_id IS NULL THEN NULL
                                                            ELSE jsonb_build_object('domain', v_old.domain, 'join_role', v_old.join_role) END),
                            p_request_id);
END
$$;
-- +goose StatementEnd

-- goal_signup_domain_org is the live organization whose sign-up domain is exactly p_email's
-- domain, with its join role: the sign-up gate's third case. No row for any other address, a
-- public webmail domain, or a deleted organization.
CREATE FUNCTION goal_signup_domain_org(p_email text) RETURNS TABLE (org_id uuid, join_role text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT s.org_id, s.join_role
  FROM org_signup_domains s
  JOIN organizations o ON o.id = s.org_id AND o.deleted_at IS NULL
  WHERE s.domain = goal_email_domain(p_email)
    AND NOT EXISTS (SELECT 1 FROM public_email_domains p WHERE p.domain = s.domain)
$$;

-- goal_apply_signup_domain joins the user p_user, whose verified sign-in email is at a live
-- organization's sign-up domain, to that organization: a member stamped system:signup-domain
-- (member.added {via: signup-domain}), with the join role on each of its factories, each audited
-- as factory_role.granted {via: signup-domain}. It reads the email from the user, so it cannot be
-- pointed at an address the user does not hold. It returns the organization, or null when it did
-- nothing: no such organization, an unverified email, a pending invitation to that organization
-- (the invitation decides the role), or a membership already there, so a second call is harmless.
-- +goose StatementBegin
CREATE FUNCTION goal_apply_signup_domain(p_user uuid) RETURNS uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_email text;
  v_org   uuid;
  v_role  text;
BEGIN
  SELECT email INTO v_email FROM auth."user" WHERE id = p_user AND "emailVerified";
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  SELECT d.org_id, d.join_role INTO v_org, v_role FROM goal_signup_domain_org(v_email) d;
  IF v_org IS NULL THEN
    RETURN NULL;
  END IF;
  PERFORM 1 FROM organizations WHERE id = v_org AND deleted_at IS NULL FOR SHARE;
  IF NOT FOUND OR EXISTS (SELECT 1 FROM auth.invitation
                          WHERE "organizationId" = v_org AND lower(email) = lower(v_email) AND status = 'pending') THEN
    RETURN NULL;
  END IF;

  INSERT INTO auth.member (id, "organizationId", "userId", role, "createdAt", "changedBy", "changedAt")
  VALUES (goal_uuidv7(), v_org, p_user, 'member', now(), 'system:signup-domain', clock_timestamp())
  ON CONFLICT ("organizationId", "userId") DO NOTHING;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;

  WITH granted AS (
    INSERT INTO factory_roles (org_id, factory_id, user_id, role, granted_by)
    SELECT v_org, f.id, p_user, v_role, 'system:signup-domain' FROM factories f WHERE f.org_id = v_org
    ON CONFLICT DO NOTHING
    RETURNING factory_id, role
  )
  INSERT INTO audit_log (org_id, actor_kind, actor_id, actor_name, action, target_kind, target_id, factory_id, data)
  SELECT v_org, 'system', 'signup-domain', 'signup-domain', 'factory_role.granted', 'member', p_user::text, g.factory_id,
         jsonb_build_object('user_id', p_user, 'factory_id', g.factory_id, 'to', g.role, 'via', 'signup-domain')
  FROM granted g;
  RETURN v_org;
END
$$;
-- +goose StatementEnd

-- 00043's member audit, with via signup-domain for goal_apply_signup_domain's members. CREATE OR
-- REPLACE keeps its triggers.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_audit_member() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_by text;
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = NEW."organizationId") THEN
      RETURN NULL;
    END IF;
    v_by := goal_auth_actor(NEW."changedBy", NEW."changedAt", 'user:' || NEW."userId");
    PERFORM goal_append_audit(NEW."organizationId", v_by, 'member.added', 'member', NEW."userId"::text,
      jsonb_build_object('user_id', NEW."userId", 'role', NEW.role, 'via', CASE
        WHEN v_by = 'system:bootstrap' THEN 'bootstrap'
        WHEN v_by = 'system:recovery' THEN 'recovery'
        WHEN v_by = 'system:signup-domain' THEN 'signup-domain'
        WHEN EXISTS (SELECT 1 FROM auth.invitation i JOIN auth."user" u ON lower(u.email) = lower(i.email)
                     WHERE i."organizationId" = NEW."organizationId" AND u.id = NEW."userId" AND i.status = 'accepted')
          THEN 'invitation'
        ELSE 'org_created' END), NULL);
  ELSIF TG_OP = 'UPDATE' THEN
    IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = NEW."organizationId") THEN
      RETURN NULL;
    END IF;
    v_by := goal_auth_actor(NEW."changedBy", NEW."changedAt", 'system:auth');
    PERFORM goal_append_audit(NEW."organizationId", v_by, 'member.role_changed', 'member', NEW."userId"::text,
      jsonb_build_object('user_id', NEW."userId", 'from', OLD.role, 'to', NEW.role), NULL);
  ELSE
    IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = OLD."organizationId") THEN
      RETURN NULL;
    END IF;
    v_by := goal_auth_actor(OLD."changedBy", OLD."changedAt", 'system:auth');
    PERFORM goal_append_audit(OLD."organizationId", v_by, 'member.removed', 'member', OLD."userId"::text,
      jsonb_build_object('user_id', OLD."userId", 'role', OLD.role, 'left', v_by = 'user:' || OLD."userId"), NULL);
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION goal_email_domain(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_set_signup_domain(uuid, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_signup_domain_org(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_apply_signup_domain(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_set_signup_domain(uuid, text, text, text, text) TO goal_app;
GRANT EXECUTE ON FUNCTION goal_signup_domain_org(text) TO goal_auth;
GRANT EXECUTE ON FUNCTION goal_apply_signup_domain(uuid) TO goal_auth;
