import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { AgentStats } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// The mock answers GET …/agents/{agent}/stats as goald does: the stations whose agents include the
// agent, by copy name or by the line's route, and 404 agent-not-found for an agent no station runs.

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

const stats = async (agent: string) => {
  const res = await fetch(`${base}/v1/orgs/acme/factories/goal/agents/${agent}/stats`);
  return { status: res.status, body: (await res.json()) as AgentStats & { code?: string } };
};

describe('agent stats', () => {
  it('finds frontend-engineer at the build station the goal line routes to it', async () => {
    const { status, body } = await stats('frontend-engineer');
    expect(status).toBe(200);
    expect(body.agent).toBe('frontend-engineer');
    expect(body.model).toBe('claude-haiku-5-5');
    expect(body.reasons).toHaveLength(4);
    expect(body.stations.map((s) => ({ id: s.id, constraint: s.constraint }))).toEqual([
      { id: 'build', constraint: true },
    ]);
  });

  it('answers agent-not-found for an agent no station runs', async () => {
    const { status, body } = await stats('nobody');
    expect(status).toBe(404);
    expect(body.code).toBe('agent-not-found');
  });

  it('still finds agents by copy name and by a station agent', async () => {
    expect((await stats('builder')).status).toBe(200);
    const reviewer = await stats('reviewer');
    expect(reviewer.status).toBe(200);
    expect(reviewer.body.stations.map((s) => s.id)).toEqual(['review']);
  });
});
