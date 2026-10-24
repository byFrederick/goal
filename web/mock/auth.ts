// The mock's sign-in (spec 3 §10.1): goald's /v1/auth/config and /v1/me, and the subset of the
// identity service's /api/auth/* the SPA calls, with Better Auth's own shapes (email codes, Google
// as a redirect round trip, get-session, sign-out, revoke-sessions, update-user, the device
// grant of goal login with goald's /v1/auth/device/{user_code}, and the organization endpoints
// of Members and Invite-Accept over the people of people.ts).
//
// Sign-in is off by default (GOAL_AUTH=none, the mock's seed), so every other screen and test runs
// as the local user. A reset turns it on: POST /__mock/reset {auth: {mode: "session",
// signed_in?: "fresh" | "stale", email?, google?: "ok" | "not-invited" | "unverified" | "failed" |
// "off", platform_admin?: false}}. Then every /v1 request but health and the auth config needs the session cookie (else
// 401 unauthenticated), changes that need a recent sign-in check the session's age (api.ts),
// creating an organization needs a platform admin (else 403 forbidden), and
// the organizations are the person's (people.ts): f.cruz@example.com (F. Cruz, platform admin
// unless the reset says platform_admin: false)
// owns every one, acme has an admin and members, j.doe@example.com is a user in none. As the
// identity service does (auth/, spec 3 §8.7), a send answers the same for every address but codes
// go only to people and pending invitees (the code is always 000000, valid 5 minutes; the 4th try
// of one code is TOO_MANY_ATTEMPTS), Google refuses an account without an invitation
// (SIGN_UP_NOT_ALLOWED) and update-user takes a name only (NAME_ONLY). The organization endpoints
// keep Better Auth's and Goal's rules: a fresh session for changes (REAUTH_REQUIRED), only owners
// touch or make an owner, an organization keeps an owner, an invitation is its recipient's only.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, AuthConfig, DeviceRequest, FactoryRole, Me, OrgRole } from '@goal/api-client';
import { invitationLifetimeMs, type MockInvitation, type MockPeople, type MockPerson } from './people.ts';

type Json = Record<string, unknown>;

/** The session cookie over http (Better Auth's cookiePrefix "goal"). */
export const sessionCookie = 'goal.session_token';

/** The one code every address gets. */
const theCode = '000000';
const codeLifetimeMs = 5 * 60_000;
const allowedAttempts = 3;
const sessionLifetimeMs = 7 * 86_400_000;
const freshMs = 10 * 60_000;

/** Who a reset signs in unless it names someone else. */
const ownerEmail = 'f.cruz@example.com';

export type GoogleOutcome = 'ok' | 'not-invited' | 'unverified' | 'failed' | 'off';

export interface MockAuthSettings {
  mode: 'none' | 'session';
  /** Signed in at the reset: a session from now, or from an hour ago (too old for fresh changes). */
  signedIn: 'fresh' | 'stale' | false;
  /** Who is signed in at the reset, and who Google signs in. */
  email: string;
  google: GoogleOutcome;
  /** Whether F. Cruz is a platform admin (true unless the reset says false). */
  ownerIsPlatformAdmin: boolean;
}

/** The settings of a reset's `auth` member; sign-in off without one. */
export function authSettings(body: unknown): MockAuthSettings {
  const b = (typeof body === 'object' && body !== null ? body : {}) as Json;
  const google = ['ok', 'not-invited', 'unverified', 'failed', 'off'].includes(String(b.google))
    ? (b.google as GoogleOutcome)
    : 'ok';
  return {
    mode: b.mode === 'session' ? 'session' : 'none',
    signedIn: b.signed_in === 'fresh' || b.signed_in === 'stale' ? b.signed_in : false,
    email: typeof b.email === 'string' && b.email.includes('@') ? b.email.toLowerCase() : ownerEmail,
    google,
    ownerIsPlatformAdmin: b.platform_admin !== false,
  };
}

interface MockSession {
  id: string;
  token: string;
  userId: string;
  createdAt: number;
  expiresAt: number;
}

interface MockCode {
  sentAt: number;
  attempts: number;
}

