import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type {
  Connection,
  KnowledgeDocumentList,
  KnowledgeDocumentPage,
  KnowledgeSearchResult,
  KnowledgeSource,
  KnowledgeSourceList,
  Problem,
} from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

let api: MockApi;
let server: Server;
let base = '';

beforeAll(async () => {
  api = createMockApi();
  server = createServer((req, res) =>
    api.handle(req, res, () => {
      res.statusCode = 404;
      res.end();
    }),
  );
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterAll(async () => {
  api.close();
  await new Promise((resolve) => server.close(resolve));
});

beforeEach(async () => {
  await fetch(`${base}/__mock/reset`, { method: 'POST' });
});

const sources = '/v1/orgs/acme/knowledge/sources';

async function call<T>(path: string, method = 'GET', body?: unknown): Promise<{ status: number; body: T }> {
  const res = await fetch(`${base}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const text = await res.text();
  return { status: res.status, body: (text ? JSON.parse(text) : undefined) as T };
}

async function upload<T>(source: string, files: Record<string, string>): Promise<{ status: number; body: T }> {
  const form = new FormData();
  for (const [name, text] of Object.entries(files)) form.append('files', new Blob([text]), name);
  const res = await fetch(`${base}${sources}/${source}/documents`, { method: 'POST', body: form });
  return { status: res.status, body: (await res.json()) as T };
}

describe('the mock knowledge base', () => {
  it('starts acme with the three synced sources the knowledge picture shows, searched as agents search', async () => {
    const { body } = await call<KnowledgeSourceList>(sources);
    expect(body.sources.map((s) => [s.name, s.kind, s.status, s.documents])).toEqual([
      ['dashboards', 'synced', 'failed', 1],
      ['goal-docs', 'synced', 'idle', 2],
      ['support-macros', 'synced', 'idle', 1],
    ]);
    const hits = await call<KnowledgeSearchResult>('/v1/orgs/acme/knowledge/search?q=refunds');
    expect(hits.body.hits.map((h) => [h.source, h.title])).toEqual([['support-macros', 'refund-policy.md']]);
    const synced = await call<KnowledgeSearchResult>('/v1/orgs/acme/knowledge/search?q=latency&source=dashboards');
    expect(synced.body.hits.map((h) => h.url)).toEqual(['https://github.com/acme/dashboards/blob/main/api-latency.md']);
  });

  it('names the factories whose current version lists a source, and keeps it while they do', async () => {
    const { body } = await call<KnowledgeSourceList>(`${sources}?factory=sre`);
    expect(body.sources.map((s) => [s.name, s.used_by.factories.map((f) => f.name)])).toEqual([
      ['dashboards', ['sre']],
    ]);
    const refused = await call<Problem>(`${sources}/dashboards`, 'DELETE');
    expect([refused.status, refused.body.code]).toEqual([409, 'knowledge-source-in-use']);
    expect((await call(`${sources}/support-macros`, 'DELETE')).status).toBe(204);
  });

  it("moves a source's updated_at when a sync ends, as goald's FinishKnowledgeSync does", async () => {
    const before = (await call<KnowledgeSource>(`${sources}/goal-docs`)).body.updated_at;
    expect((await call(`${sources}/goal-docs/sync`, 'POST')).status).toBe(202);
    expect((await call<KnowledgeSource>(`${sources}/goal-docs`)).body.updated_at).not.toBe(before);

    const created = await call<KnowledgeSource>(sources, 'POST', {
      name: 'handbook',
      title: 'Handbook',
      kind: 'upload',
    });
    expect(created.status).toBe(201);
    const uploaded = (await call<KnowledgeSource>(`${sources}/handbook`)).body.updated_at;
    await upload('handbook', { 'leave.md': '# Leave' });
    expect((await call<KnowledgeSource>(`${sources}/handbook`)).body.updated_at).not.toBe(uploaded);
  });

  it('uploads files, indexes them, serves the original as an attachment and deletes them', async () => {
    await call(sources, 'POST', { name: 'handbook', title: 'Handbook', kind: 'upload' });
    const up = await upload<KnowledgeDocumentList>('handbook', { 'leave.md': '# Leave\n\nAsk your lead.' });
    expect(up.status).toBe(202);
    expect(up.body.documents).toMatchObject([{ title: 'leave.md', status: 'pending', source: 'handbook' }]);
    const id = up.body.documents[0]?.id ?? '';

    const page = await call<KnowledgeDocumentPage>(`${sources}/handbook/documents`);
    expect(page.body.documents.find((d) => d.id === id)?.status).toBe('indexed');

    const content = await fetch(`${base}/v1/orgs/acme/knowledge/documents/${id}/content`);
    expect(content.headers.get('content-disposition')).toBe('attachment; filename="leave.md"');
    expect(content.headers.get('x-content-type-options')).toBe('nosniff');
    expect(await content.text()).toBe('# Leave\n\nAsk your lead.');

    expect((await call(`/v1/orgs/acme/knowledge/documents/${id}`, 'DELETE')).status).toBe(204);
    expect((await call<Problem>(`/v1/orgs/acme/knowledge/documents/${id}`)).body.code).toBe(
      'knowledge-document-not-found',
    );
  });

  it('refuses as goald does', async () => {
    const refusals: [string, string, unknown, number, string?][] = [
      [sources, 'POST', { name: 'dashboards', title: 'Again', kind: 'upload' }, 409, 'knowledge-source-name-taken'],
      [
        sources,
        'POST',
        { name: 'models', title: 'Models', kind: 'synced', connection: 'claude' },
        400,
        'connection-cannot-sync',
      ],
      [
        sources,
        'POST',
        { name: 'wiki', title: 'Wiki', kind: 'synced', connection: 'wiki' },
        404,
        'connection-not-found',
      ],
      [sources, 'POST', { name: 'notes', title: 'Notes', kind: 'upload', sync_every_seconds: 60 }, 400],
      [`${sources}/nothing`, 'GET', undefined, 404, 'knowledge-source-not-found'],
      ['/v1/orgs/acme/knowledge/search?q=x&source=wiki', 'GET', undefined, 404, 'knowledge-source-not-found'],
    ];
    for (const [path, method, body, status, code] of refusals) {
      const res = await call<Problem>(path, method, body);
      expect([path, res.status, res.body.code]).toEqual([path, status, code]);
    }
    await call(sources, 'POST', { name: 'handbook', title: 'Handbook', kind: 'upload' });
    const deck = await upload<Problem>('handbook', { 'deck.pptx': 'PK' });
    expect([deck.status, deck.body.code]).toEqual([415, 'unsupported-document-type']);
  });

  it("lists a live source in its connection's used_by and keeps the connection while it reads it", async () => {
    const live = await call<KnowledgeSource>(sources, 'POST', {
      name: 'repo-search',
      title: 'Repository search',
      kind: 'live',
      connection: 'github',
    });
    expect(live.status).toBe(201);
    expect((await call<Problem>(`${sources}/repo-search/sync`, 'POST')).body.code).toBe('knowledge-source-live');
    const github = await call<Connection>('/v1/orgs/acme/connections/github');
    expect(github.body.used_by.knowledge_sources).toEqual(['dashboards', 'goal-docs', 'support-macros', 'repo-search']);
  });
});
