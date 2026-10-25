// The organizations' API keys for the mock API (spec 3 §8.9): GET, POST /v1/orgs/{org}/api-keys
// and DELETE …/api-keys/{key}, refusing as goald does (scope-not-allowed, api-key-name-taken,
// factory-not-found, 400 for a malformed key). A created key's secret is answered once with
// Cache-Control: no-store and kept nowhere; the mock keeps its hint. acme starts with the
// board's keys: three live ones and one revoked.
//
// Personal tokens (3-12b) too: GET, POST /v1/orgs/{org}/personal-tokens and DELETE …/{token}, the
// signed-in person's own (another's is 404; with sign-in off there are none and creating one is
// refused), and GET /v1/orgs/{org}/member-tokens and DELETE …/{token}, every member's. acme starts
// with F. Cruz's laptop token, one of theirs revoked, and two other members' tokens.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, ApiKey, Factory, FactoryRole, Org, PersonalToken, User } from '@goal/api-client';
import { directory, type MockPerson } from './people.ts';

const dayMs = 86_400_000;
const hourMs = 3_600_000;

/** The permissions keys may carry and, for a factory permission, the lowest role holding it. */
const keyScopes: Record<string, FactoryRole | null> = {
  authenticated: null,
  'org:read': null,
  'audit:read': null,
  'connection:read': null,
  'factory:create': null,
  'factory:read': 'viewer',
  'item:create': 'approver',
  'item:decide': 'approver',
  'item:operate': 'approver',
  'andon:manage': 'approver',
  'definition:edit': 'editor',
  'definition:publish': 'editor',
  'factory:manage': 'editor',
};
const ladder: FactoryRole[] = ['viewer', 'approver', 'editor'];

/** The permissions personal tokens may carry (`pat: true`) and whether each is a factory's. */
const tokenScopes: Record<string, 'org' | 'factory'> = {
  authenticated: 'org',
  'org:read': 'org',
  'connection:read': 'org',
  'factory:create': 'org',
  'factory:read': 'factory',
  'item:create': 'factory',
  'item:decide': 'factory',
  'item:operate': 'factory',
  'andon:manage': 'factory',
  'definition:edit': 'factory',
  'definition:publish': 'factory',
};

