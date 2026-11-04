// The organizations' knowledge as files for the mock API (ADR 0038): GET and POST
// /v1/orgs/{org}/knowledge/sources, GET, PATCH and DELETE …/sources/{source}, POST …/{source}/sync,
// POST (multipart files) and DELETE …/{source}/uploads, refusing as goald does
// (knowledge-source-name-taken, connection-not-found, connection-cannot-sync,
// knowledge-source-not-found, knowledge-source-in-use, knowledge-source-live,
// knowledge-file-not-found, not-an-upload-source, upload-too-large, unsupported-document-type).
// The mock syncs at once: a sync, or an upload, makes the files of its source current,
// broadcasting `event: knowledge` as syncing, then idle; its end moves updated_at, as goald's
// does. A source's used_by is the factories whose current version lists it
// (knowledge.sources). acme starts with three sources synced through github, as the knowledge
// picture shows them: dashboards, whose last sync failed (it shows Retry now), goal-docs, and
// support-macros, synced daily. Upload sources are created by the flows that upload to them.
import { createHash } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, Factory, KnowledgeEvent, KnowledgeSource, KnowledgeSourceKind, Org } from '@goal/api-client';

const dayMs = 86_400_000;
const namePattern = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;
const kinds: readonly KnowledgeSourceKind[] = ['upload', 'repository', 'confluence', 'synced', 'live'];
/** The file types an upload takes, by extension (internal/knowledge.TypeOf). */
const types: Record<string, string> = {
  '.txt': 'text/plain',
  '.text': 'text/plain',
  '.md': 'text/markdown',
  '.markdown': 'text/markdown',
  '.html': 'text/html',
  '.htm': 'text/html',
  '.json': 'application/json',
  '.yaml': 'application/yaml',
  '.yml': 'application/yaml',
  '.pdf': 'application/pdf',
  '.docx': 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  '.xlsx': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  '.pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  '.odt': 'application/vnd.oasis.opendocument.text',
  '.ods': 'application/vnd.oasis.opendocument.spreadsheet',
};
/** GOAL_UPLOAD_MAX_BYTES's default. */
const maxUpload = 25 << 20;

/** A source as the mock keeps it, with its documents' content. */
interface MockSource {
  source: KnowledgeSource;
  docs: MockDocument[];
}

/** One file of a source: an upload's original or a synced file. */
interface KnowledgeDocument {
  id: string;
  source: string;
  title: string;
  mime: string;
  bytes: number;
  status: 'pending' | 'indexed' | 'failed' | 'skipped';
  indexed_at: string | null;
  /** Where a synced file came from. */
  url?: string;
}

interface MockDocument {
  document: KnowledgeDocument;
  text: string;
}

export interface MockKnowledge {
  /** Starts over with the seed, dated from now. */
  reset: (now: number) => void;
  /** The names of org's sources. */
  names: (org: string) => string[];
  /** The names of org's sources that read the connection named connection. */
  readers: (org: string, connection: string) => string[];
  /** Serves the organization's knowledge routes; false for any other. */
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
  factories: (org: string) => Factory[];
  /** The knowledge sources the factory's current version may search. */
  listedBy: (factory: Factory) => string[];
  /** The kind's sync hook of org's connection name: '' for a kind that cannot sync; undefined for no connection. */
  connectionSync: (org: string, name: string) => string | undefined;
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

const titles: Record<number, string> = {
  400: 'Bad Request',
  404: 'Not Found',
  409: 'Conflict',
  413: 'Request Entity Too Large',
  415: 'Unsupported Media Type',
};

const refuse = (res: ServerResponse, status: number, detail: string, code?: string) =>
  send(res, status, { title: titles[status] ?? 'Error', status, detail, ...(code ? { code } : {}) });

const strings = (v: unknown): Record<string, string> =>
  v && typeof v === 'object' ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, String(x)])) : {};

