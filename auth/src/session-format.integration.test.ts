// The session format contract (spec 3 §13, ADR 0007): goald reads Better Auth's sessions itself,
// per request, from the cookie or a bearer token and auth.session, without Better Auth's secret.
// These tests pin what it relies on, for the pinned better-auth: the cookie's name and attributes,
// its value `<token>.<signature>` whose token is auth.session.token, the device grant's access
// token being that raw token, and the shape of auth.session. An upgrade that changes any of it
// fails here before goald misreads a session.
import pg from 'pg';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { cliClientId } from './auth.js';
import { createTestDatabase, type TestDatabase } from './testing/database.js';
import { harness } from './testing/service.js';

const email = 'ada@example.com';

let db: TestDatabase;
let pool: pg.Pool;

beforeAll(async () => {
  db = await createTestDatabase();
  pool = new pg.Pool({ connectionString: db.url('goal_auth') });
  // Sign-up is invite-only (spec 3 §8.7); the user exists, as after their first sign-in.
  await pool.query(`INSERT INTO auth."user" (name, email, "emailVerified") VALUES ('Ada', $1, true)`, [email]);
});

afterAll(async () => {
  await pool?.end();
  await db?.drop();
});

/** service is the auth service's app at publicURL; signIn signs the test's user in by code. */
function service(publicURL: string) {
  const h = harness(pool, { publicURL });
  return { ...h, signIn: async (address = email) => (await h.signIn(address)).res };
}

interface Cookie {
  name: string;
  value: string;
  attributes: Map<string, string>;
}

function sessionCookie(res: Response): Cookie {
  const raw = res.headers.getSetCookie().find((c) => c.includes('session_token='));
  if (!raw) throw new Error(`no session cookie in ${JSON.stringify(res.headers.getSetCookie())}`);
  const [pair = '', ...rest] = raw.split(';').map((p) => p.trim());
  const at = pair.indexOf('=');
  const attributes = new Map(
    rest.map((a) => {
      const [k = '', v = ''] = a.split('=');
      return [k.toLowerCase(), v];
    }),
  );
  return { name: pair.slice(0, at), value: pair.slice(at + 1), attributes };
}

async function sessionTokens(): Promise<string[]> {
  const { rows } = await pool.query<{ token: string }>(
    `SELECT s.token FROM auth.session s JOIN auth."user" u ON u.id = s."userId" WHERE u.email = $1`,
    [email],
  );
  return rows.map((r) => r.token);
}

describe('the session cookie', () => {
  it('is goal.session_token on http: HttpOnly, SameSite=Lax, Path=/, not Secure, <token>.<signature>', async () => {
    const res = await service('http://127.0.0.1:8080').signIn();
    expect(res.status).toBe(200);

    const cookie = sessionCookie(res);
    expect(cookie.name).toBe('goal.session_token');
    expect(cookie.attributes.has('httponly')).toBe(true);
    expect(cookie.attributes.get('samesite')?.toLowerCase()).toBe('lax');
    expect(cookie.attributes.get('path')).toBe('/');
    expect(cookie.attributes.has('secure')).toBe(false);
    expect(cookie.attributes.get('max-age')).toBe(String(7 * 86400));

    // goald URL-decodes the value and takes the part before the first dot (spec 3 §8.3).
    const [, token, signature] = /^([A-Za-z0-9]{32})\.(.+)$/.exec(decodeURIComponent(cookie.value)) ?? [];
    expect(token).toBeDefined();
    expect(signature).not.toContain('.');
    expect(await sessionTokens()).toContain(token);
  });

  it('is __Secure-goal.session_token and Secure when GOAL_PUBLIC_URL is https', async () => {
    const res = await service('https://goal.example.com').signIn();
    expect(res.status).toBe(200);

    const cookie = sessionCookie(res);
    expect(cookie.name).toBe('__Secure-goal.session_token');
    expect(cookie.attributes.has('secure')).toBe(true);
    expect(cookie.attributes.has('httponly')).toBe(true);
  });
});

