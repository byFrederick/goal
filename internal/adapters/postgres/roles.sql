-- Goal's database roles. Cluster-wide, so they live outside the migrations: `goal db init`, the
-- integration-test harness and `task migrate-check` run this file before migrating. Idempotent and
-- safe to run concurrently: an existing role is left as it is (a concurrent create shows up as
-- duplicate_object or unique_violation). All start without LOGIN; goal db init gives them LOGIN
-- and a password.
--
-- goal_owner owns the schema and runs the migrations. goal_app runs goald: it owns nothing, so
-- row-level security binds it, and every migration grants it exactly what it needs. goal_auth runs
-- the identity service (auth/): it owns nothing either, has DML on Better Auth's tables in schema
-- auth only (00041) and no privilege on Goal's tables; migration 00040 grants to it, so goald
-- refuses to migrate while it is missing.
DO $$ BEGIN
  CREATE ROLE goal_owner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB;
EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$;

DO $$ BEGIN
  CREATE ROLE goal_app NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOINHERIT;
EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$;

DO $$ BEGIN
  CREATE ROLE goal_auth NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOINHERIT;
EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$;
