import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// The mock answers GET and PUT /v1/orgs/{org}/settings/notifications as goald does (ADR 0037): the secret
// that holds the Slack bot token, seeded as slack-bot in acme; PUT stores a name or null, 404 secret-not-found
// for an unknown secret, 403 for a caller without org:manage.

let api: MockApi;
let server: Server;
let base = '';
const path = '/v1/orgs/acme/settings/notifications';

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

const reset = (role?: string) =>
  fetch(`${base}/__mock/reset`, { method: 'POST', body: JSON.stringify(role ? { role } : {}) });
const put = (slack_secret: unknown) =>
  fetch(`${base}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ slack_secret }),
  });

beforeEach(async () => {
  await reset();
});

describe('the mock’s organization notification settings', () => {
  it('starts with slack-bot, a secret acme holds', async () => {
    const res = await fetch(`${base}${path}`);
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ slack_secret: 'slack-bot' });
    // slack-bot is a secret acme holds, so storing it again is accepted.
    expect((await put('slack-bot')).status).toBe(200);
  });

  it('stores a secret name and turns Slack off with null', async () => {
    const other = await put('linear-key');
    expect(other.status).toBe(200);
    expect(await (await fetch(`${base}${path}`)).json()).toEqual({ slack_secret: 'linear-key' });
    expect(await (await put(null)).json()).toEqual({ slack_secret: null });
    expect(await (await fetch(`${base}${path}`)).json()).toEqual({ slack_secret: null });
  });

  it('refuses an unknown secret with 404 secret-not-found', async () => {
    const res = await put('no-such-secret');
    expect(res.status).toBe(404);
    expect(await res.json()).toMatchObject({ code: 'secret-not-found' });
    expect(await (await fetch(`${base}${path}`)).json()).toEqual({ slack_secret: 'slack-bot' });
  });

  it('refuses a change from a caller without org:manage with 403', async () => {
    await reset('viewer');
    expect((await put(null)).status).toBe(403);
    expect((await fetch(`${base}${path}`)).status).toBe(200);
  });
});
