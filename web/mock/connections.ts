// The organizations' connections and connection kinds for the mock API (spec 4 §8.2): GET
// /v1/orgs/{org}/connection-kinds, PUT and DELETE …/connection-kinds/{kind}, GET and POST
// …/connections, GET, PATCH and DELETE …/connections/{connection} and POST …/{connection}/test,
// refusing as goald does (field-unknown, field-required, connection-name-taken,
// connection-kind-not-found, connection-in-use, connection-not-testable, connection-kind-invalid,
// connection-kind-name-taken, connection-kind-in-use, a fresh sign-in for every change). The
// platform's kinds are goald's own files (internal/connkind/kinds), read once. Secret values are
// write-only: kept nowhere but as the names of the secret fields set. Each change and test is
// audited and broadcast as `event: connection`. acme starts with GitHub and a Claude subscription
// that its goal factory names, a failing Datadog and a Playwright MCP server.
import { readdirSync, readFileSync } from 'node:fs';
import type { IncomingMessage, ServerResponse } from 'node:http';
import type {
  Actor,
  Connection,
  ConnectionEvent,
  ConnectionKind,
  ConnectionKindField,
  ConnectionTestResult,
  Factory,
  Org,
} from '@goal/api-client';
import { parse } from 'yaml';

const dayMs = 86_400_000;
const hourMs = 3_600_000;

const namePattern = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/** A kind as the mock keeps it: its contract shape and what the rules need of its file. */
interface MockKind {
  kind: ConnectionKind;
  /** Its test's expected status; undefined without a test. */
  expect?: number;
}

/** A kind file (schema/connection-kind.schema.json) as the mock reads it. */
interface KindFile {
  metadata?: { name?: unknown; title?: unknown; description?: unknown };
  spec?: {
    fields?: Partial<ConnectionKindField>[];
    env?: Record<string, string>;
    cli?: unknown;
    git?: unknown;
    mcp?: unknown;
    model?: boolean;
    codeHost?: string;
    mint?: string;
    sync?: string;
    test?: { expect?: number };
    docs?: string;
  };
}

/** A kind file as the contract's ConnectionKind; undefined when it is not one. */
function kindOf(file: KindFile, platform: boolean): MockKind | undefined {
  const m = file.metadata;
  const s = file.spec;
  if (!m || !s || typeof m.name !== 'string' || !namePattern.test(m.name) || typeof m.title !== 'string')
    return undefined;
  if (!Array.isArray(s.fields) || s.fields.some((f) => typeof f.name !== 'string' || typeof f.title !== 'string'))
    return undefined;
  const env = Object.keys(s.env ?? {}).sort();
  const kind: ConnectionKind = {
    name: m.name,
    title: m.title,
    ...(typeof m.description === 'string' ? { description: m.description } : {}),
    platform,
    fields: s.fields.map((f) => ({
      name: String(f.name),
      title: String(f.title),
      ...(f.description ? { description: f.description } : {}),
      secret: Boolean(f.secret),
      required: Boolean(f.required),
      ...(f.default ? { default: f.default } : {}),
    })),
    modes: [
      ...(env.length > 0 ? (['env'] as const) : []),
      ...(s.mcp ? (['mcp'] as const) : []),
      ...(s.cli ? (['cli'] as const) : []),
      ...(s.git ? (['git'] as const) : []),
    ],
    model: Boolean(s.model),
    ...(s.codeHost ? { code_host: s.codeHost } : {}),
    ...(s.mint ? { mint: s.mint } : {}),
    ...(s.sync ? { sync: s.sync } : {}),
    testable: Boolean(s.test),
    env,
    ...(s.docs ? { docs: s.docs } : {}),
  };
  return s.test ? { kind, expect: s.test.expect ?? 200 } : { kind };
}

