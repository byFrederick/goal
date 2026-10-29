// Sign-in (spec 3 §8.7, threat model §11.1 rows 10–13): email codes, who gets one, the send cap,
// the lockout, the sign-up gate for codes and Google, the bootstrap owner, and Better Auth's rate
// limits, on PostgreSQL with Goal's migrations. Google's token endpoint is stubbed; nothing here
// reaches Google or a mail server.
import { randomUUID } from 'node:crypto';
import pg from 'pg';
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { emailSubject, GoalDatabase } from './goal.js';
import { MemoryMailer } from './mail/memory.js';
import { limits, SignIn } from './signin.js';
import { createTestDatabase, type TestDatabase } from './testing/database.js';
import { cookieHeader, Fixtures, type HarnessOptions, harness, silent } from './testing/service.js';

let db: TestDatabase;
let pool: pg.Pool;
let admin: pg.Pool;
let fx: Fixtures;
let goal: GoalDatabase;
let org: string;
let inviter: string;

beforeAll(async () => {
  db = await createTestDatabase();
  pool = new pg.Pool({ connectionString: db.url('goal_auth') });
  admin = new pg.Pool({ connectionString: db.url() });
  fx = new Fixtures(admin);
  goal = new GoalDatabase(pool);
  org = await fx.org('acme', 'Acme');
  inviter = await fx.user('okafor@example.com', 'M. Okafor');
  await fx.member(org, inviter, 'owner');
});

