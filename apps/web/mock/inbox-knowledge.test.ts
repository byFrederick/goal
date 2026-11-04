import { createHash } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Factory, Inbox, KnowledgePreview, KnowledgeSourceList, Problem } from '@goal/api-client';
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

async function call<T>(path: string, init: RequestInit = {}): Promise<{ status: number; body: T }> {
  const res = await fetch(`${base}${path}`, init);
  const text = await res.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {}
  return { status: res.status, body: body as T };
}

const entries = async () => (await call<Inbox>('/v1/orgs/acme/inbox')).body.entries;

describe('the inbox entries about knowledge and conversion', () => {
  it('makes a knowledge_source entry for a factory that lists a source failing three syncs', async () => {
    const sreFactory = (await call<Factory>('/v1/orgs/acme/factories/sre')).body;
    const sources = (await call<KnowledgeSourceList>('/v1/orgs/acme/knowledge/sources')).body.sources;
    const dashboards = sources.find((s) => s.name === 'dashboards');
    const found = (await entries()).filter((e) => e.kind === 'knowledge_source');
    expect(found.map((e) => e.id)).toEqual([`knowledge_source:${sreFactory.id}:dashboards`]);
    expect(found[0]).toMatchObject({
      item: null,
      since: dashboards?.updated_at,
      knowledge_source: { source: 'dashboards', error: dashboards?.last_error, failed_syncs: 3 },
    });
  });

  it('makes none once the source syncs again', async () => {
    await call('/v1/orgs/acme/knowledge/sources/dashboards/sync', { method: 'POST' });
    expect((await entries()).filter((e) => e.kind === 'knowledge_source')).toEqual([]);
  });

  it('seeds one conversion entry, with every problem as a sentence in its reason', async () => {
    const found = (await entries()).filter((e) => e.kind === 'conversion');
    expect(found).toHaveLength(1);
    const [entry] = found;
    expect(entry?.id).toBe(`conversion:${entry?.factory.id}`);
    expect(entry?.item).toBeNull();
    const problems = entry?.conversion?.problems ?? [];
    expect(problems.length).toBeGreaterThanOrEqual(2);
    expect(problems).toContain(
      'connection aws-prod uses the aws kind, which Goal no longer converts: remove it from the factory',
    );
    expect(entry?.conversion?.reason).toBe(problems.map((p) => `${p.charAt(0).toUpperCase()}${p.slice(1)}.`).join(' '));
  });
});