/** goald's platform kinds, from internal/connkind/kinds. */
function platformKinds(): MockKind[] {
  const dir = new URL('../../internal/connkind/kinds/', import.meta.url);
  return readdirSync(dir)
    .filter((f) => f.endsWith('.yaml'))
    .sort()
    .map((f) => kindOf(parse(readFileSync(new URL(f, dir), 'utf8')) as KindFile, true))
    .filter((k): k is MockKind => k !== undefined);
}

/** A connection as the mock keeps it: no secret value, only the names set. */
interface MockConnection {
  connection: Connection;
  /** The names of the factories that name it (the mock has no definitions to read them from). */
  usedBy: string[];
}

export interface MockConnections {
  /** Starts over with the seed's connections and no organization kinds, dated from now. */
  reset: (now: number) => void;
  /** Serves the organization's connection routes; false for any other. */
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
  actor: (req: IncomingMessage) => Actor;
  /** Answers 403 reauth-required, and reports true, when the caller's sign-in is not recent. */
  stale: (req: IncomingMessage, res: ServerResponse) => boolean;
  factories: (org: string) => Factory[];
  audit: (
    org: string,
    by: Actor,
    action: string,
    target: { kind: string; id: string },
    data: Record<string, unknown>,
  ) => void;
  broadcast: (org: string, event: string, data: unknown) => void;
  acme: string;
}

const send = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  if (status === 204) return res.end();
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const titles: Record<number, string> = { 400: 'Bad Request', 404: 'Not Found', 409: 'Conflict' };

const refuse = (res: ServerResponse, status: number, detail: string, code?: string) =>
  send(res, status, { title: titles[status] ?? 'Error', status, detail, ...(code ? { code } : {}) });

const strings = (v: unknown): Record<string, string> =>
  v && typeof v === 'object' ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, String(x)])) : {};

/** acme's connections, dated from now. */
function seedConnections(now: number, goal: Factory | undefined, by: Actor): MockConnection[] {
  const at = (ms: number) => new Date(now + ms).toISOString();
  const made = (
    n: number,
    name: string,
    kind: string,
    extra: Partial<Connection>,
    usedBy: string[] = [],
  ): MockConnection => ({
    connection: {
      id: `019a1c00-0000-7000-8000-0000000000c${n}`,
      name,
      kind,
      description: '',
      fields: {},
      secrets_set: [],
      status: 'untested',
      used_by: { factories: [], knowledge_sources: [] },
      last_used_at: null,
      last_test: null,
      created_by: by,
      created_at: at(-30 * dayMs),
      updated_by: by,
      updated_at: at(-30 * dayMs),
      ...extra,
    },
    usedBy,
  });
  const goalName = goal ? [goal.name] : [];
  return [
    made(
      1,
      'github',
      'github',
      {
        description: "The goal factory's repositories",
        secrets_set: ['token'],
        status: 'ok',
        last_used_at: at(-2 * hourMs),
        last_test: { ok: true, status: 200, at: at(-1 * dayMs) },
      },
      goalName,
    ),
    made(2, 'claude', 'claude-subscription', { secrets_set: ['token'], last_used_at: at(-2 * hourMs) }, goalName),
    made(3, 'datadog', 'datadog', {
      fields: { site: 'datadoghq.eu' },
      secrets_set: ['api_key', 'app_key'],
      status: 'failing',
      last_test: {
        ok: false,
        status: 403,
        error: 'GET https://api.datadoghq.eu/api/v1/validate answered 403, not 200',
        at: at(-3 * hourMs),
      },
    }),
    made(4, 'playwright', 'mcp-server', { fields: { command: 'npx -y @playwright/mcp@0.0.41' } }),
  ];
}