const send = (res: ServerResponse, status: number, body: unknown, headers: Record<string, string> = {}) => {
  res.statusCode = status;
  for (const [name, value] of Object.entries(headers)) res.setHeader(name, value);
  if (status === 204) return res.end();
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const refuse = (res: ServerResponse, status: number, detail: string, code?: string) =>
  send(res, status, { title: status === 409 ? 'Conflict' : 'Bad Request', status, detail, ...(code ? { code } : {}) });

const base64url = (n: number) => {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
  return Array.from({ length: n }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
};

export interface MockKeys {
  /** Starts over with the seed's keys, dated from now. */
  reset: (now: number) => void;
  /** Serves the organization's key routes; false for any other. */
  handle: (
    org: Org,
    rest: string,
    method: string,
    req: IncomingMessage,
    res: ServerResponse,
    body: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
}

interface Deps {
  clock: () => number;
  /** Who the request acts for. */
  actor: (req: IncomingMessage) => Actor;
  /** The signed-in person; undefined under sign-in off. */
  person: (req: IncomingMessage) => MockPerson | undefined;
  /**
   * Whether the user is a member of the organization now: a removed member's tokens go with the
   * membership (goald's foreign key to auth.member cascades), so they are never listed again.
   */
  member: (org: string, user: string) => boolean;
  /** The organization's factories. */
  factories: (org: string) => Factory[];
  acme: string;
}

/** A person of the mock (people.ts) as goald names a user. */
const userOf = (p: MockPerson): User => ({ id: p.id, name: p.name, email: p.email });
const actorOf = (p: MockPerson): Actor => ({ kind: 'user', id: p.id, name: p.name });

const cruz = actorOf(directory.owner);
const okafor = actorOf(directory.okafor);
const cruzUser = userOf(directory.owner);
const okaforUser = userOf(directory.okafor);
const lindqvistUser = userOf(directory.lindqvist);

/** acme's personal tokens, dated from now. */
function seedTokens(now: number): PersonalToken[] {
  const at = (ms: number) => new Date(now + ms).toISOString();
  return [
    {
      id: '019a1c00-0000-7000-8000-0000000000d1',
      name: 'laptop',
      hint: 'goal_pat_Q7mk',
      scopes: ['factory:read', 'item:create', 'item:decide', 'org:read'],
      owner: cruzUser,
      created_at: at(-6 * dayMs),
      expires_at: at(24 * dayMs),
      last_used_at: at(-1 * hourMs),
      revoked_at: null,
      revoked_by: null,
    },
    {
      id: '019a1c00-0000-7000-8000-0000000000d2',
      name: 'old-desktop',
      hint: 'goal_pat_x2Vn',
      scopes: ['factory:read', 'org:read'],
      owner: cruzUser,
      created_at: at(-40 * dayMs),
      expires_at: at(20 * dayMs),
      last_used_at: at(-12 * dayMs),
      revoked_at: at(-10 * dayMs),
      revoked_by: cruz,
    },
    {
      id: '019a1c00-0000-7000-8000-0000000000d3',
      name: 'build-box',
      hint: 'goal_pat_Lr8w',
      scopes: ['factory:read', 'item:create', 'org:read'],
      owner: okaforUser,
      created_at: at(-2 * dayMs),
      expires_at: at(5 * dayMs),
      last_used_at: null,
      revoked_at: null,
      revoked_by: null,
    },
    {
      id: '019a1c00-0000-7000-8000-0000000000d4',
      name: 'laptop',
      hint: 'goal_pat_b4Ty',
      scopes: ['factory:read', 'item:decide', 'org:read'],
      owner: lindqvistUser,
      created_at: at(-15 * dayMs),
      expires_at: at(75 * dayMs),
      last_used_at: at(-3 * dayMs),
      revoked_at: null,
      revoked_by: null,
    },
  ];
}

/** The board's keys of acme, dated from now. */
function seedKeys(now: number, factories: Factory[]): ApiKey[] {
  const id = (name: string) => factories.find((f) => f.name === name)?.id ?? name;
  const at = (ms: number) => new Date(now + ms).toISOString();
  return [
    {
      id: '019a1c00-0000-7000-8000-0000000000e1',
      name: 'ci-deploy',
      hint: 'goal_sk_Ab3x',
      scopes: ['item:create', 'item:decide', 'org:read'],
      role_ceiling: 'approver',
      factories: [id('goal')],
      created_by: cruz,
      created_at: at(-22 * dayMs),
      expires_at: at(68 * dayMs),
      last_used_at: at(-2 * hourMs),
      revoked_at: null,
      revoked_by: null,
    },
    {
      id: '019a1c00-0000-7000-8000-0000000000e2',
      name: 'nightly-evals',
      hint: 'goal_sk_Q7mz',
      scopes: ['factory:read', 'audit:read'],
      role_ceiling: 'viewer',
      factories: null,
      created_by: okafor,
      created_at: at(-81 * dayMs),
      expires_at: at(9 * dayMs),
      last_used_at: at(-6 * hourMs),
      revoked_at: null,
      revoked_by: null,
    },
    {
      id: '019a1c00-0000-7000-8000-0000000000e3',
      name: 'definitions-sync',
      hint: 'goal_sk_t0Rk',
      scopes: ['definition:edit', 'definition:publish', 'factory:read'],
      role_ceiling: 'editor',
      factories: [id('goal')],
      created_by: cruz,
      created_at: at(-3 * dayMs),
      expires_at: at(87 * dayMs),
      last_used_at: null,
      revoked_at: null,
      revoked_by: null,
    },
    {
      id: '019a1c00-0000-7000-8000-0000000000e4',
      name: 'support-bot',
      hint: 'goal_sk_9fLe',
      scopes: ['item:create', 'factory:read', 'org:read'],
      role_ceiling: 'approver',
      factories: [id('support')],
      created_by: okafor,
      created_at: at(-24 * dayMs),
      expires_at: at(66 * dayMs),
      last_used_at: at(-5 * dayMs),
      revoked_at: at(-4 * dayMs),
      revoked_by: cruz,
    },
  ];
}

export function createKeys({ clock, actor, person, member, factories, acme }: Deps): MockKeys {
  let keys = new Map<string, ApiKey[]>();
  let tokens = new Map<string, PersonalToken[]>();

  const reset = (now: number) => {
    keys = new Map([[acme, seedKeys(now, factories(acme))]]);
    tokens = new Map([[acme, seedTokens(now)]]);
  };
  reset(clock());

  const listed = (org: string) => {
    const now = clock();
    return (keys.get(org) ?? [])
      .filter((k) => !k.revoked_at || now - Date.parse(k.revoked_at) < 30 * dayMs)
      .sort(
        (a, b) =>
          Number(Boolean(a.revoked_at)) - Number(Boolean(b.revoked_at)) || b.created_at.localeCompare(a.created_at),
      );
  };

  async function create(org: Org, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const name = typeof b.name === 'string' ? b.name.trim() : '';
    if (name.length < 1 || name.length > 80) return refuse(res, 400, "a key's name is 1 to 80 characters");
    const days = b.expires_in_days ?? 90;
    if (!Number.isInteger(days) || Number(days) < 1 || Number(days) > 90)
      return refuse(res, 400, 'a key expires in 1 to 90 days');
    const ceiling = b.role_ceiling as FactoryRole;
    if (!ladder.includes(ceiling)) return refuse(res, 400, 'role_ceiling is viewer, approver or editor');
    const scopes = Array.isArray(b.scopes) ? [...new Set(b.scopes.map(String))].sort() : [];
    if (scopes.length === 0) return refuse(res, 400, 'a key has 1 to 64 scopes');
    for (const s of scopes) {
      const role = keyScopes[s];
      if (role === undefined)
        return refuse(res, 400, `${s} is not a permission API keys may carry`, 'scope-not-allowed');
      if (role && ladder.indexOf(role) > ladder.indexOf(ceiling)) {
        return refuse(res, 400, `${s} needs a ceiling of at least ${role}`, 'scope-not-allowed');
      }
    }
    let reach: string[] | null = null;
    if (Array.isArray(b.factories)) {
      if (b.factories.length === 0) return refuse(res, 400, 'name at least one factory, or leave factories out');
      reach = [];
      for (const ref of b.factories.map(String)) {
        const f = factories(org.id).find((x) => x.id === ref || x.name === ref);
        if (!f) return send(res, 404, { title: 'Factory not found', status: 404, code: 'factory-not-found' });
        if (!reach.includes(f.id)) reach.push(f.id);
      }
    }
    const mine = keys.get(org.id) ?? [];
    if (mine.some((k) => !k.revoked_at && k.name.toLowerCase() === name.toLowerCase())) {
      return refuse(res, 409, `another live key is named ${name}`, 'api-key-name-taken');
    }
    const now = clock();
    const secret = `goal_sk_${base64url(43)}`;
    const key: ApiKey = {
      id: crypto.randomUUID(),
      name,
      hint: secret.slice(0, 12),
      scopes,
      role_ceiling: ceiling,
      factories: reach,
      created_by: actor(req),
      created_at: new Date(now).toISOString(),
      expires_at: new Date(now + Number(days) * dayMs).toISOString(),
      last_used_at: null,
      revoked_at: null,
      revoked_by: null,
    };
    keys.set(org.id, [...mine, key]);
    return send(res, 201, { key, secret }, { 'Cache-Control': 'no-store' });
  }

  /** Live tokens and those revoked in the last 30 days, live ones first, newest first. */
  const listedTokens = (org: string, owner?: string) => {
    const now = clock();
    return (tokens.get(org) ?? [])
      .filter(
        (t) =>
          member(org, t.owner.id) &&
          (owner === undefined || t.owner.id === owner) &&
          (!t.revoked_at || now - Date.parse(t.revoked_at) < 30 * dayMs),
      )
      .sort(
        (a, b) =>
          Number(Boolean(a.revoked_at)) - Number(Boolean(b.revoked_at)) || b.created_at.localeCompare(a.created_at),
      );
  };

  function createToken(org: Org, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const who = person(req);
    if (!who) {
      return send(res, 403, {
        title: 'Forbidden',
        status: 403,
        detail: 'personal tokens need sign-in (GOAL_AUTH=session)',
        code: 'forbidden',
      });
    }
    const me = userOf(who);
    const name = typeof b.name === 'string' ? b.name.trim() : '';
    if (name.length < 1 || name.length > 80) return refuse(res, 400, "a token's name is 1 to 80 characters");
    const days = b.expires_in_days ?? 30;
    if (!Number.isInteger(days) || Number(days) < 1 || Number(days) > 90)
      return refuse(res, 400, 'a token expires in 1 to 90 days');
    const scopes = Array.isArray(b.scopes) ? [...new Set(b.scopes.map(String))].sort() : [];
    if (scopes.length === 0) return refuse(res, 400, 'a token has 1 to 64 scopes');
    for (const s of scopes) {
      const level = tokenScopes[s];
      if (level === undefined)
        return refuse(res, 400, `${s} is not a permission personal tokens may carry`, 'scope-not-allowed');
      const held =
        level === 'org'
          ? s === 'authenticated' || org.can.includes(s)
          : factories(org.id).some((f) => f.can.includes(s));
      if (!held) return refuse(res, 400, `you do not hold ${s}`, 'scope-not-allowed');
    }
    const mine = tokens.get(org.id) ?? [];
    if (mine.some((t) => t.owner.id === me.id && !t.revoked_at && t.name.toLowerCase() === name.toLowerCase())) {
      return refuse(res, 409, `another live token of yours is named ${name}`, 'personal-token-name-taken');
    }
    const now = clock();
    const secret = `goal_pat_${base64url(43)}`;
    const token: PersonalToken = {
      id: crypto.randomUUID(),
      name,
      hint: secret.slice(0, 13),
      scopes,
      owner: me,
      created_at: new Date(now).toISOString(),
      expires_at: new Date(now + Number(days) * dayMs).toISOString(),
      last_used_at: null,
      revoked_at: null,
      revoked_by: null,
    };
    tokens.set(org.id, [...mine, token]);
    return send(res, 201, { token, secret }, { 'Cache-Control': 'no-store' });
  }

  /** Revokes the token of org with id (the owner's only, when owner is set); false: no such token. */
  const revokeToken = (org: string, id: string, by: Actor, owner?: string) => {
    const token = (tokens.get(org) ?? []).find((t) => t.id === id && (owner === undefined || t.owner.id === owner));
    if (!token) return false;
    if (!token.revoked_at) {
      token.revoked_at = new Date(clock()).toISOString();
      token.revoked_by = by;
    }
    return true;
  };

  const notFound = (res: ServerResponse) =>
    send(res, 404, { title: 'Not Found', status: 404, detail: 'no such personal token' });

  return {
    reset,
    async handle(org, rest, method, req, res, body) {
      if (rest === '/personal-tokens' && method === 'GET') {
        const me = person(req);
        send(res, 200, { tokens: me ? listedTokens(org.id, me.id) : [] });
        return true;
      }
      if (rest === '/personal-tokens' && method === 'POST') {
        createToken(org, req, res, await body());
        return true;
      }
      const own = rest.match(/^\/personal-tokens\/([^/]+)$/);
      if (own && method === 'DELETE') {
        const me = person(req);
        const id = decodeURIComponent(own[1] ?? '');
        if (me && revokeToken(org.id, id, actor(req), me.id)) send(res, 204, null);
        else notFound(res);
        return true;
      }
      if (rest === '/member-tokens' && method === 'GET') {
        send(res, 200, { tokens: listedTokens(org.id) });
        return true;
      }
      const member = rest.match(/^\/member-tokens\/([^/]+)$/);
      if (member && method === 'DELETE') {
        if (revokeToken(org.id, decodeURIComponent(member[1] ?? ''), actor(req))) send(res, 204, null);
        else notFound(res);
        return true;
      }
      if (rest === '/api-keys' && method === 'GET') {
        send(res, 200, { keys: listed(org.id) });
        return true;
      }
      if (rest === '/api-keys' && method === 'POST') {
        await create(org, req, res, await body());
        return true;
      }
      const m = rest.match(/^\/api-keys\/([^/]+)$/);
      if (m && method === 'DELETE') {
        const key = (keys.get(org.id) ?? []).find((k) => k.id === decodeURIComponent(m[1] ?? ''));
        if (!key) {
          send(res, 404, { title: 'Not Found', status: 404, detail: 'no such API key' });
          return true;
        }
        if (!key.revoked_at) {
          key.revoked_at = new Date(clock()).toISOString();
          key.revoked_by = actor(req);
        }
        send(res, 204, null);
        return true;
      }
      return false;
    },
  };
}
