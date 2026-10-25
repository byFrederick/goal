// The organizations' knowledge bases for the mock API (spec 4 §8.2): GET and POST
// /v1/orgs/{org}/knowledge/sources, GET, PATCH and DELETE …/sources/{source}, POST …/{source}/sync,
// GET and POST (multipart files) …/{source}/documents, GET and DELETE …/knowledge/documents/{document},
// GET …/{document}/content and GET …/knowledge/search, refusing as goald does
// (knowledge-source-name-taken, connection-not-found, connection-cannot-sync,
// knowledge-source-not-found, knowledge-source-in-use, knowledge-source-live,
// knowledge-document-not-found, not-an-upload-source, upload-too-large, unsupported-document-type).
// The mock indexes at once: a sync, or an upload, makes its source's pending documents indexed,
// broadcasting `event: knowledge` as syncing, then idle; its end moves updated_at, as goald's
// does. Its search ranks documents by how often they hold the query's words. A source's used_by is the factories whose current version lists it
// (knowledge.sources). acme starts with two upload sources, handbook (which sre lists) and
// postmortems, and two synced through github: goal-docs and runbooks, whose last sync failed.
import { createHash } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import type {
  Actor,
  Factory,
  KnowledgeDocument,
  KnowledgeEvent,
  KnowledgeHit,
  KnowledgeSource,
  KnowledgeSourceKind,
  Org,
} from '@goal/api-client';

const dayMs = 86_400_000;
const namePattern = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;
const kinds: readonly KnowledgeSourceKind[] = ['upload', 'synced', 'live'];
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
};
/** GOAL_UPLOAD_MAX_BYTES's default. */
const maxUpload = 25 << 20;