/** A CLI sign-in of the device grant (Better Auth's deviceCode row and its device.requested event). */
interface MockDevice {
  deviceCode: string;
  userCode: string;
  status: 'pending' | 'approved' | 'denied';
  /** Who opened the code (GET /device); only they approve or deny it. */
  userId: string | null;
  ip: string;
  userAgent: string;
  requestedAt: number;
  expiresAt: number;
}

/** Better Auth's user code alphabet (no 0, 1, I, O). */
const userCodeChars = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
const deviceLifetimeMs = 10 * 60_000;
const normalUserCode = (code: string) => code.replace(/[^a-zA-Z0-9]/g, '').toUpperCase();

/** A request's address, loopback ones as one ("127.0.0.1"), as goald compares them. */
const clientIp = (req: IncomingMessage): string => {
  const ip = (req.socket.remoteAddress ?? '').replace(/^::ffff:/, '');
  return ip === '::1' || ip.startsWith('127.') ? '127.0.0.1' : ip;
};

export interface MockAuth {
  /** Starts over with these settings; answers the Set-Cookie of the session it signs in, if any. */
  reset: (settings: MockAuthSettings) => string | undefined;
  /** Serves /v1/auth/config, /v1/me and /api/auth/*; false for any other path. */
  handle: (req: IncomingMessage, res: ServerResponse, path: string, method: string) => Promise<boolean>;
  /**
   * Under sign-in, refuses a /v1 request without a session (401), and creating an organization
   * to someone who is not a platform admin (403); true when it answered.
   */
  refuse: (req: IncomingMessage, res: ServerResponse, path: string, method: string) => boolean;
  /** Who the request acts for, as goald records it: the signed-in person, else the local user. */
  actor: (req: IncomingMessage) => Actor;
  /**
   * Who sends the request: null under sign-in off (the local user, who may do everything) or without
   * a session, else the signed-in person and whether their session is fresh (within 10 minutes).
   */
  who: (req: IncomingMessage) => { person: MockPerson; fresh: boolean } | null;
}

/** What the identity service reads of the people and of goald's organizations and factories. */
export interface MockDirectory {
  people: () => MockPeople;
  org: (id: string) => { id: string; name: string; slug: string } | undefined;
  factories: (org: string) => { id: string; name: string }[];
}

const alnum = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
const randomToken = () => Array.from({ length: 32 }, () => alnum[Math.floor(Math.random() * alnum.length)]).join('');

const send = (res: ServerResponse, status: number, body: unknown, headers: Record<string, string | string[]> = {}) => {
  res.statusCode = status;
  for (const [name, value] of Object.entries(headers)) res.setHeader(name, value);
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

/** A Better Auth error: {code, message}, as its APIError answers. */
const authError = (res: ServerResponse, status: number, code: string, message: string) => {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json');
  res.end(JSON.stringify({ code, message }));
};

async function readJson(req: IncomingMessage): Promise<Json> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  try {
    return chunks.length ? (JSON.parse(Buffer.concat(chunks).toString('utf8')) as Json) : {};
  } catch {
    return {};
  }
}

function cookieValue(req: IncomingMessage, name: string): string | undefined {
  for (const part of (req.headers.cookie ?? '').split(';')) {
    const [key, ...rest] = part.trim().split('=');
    if (key === name) return decodeURIComponent(rest.join('='));
  }
  return undefined;
}

const setCookie = (token: string) =>
  `${sessionCookie}=${encodeURIComponent(`${token}.${randomToken().slice(0, 22)}`)}; Path=/; HttpOnly; SameSite=Lax; Max-Age=${sessionLifetimeMs / 1000}`;
const clearCookie = `${sessionCookie}=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0`;

