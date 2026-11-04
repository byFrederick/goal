import { createHash } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

type KnowledgePreview = { section: string };
type Problem = { code?: string };

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

async function call<T>(path: string, init: RequestInit = {}): Promise<{ status: number; body: T }> {
  const res = await fetch(`${base}${path}`, init);
  const text = await res.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {}
  return { status: res.status, body: body as T };
}

const sha = (text: string) => createHash('sha256').update(text).digest('hex');
const goal = '/v1/orgs/acme/factories/goal';

/** Lists sources in goal's draft: its seed lists none. */
async function list(sources: string) {
  const file = await call<string>(`${goal}/draft/files/factory.yaml`);
  const yaml = file.body.replace('\n  agents:\n', `\n  knowledge: { sources: [${sources}] }\n  agents:\n`);
  const saved = await call(`${goal}/draft/files/factory.yaml`, {
    method: 'PUT',
    headers: { 'If-Match': `"${sha(file.body)}"` },
    body: yaml,
  });
  expect(saved.status).toBe(200);
}

const preview = (agent: string) => call<KnowledgePreview>(`${goal}/draft/knowledge-preview?agent=${agent}`);

describe('the draft knowledge preview', () => {
  it('says nothing while the draft lists no source', async () => {
    const { status, body } = await preview('reviewer');
    expect([status, body.section]).toEqual([200, '']);
  });

  it('lists the sources the draft lists, with their read-when and file count', async () => {
    await list('goal-docs, { name: brand-guide, readWhen: "You write copy.\\n" }');
    const { status, body } = await preview('reviewer');
    expect(status).toBe(200);
    expect(body.section).toContain('## Knowledge');
    expect(body.section).toContain('- `/knowledge/goal-docs/`: 5 files.');
    expect(body.section).toContain('- `/knowledge/brand-guide/`: You write copy. 2 files.');
  });

  it('names a listed source with no snapshot as not synced yet', async () => {
    await list('goal-docs, ghost');
    const { body } = await preview('reviewer');
    expect(body.section).toContain('Not synced yet, so left out: `ghost`.');
  });

  it('refuses an agent the draft does not declare', async () => {
    const { status, body } = await call<Problem>(`${goal}/draft/knowledge-preview?agent=nobody`);
    expect([status, body.code]).toEqual([404, 'agent-not-found']);
  });

  it('refuses while factory.yaml does not parse', async () => {
    const file = await call<string>(`${goal}/draft/files/factory.yaml`);
    await call(`${goal}/draft/files/factory.yaml`, {
      method: 'PUT',
      headers: { 'If-Match': `"${sha(file.body)}"` },
      body: 'spec: [unclosed',
    });
    const { status, body } = await call<Problem>(`${goal}/draft/knowledge-preview?agent=reviewer`);
    expect([status, body.code]).toEqual([409, 'yaml-invalid']);
  });
});