describe('the device grant', () => {
  it('gives `goal login` the raw 32-character session token, which bearer accepts', async () => {
    const s = service('http://127.0.0.1:8080');
    const cookie = sessionCookie(await s.signIn());

    const code = await s.post('/device/code', { client_id: cliClientId });
    expect(code.status).toBe(200);
    const { device_code: deviceCode, user_code: userCode } = (await code.json()) as Record<string, string>;
    expect(userCode).toMatch(/^[A-Z0-9]{8}$/);

    // Device-Approve opens the code first, which claims it for the signed-in user, then approves.
    const signedIn = { cookie: `${cookie.name}=${cookie.value}`, origin: 'http://127.0.0.1:8080' };
    const claim = await s.app.request(`/api/auth/device?user_code=${userCode}`, { headers: signedIn });
    expect(claim.status).toBe(200);

    const approve = await s.post('/device/approve', { userCode }, signedIn);
    expect(approve.status).toBe(200);

    const token = await s.post('/device/token', {
      grant_type: 'urn:ietf:params:oauth:grant-type:device_code',
      device_code: deviceCode,
      client_id: cliClientId,
    });
    expect(token.status).toBe(200);
    const { access_token: accessToken } = (await token.json()) as Record<string, string>;
    expect(accessToken).toMatch(/^[A-Za-z0-9]{32}$/);
    expect(await sessionTokens()).toContain(accessToken);

    const session = await s.app.request('/api/auth/get-session', {
      headers: { authorization: `Bearer ${accessToken}` },
    });
    expect(((await session.json()) as { user: { email: string } }).user.email).toBe(email);
  });

  it('serves no other client', async () => {
    const res = await service('http://127.0.0.1:8080').post('/device/code', { client_id: 'another-cli' });

    expect(res.status).toBe(400);
  });
});

describe('auth.session', () => {
  it('has the columns goald reads, a unique token and UUIDv7 ids from goal_uuidv7()', async () => {
    const { rows: columns } = await pool.query<{ name: string; type: string; nullable: string }>(`
      SELECT column_name AS name, data_type AS type, is_nullable AS nullable FROM information_schema.columns
      WHERE table_schema = 'auth' AND table_name = 'session' ORDER BY column_name`);
    expect(columns).toEqual([
      { name: 'activeOrganizationId', type: 'text', nullable: 'YES' },
      { name: 'createdAt', type: 'timestamp with time zone', nullable: 'NO' },
      { name: 'expiresAt', type: 'timestamp with time zone', nullable: 'NO' },
      { name: 'id', type: 'uuid', nullable: 'NO' },
      { name: 'ipAddress', type: 'text', nullable: 'YES' },
      { name: 'token', type: 'text', nullable: 'NO' },
      { name: 'updatedAt', type: 'timestamp with time zone', nullable: 'NO' },
      { name: 'userAgent', type: 'text', nullable: 'YES' },
      { name: 'userId', type: 'uuid', nullable: 'NO' },
    ]);

    const { rows: unique } = await pool.query(`
      SELECT 1 FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
      WHERE i.indrelid = 'auth.session'::regclass AND i.indisunique AND i.indnatts = 1 AND a.attname = 'token'`);
    expect(unique).toHaveLength(1);

    const { rows: sessions } = await pool.query<{ id: string; ip: string; expires: boolean }>(`
      SELECT id, "ipAddress" AS ip, "expiresAt" BETWEEN now() + interval '6 days 23 hours' AND now() + interval '7 days 1 minute' AS expires
      FROM auth.session`);
    expect(sessions.length).toBeGreaterThan(0);
    for (const s of sessions) {
      expect(s.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
      expect(s.ip).toMatch(/^198\.51\.100\.\d+$/);
      expect(s.expires).toBe(true);
    }
  });
});
