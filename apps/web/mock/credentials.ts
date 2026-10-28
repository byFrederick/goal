// The organizations' credentials for the mock API (ADR 0017): GET …/credentials, GET, PATCH and
// DELETE …/credentials/{credential}, refusing as goald does (credential-not-found,
// credential-name-taken, credential-in-use naming the connections, a fresh sign-in for every
// change). A value is never kept: a replace is acknowledged and dropped, and no answer carries
// one. acme starts with the seven credentials of the Credentials picture; their expiry dates are
// set from the mock clock, so the reminder window shows whenever the mock runs.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, Credential, CredentialType, Factory, Org } from '@goal/api-client';

/** The contract's CredentialName pattern (contracts/api/openapi.yaml). */
const credentialName = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

const minuteMs = 60_000;
const hourMs = 3_600_000;
const dayMs = 86_400_000;

interface Deps {
  clock: () => number;
  actor: (req: IncomingMessage) => Actor;
  stale: (req: IncomingMessage, res: ServerResponse) => boolean;
  factories: (org: string) => Factory[];
  acme: string;
}

export interface MockCredentials {
  reset: (now: number) => void;
  handle: (
    org: Org,
    rest: string,
    method: string,
    req: IncomingMessage,
    res: ServerResponse,
    body: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
}

/** A seeded credential: its scope and agents by factory name, which the answer resolves to ids. */
interface Seed {
  name: string;
  type: CredentialType;
  description: string;
  /** The factories' names, or null for every factory. */
  factories: string[] | null;
  /** Per factory name, the agents that get it. */
  agents: Record<string, string[]>;
  /** Days from now until it expires, and the reminder's days before that; null: no expiry. */
  expiresInDays: number | null;
  remindDays: number;
  /** Milliseconds before now of the last run that opened it; null: never. */
  usedAgo: number | null;
  /** The connections that refer to it as {{name}}. */
  connections: string[];
}

interface Stored {
  credential: Credential;
  seed: Seed;
}

const send = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  if (status === 204) return res.end();
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const refuse = (res: ServerResponse, status: number, detail: string, code: string) =>
  send(res, status, { title: status === 404 ? 'Not Found' : 'Conflict', status, detail, code });

function seeds(): Seed[] {
  return [
    {
      name: 'github-app',
      type: 'app_key',
      description: 'The GitHub App that opens pull requests',
      factories: null,
      agents: {},
      expiresInDays: 46,
      remindDays: 14,
      usedAgo: 2 * minuteMs,
      connections: ['github'],
    },
    {
      name: 'cloudflare-token',
      type: 'api_key',
      description: 'Deploys the preview workers',
      factories: ['software', 'sre'],
      agents: {},
      expiresInDays: 2,
      remindDays: 7,
      usedAgo: hourMs,
      connections: ['cloudflare', 'cloudflare-preview'],
    },
    {
      name: 'claude-subscription',
      type: 'oauth_token',
      description: 'The model subscription every agent runs on',
      factories: null,
      agents: {},
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 0,
      connections: ['claude'],
    },
    {
      name: 'zendesk-api',
      type: 'api_key',
      description: 'Reads and answers support tickets',
      factories: ['support'],
      agents: {},
      expiresInDays: 69,
      remindDays: 14,
      usedAgo: 2 * hourMs,
      connections: ['zendesk'],
    },
    {
      name: 'runbooks-token',
      type: 'api_key',
      description: 'The runbooks MCP server',
      factories: ['sre'],
      agents: {},
      expiresInDays: null,
      remindDays: 0,
      usedAgo: dayMs,
      connections: ['runbooks'],
    },
    {
      name: 'deploy-preview-ssh',
      type: 'ssh_key',
      description: 'Logs in to the preview hosts',
      factories: ['software'],
      agents: { software: ['infra-engineer'] },
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 7 * dayMs,
      connections: [],
    },
    {
      name: 'gcp-readonly',
      type: 'cloud_role',
      description: 'Read-only access for incident investigation',
      factories: ['sre'],
      agents: { sre: ['investigator'] },
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 3 * hourMs,
      connections: [],
    },
  ];
}

export function createCredentials({ clock, actor, stale, factories, acme }: Deps): MockCredentials {
  let stored = new Map<string, Stored[]>();

  const reset = (now: number) => {
    const by: Actor = { kind: 'user', id: 'local', name: 'Local user' };
    stored = new Map([
      [
        acme,
        seeds().map((seed) => ({
          seed,
          credential: {
            id: `cred-${seed.name}`,
            name: seed.name,
            type: seed.type,
            description: seed.description,
            scope: { all_factories: seed.factories === null, factories: [] },
            agents: {},
            expires_at: seed.expiresInDays === null ? null : new Date(now + seed.expiresInDays * dayMs).toISOString(),
            remind_days: seed.remindDays,
            last_used_at: seed.usedAgo === null ? null : new Date(now - seed.usedAgo).toISOString(),
            used_by: { connections: [], agents: [] },
            created_by: by,
            created_at: new Date(now - 90 * dayMs).toISOString(),
            updated_by: by,
            updated_at: new Date(now - 90 * dayMs).toISOString(),
          },
        })),
      ],
    ]);
  };
  reset(clock());

  const list = (org: string) => stored.get(org) ?? [];

  /** The credential as the contract has it: its scope and agents as factory ids. */
  const out = (org: string, s: Stored): Credential => {
    const byName = new Map(factories(org).map((f) => [f.name, f.id]));
    const seed = s.seed;
    const ids = (seed.factories ?? []).flatMap((name) => byName.get(name) ?? []);
    const agents = Object.fromEntries(
      Object.entries(seed.agents).flatMap(([name, list]) => (byName.has(name) ? [[byName.get(name), list]] : [])),
    );
    return {
      ...structuredClone(s.credential),
      scope: { all_factories: seed.factories === null, factories: seed.factories === null ? [] : ids },
      agents,
      used_by: {
        connections: [...seed.connections].sort(),
        agents: Object.entries(seed.agents).flatMap(([name, list]) =>
          list.map((agent) => ({ factory_id: byName.get(name) ?? name, agent })),
        ),
      },
    };
  };

  const named = (org: string, name: string) => list(org).find((s) => s.credential.name === name);

  /** The names an import's text defines: a .env's keys, or the keys under a YAML credentials: map; null when it does not parse. */
  const namesOf = (format: string, text: string): string[] | null => {
    const lines = text.split('\n');
    if (format === 'env') {
      return lines.flatMap((line) => {
        const key = /^\s*([A-Za-z_][A-Za-z0-9_]*)=/.exec(line)?.[1];
        return key ? [key.toLowerCase().replace(/_/g, '-')] : [];
      });
    }
    const top = lines.findIndex((line) => /^credentials:\s*$/.test(line));
    if (top < 0) return null;
    return lines.slice(top + 1).flatMap((line) => {
      const key = /^\s+([^\s:#]+):/.exec(line)?.[1];
      return key ? [key] : [];
    });
  };

  /** A new credential as the contract has it: no value is kept, and the scope and agents are the seed's by name. */
  const add = (
    org: string,
    name: string,
    input: Partial<Credential>,
    scope: string[] | null,
    agents: Record<string, string[]>,
    by: Actor,
    now: number,
  ) => {
    const expiresAt = input.expires_at ?? null;
    const seed: Seed = {
      name,
      type: input.type ?? 'api_key',
      description: input.description ?? '',
      factories: scope,
      agents,
      expiresInDays: null,
      remindDays: input.remind_days ?? 0,
      usedAgo: null,
      connections: [],
    };
    const credential: Credential = {
      id: `cred-${name}`,
      name,
      type: seed.type,
      description: seed.description,
      scope: { all_factories: scope === null, factories: [] },
      agents: {},
      expires_at: expiresAt,
      remind_days: seed.remindDays,
      last_used_at: null,
      used_by: { connections: [], agents: [] },
      created_by: by,
      created_at: new Date(now).toISOString(),
      updated_by: by,
      updated_at: new Date(now).toISOString(),
    };
    list(org).push({ credential, seed });
    return credential;
  };

  return {
    reset,
    async handle(org, rest, method, req, res, body) {
      if (rest === '/credentials' && method === 'GET') {
        const credentials = list(org.id)
          .map((s) => out(org.id, s))
          .sort((a, b) => a.name.localeCompare(b.name));
        send(res, 200, { credentials });
        return true;
      }
      if (rest === '/credentials' && method === 'POST') {
        if (stale(req, res)) return true;
        const input = (await body()) as Partial<Credential> & Record<string, unknown>;
        const name = typeof input.name === 'string' ? input.name : '';
        if (!credentialName.test(name) || typeof input.value !== 'string' || input.value === '') {
          send(res, 400, {
            title: 'Bad Request',
            status: 400,
            detail: `"${name}" is not a credential name`,
            code: 'credential-invalid',
          });
          return true;
        }
        if (named(org.id, name)) {
          refuse(res, 409, `credential "${name}" already exists`, 'credential-name-taken');
          return true;
        }
        const scope = input.scope as { all_factories?: boolean; factories?: string[] } | undefined;
        const everyFactory = scope?.all_factories === true;
        const byId = new Map(factories(org.id).map((f) => [f.id, f.name]));
        const names = (scope?.factories ?? []).flatMap((id) => byId.get(id) ?? []);
        const agents = Object.fromEntries(
          Object.entries((input.agents ?? {}) as Record<string, string[]>).flatMap(([id, list]) =>
            byId.has(id) ? [[byId.get(id) as string, list]] : [],
          ),
        );
        const saved = add(org.id, name, input, everyFactory ? null : names, agents, actor(req), clock());
        send(res, 201, out(org.id, named(org.id, saved.name) as Stored));
        return true;
      }
      if (rest === '/credentials/import' && method === 'POST') {
        if (stale(req, res)) return true;
        const input = await body();
        const format = input.format;
        const text = typeof input.text === 'string' ? input.text : '';
        const names = format === 'env' || format === 'yaml' ? namesOf(format, text) : null;
        if (!names || names.some((name) => !credentialName.test(name))) {
          send(res, 400, {
            title: 'Bad Request',
            status: 400,
            detail: 'the import does not parse, or a name is not a credential name',
            code: 'credential-invalid',
          });
          return true;
        }
        const dryRun = input.dry_run === true;
        const onConflict = input.on_conflict === 'replace' ? 'replace' : 'skip';
        const unique = [...new Set(names)];
        const outcomes = unique.map((name) => {
          const existing = named(org.id, name);
          if (!existing) {
            if (!dryRun) add(org.id, name, {}, [], {}, actor(req), clock());
            return { name, exists: false, connections: 0, ...(dryRun ? {} : { outcome: 'created' as const }) };
          }
          const connections = existing.seed.connections.length;
          if (dryRun) return { name, exists: true, connections };
          if (onConflict === 'replace') {
            existing.credential.updated_by = actor(req);
            existing.credential.updated_at = new Date(clock()).toISOString();
            return { name, exists: true, connections, outcome: 'replaced' as const };
          }
          return { name, exists: true, connections, outcome: 'skipped' as const };
        });
        send(res, 200, { dry_run: dryRun, credentials: outcomes });
        return true;
      }
      const m = rest.match(/^\/credentials\/([^/]+)$/);
      if (!m) return false;
      if (!['GET', 'PATCH', 'DELETE'].includes(method)) return false;
      if (method !== 'GET' && stale(req, res)) return true;
      const name = decodeURIComponent(m[1] ?? '');
      const s = named(org.id, name);
      if (!s) {
        refuse(res, 404, `credential "${name}" not found`, 'credential-not-found');
        return true;
      }
      if (method === 'GET') {
        send(res, 200, out(org.id, s));
        return true;
      }
      if (method === 'PATCH') {
        const patch = await body();
        const next = typeof patch.name === 'string' ? patch.name : name;
        if (next !== name && named(org.id, next)) {
          refuse(res, 409, `credential "${next}" already exists`, 'credential-name-taken');
          return true;
        }
        // The value is never kept: a replace is acknowledged and only moves updated_at.
        s.credential.name = next;
        if (typeof patch.description === 'string') s.credential.description = patch.description;
        s.credential.updated_by = actor(req);
        s.credential.updated_at = new Date(clock()).toISOString();
        send(res, 200, out(org.id, s));
        return true;
      }
      if (s.seed.connections.length > 0) {
        refuse(
          res,
          409,
          `credential ${name} is in use by connections ${s.seed.connections.join(', ')}`,
          'credential-in-use',
        );
        return true;
      }
      list(org.id).splice(list(org.id).indexOf(s), 1);
      send(res, 204, null);
      return true;
    },
  };
}