afterAll(async () => {
  await Promise.all([pool?.end(), admin?.end()]);
  await db?.drop();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

let n = 0;
/** address is a new address for each use, so tests never share caps or lockouts. */
function address(name: string): string {
  return `${name}-${++n}-${randomUUID().slice(0, 8)}@example.com`;
}

const app = (opts: HarnessOptions = {}) => harness(pool, opts);

async function users(email: string): Promise<number> {
  const { rows } = await admin.query(`SELECT 1 FROM auth."user" WHERE email = $1`, [email]);
  return rows.length;
}

describe('a sign-in code', () => {
  it('is 6 digits, works once, and is stored hashed under a hashed identifier', async () => {
    const email = address('ada');
    await fx.user(email);
    const h = app();

    const code = await h.requestCode(email);
    expect(code).toMatch(/^\d{6}$/);
    const { rows } = await admin.query<{ identifier: string; value: string }>(
      `SELECT identifier, value FROM auth.verification`,
    );
    expect(rows.length).toBeGreaterThan(0);
    for (const r of rows) {
      expect(r.identifier).not.toContain('@');
      expect(r.value).not.toContain(code);
    }

    const first = await h.post('/sign-in/email-otp', { email, otp: code });
    expect(first.status).toBe(200);
    expect(cookieHeader(first)).toContain('goal.session_token=');
    expect((await h.post('/sign-in/email-otp', { email, otp: code })).status).toBe(400);
  });

  it('expires after 5 minutes', async () => {
    const email = address('slow');
    await fx.user(email);
    const h = app();
    const code = await h.requestCode(email);

    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(Date.now() + 301_000);
    const res = await h.post('/sign-in/email-otp', { email, otp: code });

    expect(res.status).toBe(400);
    expect(await res.json()).toMatchObject({ code: 'OTP_EXPIRED' });
  });

  it('allows 3 attempts, then it is void even when right', async () => {
    const email = address('guess');
    await fx.user(email);
    const h = app();
    const code = await h.requestCode(email);
    const wrong = code === '000000' ? '111111' : '000000';

    for (let i = 0; i < 3; i++) {
      expect((await h.post('/sign-in/email-otp', { email, otp: wrong })).status).toBe(400);
    }
    const res = await h.post('/sign-in/email-otp', { email, otp: code });

    expect(res.status).toBe(403);
    expect(await res.json()).toMatchObject({ code: 'TOO_MANY_ATTEMPTS' });
  });

  it('is never logged, nor its address or the session it signs in (row 24)', async () => {
    const email = address('quiet');
    await fx.user(email);
    const lines: string[] = [];
    const h = app({ log: { log: (level, event, fields) => lines.push(JSON.stringify({ level, event, ...fields })) } });

    const code = await h.requestCode(email);
    const res = await h.post('/sign-in/email-otp', { email, otp: code });
    expect(res.status).toBe(200);
    const token = /goal\.session_token=([^.;]+)/.exec(cookieHeader(res))?.[1] ?? '';
    expect(token).not.toBe('');

    expect(lines.length).toBeGreaterThan(0);
    for (const secret of [code, email, token]) expect(lines.join('\n')).not.toContain(secret);
  });

  it('is sent only for signing in', async () => {
    const email = address('verify');
    await fx.user(email);

    const res = await app().post('/email-otp/send-verification-otp', { email, type: 'forget-password' });

    expect(res.status).toBe(400);
    expect(await res.json()).toMatchObject({ code: 'SIGN_IN_ONLY' });
  });
});

describe('who gets a code', () => {
  it('a user and an invitee get one; a stranger gets the same answer and no mail', async () => {
    const user = address('user');
    await fx.user(user);
    const invitee = address('invitee');
    await fx.invitation(org, invitee, inviter);
    const stranger = address('stranger');
    const h = app();

    const answers = [];
    for (const email of [user, invitee, stranger]) {
      const res = await h.post('/email-otp/send-verification-otp', { email, type: 'sign-in' });
      answers.push({ status: res.status, body: await res.text() });
    }

    expect(answers).toEqual(Array(3).fill({ status: 200, body: JSON.stringify({ success: true }) }));
    await vi.waitFor(() => {
      expect(h.codes(user)).toHaveLength(1);
      expect(h.codes(invitee)).toHaveLength(1);
    });
    await vi.waitFor(async () =>
      expect((await fx.events('otp.suppressed')).filter((e) => e.subject === emailSubject(stranger))).toEqual([
        expect.objectContaining({ ip: h.ip, data: { reason: 'unknown' } }),
      ]),
    );
    expect(h.mailer.to(stranger)).toEqual([]);
  });

  it('answers before the mail is sent, so a slow mailer cannot tell users from strangers', async () => {
    const email = address('timed');
    await fx.user(email);
    const h = app({ mailer: new MemoryMailer(1500) });

    const started = performance.now();
    const res = await h.post('/email-otp/send-verification-otp', { email, type: 'sign-in' });
    const took = performance.now() - started;

    expect(res.status).toBe(200);
    expect(took).toBeLessThan(750);
    expect(h.codes(email)).toHaveLength(0);
    await vi.waitFor(() => expect(h.codes(email)).toHaveLength(1), { timeout: 5000 });
  });

  it(`gets at most ${limits.codesPerHour} an hour`, async () => {
    const email = address('bombed');
    await fx.user(email);
    const mailer = new MemoryMailer();
    let now = Date.now();
    const clock = () => new Date(now);

    // Better Auth's per-address-of-the-client limit is 3 a minute, so each burst comes from a new client.
    for (let i = 0; i < limits.codesPerHour + 1; i++) {
      const res = await app({ mailer, clock }).post('/email-otp/send-verification-otp', { email, type: 'sign-in' });
      expect(res.status).toBe(200);
      await vi.waitFor(async () => expect(await sends(email)).toBe(i + 1));
    }
    expect(mailer.to(email)).toHaveLength(limits.codesPerHour);
    expect((await fx.events('otp.suppressed')).filter((e) => e.subject === emailSubject(email))).toEqual([
      expect.objectContaining({ data: { reason: 'cap' } }),
    ]);

    now += 3600_000 + 60_000;
    await app({ mailer, clock }).requestCode(email);
    expect(mailer.to(email)).toHaveLength(limits.codesPerHour + 1);
  });
});

/** sends counts the send decisions (sent or suppressed) recorded for email. */
async function sends(email: string): Promise<number> {
  const { rows } = await admin.query(
    `SELECT 1 FROM auth_events WHERE kind IN ('otp.sent', 'otp.suppressed') AND subject = $1`,
    [emailSubject(email)],
  );
  return rows.length;
}

describe('the lockout', () => {
  it(`locks an address after ${limits.failuresPerDay} failed codes in a day and mails its owner once`, async () => {
    const email = address('locked');
    await fx.user(email);
    const mailer = new MemoryMailer();
    let wrong: { status: number; body: string } | undefined;

    // 5 failures per client a minute is Better Auth's limit; spread them over clients.
    for (let i = 0; i < limits.failuresPerDay + 2; i++) {
      const res = await app({ mailer }).post('/sign-in/email-otp', { email, otp: '000000' });
      const answer = { status: res.status, body: await res.text() };
      wrong ??= answer;
      expect(answer).toEqual(wrong);
    }

    expect(wrong?.status).toBe(400);
    expect(await goal.count('otp.failed', emailSubject(email), new Date(Date.now() - 60_000))).toBe(
      limits.failuresPerDay,
    );
    expect((await fx.events('otp.locked')).filter((e) => e.subject === emailSubject(email))).toHaveLength(1);
    await vi.waitFor(() => expect(mailer.to(email).map((m) => m.subject)).toEqual(['Goal sign-in by code is locked']));

    const h = app({ mailer });
    await h.post('/email-otp/send-verification-otp', { email, type: 'sign-in' });
    await vi.waitFor(async () =>
      expect((await fx.events('otp.suppressed')).filter((e) => e.subject === emailSubject(email))).toEqual([
        expect.objectContaining({ data: { reason: 'locked' } }),
      ]),
    );
    expect(h.codes(email)).toEqual([]);
  });

  it('answers even the right code like a wrong one, until the oldest failure is a day old', async () => {
    const email = address('patient');
    await fx.user(email);
    let now = Date.now();
    const clock = () => new Date(now);
    const h = app({ clock });
    const code = await h.requestCode(email);
    for (let i = 0; i < limits.failuresPerDay; i++) {
      await goal.record({ kind: 'otp.failed', subject: emailSubject(email) });
    }

    const locked = await h.post('/sign-in/email-otp', { email, otp: code });
    expect(locked.status).toBe(400);
    expect(await locked.text()).toBe(JSON.stringify({ message: 'Invalid OTP', code: 'INVALID_OTP' }));

    now += 24 * 3600_000 + 60_000;
    expect((await h.post('/sign-in/email-otp', { email, otp: code })).status).toBe(200);
  });
});

/** codeFor makes a sign-in code as Better Auth's server API would, sent or not. */
async function codeFor(h: ReturnType<typeof app>, email: string): Promise<string> {
  return h.auth.api.createVerificationOTP({ body: { email, type: 'sign-in' } });
}

describe('sign-up', () => {
  it('creates the user of a pending invitee on their first sign-in', async () => {
    const email = address('newcomer');
    await fx.invitation(org, email, inviter);

    const { res } = await app().signIn(email);

    expect(res.status).toBe(200);
    const { rows } = await admin.query(`SELECT id, "emailVerified" FROM auth."user" WHERE email = $1`, [email]);
    expect(rows).toEqual([{ id: expect.any(String), emailVerified: true }]);
    expect((await fx.events('user.created')).filter((e) => e.user_id === rows[0].id)).toEqual([
      expect.objectContaining({ data: { method: 'email_code' } }),
    ]);
    expect((await fx.events('sign_in.succeeded')).filter((e) => e.user_id === rows[0].id)).toEqual([
      expect.objectContaining({ data: { method: 'email_code' } }),
    ]);
  });

  it.each([
    ['a stranger', async () => {}],
    [
      'an expired invitation',
      async (email: string) => {
        await fx.invitation(org, email, inviter, { expiresAt: new Date(Date.now() - 1000) });
      },
    ],
    [
      'a pending invitation of a deleted organization (review L1)',
      async (email: string) => {
        const gone = await fx.org(`gone-${randomUUID().slice(0, 8)}`, 'Gone');
        await admin.query(`UPDATE organizations SET deleted_at = now() WHERE id = $1`, [gone]);
        await fx.invitationAsBefore00049(gone, email, inviter);
      },
    ],
  ])('refuses %s, even with the right code', async (_, setup) => {
    const email = address('refused');
    await setup(email);
    const h = app();

    const res = await h.post('/sign-in/email-otp', { email, otp: await codeFor(h, email) });

    expect(res.status).toBe(403);
    expect(await res.json()).toMatchObject({ code: 'SIGN_UP_NOT_ALLOWED' });
    expect(cookieHeader(res)).not.toContain('session_token=');
    expect(await users(email)).toBe(0);
    expect((await fx.events('sign_up.refused')).filter((e) => e.subject === emailSubject(email))).toEqual([
      expect.objectContaining({ data: { method: 'email_code' } }),
    ]);
  });
});

describe('sign-up by domain (ADR 0023, decision 7)', () => {
  let byDomain: string;
  let factory: string;
  const at = (name: string) => `${name}-${++n}-${randomUUID().slice(0, 8)}@acme.example.com`;

  beforeAll(async () => {
    byDomain = await fx.org(`domain-${randomUUID().slice(0, 8)}`, 'Acme by domain');
    factory = await fx.factory(byDomain, 'line');
    await fx.signupDomain(byDomain, 'acme.example.com', 'approver');
  });

  async function membership(email: string) {
    const { rows } = await admin.query<{ role: string; factory_role: string | null }>(
      `SELECT m.role, (SELECT r.role FROM factory_roles r WHERE r.user_id = u.id AND r.factory_id = $3) AS factory_role
       FROM auth."user" u JOIN auth.member m ON m."userId" = u.id AND m."organizationId" = $2 WHERE u.email = $1`,
      [email, byDomain, factory],
    );
    return rows;
  }

  it('admits a verified email at the domain, sends it a code, and joins it with the join role', async () => {
    const email = at('ada');
    const mailer = new MemoryMailer();

    const { res } = await app({ mailer }).signIn(email);

    expect(res.status).toBe(200);
    expect(mailer.sent.filter((m) => m.to === email)).toHaveLength(1);
    expect(await membership(email)).toEqual([{ role: 'member', factory_role: 'approver' }]);
    const { rows } = await admin.query(
      `SELECT data->>'via' AS via FROM audit_log a JOIN auth."user" u ON a.target_id = u.id::text
       WHERE u.email = $1 AND a.action = 'member.added'`,
      [email],
    );
    expect(rows).toEqual([{ via: 'signup-domain' }]);
  });

  it.each([
    ['another domain', () => address('stranger')],
    ['a subdomain', () => `eu-${++n}-${randomUUID().slice(0, 8)}@eu.acme.example.com`],
    ['a domain that only ends like it', () => `x-${++n}-${randomUUID().slice(0, 8)}@notacme.example.com`],
  ])('refuses %s, even with the right code', async (_, make) => {
    const email = make();
    const h = app();

    const res = await h.post('/sign-in/email-otp', { email, otp: await codeFor(h, email) });

    expect(res.status).toBe(403);
    expect(await res.json()).toMatchObject({ code: 'SIGN_UP_NOT_ALLOWED' });
    expect(await users(email)).toBe(0);
    expect((await fx.events('sign_up.refused')).filter((e) => e.subject === emailSubject(email))).toEqual([
      expect.objectContaining({ data: { method: 'email_code' } }),
    ]);
  });

  it('refuses an email at the domain that is not verified', async () => {
    // Google's unverified emails stop before the hook, and every other way in verifies the email,
    // so the gate is called as Better Auth would call it.
    const email = at('unverified');
    const gate = new SignIn({
      db: goal,
      mailer: undefined,
      log: silent,
      clock: () => new Date(),
      bootstrapOwnerEmail: undefined,
    });

    await expect(
      gate.allowOnlyInvitedOrBootstrap(
        { email, emailVerified: false } as Parameters<typeof gate.allowOnlyInvitedOrBootstrap>[0],
        null,
      ),
    ).rejects.toMatchObject({ body: { code: 'SIGN_UP_NOT_ALLOWED' } });
    expect((await fx.events('sign_up.refused')).filter((e) => e.subject === emailSubject(email))).toHaveLength(1);
  });

  it('refuses the domain of a deleted organization', async () => {
    const gone = await fx.org(`gone-${randomUUID().slice(0, 8)}`, 'Gone');
    await fx.signupDomain(gone, 'gone.example.com');
    await admin.query(`UPDATE organizations SET deleted_at = now() WHERE id = $1`, [gone]);
    const email = `ada-${++n}@gone.example.com`;
    const h = app();

    const res = await h.post('/sign-in/email-otp', { email, otp: await codeFor(h, email) });

    expect(res.status).toBe(403);
    expect(await users(email)).toBe(0);
    expect((await fx.events('sign_up.refused')).filter((e) => e.subject === emailSubject(email))).toHaveLength(1);
  });

  it('still admits an invitee at the domain by the invitation, with its role', async () => {
    const email = at('invitee');
    await fx.invitation(byDomain, email, inviter, { role: 'admin' });

    const { res } = await app().signIn(email);

    expect(res.status).toBe(200);
    expect(await membership(email)).toEqual([]);
    const { rows } = await admin.query(`SELECT status FROM auth.invitation WHERE email = $1`, [email]);
    expect(rows).toEqual([{ status: 'pending' }]);
  });
});

describe('Google', () => {
  const google = {
    GOAL_AUTH_GOOGLE: 'on',
    GOOGLE_CLIENT_ID: 'goal-test.apps.example.com',
    GOOGLE_CLIENT_SECRET: 'test-only',
  };

  /** idToken is an unsigned ID token for claims, as Google's token endpoint would return one. */
  function idToken(claims: Record<string, unknown>): string {
    const part = (o: object) => Buffer.from(JSON.stringify(o)).toString('base64url');
    const now = Math.floor(Date.now() / 1000);
    return `${part({ alg: 'RS256', kid: 'test' })}.${part({
      iss: 'https://accounts.google.com',
      aud: google.GOOGLE_CLIENT_ID,
      iat: now,
      exp: now + 3600,
      ...claims,
    })}.signature`;
  }

  /** stubGoogle answers Google's token endpoint with tokens for claims; nothing else is stubbed. */
  function stubGoogle(claims: Record<string, unknown>) {
    const real = globalThis.fetch;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = input instanceof Request ? input.url : String(input);
      if (url.startsWith('https://oauth2.googleapis.com/token')) {
        return Response.json({
          access_token: 'google-access-token',
          refresh_token: 'google-refresh-token',
          id_token: idToken(claims),
          expires_in: 3599,
          token_type: 'Bearer',
          scope: 'openid email profile',
        });
      }
      return real(input, init);
    });
  }

  /** signInWithGoogle runs the redirect flow: start, then Google's callback with a code. */
  async function signInWithGoogle(claims: Record<string, unknown>, env: Record<string, string> = google) {
    stubGoogle(claims);
    const h = app({ env });
    const start = await h.post('/sign-in/social', { provider: 'google', callbackURL: '/' });
    expect(start.status).toBe(200);
    const { url } = (await start.json()) as { url: string };
    const state = new URL(url).searchParams.get('state') ?? '';
    const callback = await h.get(`/callback/google?code=test-code&state=${encodeURIComponent(state)}`, {
      cookie: cookieHeader(start),
    });
    return { url: new URL(url), callback };
  }

  it('asks for no offline access and keeps no refresh token, the access token encrypted', async () => {
    const email = address('googler');
    await fx.invitation(org, email, inviter);

    const { url, callback } = await signInWithGoogle({ sub: `g-${n}`, email, email_verified: true, name: 'Grace' });

    expect(url.origin + url.pathname).toBe('https://accounts.google.com/o/oauth2/v2/auth');
    expect(url.searchParams.get('access_type')).toBeNull();
    expect(url.searchParams.get('prompt')).toBe('select_account');
    expect(callback.status).toBe(302);
    expect(callback.headers.get('location')).toBe('/');
    expect(cookieHeader(callback)).toContain('goal.session_token=');
    const { rows } = await admin.query(
      `SELECT a."refreshToken", a."accessToken", u.name, u.image FROM auth.account a JOIN auth."user" u ON u.id = a."userId"
       WHERE u.email = $1 AND a."providerId" = 'google'`,
      [email],
    );
    expect(rows).toHaveLength(1);
    expect(rows[0].refreshToken).toBeNull();
    expect(rows[0].accessToken).not.toContain('google-access-token');
    expect(rows[0]).toMatchObject({ name: 'Grace', image: null });
  });

  it('is the only way in under GOAL_AUTH_EMAIL=off: no code is ever sent, an invitee still signs up', async () => {
    const googleOnly = { ...google, GOAL_AUTH_EMAIL: 'off' };
    const email = address('google-only');
    await fx.invitation(org, email, inviter);

    const send = await app({ env: googleOnly }).post('/email-otp/send-verification-otp', { email, type: 'sign-in' });
    const { callback } = await signInWithGoogle({ sub: `g-${n}`, email, email_verified: true }, googleOnly);

    expect(send.status).toBe(404);
    expect(callback.status).toBe(302);
    expect(cookieHeader(callback)).toContain('goal.session_token=');
    expect(await users(email)).toBe(1);
  });

  it.each(['https://attacker.example/', 'http://127.0.0.1:9999/'])(
    'sends nobody to %s afterwards: callbacks stay on GOAL_PUBLIC_URL (row 27)',
    async (callbackURL) => {
      const res = await app({ env: google }).post('/sign-in/social', { provider: 'google', callbackURL });

      expect(res.status).toBe(403);
    },
  );

  it('signs a user of email codes in as the same user', async () => {
    const email = address('linked');
    const id = await fx.user(email);

    const { callback } = await signInWithGoogle({ sub: `g-${n}`, email, email_verified: true });

    expect(callback.status).toBe(302);
    expect(callback.headers.get('location')).toBe('/');
    const { rows } = await admin.query(
      `SELECT "userId" FROM auth.account WHERE "providerId" = 'google' AND "userId" = $1`,
      [id],
    );
    expect(rows).toHaveLength(1);
  });

  it('does not join a Google account whose email Google has not verified to the user of that email', async () => {
    const email = address('unverified');
    const id = await fx.user(email);

    const { callback } = await signInWithGoogle({ sub: `g-${n}`, email, email_verified: false });

    expect(callback.headers.get('location')).toBe('http://127.0.0.1:8080/sign-in?error=unable_to_get_user_info');
    expect(cookieHeader(callback)).not.toContain('session_token=');
    const { rows } = await admin.query(`SELECT 1 FROM auth.account WHERE "userId" = $1`, [id]);
    expect(rows).toHaveLength(0);
  });

  it.each([
    ['a stranger', false, true, 'SIGN_UP_NOT_ALLOWED&error_description=Only+invited+people+can+sign+up'],
    ['an invitee whose Google email is not verified', true, false, 'unable_to_get_user_info'],
  ])('refuses %s', async (_, invited, verified, error) => {
    const email = address('google-refused');
    if (invited) await fx.invitation(org, email, inviter);

    const { callback } = await signInWithGoogle({ sub: `g-${n}`, email, email_verified: verified });

    expect(callback.status).toBe(302);
    expect(callback.headers.get('location')).toBe(`http://127.0.0.1:8080/sign-in?error=${error}`);
    expect(cookieHeader(callback)).not.toContain('session_token=');
    expect(await users(email)).toBe(0);
  });
});

