// The organizations' connections and connection kinds for the mock API (spec 4 §8.2): GET
// /v1/orgs/{org}/connection-kinds, PUT and DELETE …/connection-kinds/{kind}, GET and POST
// …/connections, GET, PATCH and DELETE …/connections/{connection} and POST …/{connection}/test,
// refusing as goald does (field-unknown, field-required, connection-name-taken,
// connection-kind-not-found, connection-in-use, connection-not-testable, connection-kind-invalid,
// connection-kind-name-taken, connection-kind-in-use, a fresh sign-in for every change). The
// platform's kinds are goald's own files (internal/connkind/kinds), read once. Secret values are
// write-only: kept nowhere but as the names of the secret fields set. Each change and test is
// audited and broadcast as `event: connection`. What uses a connection is read from the
// factories' current versions (their spec.connections, model and repository connections), as
// goald does. acme starts with the connections its factories name and a few more: a PagerDuty key
// whose test fails until its key is replaced, a GitLab account never tested, an HTTP API key whose
// secret is missing.
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

const minuteMs = 60_000;
const hourMs = 3_600_000;
const dayMs = 86_400_000;

const namePattern = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;
const toolNamePattern = /^[A-Za-z0-9_.-]{1,128}$/;

/**
 * The tools a PATCH sets, as the contract says: each named once, with a mode allow, ask or off. Sorted
 * by name, as goald answers them. A string is the refusal's detail.
 */
function toolsOf(value: unknown): Connection['tools'] | string {
  if (!Array.isArray(value)) return 'tools must be a list of tool modes';
  const seen = new Set<string>();
  const tools: Connection['tools'] = [];
  for (const entry of value) {
    const { name, mode } = (entry ?? {}) as Record<string, unknown>;
    if (typeof name !== 'string' || !toolNamePattern.test(name)) {
      return `tool name "${String(name)}" must be 1 to 128 letters, digits, underscores, dots or dashes`;
    }
    if (seen.has(name)) return `tool "${name}" is named twice`;
    if (mode !== 'allow' && mode !== 'ask' && mode !== 'off') return `tool "${name}" mode must be allow, ask or off`;
    seen.add(name);
    tools.push({ name, mode });
  }
  return tools.sort((a, b) => a.name.localeCompare(b.name));
}

/** A kind as the mock keeps it: its contract shape and what the rules need of its file. */
interface MockKind {
  kind: ConnectionKind;
  /** Its test's expected status; undefined without a test. */
  expect?: number;
  /** The host its git block signs in to, as written ({{host}}); undefined without one. */
  gitHost?: string;
  /** Its test's URL, as written ({{api_url}}/user); undefined without a test. */
  testUrl?: string;
}

