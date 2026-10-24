// The organizations' API keys for the mock API (spec 3 §8.9): GET, POST /v1/orgs/{org}/api-keys
// and DELETE …/api-keys/{key}, refusing as goald does (scope-not-allowed, api-key-name-taken,
// factory-not-found, 400 for a malformed key). A created key's secret is answered once with
// Cache-Control: no-store and kept nowhere; the mock keeps its hint. acme starts with the
// board's keys: three live ones and one revoked.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, ApiKey, Factory, FactoryRole, Org } from '@goal/api-client';

const dayMs = 86_400_000;
const hourMs = 3_600_000;

/** The permissions keys may carry and, for a factory permission, the lowest role holding it. */
const keyScopes: Record<string, FactoryRole | null> = {
  authenticated: null,
  'org:read': null,
  'audit:read': null,
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
  /** The organization's factories. */
  factories: (org: string) => Factory[];
  acme: string;
}

const cruz: Actor = { kind: 'user', id: '019a1c00-0000-7000-8000-00000000c001', name: 'F. Cruz' };
const okafor: Actor = { kind: 'user', id: '019a1c00-0000-7000-8000-00000000c002', name: 'M. Okafor' };

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

export function createKeys({ clock, actor, factories, acme }: Deps): MockKeys {
  let keys = new Map<string, ApiKey[]>();

  const reset = (now: number) => {
    keys = new Map([[acme, seedKeys(now, factories(acme))]]);
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

  return {
    reset,
    async handle(org, rest, method, req, res, body) {
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