/** acme's sources, dated from now. */
function seed(now: number, by: Actor): MockSource[] {
  const at = (ms: number) => new Date(now + ms).toISOString();
  const hourMs = 3_600_000;
  let n = 0;
  const doc = (source: string, title: string, text: string, extra: Partial<KnowledgeDocument> = {}): MockDocument => {
    n += 1;
    return {
      document: {
        id: `019a1c00-0000-7000-8000-0000000000d${n.toString(16)}`,
        source,
        title,
        mime: 'text/markdown',
        bytes: text.length,
        status: 'indexed',
        indexed_at: at(-1 * dayMs),
        ...extra,
      },
      text,
    };
  };
  const source = (
    i: number,
    s: Pick<KnowledgeSource, 'name' | 'title' | 'kind'> & Partial<KnowledgeSource>,
  ): KnowledgeSource => ({
    id: `019a1c00-0000-7000-8000-0000000000e${i}`,
    config: {},
    read_when: '',
    sync_every_seconds: 3600,
    status: 'idle',
    last_synced_at: at(-1 * dayMs),
    next_sync_at: at(-1 * dayMs + hourMs),
    latest_snapshot: null,
    used_by: { factories: [], readers: [] },
    created_by: by,
    created_at: at(-30 * dayMs),
    updated_at: at(-30 * dayMs),
    ...s,
  });
  const blob = (repo: string, path: string) => `https://github.com/acme/${repo}/blob/main/${path}`;
  return [
    {
      source: source(1, {
        name: 'dashboards',
        title: 'Dashboards',
        kind: 'synced',
        connection: 'github',
        config: { repository: 'https://github.com/acme/dashboards', include: '**/*.md' },
        status: 'failed',
        last_error: 'the server refused the token',
        last_synced_at: at(-1 * hourMs),
        next_sync_at: at(hourMs),
      }),
      docs: [
        doc(
          'dashboards',
          'api-latency.md',
          '# API latency\n\nP95 latency per route, with the panel notes. Read the notes before you act on a spike.',
          { url: blob('dashboards', 'api-latency.md'), indexed_at: at(-1 * hourMs) },
        ),
      ],
    },
    {
      source: source(2, {
        name: 'goal-docs',
        title: 'goal docs',
        kind: 'synced',
        connection: 'github',
        config: { repository: 'https://github.com/acme/goal', branch: 'main', include: 'docs/**/*.md' },
        last_synced_at: at(-45 * 60_000),
        next_sync_at: at(15 * 60_000),
      }),
      docs: [
        {
          ...doc(
            'goal-docs',
            'AGENTS.md',
            '# Goal\n\nThe index for every agent and human working in this repo. Each rule lives in one place.',
            { url: blob('goal', 'AGENTS.md'), indexed_at: at(-45 * 60_000) },
          ),
        },
        {
          ...doc(
            'goal-docs',
            'factory-definition.md',
            '# Factory definition\n\nA factory is one YAML file: its stations, agents, work types and the knowledge its agents may search.',
            { url: blob('goal', 'docs/engineering/factory-definition.md'), indexed_at: at(-45 * 60_000) },
          ),
        },
        {
          ...doc(
            'goal-docs',
            'MASTER.md',
            '# Design system\n\nThe palette, the type scale and the components. Use the token names, never a hex value.',
            { url: blob('goal', 'docs/design/MASTER.md'), indexed_at: at(-45 * 60_000) },
          ),
        },
        {
          ...doc(
            'goal-docs',
            'testing.md',
            '# Testing\n\nTest the engine with in-memory port fakes; test adapters against real PostgreSQL.',
            { url: blob('goal', 'docs/engineering/testing.md'), indexed_at: at(-45 * 60_000) },
          ),
        },
        doc(
          'goal-docs',
          'frontend.md',
          '# Frontend and monorepo\n\n## Live updates\n\nThe stream invalidates the queries an event can change, so screens update without polling.',
          { url: blob('goal', 'docs/engineering/frontend.md'), indexed_at: at(-45 * 60_000) },
        ),
      ],
    },
    {
      source: source(4, {
        name: 'product-specs',
        title: 'Product specs',
        kind: 'confluence',
        secret: 'confluence-token',
        config: { site: 'https://acme.atlassian.net', email: 'bot@example.com', spaces: 'PROD', root_page: '12345' },
        status: 'failed',
        last_error: 'Confluence refused the token',
        last_synced_at: at(-2 * hourMs),
        next_sync_at: at(hourMs),
      }),
      docs: [
        doc('product-specs', 'launch-checklist.md', '# Launch checklist\n\nWho signs off before a release goes out.', {
          indexed_at: at(-2 * hourMs),
        }),
      ],
    },
    {
      source: source(3, {
        name: 'support-macros',
        title: 'Support macros',
        kind: 'synced',
        connection: 'github',
        config: { repository: 'https://github.com/acme/support-macros', include: '**/*.md' },
        // Only when someone presses Sync (ADR 0038 decision 1): no next sync.
        sync_every_seconds: 0,
        last_synced_at: at(-6 * hourMs),
        next_sync_at: null,
      }),
      docs: [
        doc(
          'support-macros',
          'refund-policy.md',
          '# Refund policy\n\nRefunds within 30 days go back to the original payment method. Say so before you offer a credit.',
          { url: blob('support-macros', 'refund-policy.md'), indexed_at: at(-6 * hourMs) },
        ),
      ],
    },
  ];
}

