// The mock's environments: the catalog, the environment test, the seeded files of the goal factory and
// the plain-key check in its validation.
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
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

const goal = '/v1/orgs/acme/factories/goal';

async function call<T>(path: string, body?: unknown): Promise<{ status: number; body: T }> {
  const res = await fetch(`${base}${path}`, {
    ...(body === undefined ? {} : { method: 'POST', body: JSON.stringify(body) }),
    headers: { 'Content-Type': 'application/json' },
  });
  const text = await res.text();
  return { status: res.status, body: (text ? JSON.parse(text) : null) as T };
}

describe('the mock catalog and environment test', () => {
  it('lists the templates of the picture', async () => {
    const { status, body } = await call<{ templates: { name: string }[] }>('/v1/catalog');
    expect(status).toBe(200);
    expect(body.templates.map((t) => t.name)).toEqual(
      expect.arrayContaining(['sentry', 'linear', 'playwright', 'github-cli', 'kubectl', 'datadog', 'stripe']),
    );
  });

  it('serves the seeded environment files', async () => {
    const res = await fetch(`${base}${goal}/draft/files/${encodeURIComponent('environments/default.yaml')}`);
    expect(res.status).toBe(200);
    expect(await res.text()).toContain('mcp.linear.app');
    const files = (await call<{ files: { path: string }[] }>(`${goal}/draft/files`)).body.files.map((f) => f.path);
    expect(files).toEqual(expect.arrayContaining(['environments/default.yaml', 'environments/deploy.yaml']));
  });

  it('tests the servers of an environment: remote ones answer, stdio ones are not started', async () => {
    const { body } = await call<{ servers: { server: string; tested: boolean; ok: boolean; tools: string[] }[] }>(
      `${goal}/environments/default/test`,
      {},
    );
    const by = Object.fromEntries(body.servers.map((s) => [s.server, s]));
    expect(by.linear?.ok).toBe(true);
    expect(by.linear?.tools).toContain('list_issues');
    expect(by.playwright).toMatchObject({ tested: false, ok: false });
  });

  it('refuses an unknown environment and an unknown server', async () => {
    expect((await call(`${goal}/environments/nope/test`, {})).status).toBe(404);
    const refused = await call<{ code: string }>(`${goal}/environments/default/test`, { server: 'ghost' });
    expect(refused.body.code).toBe('environment-server-not-found');
  });

  it('tests a template with the secrets picked for its slots', async () => {
    const ok = await call<{ servers: { tools: string[] }[] }>(`${goal}/environments/default/test`, {
      template: { name: 'sentry', secrets: { token: 'sentry-token' } },
    });
    expect(ok.body.servers[0]?.tools).toHaveLength(9);
    const missing = await call<{ code: string }>(`${goal}/environments/default/test`, {
      template: { name: 'sentry', secrets: {} },
    });
    expect(missing.body.code).toBe('environment-test-invalid');
    const failed = await call<{ servers: { ok: boolean; error: string }[] }>(`${goal}/environments/default/test`, {
      template: { name: 'sentry', secrets: { token: 'bad-key' } },
    });
    expect(failed.body.servers[0]).toMatchObject({ ok: false, error: expect.stringContaining('401') });
  });

  it('reports a key in plain text with its Move to Secrets fix', async () => {
    const { body } = await call<{ problems: { code: string; pointer: string; fixes: { label: string }[] }[] }>(
      `${goal}/draft/validate`,
      {},
    );
    const plain = body.problems.filter((p) => p.code === 'plain-key');
    expect(plain.map((p) => p.pointer)).toContain('/environments/default/env/LINEAR_API_KEY');
    expect(plain[0]?.fixes[0]?.label).toBe('Move to Secrets');
  });
});
