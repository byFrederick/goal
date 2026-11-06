import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// The mock answers GET /v1/orgs/{org}/webhooks as goald does (ADR 0040): acme's two webhooks, by secret name
// and never by value; any other organization has none.

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
  await fetch(`${base}/__mock/reset`, { method: 'POST', body: '{}' });
});

describe('the mock’s organization webhooks', () => {
  it('lists acme’s webhooks with the fields of the contract and no secret value', async () => {
    const res = await fetch(`${base}/v1/orgs/acme/webhooks`);
    expect(res.status).toBe(200);
    const text = await res.text();
    const { webhooks } = JSON.parse(text) as { webhooks: Record<string, unknown>[] };

    expect(webhooks.map((w) => w.name)).toEqual(['acme-deploys', 'uptime']);
    for (const w of webhooks) {
      for (const field of ['id', 'name', 'sender', 'address', 'secret', 'enabled', 'token', 'openings', 'triggers']) {
        expect(w, String(w.name)).toHaveProperty(field);
      }
      expect(w.days).toHaveLength(7);
    }
    expect(webhooks[0]).toMatchObject({
      secret: 'acme-deploys-hook-secret',
      openings: [{ factory: expect.any(String) }],
      triggers: [{ factory_name: 'software', trigger: 'Deploy failed' }],
    });
    expect(text).not.toContain('secret_value');
  });

  it('answers an empty list for an organization that is not acme', async () => {
    const res = await fetch(`${base}/v1/orgs/goal/webhooks`);
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ webhooks: [] });
  });
});