export function createKnowledge({
  clock,
  actor,
  factories,
  listedBy,
  connectionSync,
  audit,
  broadcast,
  acme,
}: Deps): MockKnowledge {
  let sources = new Map<string, MockSource[]>();

  const reset = (now: number) => {
    sources = new Map([[acme, seed(now, { kind: 'user', id: 'local', name: 'Local user' })]]);
  };
  reset(clock());

  const list = (org: string) => sources.get(org) ?? [];
  const named = (org: string, name: string) => list(org).find((s) => s.source.name === name);
  /** The factories whose current version may search the source. */
  const usersOf = (org: string, s: MockSource) => factories(org).filter((f) => listedBy(f).includes(s.source.name));

  /** The source as the contract has it: its documents counted, what uses it by current names. */
  const out = (org: string, s: MockSource): KnowledgeSource => ({
    ...structuredClone(s.source),
    latest_snapshot: snapshotOf(s),
    used_by: {
      factories: usersOf(org, s).map((f) => ({ id: f.id, name: f.name })),
      readers: usersOf(org, s).map((f) => ({
        id: f.id,
        name: f.name,
        agents: [],
        stations: [],
        lanes: [],
        work_types: [],
      })),
    },
  });

  /** A source's files as the mock has them: one per document, at its title as path. */
  const filesOf = (s: MockSource) =>
    s.docs
      .map((d) => ({
        source: s.source.name,
        path: d.document.title,
        title: d.document.title,
        bytes: d.document.bytes,
        version: d.document.id,
      }))
      .sort((a, b) => a.path.localeCompare(b.path));
  const snapshotOf = (s: MockSource) =>
    s.docs.length === 0
      ? null
      : {
          files: s.docs.length,
          failed: 0,
          bytes: s.docs.reduce((n, d) => n + d.document.bytes, 0),
          synced_at: s.source.last_synced_at ?? s.source.created_at,
        };
  const findIn = (files: ReturnType<typeof filesOf>, find: string) =>
    files.filter(
      (f) =>
        f.path.toLowerCase().startsWith(find.toLowerCase()) || f.title.toLowerCase().startsWith(find.toLowerCase()),
    );

  const event = (org: string, s: MockSource) =>
    broadcast(org, 'knowledge', {
      source_id: s.source.id,
      name: s.source.name,
      status: s.source.status,
    } satisfies KnowledgeEvent);

  /** The mock's sync: pending documents indexed at once, the next sync an interval on. */
  const sync = (org: string, s: MockSource) => {
    s.source.status = 'syncing';
    event(org, s);
    const now = clock();
    for (const d of s.docs) {
      if (d.document.status === 'pending') {
        d.document.status = 'indexed';
        d.document.indexed_at = new Date(now).toISOString();
      }
    }
    s.source.status = 'idle';
    delete s.source.last_error;
    s.source.last_synced_at = new Date(now).toISOString();
    // goald's FinishKnowledgeSync moves updated_at too: a form keyed by it would lose its edits.
    s.source.updated_at = s.source.last_synced_at;
    s.source.next_sync_at =
      s.source.sync_every_seconds === 0 ? null : new Date(now + s.source.sync_every_seconds * 1000).toISOString();
    event(org, s);
  };

  function create(org: Org, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const name = typeof b.name === 'string' ? b.name : '';
    const title = typeof b.title === 'string' ? b.title.trim() : '';
    const kind = kinds.find((k) => k === b.kind);
    const connection = typeof b.connection === 'string' ? b.connection : '';
    // A synced source reads with a secret (ADR 0037), not a connection.
    const secret = typeof b.secret === 'string' ? b.secret : '';
    const every = typeof b.sync_every_seconds === 'number' ? b.sync_every_seconds : 3600;
    if (!namePattern.test(name))
      return refuse(res, 400, `name "${name}" must be 3 to 40 lowercase letters, digits or dashes`);
    if (title.length === 0 || title.length > 120) return refuse(res, 400, 'a source title has 1 to 120 characters');
    if (!kind)
      return refuse(res, 400, `source kind "${String(b.kind)}" is not upload, repository, confluence, synced or live`);
    if ((kind === 'synced' || kind === 'repository' || kind === 'confluence') && connection === '') {
      if (every !== 0 && every < 900)
        return refuse(res, 400, 'a source syncs at most every 15 minutes, or only on demand (0)');
      if (named(org.id, name))
        return refuse(res, 409, `knowledge source "${name}" is taken`, 'knowledge-source-name-taken');
      return createOn(org, req, res, {
        name,
        title,
        kind,
        connection: '',
        secret,
        every,
        config: strings(b.config),
        readWhen: typeof b.read_when === 'string' ? b.read_when : '',
      });
    }
    if ((kind === 'upload') !== (connection === '')) {
      return refuse(
        res,
        400,
        kind === 'upload' ? 'an upload source reads no connection' : `a ${kind} source reads a connection`,
      );
    }
    if (every !== 0 && every < 900)
      return refuse(res, 400, 'a source syncs at most every 15 minutes, or only on demand (0)');
    const hook = connection ? connectionSync(org.id, connection) : '';
    if (hook === undefined) return refuse(res, 404, `connection "${connection}" not found`, 'connection-not-found');
    if (kind === 'synced' && hook === '') {
      return refuse(res, 400, `connection "${connection}"'s kind cannot sync`, 'connection-cannot-sync');
    }
    if (named(org.id, name))
      return refuse(res, 409, `knowledge source "${name}" is taken`, 'knowledge-source-name-taken');
    return createOn(org, req, res, {
      name,
      title,
      kind,
      connection,
      secret: '',
      every,
      config: strings(b.config),
      readWhen: typeof b.read_when === 'string' ? b.read_when : '',
    });
  }

  function createOn(
    org: Org,
    req: IncomingMessage,
    res: ServerResponse,
    v: {
      name: string;
      title: string;
      kind: (typeof kinds)[number];
      connection: string;
      secret: string;
      every: number;
      config: Record<string, string>;
      readWhen: string;
    },
  ) {
    const { name, title, kind, connection, secret, every } = v;
    const by = actor(req);
    const now = new Date(clock()).toISOString();
    const s: MockSource = {
      source: {
        id: crypto.randomUUID(),
        name,
        title,
        kind,
        ...(connection ? { connection } : {}),
        ...(secret ? { secret } : {}),
        config: v.config,
        read_when: v.readWhen,
        sync_every_seconds: every,
        status: 'idle',
        last_synced_at: null,
        next_sync_at: kind === 'live' || every === 0 ? null : now,
        latest_snapshot: null,
        used_by: { factories: [], readers: [] },
        created_by: by,
        created_at: now,
        updated_at: now,
      },
      docs: [],
    };
    sources.set(org.id, [...list(org.id), s]);
    audit(
      org.id,
      by,
      'knowledge_source.created',
      { kind: 'knowledge_source', id: s.source.id },
      { name, kind, connection },
    );
    send(res, 201, out(org.id, s));
  }

  function update(org: Org, s: MockSource, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const title = typeof b.title === 'string' ? b.title.trim() : s.source.title;
    const every = typeof b.sync_every_seconds === 'number' ? b.sync_every_seconds : s.source.sync_every_seconds;
    if (title.length === 0 || title.length > 120) return refuse(res, 400, 'a source title has 1 to 120 characters');
    if (every !== 0 && every < 900)
      return refuse(res, 400, 'a source syncs at most every 15 minutes, or only on demand (0)');
    s.source.title = title;
    s.source.sync_every_seconds = every;
    if (every === 0) s.source.next_sync_at = null;
    else if (!s.source.next_sync_at) s.source.next_sync_at = new Date(clock()).toISOString();
    if (b.config !== undefined) s.source.config = strings(b.config);
    if (s.source.kind === 'synced' && typeof b.secret === 'string' && b.secret !== '') s.source.secret = b.secret;
    s.source.updated_at = new Date(clock()).toISOString();
    audit(
      org.id,
      actor(req),
      'knowledge_source.updated',
      { kind: 'knowledge_source', id: s.source.id },
      { name: s.source.name },
    );
    send(res, 200, out(org.id, s));
  }

  function remove(org: Org, s: MockSource, req: IncomingMessage, res: ServerResponse) {
    const users = usersOf(org.id, s).map((f) => f.name);
    if (users.length > 0) {
      return refuse(
        res,
        409,
        `knowledge source "${s.source.name}" may be searched by ${users.join(', ')}`,
        'knowledge-source-in-use',
      );
    }
    sources.set(
      org.id,
      list(org.id).filter((x) => x !== s),
    );
    audit(
      org.id,
      actor(req),
      'knowledge_source.deleted',
      { kind: 'knowledge_source', id: s.source.id },
      { name: s.source.name },
    );
    send(res, 204, null);
  }

  function removeUpload(org: Org, s: MockSource, url: URL, req: IncomingMessage, res: ServerResponse) {
    if (s.source.kind !== 'upload') {
      return refuse(res, 409, `knowledge source "${s.source.name}" is ${s.source.kind}`, 'not-an-upload-source');
    }
    const path = url.searchParams.get('path') ?? '';
    const d = s.docs.find((x) => x.document.title === path);
    if (!d)
      return refuse(
        res,
        404,
        `knowledge source "${s.source.name}" has no upload at "${path}"`,
        'knowledge-file-not-found',
      );
    s.docs = s.docs.filter((x) => x !== d);
    audit(
      org.id,
      actor(req),
      'knowledge_document.deleted',
      { kind: 'knowledge_document', id: d.document.id },
      { title: d.document.title },
    );
    send(res, 204, null);
  }

  async function upload(org: Org, s: MockSource, req: IncomingMessage, res: ServerResponse) {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(chunk as Buffer);
    let form: FormData;
    try {
      form = await new Response(Buffer.concat(chunks), {
        headers: { 'Content-Type': req.headers['content-type'] ?? '' },
      }).formData();
    } catch {
      return refuse(res, 400, 'the body is multipart/form-data with files');
    }
    const files = form.getAll('files').filter((f): f is File => typeof f !== 'string');
    if (files.length === 0) return refuse(res, 400, 'no files to upload');
    if (s.source.kind !== 'upload') {
      return refuse(res, 409, `knowledge source "${s.source.name}" is ${s.source.kind}`, 'not-an-upload-source');
    }
    const read = await Promise.all(
      files.map(async (f) => ({ name: f.name, body: Buffer.from(await f.arrayBuffer()) })),
    );
    for (const f of read) {
      if (f.body.length > maxUpload)
        return refuse(res, 413, `"${f.name}" has more than ${maxUpload} bytes`, 'upload-too-large');
      if (!types[extension(f.name)]) {
        return refuse(
          res,
          415,
          `"${f.name}" is not text, Markdown, HTML, JSON, YAML, PDF or an Office file`,
          'unsupported-document-type',
        );
      }
    }
    const by = actor(req);
    const stored = read.map((f) => {
      const id = uuidOf(createHash('sha256').update(s.source.id).update(f.body).digest('hex'));
      const same = s.docs.find((d) => d.document.id === id);
      if (same) return structuredClone(same.document);
      const d: MockDocument = {
        document: {
          id,
          source: s.source.name,
          title: f.name,
          mime: types[extension(f.name)] ?? 'text/plain',
          bytes: f.body.length,
          status: 'pending',
          indexed_at: null,
        },
        text: f.body.toString('utf8'),
      };
      s.docs.push(d);
      audit(
        org.id,
        by,
        'knowledge_document.uploaded',
        { kind: 'knowledge_document', id: d.document.id },
        { title: f.name },
      );
      return structuredClone(d.document);
    });
    send(res, 202, { files: stored.map((d) => ({ path: d.title, bytes: d.bytes })) });
  }

  return {
    reset,
    names: (org) => list(org).map((s) => s.source.name),
    readers: (org, connection) =>
      list(org)
        .filter((s) => s.source.connection === connection)
        .map((s) => s.source.name),
    async handle(org, rest, method, req, res, body) {
      const url = new URL(req.url ?? '/', 'http://mock');
      if (rest === '/knowledge/sources' && method === 'GET') {
        const ref = url.searchParams.get('factory');
        const factory = ref ? factories(org.id).find((f) => f.id === ref || f.name === ref) : undefined;
        if (ref && !factory) {
          send(res, 404, { title: 'Factory not found', status: 404, code: 'factory-not-found' });
          return true;
        }
        const shown = list(org.id)
          .filter((s) => !factory || listedBy(factory).includes(s.source.name))
          .sort((a, b) => a.source.name.localeCompare(b.source.name));
        send(res, 200, { sources: shown.map((s) => out(org.id, s)) });
        return true;
      }
      if (rest === '/knowledge/sources' && method === 'POST') {
        create(org, req, res, await body());
        return true;
      }
      if (rest === '/knowledge/check' && method === 'POST') {
        const input = await body();
        send(res, 200, {
          access: { ok: true },
          files: 12,
          first_files: ['db/disk.md', 'db/backups.md', 'k8s/pods.md'],
          bytes: 2_400_000,
          limits: { files: 20000, bytes: 50 << 20, file_bytes: 5 << 20 },
          over_limit: [],
          warnings: [],
          line: `/knowledge/${String(input.name ?? '')} — ${String(input.read_when ?? '')} 12 files.`,
        });
        return true;
      }
      if (rest === '/knowledge/files' && method === 'GET') {
        const find = url.searchParams.get('find') ?? '';
        send(res, 200, {
          files: list(org.id)
            .flatMap((s) => findIn(filesOf(s), find))
            .slice(0, 8),
        });
        return true;
      }
      if (rest === '/knowledge/reads' && method === 'GET') {
        const only = url.searchParams.get('source');
        const day = new Date().toISOString().slice(0, 10);
        const reads = list(org.id)
          .filter((s) => !only || s.source.name === only)
          .flatMap((s) =>
            filesOf(s)
              .slice(0, 3)
              .map((f, i) => ({
                source: s.source.name,
                path: f.path,
                factory_id: '',
                factory: 'Software',
                day,
                reads: 9 - i * 3,
                searches: 0,
              })),
          );
        send(res, 200, { reads });
        return true;
      }
      const filesRoute = rest.match(/^\/knowledge\/sources\/([^/]+)\/files$/);
      if (filesRoute && method === 'GET') {
        const s = named(org.id, decodeURIComponent(filesRoute[1] ?? ''));
        if (!s) refuse(res, 404, 'knowledge source not found', 'knowledge-source-not-found');
        else send(res, 200, { files: findIn(filesOf(s), url.searchParams.get('find') ?? '') });
        return true;
      }
      const src = rest.match(/^\/knowledge\/sources\/([^/]+)(\/sync|\/uploads)?$/);
      if (!src) return false;
      const name = decodeURIComponent(src[1] ?? '');
      const s = named(org.id, name);
      const known = { '': ['GET', 'PATCH', 'DELETE'], '/sync': ['POST'], '/uploads': ['POST', 'DELETE'] }[src[2] ?? ''];
      if (!known?.includes(method)) return false;
      if (!s) {
        refuse(res, 404, `knowledge source "${name}" not found`, 'knowledge-source-not-found');
        return true;
      }
      if (src[2] === '/sync') {
        if (s.source.kind === 'live')
          refuse(res, 409, `knowledge source "${name}" is live and never syncs`, 'knowledge-source-live');
        else {
          res.statusCode = 202;
          res.end();
          sync(org.id, s);
        }
      } else if (src[2] === '/uploads') {
        if (method === 'POST') {
          await upload(org, s, req, res);
          if (res.statusCode === 202) sync(org.id, s);
        } else removeUpload(org, s, url, req, res);
      } else if (method === 'GET') send(res, 200, out(org.id, s));
      else if (method === 'PATCH') update(org, s, req, res, await body());
      else remove(org, s, req, res);
      return true;
    },
  };
}

function extension(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot < 0 ? '' : name.slice(dot).toLowerCase();
}

/** A document id from a hash of its source and content, so the same upload is the same document. */
function uuidOf(hash: string): string {
  return `${hash.slice(0, 8)}-${hash.slice(8, 12)}-7${hash.slice(13, 16)}-8${hash.slice(17, 20)}-${hash.slice(20, 32)}`;
}
