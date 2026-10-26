// The auth service's app for integration tests: Better Auth configured as in production on a test
// database (as goal_auth), mail kept in memory, and requests that come, like goald's proxy sends
// them, with an Origin and an X-Goal-Client-Ip of their own, so Better Auth's rate limits (kept in
// the database) count each harness apart. Fixtures write rows as the superuser, the way goald and
// earlier sign-ins would have.
import { randomBytes, randomUUID } from 'node:crypto';
import type pg from 'pg';
import { vi } from 'vitest';
import { createApp, proxySecretHeader } from '../app.js';
import { createAuth } from '../auth.js';
import type { Clock } from '../goal.js';
import type { Logger } from '../log.js';
import { MemoryMailer } from '../mail/memory.js';
import { type Env, loadSettings } from '../settings.js';

export const silent: Logger = { log: () => {} };

let clients = 0;

/** One throwaway secret per test file, so a session cookie from one harness works in another. */
const secret = randomBytes(32).toString('hex');

/** The secret goald's proxy would send; every harness request carries it. */
export const proxySecret = randomBytes(32).toString('hex');

export interface HarnessOptions {
  publicURL?: string;
  env?: Env;
  clock?: Clock;
  mailer?: MemoryMailer;
  log?: Logger;
}

export type Harness = ReturnType<typeof harness>;

export function harness(pool: pg.Pool, opts: HarnessOptions = {}) {
  const publicURL = opts.publicURL ?? 'http://127.0.0.1:8080';
  const mailer = opts.mailer ?? new MemoryMailer();
  const ip = `198.51.100.${++clients}`;
  const settings = loadSettings({
    GOAL_PUBLIC_URL: publicURL,
    BETTER_AUTH_SECRET: secret,
    GOAL_AUTH_PROXY_SECRET: proxySecret,
    ...opts.env,
  });
  const auth = createAuth({
    settings,
    pool,
    log: opts.log ?? silent,
    // As startService: no mail at all while email codes are off.
    ...(settings.smtp ? { mailer } : {}),
    ...(opts.clock ? { clock: opts.clock } : {}),
  });
  const app = createApp(auth, proxySecret);
  const headers = (extra: Record<string, string>) => ({
    origin: publicURL,
    'x-goal-client-ip': ip,
    [proxySecretHeader]: proxySecret,
    ...extra,
  });

  const post = (path: string, body: unknown, extra: Record<string, string> = {}) =>
    app.request(`/api/auth${path}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', ...headers(extra) },
      body: JSON.stringify(body),
    });
  const get = (path: string, extra: Record<string, string> = {}) =>
    app.request(`/api/auth${path}`, { headers: headers(extra) });

  /** codes are the sign-in codes mailed to address, oldest first. */
  const codes = (address: string) =>
    mailer.to(address).flatMap((m) => /sign-in code is (\d{6})\./.exec(m.text)?.[1] ?? []);

  /** requestCode asks for a code and waits for the background send to mail it. */
  const requestCode = async (address: string): Promise<string> => {
    const before = codes(address).length;
    const res = await post('/email-otp/send-verification-otp', { email: address, type: 'sign-in' });
    if (res.status !== 200) throw new Error(`send-verification-otp: ${res.status}`);
    return vi.waitFor(() => {
      const all = codes(address);
      if (all.length <= before) throw new Error(`no code mailed to ${address}`);
      return all[all.length - 1] as string;
    });
  };

  /** signIn signs address in by code and returns the response and its session cookie header. */
  const signIn = async (address: string) => {
    const res = await post('/sign-in/email-otp', { email: address, otp: await requestCode(address) });
    return { res, cookie: cookieHeader(res) };
  };

  return { app, auth, mailer, ip, publicURL, post, get, codes, requestCode, signIn };
}

/** cookieHeader turns a response's Set-Cookie headers into a Cookie request header. */
export function cookieHeader(res: Response): string {
  return res.headers
    .getSetCookie()
    .map((c) => c.split(';')[0])
    .join('; ');
}

/** Fixtures writes rows directly, as the database superuser. */
export class Fixtures {
  constructor(readonly pool: pg.Pool) {}

  async user(email: string, name = email.split('@')[0] ?? '', emailVerified = true): Promise<string> {
    const { rows } = await this.pool.query<{ id: string }>(
      `INSERT INTO auth."user" (name, email, "emailVerified") VALUES ($1, $2, $3) RETURNING id`,
      [name, email, emailVerified],
    );
    return rows[0]?.id as string;
  }

  /** org creates a live organization, Goal's row and Better Auth's (as goal_provision_org does). */
  async org(slug: string, name = slug): Promise<string> {
    const id = randomUUID();
    await this.pool.query(`INSERT INTO organizations (id, slug, name, mark) VALUES ($1, $2, $3, 'Ac')`, [
      id,
      slug,
      name,
    ]);
    await this.pool.query(`INSERT INTO auth.organization (id, name, slug, "createdAt") VALUES ($1, $2, $3, now())`, [
      id,
      name,
      slug,
    ]);
    return id;
  }

  async member(org: string, user: string, role: string): Promise<string> {
    const { rows } = await this.pool.query<{ id: string }>(
      `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt") VALUES ($1, $2, $3, now()) RETURNING id`,
      [org, user, role],
    );
    return rows[0]?.id as string;
  }

  async factory(org: string, name: string): Promise<string> {
    const id = randomUUID();
    await this.pool.query(`INSERT INTO factories (id, org_id, name) VALUES ($1, $2, $3)`, [id, org, name]);
    return id;
  }

  async invitation(
    org: string,
    email: string,
    inviter: string,
    { role = 'member', expiresAt = new Date(Date.now() + 72 * 3600_000), factoryRoles = null as string | null } = {},
  ): Promise<string> {
    const { rows } = await this.pool.query<{ id: string }>(
      `INSERT INTO auth.invitation ("organizationId", email, role, status, "expiresAt", "inviterId", "factoryRoles")
       VALUES ($1, $2, $3, 'pending', $4, $5, $6) RETURNING id`,
      [org, email, role, expiresAt, inviter, factoryRoles],
    );
    return rows[0]?.id as string;
  }

  /**
   * invitationAsBefore00049 writes a pending invitation with the database's triggers off, as one a
   * lingering owner of a deleted organization could make before migration 00049 refused it.
   */
  async invitationAsBefore00049(org: string, email: string, inviter: string): Promise<void> {
    const c = await this.pool.connect();
    try {
      await c.query('BEGIN');
      await c.query(`SET LOCAL session_replication_role = replica`);
      await c.query(
        `INSERT INTO auth.invitation ("organizationId", email, role, status, "expiresAt", "inviterId")
         VALUES ($1, $2, 'member', 'pending', now() + interval '72 hours', $3)`,
        [org, email, inviter],
      );
      await c.query('COMMIT');
    } finally {
      c.release();
    }
  }

  /** age makes every session of user look signed in that many milliseconds ago. */
  async age(user: string, ms: number): Promise<void> {
    await this.pool.query(
      `UPDATE auth.session SET "createdAt" = now() - make_interval(secs => $2::double precision / 1000) WHERE "userId" = $1`,
      [user, ms],
    );
  }

  async events(
    kind: string,
  ): Promise<{ user_id: string | null; subject: string | null; ip: string | null; data: Record<string, unknown> }[]> {
    const { rows } = await this.pool.query(
      `SELECT user_id, subject, host(ip) AS ip, data FROM auth_events WHERE kind = $1 ORDER BY id`,
      [kind],
    );
    return rows;
  }
}
