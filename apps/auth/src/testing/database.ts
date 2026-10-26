// A throwaway database for the auth service's integration tests, in the PostgreSQL at
// GOAL_TEST_DATABASE_URL (compose's, as its superuser; task test-integration sets it): the roles of
// roles.sql, the grants goal db init gives, then the goose migrations as goal_owner, as goald
// applies them. Its URLs switch role with the startup parameter, so no role's password is needed
// or changed. drop removes it.
import { randomBytes } from 'node:crypto';
import { readdir, readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import pg from 'pg';

const repo = fileURLToPath(new URL('../../../../', import.meta.url));

export interface TestDatabase {
  name: string;
  /** url connects to the database as the superuser, or switches to role. */
  url(role?: string): string;
  drop(): Promise<void>;
}

/** createTestDatabase migrates a new database to upTo (a migration's number prefix) or to the head. */
export async function createTestDatabase(upTo?: string): Promise<TestDatabase> {
  const base = process.env.GOAL_TEST_DATABASE_URL;
  if (!base) throw new Error('GOAL_TEST_DATABASE_URL is not set; run task test-integration');

  const name = `goal_test_auth_${randomBytes(6).toString('hex')}`;
  const url = (role?: string) => {
    const u = new URL(base);
    u.pathname = `/${name}`;
    if (role) u.searchParams.set('options', `-c role=${role}`);
    return u.toString();
  };

  const roles = await readFile(`${repo}internal/adapters/postgres/roles.sql`, 'utf8');
  await withClient(base, async (c) => {
    await c.query(`BEGIN; SELECT pg_advisory_xact_lock(hashtext('goal.roles'));\n${roles}\n;\nCOMMIT`);
    await c.query(`CREATE DATABASE ${name}`);
  });

  const files = (await readdir(`${repo}db/migrations`)).filter((f) => f.endsWith('.sql')).sort();
  await withClient(url(), async (c) => {
    await c.query(`
      REVOKE TEMPORARY, CONNECT ON DATABASE ${name} FROM PUBLIC;
      GRANT CONNECT, CREATE ON DATABASE ${name} TO goal_owner;
      GRANT CONNECT ON DATABASE ${name} TO goal_app, goal_auth;
      GRANT USAGE, CREATE ON SCHEMA public TO goal_owner;
      GRANT USAGE ON SCHEMA public TO goal_app, goal_auth;
      CREATE EXTENSION IF NOT EXISTS vector`);
    for (const f of files) {
      // A goose file is plain SQL whose annotations are comments; each runs in one transaction.
      const sql = await readFile(`${repo}db/migrations/${f}`, 'utf8');
      await c.query(`BEGIN; SET LOCAL ROLE goal_owner;\n${sql}\n;\nCOMMIT`);
      if (upTo && f.startsWith(upTo)) break;
    }
  });

  return {
    name,
    url,
    drop: () => withClient(base, (c) => c.query(`DROP DATABASE IF EXISTS ${name} WITH (FORCE)`).then(() => {})),
  };
}

async function withClient<T>(url: string, fn: (c: pg.Client) => Promise<T>): Promise<T> {
  const c = new pg.Client({ connectionString: url });
  await c.connect();
  try {
    return await fn(c);
  } finally {
    await c.end();
  }
}