/** A kind file (contracts/definitions/connection-kind.schema.json) as the mock reads it. */
interface KindFile {
  metadata?: { name?: unknown; title?: unknown; description?: unknown };
  spec?: {
    fields?: Partial<ConnectionKindField>[];
    env?: Record<string, string>;
    cli?: unknown;
    git?: { host?: unknown };
    mcp?: unknown;
    model?: boolean;
    codeHost?: string;
    mint?: string;
    sync?: string;
    test?: { expect?: number; url?: string };
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
  return {
    kind,
    ...(s.test ? { expect: s.test.expect ?? 200 } : {}),
    ...(typeof s.git?.host === 'string' ? { gitHost: s.git.host } : {}),
    ...(typeof s.test?.url === 'string' ? { testUrl: s.test.url } : {}),
  };
}

/**
 * goald's checkTestHost: a kind with a git login is tested on its git host or that host's api.
 * subdomain, so a GitHub Enterprise connection left with github.com's api_url is refused, naming
 * the field, before its token goes anywhere. A template that does not render is left to the test.
 */
function testHostRefusal(kind: MockKind, fields: Record<string, string>): string | undefined {
  if (!kind.gitHost || !kind.testUrl) return undefined;
  const values = Object.fromEntries(kind.kind.fields.map((f) => [f.name, fields[f.name] || f.default || '']));
  const refs: string[] = [];
  const render = (t: string) =>
    t.replace(/\{\{\s*([a-z_]+)\s*\}\}/g, (_, name: string) => {
      refs.push(name);
      return values[name] ?? '';
    });
  const host = render(kind.gitHost).toLowerCase();
  refs.length = 0;
  let got: string;
  try {
    got = new URL(render(kind.testUrl)).host.toLowerCase();
  } catch {
    return undefined;
  }
  if (!host || got === host || got === `api.${host}`) return undefined;
  return `test.url (from ${refs.join(', ')}) is on ${got}, not on ${host} or api.${host}, the host git signs in to: set it to that host's API`;
}

/** goald's platform kinds, from internal/connkind/kinds. */
function platformKinds(): MockKind[] {
  const dir = new URL('../../../internal/connkind/kinds/', import.meta.url);
  return readdirSync(dir)
    .filter((f) => f.endsWith('.yaml'))
    .sort()
    .map((f) => kindOf(parse(readFileSync(new URL(f, dir), 'utf8')) as KindFile, true))
    .filter((k): k is MockKind => k !== undefined);
}

/** A connection as the mock keeps it: no secret value, only the names set. */
interface MockConnection {
  connection: Connection;
  /** The error its next test answers (a revoked key) until a secret is replaced. */
  failing?: { status: number; error: string };
}

/** What a run gets from one connection, without a value (domain.ConnectionKind.Outline). */
export interface ConnectionOutline {
  name: string;
  kind: string;
  /** The kind's title, as the connections check names it. */
  title: string;
  model: boolean;
  /** The env names a run gets, templates filled in from the connection's fields. */
  env: string[];
  /** The host its git block signs in to; '' for none. */
  gitHost: string;
  /** Whether its kind names a code host, so merges can use it. */
  codeHost: boolean;
}

export interface MockConnections {
  /** Starts over with the seed's connections and no organization kinds, dated from now. */
  reset: (now: number) => void;
  /** What a run gets from each of the organization's connections (the definitions' check). */
  outlines: (org: string) => ConnectionOutline[];
  /** The sync hook of the kind of org's connection name ('' when it cannot sync); undefined for no connection. */
  syncOf: (org: string, name: string) => string | undefined;
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
  /** The connection names a factory's current version uses (none before its first publish). */
  names: (factory: Factory) => string[];
  audit: (
    org: string,
    by: Actor,
    action: string,
    target: { kind: string; id: string },
    data: Record<string, unknown>,
    at?: number,
  ) => void;
  broadcast: (org: string, event: string, data: unknown) => void;
  /** The knowledge sources of org that read the connection named name. */
  sources: (org: string, name: string) => string[];
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

/** A kind's template ({{field}}) filled in from the connection's fields and the kind's defaults. */
function render(template: string, kind: ConnectionKind, fields: Record<string, string>): string {
  return template.replace(/\{\{([a-z0-9_]+)\}\}/g, (all, name: string) => {
    const field = kind.fields.find((f) => f.name === name);
    return fields[name] || field?.default || all;
  });
}

const system: Actor = { kind: 'system', id: 'connection-tests', name: 'Goal' };

/** acme's connections, dated from now, with the audit entries of their recent use. */
function seedConnections(now: number, by: Actor): { conns: MockConnection[]; history: SeedEntry[] } {
  const at = (ms: number) => new Date(now + ms).toISOString();
  const made = (
    n: number,
    name: string,
    kind: string,
    extra: Partial<Connection>,
    failing?: MockConnection['failing'],
  ) => {
    const connection: Connection = {
      id: `019a1c00-0000-7000-8000-0000000000${String(n).padStart(2, '0')}`,
      name,
      kind,
      description: '',
      fields: {},
      secrets_set: [],
      status: 'untested',
      used_by: { factories: [], knowledge_sources: [] },
      last_used_at: null,
      last_test: null,
      tools: [],
      waiting_approvals: 0,
      created_by: by,
      created_at: at(-30 * dayMs),
      updated_by: by,
      updated_at: at(-30 * dayMs),
      ...extra,
    };
    return failing ? { connection, failing } : { connection };
  };
  const ok = (ago: number) => ({ ok: true, status: 200, at: at(-ago) });
  const conns: MockConnection[] = [
    made(1, 'github', 'github', {
      description: 'acme-bot, fine-grained token',
      secrets_set: ['token'],
      status: 'ok',
      last_used_at: at(-4 * minuteMs),
      last_test: ok(2 * hourMs),
    }),
    made(2, 'claude', 'anthropic', {
      description: 'Acme’s API key, model access',
      secrets_set: ['key'],
      status: 'ok',
      last_used_at: at(-1 * minuteMs),
      last_test: ok(2 * hourMs),
    }),
    made(3, 'datadog', 'datadog', {
      fields: { site: 'datadoghq.eu' },
      secrets_set: ['api_key', 'app_key'],
      status: 'ok',
      last_used_at: at(-9 * minuteMs),
      last_test: ok(2 * hourMs),
    }),
    made(4, 'playwright', 'mcp-server', {
      description: 'npx @playwright/mcp, no secret',
      fields: { command: 'npx -y @playwright/mcp@0.0.41' },
      last_used_at: at(-40 * minuteMs),
      tools: [
        { name: 'create_comment', mode: 'ask' },
        { name: 'update_issue_status', mode: 'allow' },
        { name: 'create_issue', mode: 'off' },
        { name: 'list_issues', mode: 'allow' },
        { name: 'get_issue', mode: 'allow' },
      ],
      waiting_approvals: 2,
    }),
    made(5, 'aws-prod', 'aws', {
      description: 'Production, read-only role',
      fields: { role_arn: 'arn:aws:iam::123456789012:role/goal-read', region: 'us-east-1' },
      secrets_set: ['access_key_id', 'secret_access_key'],
      last_used_at: at(-1 * minuteMs),
    }),
    made(
      6,
      'pagerduty',
      'pagerduty',
      {
        description: 'acme.pagerduty.com',
        secrets_set: ['token'],
        status: 'failing',
        last_used_at: at(-2 * hourMs),
        last_test: {
          ok: false,
          status: 401,
          error: 'GET https://api.pagerduty.com/abilities answered 401, not 200: the key was revoked',
          at: at(-3 * hourMs),
        },
      },
      { status: 401, error: 'GET https://api.pagerduty.com/abilities answered 401, not 200: the key was revoked' },
    ),
    made(7, 'gitlab-infra', 'gitlab', {
      description: 'gitlab.example.com',
      fields: { host: 'gitlab.example.com' },
      secrets_set: ['token'],
    }),
    made(8, 'status-api', 'http-api-key', {
      description: 'status.example.com/api',
      fields: { prefix: 'STATUS', base_url: 'https://status.example.com/api' },
    }),
    // The Slack connection the account page's Slack column needs to be live (ADR 0023, decision 5).
    made(9, 'slack', 'slack', {
      description: 'Slack workspace for acme',
      secrets_set: ['token'],
    }),
  ];
  const id = (name: string) => conns.find((c) => c.connection.name === name)?.connection.id ?? '';
  const agent = (name: string): Actor => ({ kind: 'agent', id: name, name });
  const history: SeedEntry[] = [
    ['connection.created', id('gitlab-infra'), by, { name: 'gitlab-infra', kind: 'gitlab' }, -4 * dayMs],
    ['connection.created', id('status-api'), by, { name: 'status-api', kind: 'http-api-key' }, -3 * dayMs],
    ['connection.secret_replaced', id('github'), by, { name: 'github', secrets: ['token'] }, -2 * dayMs],
    [
      'connection.tested',
      id('pagerduty'),
      system,
      { name: 'pagerduty', ok: false, error: 'GET https://api.pagerduty.com/abilities answered 401, not 200' },
      -3 * hourMs,
    ],
    ['connection.used', id('pagerduty'), agent('investigator'), { name: 'pagerduty', run_id: 'run-147' }, -2 * hourMs],
    ['connection.used', id('aws-prod'), agent('investigator'), { name: 'aws-prod', run_id: 'run-151' }, -minuteMs],
  ];
  return { conns, history };
}

/** One seeded audit entry: action, connection id, by, data, when (ms from now). */
type SeedEntry = [string, string, Actor, Record<string, unknown>, number];

export function createConnections({
  clock,
  actor,
  stale,
  factories,
  names,
  audit,
  broadcast,
  sources,
  acme,
}: Deps): MockConnections {
  const platform = platformKinds();
  let orgKinds = new Map<string, MockKind[]>();
  let conns = new Map<string, MockConnection[]>();

  const reset = (now: number) => {
    orgKinds = new Map();
    const by: Actor = { kind: 'user', id: 'local', name: 'Local user' };
    const { conns: seeded, history } = seedConnections(now, by);
    conns = new Map([[acme, seeded]]);
    for (const [action, id, who, data, ago] of history.sort((a, b) => a[4] - b[4])) {
      audit(acme, who, action, { kind: 'connection', id }, data, now + ago);
    }
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
  /** The factories whose current version names the connection. */
  const usersOf = (org: string, name: string) => factories(org).filter((f) => names(f).includes(name));

  /** The connection as the contract has it: what uses it, by the factories' current names. */
  const out = (org: string, c: MockConnection, detail = false): Connection => {
    const kind = kindNamed(org, c.connection.kind);
    const missing = kind?.kind.fields.some((f) => f.secret && f.required && !c.connection.secrets_set.includes(f.name));
    const status = missing ? 'missing-secret' : c.connection.status;
    const base: Connection = {
      ...structuredClone(c.connection),
      status,
      used_by: {
        factories: usersOf(org, c.connection.name).map((f) => ({ id: f.id, name: f.name })),
        knowledge_sources: sources(org, c.connection.name),
      },
    };
    if (!detail) return base;
    return {
      ...base,
      env: kind ? outline(kind, c).env : [],
      ...(kind?.kind.docs ? { docs: kind.kind.docs } : {}),
    };
  };

  function outline(kind: MockKind, c: MockConnection): ConnectionOutline {
    const fields = c.connection.fields;
    const env = kind.kind.env
      .map((e) => render(e, kind.kind, fields))
      .filter((e) => !e.includes('{{'))
      .sort();
    return {
      name: c.connection.name,
      kind: kind.kind.name,
      title: kind.kind.title,
      model: kind.kind.model,
      env,
      gitHost: kind.gitHost ? render(kind.gitHost, kind.kind, fields) : '',
      codeHost: Boolean(kind.kind.code_host),
    };
  }

  const event = (org: string, c: Connection, change: ConnectionEvent['change']) =>
    broadcast(org, 'connection', { connection_id: c.id, name: c.name, change } satisfies ConnectionEvent);

  /** The rule a connection's values break against its kind, as [detail, code]; undefined for none. */
  const fieldRefusal = (
    kind: MockKind,
    fields: Record<string, string>,
    secrets: string[],
  ): [string, string?] | undefined => {
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
    if (missing) return [`field "${missing.name}" is required`, 'field-required'];
    const offHost = testHostRefusal(kind, fields);
    return offHost ? [offHost] : undefined;
  };

  /** 409 connection-in-use naming the factories, as goald does; false when nothing uses it. */
  const inUse = (org: string, c: MockConnection, res: ServerResponse) => {
    const users = usersOf(org, c.connection.name).map((f) => f.name);
    if (users.length === 0) return false;
    refuse(
      res,
      409,
      `connection "${c.connection.name}" is used by ${users.map((u) => `${u}'s current version`).join(', ')}`,
      'connection-in-use',
    );
    return true;
  };

  /** Answers 409 connection-in-use, and reports true, while knowledge sources read c. */
  const readBy = (org: string, c: MockConnection, res: ServerResponse) => {
    const readers = sources(org, c.connection.name);
    if (readers.length === 0) return false;
    refuse(
      res,
      409,
      `connection "${c.connection.name}": knowledge sources read it: ${readers.join(', ')}`,
      'connection-in-use',
    );
    return true;
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
        tools: [],
        waiting_approvals: 0,
        created_by: by,
        created_at: now,
        updated_by: by,
        updated_at: now,
      },
    };
    conns.set(org.id, [...list(org.id), c]);
    audit(
      org.id,
      by,
      'connection.created',
      { kind: 'connection', id: c.connection.id },
      { name, kind: kind.kind.name, fields: Object.keys(fields).sort(), secrets },
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
    if (name !== c.connection.name && inUse(org.id, c, res)) return;
    if (name !== c.connection.name && named(org.id, name)) {
      return refuse(res, 409, `connection "${name}" is taken`, 'connection-name-taken');
    }
    const fields =
      b.fields === undefined
        ? c.connection.fields
        : Object.fromEntries(Object.entries(strings(b.fields)).filter(([, v]) => v !== ''));
    const secrets = new Set(c.connection.secrets_set);
    for (const [k, v] of Object.entries(strings(b.secrets))) {
      if (v === '') secrets.delete(k);
      else secrets.add(k);
    }
    const broken = fieldRefusal(kind, fields, [...secrets].sort());
    if (broken) return refuse(res, 400, ...broken);
    if (b.tools !== undefined && !kind.kind.modes.includes('mcp')) {
      return refuse(res, 400, `kind "${kind.kind.name}" has no MCP server, so its tools have no mode`);
    }
    const tools = b.tools === undefined ? c.connection.tools : toolsOf(b.tools);
    if (typeof tools === 'string') return refuse(res, 400, tools);
    const by = actor(req);
    const from = c.connection.name;
    Object.assign(c.connection, {
      name,
      description: typeof b.description === 'string' ? b.description : c.connection.description,
      fields,
      secrets_set: [...secrets].sort(),
      tools,
      updated_by: by,
      updated_at: new Date(clock()).toISOString(),
    });
    const target = { kind: 'connection', id: c.connection.id };
    if (b.name !== undefined || b.description !== undefined || b.fields !== undefined) {
      audit(org.id, by, 'connection.updated', target, { name, ...(name !== from ? { from } : {}) });
    }
    if (b.secrets !== undefined) {
      audit(org.id, by, 'connection.secret_replaced', target, {
        name,
        secrets: Object.keys(strings(b.secrets)).sort(),
      });
      // A replaced key is a fresh one: its next test passes.
      delete c.failing;
    }
    event(org.id, c.connection, 'updated');
    send(res, 200, out(org.id, c));
  }

  function test(org: Org, c: MockConnection, req: IncomingMessage, res: ServerResponse) {
    const kind = kindNamed(org.id, c.connection.kind);
    if (!kind?.expect) return refuse(res, 409, `kind ${c.connection.kind} has no test`, 'connection-not-testable');
    const at = new Date(clock()).toISOString();
    const result: ConnectionTestResult = c.failing
      ? { ok: false, status: c.failing.status, error: c.failing.error, at }
      : { ok: true, status: kind.expect, at };
    Object.assign(c.connection, { last_test: result, status: result.ok ? 'ok' : 'failing' });
    audit(
      org.id,
      actor(req),
      'connection.tested',
      { kind: 'connection', id: c.connection.id },
      { name: c.connection.name, ok: result.ok, ...(result.error ? { error: result.error } : {}) },
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
        'connection kind: the file breaks contracts/definitions/connection-kind.schema.json',
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
    // A field made secret that a connection holds as a plain value (goald's CheckHeldPlain): never the value.
    const secret = new Set(kind.kind.fields.filter((f) => f.secret).map((f) => f.name));
    const held = list(org.id)
      .filter((c) => c.connection.kind === name)
      .map(
        (c) =>
          [
            c.connection.name,
            Object.keys(c.connection.fields)
              .filter((f) => secret.has(f))
              .sort(),
          ] as const,
      )
      .filter(([, fields]) => fields.length > 0);
    if (held.length > 0) {
      const detail = held
        .map(
          ([c, fields]) =>
            `connection ${c} holds ${fields.join(', ')} as a plain value, which kind ${name} makes secret: clear it on the connection first`,
        )
        .join('\n');
      return refuse(res, 409, detail, 'connection-kind-in-use');
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
    outlines: (org) =>
      list(org).flatMap((c) => {
        const kind = kindNamed(org, c.connection.kind);
        return kind ? [outline(kind, c)] : [];
      }),
    syncOf: (org, name) => {
      const c = named(org, name);
      return c ? (kindNamed(org, c.connection.kind)?.kind.sync ?? '') : undefined;
    },
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
        const used = factory ? names(factory) : [];
        const shown = list(org.id)
          .filter((c) => (!kind || c.connection.kind === kind) && (!factory || used.includes(c.connection.name)))
          .sort((a, b) => a.connection.name.localeCompare(b.connection.name));
        send(res, 200, { connections: shown.map((c) => out(org.id, c)) });
        return true;
      }
      if (rest === '/connections' && method === 'POST') {
        if (!stale(req, res)) create(org, req, res, await body());
        return true;
      }
      const repos = rest.match(/^\/connections\/([^/]+)\/repositories$/);
      if (repos && method === 'GET') {
        // goald's repositories of a code host connection (listConnectionRepositories): the account's, or an
        // owner's. The mock signs in as acme; acme-labs is an organization with no repositories.
        const name = decodeURIComponent(repos[1] ?? '');
        const c = named(org.id, name);
        if (!c) {
          refuse(res, 404, `connection "${name}" not found`, 'connection-not-found');
          return true;
        }
        if (!kindNamed(org.id, c.connection.kind)?.kind.code_host) {
          refuse(res, 422, `connection "${name}" is not a code host`, 'connection-not-code-host');
          return true;
        }
        const owner = new URL(req.url ?? '/', 'http://mock').searchParams.get('owner') ?? 'acme';
        const owners = [
          { login: 'acme', kind: 'user' },
          { login: 'acme-labs', kind: 'organization' },
        ];
        if (!owners.some((o) => o.login === owner)) {
          refuse(res, 404, `owner "${owner}" not found`, 'owner-not-found');
          return true;
        }
        const ago = (ms: number) => new Date(clock() - ms).toISOString();
        const repositories = (
          owner === 'acme'
            ? [
                { name: 'goal', languages: ['Go', 'TypeScript'], updated_at: ago(2 * hourMs) },
                { name: 'goal-factories', languages: ['YAML'], updated_at: ago(5 * hourMs) },
                { name: 'site', languages: ['Astro'], updated_at: ago(21 * dayMs) },
                { name: 'dotfiles', languages: ['Shell'], updated_at: ago(60 * dayMs) },
              ]
            : []
        ).map((r) => ({
          full_name: `${owner}/${r.name}`,
          clone_url: `https://github.com/${owner}/${r.name}.git`,
          languages: r.languages,
          updated_at: r.updated_at,
        }));
        send(res, 200, { login: 'acme', owner, owners, repositories, truncated: false });
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
      else if (!inUse(org.id, c, res) && !readBy(org.id, c, res)) {
        conns.set(
          org.id,
          list(org.id).filter((x) => x !== c),
        );
        audit(
          org.id,
          actor(req),
          'connection.deleted',
          { kind: 'connection', id: c.connection.id },
          { name: c.connection.name, kind: c.connection.kind },
        );
        event(org.id, c.connection, 'deleted');
        send(res, 204, null);
      }
      return true;
    },
  };
}