describe('Better Auth’s rate limits', () => {
  it('apply the rules as configured: their keys need no /api/auth prefix', async () => {
    const h = app();
    const statuses = [];
    for (let i = 0; i < 11; i++) {
      statuses.push((await h.post('/sign-in/social', { provider: 'google', callbackURL: '/' })).status);
    }

    // Better Auth's own rule for /sign-in/* is 3 in 10 seconds; Goal's custom rule is 10 a minute.
    expect(statuses.slice(0, 10)).not.toContain(429);
    expect(statuses[10]).toBe(429);
    const { rows } = await admin.query(`SELECT key FROM auth."rateLimit" WHERE key LIKE $1`, [`%${h.ip}%`]);
    expect(rows.map((r) => r.key)).toEqual([expect.stringContaining('/sign-in/social')]);
    expect(rows[0]?.key).not.toContain('/api/auth');
  });

  it('allow 3 code requests a minute from one client', async () => {
    const h = app();
    const statuses = [];
    for (let i = 0; i < 4; i++) {
      statuses.push(
        (await h.post('/email-otp/send-verification-otp', { email: address('ip'), type: 'sign-in' })).status,
      );
    }

    expect(statuses).toEqual([200, 200, 200, 429]);
  });
});

describe('the bootstrap owner', () => {
  let fresh: TestDatabase;
  let freshPool: pg.Pool;
  let freshAdmin: pg.Pool;

  afterEach(async () => {
    await Promise.all([freshPool?.end(), freshAdmin?.end()]);
    await fresh?.drop();
  });

  async function install() {
    fresh = await createTestDatabase();
    freshPool = new pg.Pool({ connectionString: fresh.url('goal_auth') });
    freshAdmin = new pg.Pool({ connectionString: fresh.url() });
    return {
      f: new Fixtures(freshAdmin),
      h: harness(freshPool, { env: { GOAL_BOOTSTRAP_OWNER_EMAIL: ' Owner@Example.com ' } }),
    };
  }

  it('signs up while no user exists and becomes platform admin and owner of every organization', async () => {
    const { f, h } = await install();
    const acme = await f.org('acme');

    const { res } = await h.signIn('owner@example.com');

    expect(res.status).toBe(200);
    const { rows: admins } = await freshAdmin.query(`SELECT added_by FROM platform_admins`);
    expect(admins).toEqual([{ added_by: 'system:bootstrap' }]);
    const { rows: owned } = await freshAdmin.query(
      `SELECT m."organizationId" AS org FROM auth.member m JOIN auth."user" u ON u.id = m."userId"
       WHERE u.email = 'owner@example.com' AND m.role = 'owner'`,
    );
    expect(owned.map((r) => r.org)).toEqual(expect.arrayContaining([acme, '019a1c00-0000-7000-8000-000000000001']));
    const { rows: audited } = await freshAdmin.query(
      `SELECT actor_kind || ':' || actor_id AS by, data->>'via' AS via FROM audit_log WHERE org_id = $1 AND action = 'member.added'`,
      [acme],
    );
    expect(audited).toEqual([{ by: 'system:bootstrap', via: 'bootstrap' }]);
  });

  it('heals a failed claim on the next sign-in', async () => {
    const { f, h } = await install();
    await f.user('owner@example.com');

    expect((await h.signIn('owner@example.com')).res.status).toBe(200);

    const { rows } = await freshAdmin.query(`SELECT 1 FROM platform_admins`);
    expect(rows).toHaveLength(1);
  });

  it('is no one special once a user exists, and nobody else may sign up first', async () => {
    const { f, h } = await install();

    const other = await h.post('/sign-in/email-otp', {
      email: 'first@example.com',
      otp: await h.auth.api.createVerificationOTP({ body: { email: 'first@example.com', type: 'sign-in' } }),
    });
    expect(other.status).toBe(403);

    await f.user('someone@example.com');
    const late = await h.post('/sign-in/email-otp', {
      email: 'owner@example.com',
      otp: await h.auth.api.createVerificationOTP({ body: { email: 'owner@example.com', type: 'sign-in' } }),
    });
    expect(late.status).toBe(403);
    const { rows } = await freshAdmin.query(`SELECT 1 FROM platform_admins`);
    expect(rows).toHaveLength(0);
  });
});
