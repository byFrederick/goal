import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// GET …/factories/{f}/agents/{agent}/stats answers as goald does (internal/api/agents.go): 404 agent-not-found
// only for an agent the definition does not declare; a declared agent on no station gets 200 with no stations
// and zero measures. The same server answers the default caller's secrets and access, which orgCan grants.

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

const stats = (agent: string) => fetch(`${base}/v1/orgs/acme/factories/goal/agents/${agent}/stats`);

describe('the mock’s agent statistics', () => {
  it('answers a station’s agent with its stations', async () => {
    const res = await stats('reviewer');
    expect(res.status).toBe(200);
    expect(((await res.json()) as { stations: unknown[] }).stations.length).toBeGreaterThan(0);
  });

  it('answers a declared agent on no station with 200, no stations and zero measures', async () => {
    const res = await stats('infra-engineer');
    expect(res.status).toBe(200);
    expect(await res.json()).toMatchObject({
      agent: 'infra-engineer',
      stations: [],
      tasks_finished: 0,
      spend_usd: 0,
    });
  });

  it('answers an agent the definition does not declare with 404 agent-not-found', async () => {
    const res = await stats('nobody');
    expect(res.status).toBe(404);
    expect(await res.json()).toMatchObject({ code: 'agent-not-found' });
  });
});

describe('the mock’s default caller', () => {
  it('reads the organization’s secrets and access', async () => {
    expect((await fetch(`${base}/v1/orgs/acme/secrets`)).status).toBe(200);
    expect((await fetch(`${base}/v1/orgs/acme/access`)).status).toBe(200);
  });
});