export function createConnections({ clock, actor, stale, factories, audit, broadcast, acme }: Deps): MockConnections {
  const platform = platformKinds();
  let orgKinds = new Map<string, MockKind[]>();
  let conns = new Map<string, MockConnection[]>();

  const reset = (now: number) => {
    orgKinds = new Map();
    const goal = factories(acme).find((f) => f.name === 'goal');
    conns = new Map([[acme, seedConnections(now, goal, { kind: 'user', id: 'local', name: 'Local user' })]]);
  };
  reset(clock());

  /** The kinds org sees: its own first, hiding a platform kind of the same name. */
  const kinds = (org: string) => {
    const own = orgKinds.get(org) ?? [];
    return [...own, ...platform.filter((p) => !own.some((k) => k.kind.name === p.kind.name))].sort((a, b) =>
      a.kind.name.localeCompare(b.kind.name),
    );
  };
  const kindNamed = (org: string, name: string) => kinds(org).find((k) => k.kind.name === name);
  const list = (org: string) => conns.get(org) ?? [];
  const named = (org: string, name: string) => list(org).find((c) => c.connection.name === name);

  /** The connection as the contract has it: what uses it, by the factories' current names. */
  const out = (org: string, c: MockConnection, detail = false): Connection => {
    const fs = factories(org).filter((f) => c.usedBy.includes(f.name));
    const kind = kindNamed(org, c.connection.kind);
    const missing = kind?.kind.fields.some((f) => f.secret && f.required && !c.connection.secrets_set.includes(f.name));
    const status = missing ? 'missing-secret' : c.connection.status;
    const base: Connection = {
      ...structuredClone(c.connection),
      status,
      used_by: { factories: fs.map((f) => ({ id: f.id, name: f.name })), knowledge_sources: [] },
    };
    if (!detail) return base;
    const env = (kind?.kind.env ?? []).filter((e) => !e.includes('{{'));
    return { ...base, env, ...(kind?.kind.docs ? { docs: kind.kind.docs } : {}) };
  };

  const event = (org: string, c: Connection, change: ConnectionEvent['change']) =>
    broadcast(org, 'connection', { connection_id: c.id, name: c.name, change } satisfies ConnectionEvent);

  /** The rule a connection's values break against its kind, as [detail, code]; undefined for none. */
  const fieldRefusal = (
    kind: MockKind,
    fields: Record<string, string>,
    secrets: string[],
  ): [string, string] | undefined => {
    for (const name of Object.keys(fields)) {
      const f = kind.kind.fields.find((x) => x.name === name);
      if (!f) return [`kind ${kind.kind.name} has no field "${name}"`, 'field-unknown'];
      if (f.secret) return [`field "${name}" is secret: set it as a secret`, 'field-unknown'];
    }
    const unknown = secrets.find((name) => !kind.kind.fields.some((x) => x.name === name && x.secret));
    if (unknown) return [`kind ${kind.kind.name} has no secret field "${unknown}"`, 'field-unknown'];
    const missing = kind.kind.fields.find(
      (f) => f.required && !(f.secret ? secrets.includes(f.name) : Boolean(fields[f.name] || f.default)),
    );
    return missing ? [`field "${missing.name}" is required`, 'field-required'] : undefined;
  };

  function create(org: Org, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const name = typeof b.name === 'string' ? b.name : '';
    if (!namePattern.test(name))
      return refuse(res, 400, `name "${name}" must be 3 to 40 lowercase letters, digits or dashes`);
    const kind = kindNamed(org.id, String(b.kind ?? ''));
    if (!kind) return refuse(res, 404, `connection kind "${String(b.kind)}" not found`, 'connection-kind-not-found');
    const fields = Object.fromEntries(Object.entries(strings(b.fields)).filter(([, v]) => v !== ''));
    const secrets = Object.entries(strings(b.secrets))
      .filter(([, v]) => v !== '')
      .map(([k]) => k)
      .sort();
    const broken = fieldRefusal(kind, fields, secrets);
    if (broken) return refuse(res, 400, ...broken);
    if (named(org.id, name)) return refuse(res, 409, `connection "${name}" is taken`, 'connection-name-taken');
    const by = actor(req);
    const now = new Date(clock()).toISOString();
    const c: MockConnection = {
      connection: {
        id: crypto.randomUUID(),
        name,
        kind: kind.kind.name,
        description: typeof b.description === 'string' ? b.description : '',
        fields,
        secrets_set: secrets,
        status: 'untested',
        used_by: { factories: [], knowledge_sources: [] },
        last_used_at: null,
        last_test: null,
        created_by: by,
        created_at: now,
        updated_by: by,
        updated_at: now,
      },
      usedBy: [],
    };
    conns.set(org.id, [...list(org.id), c]);
    audit(
      org.id,
      by,
      'connection.created',
      { kind: 'connection', id: c.connection.id },
      { name, kind: kind.kind.name, secrets },
    );
    event(org.id, c.connection, 'created');
    send(res, 201, out(org.id, c));
  }

  function update(org: Org, c: MockConnection, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const kind = kindNamed(org.id, c.connection.kind);
    if (!kind) return refuse(res, 404, `connection kind "${c.connection.kind}" not found`, 'connection-kind-not-found');
    const name = typeof b.name === 'string' ? b.name : c.connection.name;
    if (!namePattern.test(name))
      return refuse(res, 400, `name "${name}" must be 3 to 40 lowercase letters, digits or dashes`);
    if (name !== c.connection.name && c.usedBy.length > 0) {
      return refuse(
        res,
        409,
        `connection "${c.connection.name}" is named by ${c.usedBy.join(', ')}`,
        'connection-in-use',
      );
    }
    if (name !== c.connection.name && named(org.id, name)) {
      return refuse(res, 409, `connection "${name}" is taken`, 'connection-name-taken');
    }
    const fields = b.fields === undefined ? c.connection.fields : strings(b.fields);
    const secrets = new Set(c.connection.secrets_set);
    for (const [k, v] of Object.entries(strings(b.secrets))) {
      if (v === '') secrets.delete(k);
      else secrets.add(k);
    }
    const broken = fieldRefusal(kind, fields, [...secrets].sort());
    if (broken) return refuse(res, 400, ...broken);
    const by = actor(req);
    Object.assign(c.connection, {
      name,
      description: typeof b.description === 'string' ? b.description : c.connection.description,
      fields,
      secrets_set: [...secrets].sort(),
      updated_by: by,
      updated_at: new Date(clock()).toISOString(),
    });
    const target = { kind: 'connection', id: c.connection.id };
    if (b.name !== undefined || b.description !== undefined || b.fields !== undefined) {
      audit(org.id, by, 'connection.updated', target, { name });
    }
    if (b.secrets !== undefined)
      audit(org.id, by, 'connection.secret_replaced', target, { name, secrets: Object.keys(strings(b.secrets)) });
    event(org.id, c.connection, 'updated');
    send(res, 200, out(org.id, c));
  }

  function test(org: Org, c: MockConnection, req: IncomingMessage, res: ServerResponse) {
    const kind = kindNamed(org.id, c.connection.kind);
    if (!kind?.expect) return refuse(res, 409, `kind ${c.connection.kind} has no test`, 'connection-not-testable');
    const result: ConnectionTestResult = { ok: true, status: kind.expect, at: new Date(clock()).toISOString() };
    Object.assign(c.connection, { last_test: result, status: 'ok' });
    audit(
      org.id,
      actor(req),
      'connection.tested',
      { kind: 'connection', id: c.connection.id },
      { name: c.connection.name, ok: true },
    );
    event(org.id, c.connection, 'tested');
    send(res, 200, result);
  }

  function putKind(org: Org, name: string, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const kind = kindOf(b as KindFile, false);
    if (!kind)
      return refuse(
        res,
        400,
        'connection kind: the file breaks schema/connection-kind.schema.json',
        'connection-kind-invalid',
      );
    if (kind.kind.name !== name) {
      return refuse(
        res,
        400,
        `metadata.name "${kind.kind.name}" is not the kind "${name}" of the path`,
        'connection-kind-invalid',
      );
    }
    const own = orgKinds.get(org.id) ?? [];
    const existing = own.some((k) => k.kind.name === name);
    if (!existing && platform.some((p) => p.kind.name === name)) {
      return refuse(res, 409, `a platform kind is named ${name}`, 'connection-kind-name-taken');
    }
    orgKinds.set(org.id, [...own.filter((k) => k.kind.name !== name), kind]);
    audit(
      org.id,
      actor(req),
      existing ? 'connection_kind.updated' : 'connection_kind.created',
      { kind: 'connection_kind', id: name },
      { name },
    );
    send(res, 200, kind.kind);
  }

  function deleteKind(org: Org, name: string, req: IncomingMessage, res: ServerResponse) {
    const own = orgKinds.get(org.id) ?? [];
    if (!own.some((k) => k.kind.name === name)) {
      return refuse(res, 404, `the organization has no kind "${name}"`, 'connection-kind-not-found');
    }
    if (list(org.id).some((c) => c.connection.kind === name)) {
      return refuse(res, 409, `connections of kind "${name}" exist`, 'connection-kind-in-use');
    }
    orgKinds.set(
      org.id,
      own.filter((k) => k.kind.name !== name),
    );
    audit(org.id, actor(req), 'connection_kind.deleted', { kind: 'connection_kind', id: name }, { name });
    send(res, 204, null);
  }

  return {
    reset,
    async handle(org, rest, method, req, res, body) {
      if (rest === '/connection-kinds' && method === 'GET') {
        send(res, 200, { kinds: kinds(org.id).map((k) => k.kind) });
        return true;
      }
      const k = rest.match(/^\/connection-kinds\/([^/]+)$/);
      if (k && (method === 'PUT' || method === 'DELETE')) {
        if (stale(req, res)) return true;
        const name = decodeURIComponent(k[1] ?? '');
        if (method === 'PUT') putKind(org, name, req, res, await body());
        else deleteKind(org, name, req, res);
        return true;
      }
      if (rest === '/connections' && method === 'GET') {
        const url = new URL(req.url ?? '/', 'http://mock');
        const kind = url.searchParams.get('kind');
        const ref = url.searchParams.get('factory');
        const factory = ref ? factories(org.id).find((f) => f.id === ref || f.name === ref) : undefined;
        if (ref && !factory) {
          send(res, 404, { title: 'Factory not found', status: 404, code: 'factory-not-found' });
          return true;
        }
        const shown = list(org.id)
          .filter((c) => (!kind || c.connection.kind === kind) && (!factory || c.usedBy.includes(factory.name)))
          .sort((a, b) => a.connection.name.localeCompare(b.connection.name));
        send(res, 200, { connections: shown.map((c) => out(org.id, c)) });
        return true;
      }
      if (rest === '/connections' && method === 'POST') {
        if (!stale(req, res)) create(org, req, res, await body());
        return true;
      }
      const m = rest.match(/^\/connections\/([^/]+)(\/test)?$/);
      if (!m) return false;
      const c = named(org.id, decodeURIComponent(m[1] ?? ''));
      const known = m[2] ? method === 'POST' : ['GET', 'PATCH', 'DELETE'].includes(method);
      if (!known) return false;
      if (method !== 'GET' && stale(req, res)) return true;
      if (!c) {
        refuse(res, 404, `connection "${decodeURIComponent(m[1] ?? '')}" not found`, 'connection-not-found');
        return true;
      }
      if (m[2]) test(org, c, req, res);
      else if (method === 'GET') send(res, 200, out(org.id, c, true));
      else if (method === 'PATCH') update(org, c, req, res, await body());
      else if (c.usedBy.length > 0) {
        refuse(res, 409, `connection "${c.connection.name}" is named by ${c.usedBy.join(', ')}`, 'connection-in-use');
      } else {
        conns.set(
          org.id,
          list(org.id).filter((x) => x !== c),
        );
        audit(
          org.id,
          actor(req),
          'connection.deleted',
          { kind: 'connection', id: c.connection.id },
          { name: c.connection.name },
        );
        event(org.id, c.connection, 'deleted');
        send(res, 204, null);
      }
      return true;
    },
  };
}
