-- +goose Up
-- Phase 3 fix (review M1): every session records how the person signed in, so goald and the auth
-- service tell the browser from goal login by the session itself, not by how its token arrives.
-- goald does not check the session cookie's signature (it holds no Better Auth secret), so a CLI
-- session sent as a cookie with any signature used to pass for the browser's. The auth service
-- writes "signInMethod" when Better Auth creates the session (a Better Auth additional field,
-- input: false): email_code (/sign-in/email-otp), google (/callback/:id, /sign-in/social), device
-- (/device/token, goal login) or other. Sessions created before this migration have none and count
-- as not the browser's: they cannot create a personal token or approve a CLI sign-in, which a
-- fresh sign-in (10 minutes) needs anyway. goal_principal now returns the method.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

ALTER TABLE auth.session ADD COLUMN "signInMethod" text
  CONSTRAINT session_sign_in_method CHECK ("signInMethod" IN ('email_code', 'google', 'device', 'other'));

-- 00045's goal_principal with sign_in_method, the session's "signInMethod" (null when untagged).
-- CREATE OR REPLACE keeps goal_app's EXECUTE.
CREATE OR REPLACE FUNCTION goal_principal(p_token text, p_org uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT jsonb_build_object(
    'user_id', u.id, 'name', u.name, 'email', u.email,
    'session_id', s.id, 'created_at', s."createdAt", 'expires_at', s."expiresAt",
    'sign_in_method', s."signInMethod",
    'platform_admin', EXISTS (SELECT 1 FROM platform_admins pa WHERE pa.user_id = u.id),
    'org_role', m.role,
    'factory_roles', coalesce((
      SELECT jsonb_agg(jsonb_build_object('factory_id', fr.factory_id, 'role', fr.role) ORDER BY fr.factory_id)
      FROM factory_roles fr WHERE m.role IS NOT NULL AND fr.org_id = m."organizationId" AND fr.user_id = u.id), '[]'::jsonb))
  FROM auth.session s
  JOIN auth."user" u ON u.id = s."userId"
  LEFT JOIN organizations o ON o.id = p_org AND o.deleted_at IS NULL
  LEFT JOIN auth.member m ON m."organizationId" = o.id AND m."userId" = u.id
  WHERE s.token = p_token AND s."expiresAt" > now()
$$;