export function createMockAuth(clock: () => number, dir: MockDirectory): MockAuth {
  let settings = authSettings(undefined);
  let sessions = new Map<string, MockSession>();
  let codes = new Map<string, MockCode>();
  let googleStates = new Map<string, { callbackURL: string; errorCallbackURL: string }>();
  let devices = new Map<string, MockDevice>();

  const pendingFor = (email: string) =>
    dir.people().invitations.filter((i) => i.email === email && i.status === 'pending' && i.expiresAt > clock());
  /** Who may get a code: people with a user, and pending invitees (who get one at their first sign-in). */
  const maySignIn = (email: string) =>
    dir.people().people.some((p) => p.email === email) || pendingFor(email).length > 0;
  const userFor = (email: string): MockPerson => {
    const people = dir.people().people;
    let user = people.find((p) => p.email === email);
    if (!user) {
      // As Better Auth signs a new person up by code: no name until they choose one.
      user = { id: crypto.randomUUID(), email, name: '', platformAdmin: false };
      people.push(user);
    }
    return user;
  };

  const startSession = (email: string, createdAt: number) => {
    const user = userFor(email);
    const token = randomToken();
    sessions.set(token, {
      id: crypto.randomUUID(),
      token,
      userId: user.id,
      createdAt,
      expiresAt: createdAt + sessionLifetimeMs,
    });
    return setCookie(token);
  };

  /** The request's live session and its person. */
  const current = (req: IncomingMessage) => {
    const token = cookieValue(req, sessionCookie)?.split('.')[0];
    const session = token ? sessions.get(token) : undefined;
    if (!session || session.expiresAt <= clock()) return undefined;
    const user = dir.people().people.find((u) => u.id === session.userId);
    return user ? { session, user } : undefined;
  };

  const config = (): AuthConfig => ({
    mode: settings.mode,
    methods: settings.mode === 'none' ? [] : settings.google === 'off' ? ['email_code'] : ['email_code', 'google'],
  });

  const iso = (ms: number) => new Date(ms).toISOString();

  const unauthenticated = (res: ServerResponse) =>
    send(
      res,
      401,
      { title: 'Not signed in', status: 401, code: 'unauthenticated' },
      {
        'WWW-Authenticate': 'Bearer realm="goal"',
      },
    );

  const betterAuth = async (req: IncomingMessage, res: ServerResponse, route: string, method: string) => {
    const body = method === 'POST' ? await readJson(req) : {};
    const ok = (data: unknown, headers: Record<string, string | string[]> = {}) => {
      res.statusCode = 200;
      for (const [name, value] of Object.entries(headers)) res.setHeader(name, value);
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify(data));
    };
    const email = typeof body.email === 'string' ? body.email.trim().toLowerCase() : '';

    if (route === '/email-otp/send-verification-otp' && method === 'POST') {
      // The same answer for every address; the code goes only to people who may sign in.
      if (maySignIn(email) && body.type === 'sign-in') codes.set(email, { sentAt: clock(), attempts: 0 });
      return ok({ success: true });
    }
    if (route === '/sign-in/email-otp' && method === 'POST') {
      const code = codes.get(email);
      if (!code) return authError(res, 400, 'INVALID_OTP', 'Invalid OTP');
      if (clock() - code.sentAt > codeLifetimeMs) {
        codes.delete(email);
        return authError(res, 400, 'OTP_EXPIRED', 'OTP expired');
      }
      if (code.attempts >= allowedAttempts) {
        codes.delete(email);
        return authError(res, 403, 'TOO_MANY_ATTEMPTS', 'Too many attempts');
      }
      if (body.otp !== theCode) {
        code.attempts += 1;
        return authError(res, 400, 'INVALID_OTP', 'Invalid OTP');
      }
      codes.delete(email);
      const cookie = startSession(email, clock());
      const user = userFor(email);
      // As the auth service answers (review M2): the session token is only in the httpOnly cookie.
      return ok({ user: { id: user.id, email: user.email, name: user.name } }, { 'Set-Cookie': cookie });
    }
    if (route === '/sign-in/social' && method === 'POST') {
      if (settings.google === 'off' || body.provider !== 'google') {
        return authError(res, 404, 'PROVIDER_NOT_FOUND', 'Provider not found');
      }
      const state = randomToken();
      const callbackURL = typeof body.callbackURL === 'string' ? body.callbackURL : '/';
      const errorCallbackURL = typeof body.errorCallbackURL === 'string' ? body.errorCallbackURL : '/sign-in';
      googleStates.set(state, { callbackURL, errorCallbackURL });
      // Google's consent screen is simulated: the browser goes straight to the callback.
      return ok({ url: `/api/auth/callback/google?state=${state}&code=mock`, redirect: true });
    }
    if (route === '/callback/google' && method === 'GET') {
      const state = new URL(req.url ?? '/', 'http://mock').searchParams.get('state') ?? '';
      const flow = googleStates.get(state);
      googleStates.delete(state);
      const back = (to: string, headers: Record<string, string> = {}) => {
        res.statusCode = 302;
        for (const [name, value] of Object.entries(headers)) res.setHeader(name, value);
        res.setHeader('Location', to);
        res.end();
      };
      if (!flow) return back('/sign-in?error=state_not_found');
      const withError = (error: string) =>
        `${flow.errorCallbackURL}${flow.errorCallbackURL.includes('?') ? '&' : '?'}error=${error}`;
      if (settings.google === 'not-invited') {
        return back(`${withError('SIGN_UP_NOT_ALLOWED')}&error_description=Only%20invited%20people%20can%20sign%20up`);
      }
      if (settings.google === 'unverified') return back(withError('unable_to_get_user_info'));
      if (settings.google === 'failed') return back(withError('access_denied'));
      return back(flow.callbackURL, { 'Set-Cookie': startSession(settings.email, clock()) });
    }
    if (route === '/get-session' && method === 'GET') {
      const now = current(req);
      if (!now) return ok(null);
      const { session, user } = now;
      // Better Auth extends a session its endpoints see (updateAge 1 day; the mock: always).
      session.expiresAt = clock() + sessionLifetimeMs;
      return ok({
        session: {
          id: session.id,
          userId: user.id,
          createdAt: iso(session.createdAt),
          expiresAt: iso(session.expiresAt),
        },
        user: { id: user.id, email: user.email, name: user.name, emailVerified: true },
      });
    }
    if (route === '/sign-out' && method === 'POST') {
      const now = current(req);
      if (now) sessions.delete(now.session.token);
      return ok({ success: true }, { 'Set-Cookie': clearCookie });
    }
    if (route === '/revoke-sessions' && method === 'POST') {
      const now = current(req);
      if (!now) return authError(res, 401, 'UNAUTHORIZED', 'Unauthorized');
      for (const [token, s] of sessions) if (s.userId === now.user.id) sessions.delete(token);
      return ok({ status: true });
    }
    if (route === '/update-user' && method === 'POST') {
      const now = current(req);
      if (!now) return authError(res, 401, 'UNAUTHORIZED', 'Unauthorized');
      const name = typeof body.name === 'string' ? body.name.trim() : '';
      if (name.length < 1 || name.length > 80 || Object.keys(body).some((k) => k !== 'name')) {
        return authError(res, 400, 'NAME_ONLY', 'Only the name can change, 1 to 80 characters');
      }
      now.user.name = name;
      return ok({ status: true });
    }
    if (route.startsWith('/device')) return deviceGrant(req, res, route, method, body);
    if (route.startsWith('/organization/')) return organization(req, res, route, method, body, ok);
    // goald's proxy answers anything off its allowlist with a 404 problem.
    return send(res, 404, { title: 'Not Found', status: 404, code: 'not-found' });
  };

  /** An OAuth error of the device grant: {error, error_description}, as Better Auth answers. */
  const oauthError = (res: ServerResponse, status: number, error: string) => {
    res.statusCode = status;
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ error, error_description: error }));
  };

  /**
   * The device authorization grant (Better Auth's device-authorization plugin, as auth/ sets it
   * up): codes for goal-cli only, 10 minutes; GET /device binds a code to the person who opens it;
   * approving needs a session from the last 10 minutes (Goal's hook: REAUTH_REQUIRED); the CLI's
   * poll answers authorization_pending until then. An X-Mock-Client-Ip header on /device/code
   * stands for a CLI on another network.
   */
  const deviceGrant = (req: IncomingMessage, res: ServerResponse, route: string, method: string, body: Json) => {
    const ok = (data: unknown) => {
      res.statusCode = 200;
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify(data));
    };
    if (route === '/device/code' && method === 'POST') {
      if (body.client_id !== 'goal-cli') return oauthError(res, 400, 'invalid_client');
      const userCode = Array.from({ length: 8 }, () => userCodeChars[Math.floor(Math.random() * 32)]).join('');
      const device: MockDevice = {
        deviceCode: randomToken(),
        userCode,
        status: 'pending',
        userId: null,
        ip: String(req.headers['x-mock-client-ip'] ?? clientIp(req)),
        userAgent: String(req.headers['user-agent'] ?? ''),
        requestedAt: clock(),
        expiresAt: clock() + deviceLifetimeMs,
      };
      devices.set(userCode, device);
      return ok({
        device_code: device.deviceCode,
        user_code: userCode,
        verification_uri: '/device',
        verification_uri_complete: `/device?user_code=${userCode}`,
        expires_in: deviceLifetimeMs / 1000,
        interval: 5,
      });
    }
    if (route === '/device/token' && method === 'POST') {
      const device = [...devices.values()].find((d) => d.deviceCode === body.device_code);
      if (!device || body.client_id !== 'goal-cli') return oauthError(res, 400, 'invalid_grant');
      if (device.expiresAt <= clock()) return oauthError(res, 400, 'expired_token');
      if (device.status === 'pending') return oauthError(res, 400, 'authorization_pending');
      devices.delete(device.userCode);
      if (device.status === 'denied') return oauthError(res, 400, 'access_denied');
      return ok({ access_token: randomToken(), token_type: 'Bearer', expires_in: sessionLifetimeMs / 1000, scope: '' });
    }

    const now = current(req);
    const query = new URL(req.url ?? '/', 'http://mock').searchParams;
    const typed =
      route === '/device' ? (query.get('user_code') ?? '') : typeof body.userCode === 'string' ? body.userCode : '';
    const device = devices.get(normalUserCode(typed));
    if (route === '/device' && method === 'GET') {
      if (!device) return oauthError(res, 400, 'invalid_request');
      if (device.expiresAt <= clock()) return oauthError(res, 400, 'expired_token');
      if (now && device.status === 'pending' && device.userId === null) device.userId = now.user.id;
      const reviewer = now !== undefined && device.userId === now.user.id;
      return ok({ user_code: typed, status: device.status, ...(reviewer ? { client_id: 'goal-cli' } : {}) });
    }
    if ((route === '/device/approve' || route === '/device/deny') && method === 'POST') {
      if (!now) return oauthError(res, 401, 'unauthorized');
      if (route === '/device/approve' && clock() - now.session.createdAt > freshMs) {
        return authError(res, 403, 'REAUTH_REQUIRED', 'Sign in again to do this');
      }
      if (device?.status !== 'pending' || device.userId === null) return oauthError(res, 400, 'invalid_request');
      if (device.expiresAt <= clock()) return oauthError(res, 400, 'expired_token');
      if (device.userId !== now.user.id) return oauthError(res, 403, 'access_denied');
      device.status = route === '/device/approve' ? 'approved' : 'denied';
      return ok({ success: true });
    }
    return send(res, 404, { title: 'Not Found', status: 404, code: 'not-found' });
  };

  /**
   * The identity service's organization endpoints (Better Auth's organization plugin with Goal's
   * hooks): each names its target, changes need a fresh session, only owners touch or make an
   * owner, an organization keeps an owner, an invitation is its recipient's only.
   */
  const organization = async (
    req: IncomingMessage,
    res: ServerResponse,
    route: string,
    method: string,
    body: Json,
    ok: (data: unknown) => void,
  ) => {
    const now = current(req);
    if (!now) return authError(res, 401, 'UNAUTHORIZED', 'Unauthorized');
    const people = dir.people();
    const fresh = clock() - now.session.createdAt <= freshMs;
    const membersOf = (org: string) => people.members[org] ?? [];
    const roleOf = (org: string, user: string) => membersOf(org).find((m) => m.userId === user)?.role;
    const mine = (org: string) => roleOf(org, now.user.id);
    const manages = (org: string) => mine(org) === 'owner' || mine(org) === 'admin';
    const invitationById = (id: unknown) => people.invitations.find((i) => i.id === id);
    const usable = (i: MockInvitation | undefined): i is MockInvitation =>
      i !== undefined && i.status === 'pending' && i.expiresAt > clock();
    const stale = () => authError(res, 403, 'REAUTH_REQUIRED', 'Sign in again to do this');
    const notRecipient = () =>
      authError(res, 403, 'YOU_ARE_NOT_THE_RECIPIENT_OF_THE_INVITATION', 'You are not the recipient of the invitation');
    const orgId = typeof body.organizationId === 'string' ? body.organizationId : '';
    const owners = (org: string) => membersOf(org).filter((m) => m.role === 'owner').length;

    if (route === '/organization/get-invitation' && method === 'GET') {
      const i = invitationById(new URL(req.url ?? '/', 'http://mock').searchParams.get('id'));
      // Better Auth's own answer for an unknown, used, cancelled or expired one: no code.
      if (!usable(i)) return send(res, 400, { message: 'Invitation not found!' });
      if (i.email !== now.user.email) return notRecipient();
      const org = dir.org(i.orgId);
      const inviter = people.people.find((p) => p.id === i.inviterId);
      const names = new Map(dir.factories(i.orgId).map((f) => [f.id, f.name]));
      return ok({
        id: i.id,
        email: i.email,
        role: i.role,
        organizationId: i.orgId,
        inviterId: i.inviterId,
        status: i.status,
        expiresAt: new Date(i.expiresAt).toISOString(),
        organizationName: org?.name ?? '',
        organizationSlug: org?.slug ?? '',
        inviterEmail: inviter?.email ?? '',
        inviterName: inviter?.name ?? null,
        factories: i.factoryRoles.flatMap((g) => {
          const name = names.get(g.factoryId);
          return name ? [{ id: g.factoryId, name, role: g.role }] : [];
        }),
      });
    }
    if (method !== 'POST') return send(res, 404, { title: 'Not Found', status: 404, code: 'not-found' });

    if (route === '/organization/accept-invitation' || route === '/organization/reject-invitation') {
      const i = invitationById(body.invitationId);
      if (!usable(i)) return authError(res, 400, 'INVITATION_NOT_FOUND', 'Invitation not found');
      if (i.email !== now.user.email) return notRecipient();
      if (route === '/organization/reject-invitation') {
        i.status = 'rejected';
        return ok({ invitation: i, member: null });
      }
      if (roleOf(i.orgId, now.user.id)) {
        return authError(res, 400, 'USER_IS_ALREADY_A_MEMBER_OF_THIS_ORGANIZATION', 'Already a member');
      }
      i.status = 'accepted';
      // goal_apply_invitation_roles: the factory roles of factories that still exist, members only.
      const factories = new Set(dir.factories(i.orgId).map((f) => f.id));
      const grants = i.role === 'member' ? i.factoryRoles.filter((g) => factories.has(g.factoryId)) : [];
      const member = {
        memberId: crypto.randomUUID(),
        userId: now.user.id,
        role: i.role,
        joinedAt: clock(),
        lastActiveAt: clock(),
        factoryRoles: Object.fromEntries(grants.map((g) => [g.factoryId, g.role])) as Record<string, FactoryRole>,
      };
      people.members[i.orgId] = [...membersOf(i.orgId), member];
      return ok({ invitation: i, member: { id: member.memberId, role: member.role } });
    }

    if (route === '/organization/invite-member') {
      const role = body.role as OrgRole;
      if (!['owner', 'admin', 'member'].includes(role)) return authError(res, 400, 'ONE_ROLE', 'One role');
      if (!fresh) return stale();
      if (!manages(orgId)) {
        return authError(res, 403, 'YOU_ARE_NOT_ALLOWED_TO_INVITE_USERS_TO_THIS_ORGANIZATION', 'Not allowed');
      }
      if (role === 'owner' && mine(orgId) !== 'owner') {
        return authError(res, 403, 'YOU_ARE_NOT_ALLOWED_TO_INVITE_USER_WITH_THIS_ROLE', 'Only owners invite owners');
      }
      const email = typeof body.email === 'string' ? body.email.trim().toLowerCase() : '';
      let grants: { factoryId: string; role: FactoryRole }[];
      try {
        grants = typeof body.factoryRoles === 'string' ? JSON.parse(body.factoryRoles) : [];
      } catch {
        return authError(res, 400, 'FACTORY_ROLES', 'factoryRoles must be JSON');
      }
      if (grants.length > 0 && role !== 'member') {
        return authError(res, 400, 'FACTORY_ROLES_FOR_MEMBERS', 'Owners and admins hold every factory role already');
      }
      const known = new Set(dir.factories(orgId).map((f) => f.id));
      if (grants.some((g) => !known.has(g.factoryId))) {
        return authError(res, 400, 'UNKNOWN_FACTORY', 'No such factory here');
      }
      const existing = people.people.find((p) => p.email === email);
      if (existing && roleOf(orgId, existing.id)) {
        return authError(res, 400, 'USER_IS_ALREADY_A_MEMBER_OF_THIS_ORGANIZATION', 'Already a member');
      }
      // cancelPendingInvitationsOnReInvite: a new invitation (Resend too) replaces a pending one.
      for (const i of people.invitations) {
        if (i.orgId === orgId && i.email === email && i.status === 'pending') i.status = 'canceled';
      }
      const invitation: MockInvitation = {
        id: crypto.randomUUID(),
        orgId,
        email,
        role,
        factoryRoles: grants,
        inviterId: now.user.id,
        createdAt: clock(),
        expiresAt: clock() + invitationLifetimeMs,
        status: 'pending',
      };
      people.invitations.push(invitation);
      return ok({ id: invitation.id, email, role, organizationId: orgId, status: 'pending' });
    }

    if (route === '/organization/cancel-invitation') {
      if (!fresh) return stale();
      const i = invitationById(body.invitationId);
      if (!i || !manages(i.orgId)) {
        return authError(res, 403, 'YOU_ARE_NOT_ALLOWED_TO_CANCEL_THIS_INVITATION', 'Not allowed');
      }
      i.status = 'canceled';
      return ok(i);
    }

    if (route === '/organization/update-member-role' || route === '/organization/remove-member') {
      if (!fresh) return stale();
      const removing = route === '/organization/remove-member';
      const target = membersOf(orgId).find((m) => m.memberId === (removing ? body.memberIdOrEmail : body.memberId));
      if (!target) return authError(res, 400, 'MEMBER_NOT_FOUND', 'Member not found');
      if (!manages(orgId)) return authError(res, 403, 'YOU_ARE_NOT_ALLOWED_TO_UPDATE_THIS_MEMBER', 'Not allowed');
      const role = body.role as OrgRole;
      if (!removing && !['owner', 'admin', 'member'].includes(role)) {
        return authError(res, 400, 'ONE_ROLE', 'One role');
      }
      if ((target.role === 'owner' || (!removing && role === 'owner')) && mine(orgId) !== 'owner') {
        return authError(res, 403, 'OWNERS_ONLY', 'Only an owner can change an owner');
      }
      if (target.role === 'owner' && owners(orgId) === 1 && (removing || role !== 'owner')) {
        return authError(res, 400, 'YOU_CANNOT_LEAVE_THE_ORGANIZATION_WITHOUT_AN_OWNER', 'Keep an owner');
      }
      if (removing) {
        people.members[orgId] = membersOf(orgId).filter((m) => m !== target);
        return ok({ member: { id: target.memberId } });
      }
      target.role = role;
      return ok({ id: target.memberId, role });
    }

    if (route === '/organization/leave') {
      const me = membersOf(orgId).find((m) => m.userId === now.user.id);
      if (!me) return authError(res, 400, 'MEMBER_NOT_FOUND', 'Member not found');
      if (me.role === 'owner' && owners(orgId) === 1) {
        return authError(res, 400, 'YOU_CANNOT_LEAVE_THE_ORGANIZATION_AS_THE_ONLY_OWNER', 'The only owner');
      }
      people.members[orgId] = membersOf(orgId).filter((m) => m !== me);
      return ok({ id: me.memberId });
    }
    return send(res, 404, { title: 'Not Found', status: 404, code: 'not-found' });
  };

  /** goald's GET /v1/auth/device/{user_code}: a pending code, with whether it came from this browser's network. */
  const deviceRequest = (req: IncomingMessage, res: ServerResponse, typed: string) => {
    const device = devices.get(normalUserCode(typed));
    if (device?.status !== 'pending' || device.expiresAt <= clock()) {
      return send(res, 404, { title: 'Code not valid', status: 404, code: 'device-code-not-found' });
    }
    return send(res, 200, {
      client_id: 'goal-cli',
      client_name: 'Goal CLI',
      requested_at: iso(device.requestedAt),
      expires_at: iso(device.expiresAt),
      ip: device.ip,
      ...(device.userAgent ? { user_agent: device.userAgent } : {}),
      same_ip: device.ip === clientIp(req),
    } satisfies DeviceRequest);
  };

  return {
    reset(next) {
      settings = next;
      const owner = dir.people().people.find((p) => p.email === ownerEmail);
      if (owner) owner.platformAdmin = settings.ownerIsPlatformAdmin;
      sessions = new Map();
      codes = new Map();
      googleStates = new Map();
      devices = new Map();
      if (settings.mode !== 'session' || !settings.signedIn) return undefined;
      return startSession(settings.email, settings.signedIn === 'fresh' ? clock() : clock() - 60 * 60_000);
    },

    async handle(req, res, path, method) {
      if (path === '/v1/auth/config' && method === 'GET') {
        send(res, 200, config());
        return true;
      }
      if (path === '/v1/me' && method === 'GET') {
        if (settings.mode === 'none') {
          send(res, 200, { kind: 'user', mode: 'none', platform_admin: false } satisfies Me);
          return true;
        }
        const now = current(req);
        if (!now) {
          unauthenticated(res);
          return true;
        }
        const { session, user } = now;
        send(res, 200, {
          kind: 'user',
          mode: 'session',
          user: { id: user.id, name: user.name, email: user.email },
          platform_admin: user.platformAdmin,
          session: {
            created_at: iso(session.createdAt),
            expires_at: iso(session.expiresAt),
            fresh_until: iso(session.createdAt + freshMs),
          },
        } satisfies Me);
        return true;
      }
      const device = path.match(/^\/v1\/auth\/device\/([^/]+)$/);
      if (device && method === 'GET') {
        if (settings.mode === 'none') {
          send(res, 404, { title: 'Code not valid', status: 404, code: 'device-code-not-found' });
        } else if (!current(req)) {
          unauthenticated(res);
        } else {
          deviceRequest(req, res, decodeURIComponent(device[1] ?? ''));
        }
        return true;
      }
      if (!path.startsWith('/api/auth/')) return false;
      if (settings.mode === 'none') {
        // goald mounts no /api/auth under GOAL_AUTH=none.
        send(res, 404, { title: 'Not Found', status: 404, code: 'not-found' });
        return true;
      }
      await betterAuth(req, res, path.slice('/api/auth'.length), method);
      return true;
    },

    refuse(req, res, path, method) {
      if (settings.mode === 'none' || path === '/v1/health') return false;
      const now = current(req);
      if (!now) {
        unauthenticated(res);
        return true;
      }
      if (path === '/v1/orgs' && method === 'POST' && !now.user.platformAdmin) {
        send(res, 403, { title: 'Forbidden', status: 403, code: 'forbidden' });
        return true;
      }
      // Managing API keys, creating a personal token and revoking a member's need a sign-in from
      // the last 10 minutes; revoking one's own token does not (the people routes and deleting the
      // organization ask in api.ts, once the caller's role holds the permission).
      const fresh =
        ((method === 'POST' || method === 'DELETE') && /^\/v1\/orgs\/[^/]+\/api-keys(\/[^/]+)?$/.test(path)) ||
        (method === 'POST' && /^\/v1\/orgs\/[^/]+\/personal-tokens$/.test(path)) ||
        (method === 'DELETE' && /^\/v1\/orgs\/[^/]+\/member-tokens\/[^/]+$/.test(path));
      if (fresh && clock() - now.session.createdAt > freshMs) {
        send(res, 403, { title: 'Sign in again', status: 403, code: 'reauth-required' });
        return true;
      }
      return false;
    },

    actor(req) {
      const now = settings.mode === 'none' ? undefined : current(req);
      return now
        ? { kind: 'user', id: now.user.id, name: now.user.name }
        : { kind: 'user', id: 'local', name: 'Local user' };
    },

    who(req) {
      if (settings.mode === 'none') return null;
      const now = current(req);
      return now ? { person: now.user, fresh: clock() - now.session.createdAt <= freshMs } : null;
    },
  };
}