/** A source as the mock keeps it, with its documents' content. */
interface MockSource {
  source: KnowledgeSource;
  docs: MockDocument[];
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

const words = (text: string) => text.toLowerCase().match(/[\p{L}\p{N}]+/gu) ?? [];

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
    sync_every_seconds: 3600,
    status: 'idle',
    last_synced_at: at(-1 * dayMs),
    next_sync_at: at(-1 * dayMs + hourMs),
    documents: 0,
    used_by: { factories: [] },
    created_by: by,
    created_at: at(-30 * dayMs),
    updated_at: at(-30 * dayMs),
    ...s,
  });
  const blob = (repo: string, path: string) => `https://github.com/acme/${repo}/blob/main/${path}`;
  return [
    {
      source: source(1, { name: 'handbook', title: 'Handbook', kind: 'upload' }),
      docs: [
        doc('handbook', 'deploy.md', '# Deploy\n\nDeploys go out on Tuesdays after the release review.'),
        doc('handbook', 'oncall.md', '# On call\n\nThe on-call engineer acknowledges a page within 15 minutes.'),
      ],
    },
    {
      source: source(2, {
        name: 'postmortems',
        title: 'Postmortems',
        kind: 'upload',
        last_synced_at: at(-5 * dayMs),
      }),
      docs: [
        doc(
          'postmortems',
          '2026-09-14 checkout workers out of memory.md',
          '# Checkout workers out of memory\n\nRoot cause: the image resize step loaded whole files into memory. ECS restarted 31 tasks in 40 minutes; the alert fired after the fifth.',
          { indexed_at: at(-5 * dayMs) },
        ),
        doc('postmortems', 'timeline.html', '', {
          mime: 'text/html',
          bytes: 412,
          status: 'skipped',
          error: 'it has no text',
          indexed_at: null,
        }),
      ],
    },
    {
      source: source(3, {
        name: 'goal-docs',
        title: 'goal docs',
        kind: 'synced',
        connection: 'github',
        config: { repository: 'https://github.com/acme/goal', branch: 'main', include: 'docs/**/*.md' },
        last_synced_at: at(-45 * 60_000),
        next_sync_at: at(15 * 60_000),
        embed_model: '@cf/baai/bge-m3',
      }),
      docs: [
        doc(
          'goal-docs',
          'docs/engineering/frontend.md',
          '# Frontend and monorepo\n\n## Live updates\n\nThe stream invalidates the queries an event can change, so screens update without polling.',
          { url: blob('goal', 'docs/engineering/frontend.md'), indexed_at: at(-45 * 60_000) },
        ),
        doc(
          'goal-docs',
          'docs/engineering/testing.md',
          '# Testing\n\nTest the engine with in-memory port fakes; test adapters against real PostgreSQL.',
          { url: blob('goal', 'docs/engineering/testing.md'), indexed_at: at(-45 * 60_000) },
        ),
      ],
    },
    {
      source: source(4, {
        name: 'runbooks',
        title: 'Runbooks',
        kind: 'synced',
        connection: 'github',
        config: { repository: 'https://github.com/acme/runbooks', include: '**/*.md' },
        status: 'failed',
        last_error: 'GET https://api.github.com/repos/acme/runbooks/git/trees/main answered 429',
        last_synced_at: at(-2 * dayMs),
        next_sync_at: at(2 * hourMs),
      }),
      docs: [
        doc(
          'runbooks',
          'ecs-task-out-of-memory.md',
          '# ECS task out of memory\n\nWhen a task stops with OutOfMemoryError, compare the container memory reservation with its hard limit before you raise either.',
          { url: blob('runbooks', 'ecs-task-out-of-memory.md'), indexed_at: at(-2 * dayMs) },
        ),
        doc('runbooks', 'paging-policy.md', '', {
          url: blob('runbooks', 'paging-policy.md'),
          status: 'failed',
          error: 'GitHub answered 429; the next sync tries again',
          indexed_at: null,
        }),
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
  const documentOf = (org: string, id: string) => {
    for (const s of list(org)) {
      const d = s.docs.find((x) => x.document.id === id);
      if (d) return { s, d };
    }
    return undefined;
  };

  /** The factories whose current version may search the source. */
  const usersOf = (org: string, s: MockSource) => factories(org).filter((f) => listedBy(f).includes(s.source.name));

  /** The source as the contract has it: its documents counted, what uses it by current names. */
  const out = (org: string, s: MockSource): KnowledgeSource => ({
    ...structuredClone(s.source),
    documents: s.docs.length,
    used_by: { factories: usersOf(org, s).map((f) => ({ id: f.id, name: f.name })) },
  });

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
    s.source.next_sync_at = new Date(now + s.source.sync_every_seconds * 1000).toISOString();
    event(org, s);
  };

  function create(org: Org, req: IncomingMessage, res: ServerResponse, b: Record<string, unknown>) {
    const name = typeof b.name === 'string' ? b.name : '';
    const title = typeof b.title === 'string' ? b.title.trim() : '';
    const kind = kinds.find((k) => k === b.kind);
    const connection = typeof b.connection === 'string' ? b.connection : '';
    const every = typeof b.sync_every_seconds === 'number' ? b.sync_every_seconds : 3600;
    if (!namePattern.test(name))
      return refuse(res, 400, `name "${name}" must be 3 to 40 lowercase letters, digits or dashes`);
    if (title.length === 0 || title.length > 120) return refuse(res, 400, 'a source title has 1 to 120 characters');
    if (!kind) return refuse(res, 400, `source kind "${String(b.kind)}" is not upload, synced or live`);
    if ((kind === 'upload') !== (connection === '')) {
      return refuse(
        res,
        400,
        kind === 'upload' ? 'an upload source reads no connection' : `a ${kind} source reads a connection`,
      );
    }
    if (every < 900) return refuse(res, 400, 'a source syncs at most every 15 minutes');
    const hook = connection ? connectionSync(org.id, connection) : '';
    if (hook === undefined) return refuse(res, 404, `connection "${connection}" not found`, 'connection-not-found');
    if (kind === 'synced' && hook === '') {
      return refuse(res, 400, `connection "${connection}"'s kind cannot sync`, 'connection-cannot-sync');
    }
    if (named(org.id, name))
      return refuse(res, 409, `knowledge source "${name}" is taken`, 'knowledge-source-name-taken');
    const by = actor(req);
    const now = new Date(clock()).toISOString();
    const s: MockSource = {
      source: {
        id: crypto.randomUUID(),
        name,
        title,
        kind,
        ...(connection ? { connection } : {}),
        config: strings(b.config),
        sync_every_seconds: every,
        status: 'idle',
        last_synced_at: null,
        next_sync_at: kind === 'live' ? null : now,
        documents: 0,
        used_by: { factories: [] },
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
    if (every < 900) return refuse(res, 400, 'a source syncs at most every 15 minutes');
    s.source.title = title;
    s.source.sync_every_seconds = every;
    if (b.config !== undefined) s.source.config = strings(b.config);
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

  function documents(s: MockSource, url: URL, res: ServerResponse) {
    const limit = Number(url.searchParams.get('limit') ?? 100);
    if (!Number.isInteger(limit) || limit < 1 || limit > 500) return refuse(res, 400, 'limit must be in [1, 500]');
    const after = url.searchParams.get('after') ?? '';
    const rest = s.docs
      .map((d) => d.document)
      .sort((a, b) => a.id.localeCompare(b.id))
      .filter((d) => d.id > after);
    const page = rest.slice(0, limit);
    const last = page.at(-1);
    send(res, 200, { documents: page, ...(rest.length > limit && last ? { next_after: last.id } : {}) });
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
        return refuse(res, 415, `"${f.name}" is not text, Markdown, HTML, JSON or YAML`, 'unsupported-document-type');
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
    send(res, 202, { documents: stored });
  }

  function search(org: Org, url: URL, res: ServerResponse) {
    const q = (url.searchParams.get('q') ?? '').trim();
    if (q.length === 0 || q.length > 1000) return refuse(res, 400, 'q is 1 to 1000 characters');
    const limit = Number(url.searchParams.get('limit') ?? 8);
    if (!Number.isInteger(limit) || limit < 1 || limit > 20) return refuse(res, 400, 'limit must be in [1, 20]');
    const names = url.searchParams.getAll('source');
    const unknown = names.find((n) => !named(org.id, n));
    if (unknown) return refuse(res, 404, `knowledge source "${unknown}" not found`, 'knowledge-source-not-found');
    const terms = new Set(words(q));
    const hits: KnowledgeHit[] = list(org.id)
      .filter((s) => names.length === 0 || names.includes(s.source.name))
      .flatMap((s) =>
        s.docs
          .filter((d) => d.document.status === 'indexed')
          .map((d) => ({ s, d, score: words(d.text).filter((w) => terms.has(w)).length })),
      )
      .filter((h) => h.score > 0)
      .sort((a, b) => b.score - a.score || a.d.document.id.localeCompare(b.d.document.id))
      .slice(0, limit)
      .map(({ s, d, score }) => ({
        document: d.document.id,
        source: s.source.name,
        title: d.document.title,
        ...(d.document.url ? { url: d.document.url } : {}),
        heading: d.text.match(/^#+\s*(.+)$/m)?.[1] ?? '',
        snippet: d.text,
        score: score / 60,
      }));
    send(res, 200, { hits });
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
      if (rest === '/knowledge/search' && method === 'GET') {
        search(org, url, res);
        return true;
      }
      const src = rest.match(/^\/knowledge\/sources\/([^/]+)(\/sync|\/documents)?$/);
      if (src) {
        const name = decodeURIComponent(src[1] ?? '');
        const s = named(org.id, name);
        const known = { '': ['GET', 'PATCH', 'DELETE'], '/sync': ['POST'], '/documents': ['GET', 'POST'] }[
          src[2] ?? ''
        ];
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
        } else if (src[2] === '/documents') {
          if (method === 'GET') documents(s, url, res);
          else {
            await upload(org, s, req, res);
            if (res.statusCode === 202) sync(org.id, s);
          }
        } else if (method === 'GET') send(res, 200, out(org.id, s));
        else if (method === 'PATCH') update(org, s, req, res, await body());
        else remove(org, s, req, res);
        return true;
      }
      const doc = rest.match(/^\/knowledge\/documents\/([^/]+)(\/content)?$/);
      if (!doc) return false;
      const id = decodeURIComponent(doc[1] ?? '');
      const known = doc[2] ? method === 'GET' : ['GET', 'DELETE'].includes(method);
      if (!known) return false;
      const found = documentOf(org.id, id);
      if (!found) {
        refuse(res, 404, `knowledge document ${id} not found`, 'knowledge-document-not-found');
        return true;
      }
      const { s, d } = found;
      if (method === 'GET' && !doc[2]) {
        send(res, 200, { document: d.document, text: d.document.status === 'indexed' ? d.text : '' });
      } else if (s.source.kind !== 'upload') {
        refuse(res, 409, `knowledge document ${id} is not an upload`, 'not-an-upload-source');
      } else if (doc[2]) {
        res.statusCode = 200;
        res.setHeader('Content-Type', 'application/octet-stream');
        res.setHeader('Content-Disposition', `attachment; filename="${d.document.title.replace(/["\\]/g, '_')}"`);
        res.setHeader('X-Content-Type-Options', 'nosniff');
        res.end(d.text);
      } else {
        s.docs = s.docs.filter((x) => x !== d);
        audit(
          org.id,
          actor(req),
          'knowledge_document.deleted',
          { kind: 'knowledge_document', id },
          { title: d.document.title },
        );
        send(res, 204, null);
      }
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
