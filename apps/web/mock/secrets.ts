// The organizations' secrets for the mock API (ADR 0037): GET and POST …/secrets, POST
// …/secrets/import, POST …/repositories/test (a token's read and push check), GET, PATCH and DELETE …/secrets/{secret}, PUT …/secrets/{secret}/value, and the
// audit entries about a secret (GET …/audit?target_kind=secret), refusing as goald does
// (secret-invalid, secret-not-found, secret-name-taken, secret-in-use naming the uses, a fresh sign-in
// for every change). A value is never kept: a create, a replace and an import are acknowledged and
// the value dropped, and no answer carries one. acme starts with the nine secrets of the Secrets
// picture; their dates are set from the mock clock, so the reminder window shows whenever the mock runs.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, AuditEntry, Factory, Org } from '@goal/api-client';
import type { Secret, SecretUse } from '../src/api/types.ts';

/** The contract's SecretName pattern (contracts/api/openapi.yaml). */
const secretName = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

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

export interface MockSecrets {
  reset: (now: number) => void;
  /** The organization's secrets by name, as GET /secrets lists them; never a value. */
  outlines: (org: string) => Secret[];
  handle: (
    org: Org,
    rest: string,
    method: string,
    url: URL,
    req: IncomingMessage,
    res: ServerResponse,
    body: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
}

/** A seeded secret: its scope by factory name, which the answer resolves to ids. */
interface Seed {
  name: string;
  description: string;
  /** The factories' names, or null for every factory. */
  factories: string[] | null;
  /** Days from now until it expires, and the reminder's days before that; null: no expiry. */
  expiresInDays: number | null;
  remindDays: number;
  /** Milliseconds before now of the last run that opened it; null: never. */
  usedAgo: number | null;
  uses: SecretUse[];
}

interface Stored {
  secret: Secret;
  /** Scope by factory name, so a seed written before the factories exist resolves in `out`. */
  scopeNames: string[] | null;
  activity: AuditEntry[];
}

const send = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  if (status === 204) return res.end();
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const refuse = (res: ServerResponse, status: number, detail: string, code: string, extra: object = {}) =>
  send(res, status, {
    title: { 400: 'Bad Request', 404: 'Not Found', 409: 'Conflict' }[status] ?? 'Error',
    status,
    detail,
    code,
    ...extra,
  });

const def = (name: string): SecretUse => ({ kind: 'definition', name });

function seeds(): Seed[] {
  return [
    {
      name: 'github-token',
      description: 'Fine-grained GitHub token',
      factories: null,
      expiresInDays: 365,
      remindDays: 14,
      usedAgo: 2 * minuteMs,
      uses: [
        def('software'),
        def('sre'),
        def('support'),
        { kind: 'knowledge_source', name: 'goal-docs' },
        { kind: 'connection', name: 'github' },
      ],
    },
    {
      name: 'linear-key',
      description: 'Linear API key',
      factories: ['software'],
      expiresInDays: null,
      remindDays: 0,
      usedAgo: hourMs,
      uses: [def('software')],
    },
    {
      name: 'sentry-token',
      description: 'Sentry auth token',
      factories: ['software', 'sre'],
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 26 * hourMs,
      uses: [def('sre')],
    },
    {
      name: 'dd-api-key',
      description: 'Datadog API key',
      factories: ['software', 'sre'],
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 3 * hourMs,
      uses: [def('software'), { kind: 'webhook', name: 'datadog-alerts' }],
    },
    {
      name: 'cloudflare-deploy',
      description: 'Workers and R2, edit',
      factories: ['software'],
      expiresInDays: 3,
      remindDays: 7,
      usedAgo: hourMs,
      uses: [def('software')],
    },
    {
      name: 'kube-staging',
      description: 'Staging kubeconfig',
      factories: ['software'],
      expiresInDays: 130,
      remindDays: 14,
      usedAgo: 7 * dayMs,
      uses: [def('software')],
    },
    {
      name: 'claude',
      description: 'Claude subscription token',
      factories: null,
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 0,
      uses: [def('software'), { kind: 'slack', name: 'Slack' }, { kind: 'connection', name: 'claude' }],
    },
    {
      name: 'slack-bot',
      description: 'Slack bot token',
      factories: null,
      expiresInDays: null,
      remindDays: 0,
      usedAgo: hourMs,
      uses: [{ kind: 'slack', name: 'Slack' }],
    },
    {
      name: 'zendesk-token',
      description: 'Zendesk API token',
      factories: ['support'],
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 2 * hourMs,
      uses: [def('support')],
    },
    {
      name: 'confluence-token',
      description: 'Confluence API token',
      factories: null,
      expiresInDays: null,
      remindDays: 0,
      usedAgo: 4 * hourMs,
      uses: [{ kind: 'knowledge_source', name: 'product-specs' }],
    },
    {
      name: 'old-npm-token',
      description: 'npm publish token',
      factories: ['software'],
      expiresInDays: null,
      remindDays: 0,
      usedAgo: null,
      uses: [],
    },
  ];
}

export function createSecrets({ clock, actor, stale, factories, acme }: Deps): MockSecrets {
  let stored = new Map<string, Stored[]>();
  let nextAudit = 1;

  const entry = (id: string, by: Actor, action: string, at: number, data: Record<string, unknown> = {}) =>
    ({
      id: nextAudit++,
      at: new Date(at).toISOString(),
      actor: by,
      action,
      target: { kind: 'secret', id },
      data,
    }) satisfies AuditEntry;

  const reset = (now: number) => {
    const by: Actor = { kind: 'user', id: 'local', name: 'Local user' };
    stored = new Map([
      [
        acme,
        seeds().map((seed) => {
          const id = `secret-${seed.name}`;
          const secret: Secret = {
            id,
            name: seed.name,
            source: 'value',
            description: seed.description,
            scope: { all_factories: seed.factories === null, factories: [] },
            expires_at: seed.expiresInDays === null ? null : new Date(now + seed.expiresInDays * dayMs).toISOString(),
            remind_days: seed.remindDays,
            last_used_at: seed.usedAgo === null ? null : new Date(now - seed.usedAgo).toISOString(),
            uses: seed.uses,
            created_by: by,
            created_at: new Date(now - 10 * dayMs).toISOString(),
            updated_by: by,
            updated_at: new Date(now - 3 * dayMs).toISOString(),
          };
          const activity = [
            ...(seed.usedAgo === null
              ? []
              : [
                  entry(
                    id,
                    { kind: 'agent', id: 'frontend-engineer', name: 'frontend-engineer' },
                    'secret.used',
                    now - seed.usedAgo,
                    { run: 147, role: 'frontend-engineer' },
                  ),
                ]),
            entry(id, by, 'secret.value_replaced', now - 3 * dayMs),
            entry(id, by, 'secret.created', now - 10 * dayMs),
          ];
          return { secret, scopeNames: seed.factories, activity };
        }),
      ],
    ]);
  };
  reset(clock());

  /** The organization's secrets; an organization with none yet (one made after the reset) gets its own list. */
  const list = (org: string) => {
    let secrets = stored.get(org);
    if (!secrets) {
      secrets = [];
      stored.set(org, secrets);
    }
    return secrets;
  };
  const named = (org: string, name: string) => list(org).find((s) => s.secret.name === name);

  /** The secret as the contract has it: its scope as factory ids. */
  const out = (org: string, s: Stored): Secret => {
    const byName = new Map(factories(org).map((f) => [f.name, f.id]));
    const ids = (s.scopeNames ?? []).flatMap((name) => byName.get(name) ?? []);
    return {
      ...structuredClone(s.secret),
      scope: { all_factories: s.scopeNames === null, factories: s.scopeNames === null ? [] : ids },
    };
  };

  /** The scope of a request as factory names, or null for every factory. */
  const scopeOf = (org: string, scope: unknown): string[] | null => {
    const input = scope as { all_factories?: boolean; factories?: string[] } | undefined;
    if (input?.all_factories === true) return null;
    const byId = new Map(factories(org).map((f) => [f.id, f.name]));
    return (input?.factories ?? []).flatMap((id) => byId.get(id) ?? []);
  };

  /** The variable names an import's text defines: a .env's keys, or the keys under a YAML secrets: map; null when it does not parse. */
  const namesOf = (format: string, text: string): string[] | null => {
    const lines = text.split('\n');
    if (format === 'env') {
      return lines.flatMap((line) => {
        const key = /^\s*([A-Za-z_][A-Za-z0-9_]*)=/.exec(line)?.[1];
        return key ? [key.toLowerCase().replace(/_/g, '-')] : [];
      });
    }
    const top = lines.findIndex((line) => /^secrets:\s*$/.test(line));
    if (top < 0) return null;
    return lines.slice(top + 1).flatMap((line) => {
      const key = /^\s+([^\s:#]+):/.exec(line)?.[1];
      return key ? [key] : [];
    });
  };

  const add = (org: string, name: string, input: Record<string, unknown>, by: Actor, now: number): Stored => {
    const id = `secret-${name}`;
    const secret: Secret = {
      id,
      name,
      source: 'value',
      description: typeof input.description === 'string' ? input.description : '',
      scope: { all_factories: false, factories: [] },
      expires_at: typeof input.expires_at === 'string' ? input.expires_at : null,
      remind_days: typeof input.remind_days === 'number' ? input.remind_days : 0,
      last_used_at: null,
      uses: [],
      created_by: by,
      created_at: new Date(now).toISOString(),
      updated_by: by,
      updated_at: new Date(now).toISOString(),
    };
    const s: Stored = {
      secret,
      scopeNames: scopeOf(org, input.scope) ?? null,
      activity: [entry(id, by, 'secret.created', now)],
    };
    if (input.scope === undefined) s.scopeNames = [];
    list(org).push(s);
    return s;
  };

  const invalid = (res: ServerResponse, detail: string) => refuse(res, 400, detail, 'secret-invalid');

  const outlines = (org: string): Secret[] =>
    list(org)
      .map((s) => out(org, s))
      .sort((a, b) => a.name.localeCompare(b.name));

  return {
    reset,
    outlines,
    async handle(org, rest, method, url, req, res, body) {
      if (rest === '/repositories/test' && method === 'POST') {
        // The token's read and push check (ADR 0037): a repository named *-readonly refuses the push, one named *-missing the read.
        const input = await body();
        const address = typeof input.url === 'string' ? input.url : '';
        const found = typeof input.secret === 'string' ? named(org.id, input.secret) : undefined;
        if (!found) {
          refuse(res, 404, `secret "${String(input.secret)}" not found`, 'secret-not-found');
          return true;
        }
        if (!/^https:\/\/(github|gitlab)\.com\/[^/]+\/[^/]+/.test(address)) {
          refuse(res, 400, `"${address}" names no repository on github.com or gitlab.com`, 'repository-url-invalid');
          return true;
        }
        const noRead = /-missing(\.git)?$/.test(address);
        const noPush = /-readonly(\.git)?$/.test(address);
        send(res, 200, {
          tested: true,
          read: noRead
            ? { ok: false, reason: 'Repository not found, or the token cannot see it.' }
            : { ok: true, reason: 'Cloned' },
          push:
            noRead || noPush
              ? { ok: false, reason: 'The token has no write access to this repository.' }
              : { ok: true, reason: 'Can push' },
        });
        return true;
      }
      if (/^\/factories\/[^/]+\/repositories\/test$/.test(rest) && method === 'POST') {
        // The factory's check (ADR 0041): as the organization's, in the FactoryRepositoryTest answer.
        const input = await body();
        const address = typeof input.url === 'string' ? input.url : '';
        const access = input.access === 'read' ? 'read' : 'write';
        if (typeof input.secret !== 'string' || !named(org.id, input.secret)) {
          refuse(res, 404, `secret "${String(input.secret)}" not found`, 'secret-not-found');
          return true;
        }
        if (!/^https:\/\/(github|gitlab)\.com\/[^/]+\/(\*|[^/]+)/.test(address)) {
          refuse(res, 400, `"${address}" names no repository on github.com or gitlab.com`, 'repository-url-invalid');
          return true;
        }
        const noRead = /-missing(\.git)?$/.test(address);
        const noPush = access === 'write' && /-readonly(\.git)?$/.test(address);
        const good = !noRead && !noPush;
        send(res, 200, {
          url: address.replace(/^https:\/\//, '').toLowerCase(),
          access,
          secret: input.secret,
          at: new Date(clock()).toISOString(),
          tested: true,
          ok: good,
          summary: noRead
            ? 'Can’t read.'
            : noPush
              ? 'Can read but not push.'
              : access === 'read'
                ? 'Can read'
                : 'Can read and push',
          read: noRead
            ? { ok: false, reason: 'Repository not found, or the token cannot see it.' }
            : { ok: true, reason: 'Cloned' },
          push: good
            ? { ok: true, reason: 'Can push' }
            : { ok: false, reason: 'The token has no write access to this repository.' },
        });
        return true;
      }
      if (rest === '/audit' && method === 'GET' && url.searchParams.get('target_kind') === 'secret') {
        const id = url.searchParams.get('target_id');
        const entries = list(org.id).find((s) => s.secret.id === id)?.activity ?? [];
        send(res, 200, { entries, next_before: null });
        return true;
      }
      if (rest === '/secrets' && method === 'GET') {
        send(res, 200, { secrets: outlines(org.id) });
        return true;
      }
      if (rest === '/secrets' && method === 'POST') {
        if (stale(req, res)) return true;
        const input = await body();
        const name = typeof input.name === 'string' ? input.name : '';
        if (!secretName.test(name)) {
          invalid(res, `"${name}" is not a secret name`);
          return true;
        }
        if (typeof input.value !== 'string' || input.value === '') {
          invalid(res, 'the value is missing');
          return true;
        }
        if (named(org.id, name)) {
          refuse(res, 409, `secret "${name}" already exists`, 'secret-name-taken');
          return true;
        }
        send(res, 201, out(org.id, add(org.id, name, input, actor(req), clock())));
        return true;
      }
      if (rest === '/secrets/import' && method === 'POST') {
        if (stale(req, res)) return true;
        const input = await body();
        const format = input.format;
        const text = typeof input.text === 'string' ? input.text : '';
        const names = format === 'env' || format === 'yaml' ? namesOf(format, text) : null;
        if (!names || names.length === 0 || names.some((name) => !secretName.test(name))) {
          invalid(res, 'the import does not parse, is empty, or a name is not a secret name');
          return true;
        }
        const dryRun = input.dry_run === true;
        const onConflict = input.on_conflict === 'replace' ? 'replace' : 'skip';
        const outcomes = [...new Set(names)].map((name) => {
          const existing = named(org.id, name);
          if (!existing) {
            if (!dryRun) add(org.id, name, {}, actor(req), clock());
            return { name, exists: false, uses: [], ...(dryRun ? {} : { outcome: 'created' as const }) };
          }
          const uses = existing.secret.uses;
          if (dryRun) return { name, exists: true, uses };
          if (onConflict === 'replace') {
            existing.secret.updated_by = actor(req);
            existing.secret.updated_at = new Date(clock()).toISOString();
            existing.activity.unshift(entry(existing.secret.id, actor(req), 'secret.value_replaced', clock()));
            return { name, exists: true, uses, outcome: 'replaced' as const };
          }
          return { name, exists: true, uses, outcome: 'skipped' as const };
        });
        send(res, 200, { dry_run: dryRun, secrets: outcomes });
        return true;
      }
      const m = rest.match(/^\/secrets\/([^/]+)(\/value)?$/);
      if (!m) return false;
      const isValue = m[2] !== undefined;
      if (!(isValue ? ['PUT'] : ['GET', 'PATCH', 'DELETE']).includes(method)) return false;
      if (method !== 'GET' && stale(req, res)) return true;
      const name = decodeURIComponent(m[1] ?? '');
      const s = named(org.id, name);
      if (!s) {
        refuse(res, 404, `secret "${name}" not found`, 'secret-not-found');
        return true;
      }
      const by = actor(req);
      const touch = (action: string, data: Record<string, unknown> = {}) => {
        s.secret.updated_by = by;
        s.secret.updated_at = new Date(clock()).toISOString();
        s.activity.unshift(entry(s.secret.id, by, action, clock(), data));
      };
      if (method === 'GET') {
        send(res, 200, out(org.id, s));
        return true;
      }
      if (method === 'PUT') {
        const input = await body();
        if (typeof input.value !== 'string' || input.value === '') {
          invalid(res, 'the value is missing');
          return true;
        }
        touch('secret.value_replaced');
        send(res, 200, out(org.id, s));
        return true;
      }
      if (method === 'PATCH') {
        const patch = await body();
        if (patch.description === undefined && patch.scope === undefined && patch.expiry === undefined) {
          invalid(res, 'the patch changes nothing');
          return true;
        }
        if (typeof patch.description === 'string') s.secret.description = patch.description;
        if (patch.scope !== undefined) s.scopeNames = scopeOf(org.id, patch.scope);
        if (patch.expiry !== undefined) {
          const expiry = patch.expiry as { expires_at?: string; remind_days?: number };
          s.secret.expires_at = expiry.expires_at ?? null;
          s.secret.remind_days = expiry.expires_at ? (expiry.remind_days ?? 0) : 0;
        }
        touch('secret.updated');
        send(res, 200, out(org.id, s));
        return true;
      }
      if (s.secret.uses.length > 0) {
        refuse(
          res,
          409,
          `secret ${name} is in use by ${s.secret.uses.map((u) => u.name).join(', ')}`,
          'secret-in-use',
          {
            uses: s.secret.uses,
          },
        );
        return true;
      }
      list(org.id).splice(list(org.id).indexOf(s), 1);
      send(res, 204, null);
      return true;
    },
  };
}
