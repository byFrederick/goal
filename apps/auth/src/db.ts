import pg from 'pg';

/** openPool connects to url (the database as goal_auth); the caller ends it. */
export function openPool(url: string): pg.Pool {
  return new pg.Pool({ connectionString: url, max: 10, application_name: 'goal-auth' });
}

/** RoleError says why the service refuses the role it connected as. */
export class RoleError extends Error {
  constructor(
    readonly role: string,
    readonly reasons: string[],
  ) {
    super(`role ${JSON.stringify(role)} ${reasons.join(', ')}; the auth service connects as goal_auth (goal db init)`);
    this.name = 'RoleError';
  }
}

/**
 * checkRole refuses to run as a role that could do more than Better Auth's DML on schema auth
 * (spec 3 §6.0): a superuser, a role with BYPASSRLS, the owner of a table, or a member of
 * goal_owner or goal_app.
 */
export async function checkRole(pool: pg.Pool): Promise<void> {
  const { rows } = await pool.query<{
    role: string;
    superuser: boolean;
    bypassrls: boolean;
    owner: boolean;
    app: boolean;
    tables: boolean;
  }>(`
    SELECT r.rolname AS role, r.rolsuper AS superuser, r.rolbypassrls AS bypassrls,
           pg_has_role(r.oid, 'goal_owner', 'MEMBER') AS owner,
           pg_has_role(r.oid, 'goal_app', 'MEMBER') AS app,
           EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner = r.oid) AS tables
    FROM pg_roles r WHERE r.rolname = current_user`);
  const r = rows[0];
  if (r === undefined) throw new RoleError('?', ['is not in pg_roles']);

  const reasons = [
    r.superuser && 'is a superuser',
    r.bypassrls && 'has BYPASSRLS',
    r.tables && 'owns tables',
    r.owner && 'is a member of goal_owner',
    r.app && 'is a member of goal_app',
  ].filter((x): x is string => typeof x === 'string');
  if (reasons.length > 0) throw new RoleError(r.role, reasons);
}
