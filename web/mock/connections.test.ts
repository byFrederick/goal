import { readdirSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Connection, ConnectionKindList, ConnectionList, ConnectionTestResult, Problem } from '@goal/api-client';
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

const connections = '/v1/orgs/acme/connections';

async function call<T>(
  path: string,
  method = 'GET',
  body?: unknown,
): Promise<{ status: number; body: T; text: string }> {
  const res = await fetch(`${base}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const text = await res.text();
  return { status: res.status, body: (text ? JSON.parse(text) : undefined) as T, text };
}

describe('the mock connections', () => {
  it("serves goald's own platform kinds", async () => {
    const files = readdirSync(new URL('../../internal/connkind/kinds/', import.meta.url)).filter((f) =>
      f.endsWith('.yaml'),
    );
    const { body } = await call<ConnectionKindList>('/v1/orgs/acme/connection-kinds');
    expect(body.kinds.map((k) => k.name).sort()).toEqual(files.map((f) => f.replace(/\.yaml$/, '')).sort());
    const github = body.kinds.find((k) => k.name === 'github');
    expect(github).toMatchObject({ platform: true, testable: true, code_host: 'github', modes: ['env', 'mcp', 'git'] });
    expect(github?.fields.find((f) => f.name === 'token')).toMatchObject({ secret: true, required: true });
  });

  it('creates a connection whose secret is never answered, and refuses as goald does', async () => {
    const secret = 'ghp_mock-secret-value';
    const created = await call<Connection>(connections, 'POST', {
      name: 'gh-two',
      kind: 'github',
      secrets: { token: secret },
    });
    expect(created.status).toBe(201);
    expect(created.body).toMatchObject({ name: 'gh-two', secrets_set: ['token'], status: 'untested' });
    expect(created.text).not.toContain(secret);

    const refusals: [unknown, number, string][] = [
      [{ name: 'gh-three', kind: 'github' }, 400, 'field-required'],
      [{ name: 'gh-three', kind: 'github', fields: { token: 't' } }, 400, 'field-unknown'],
      [{ name: 'gh-three', kind: 'nothing' }, 404, 'connection-kind-not-found'],
      [{ name: 'gh-two', kind: 'github', secrets: { token: 't' } }, 409, 'connection-name-taken'],
    ];
    for (const [body, status, code] of refusals) {
      const r = await call<Problem>(connections, 'POST', body);
      expect([r.status, r.body.code]).toEqual([status, code]);
    }
  });

  it("keeps what the goal factory names, and tests a kind's connection", async () => {
    const list = await call<ConnectionList>(`${connections}?factory=goal`);
    expect(list.body.connections.map((c) => c.name)).toEqual(['claude', 'github']);
    expect((await call<Problem>(`${connections}/github`, 'DELETE')).body.code).toBe('connection-in-use');
    expect((await call<Problem>(`${connections}/github`, 'PATCH', { name: 'repos' })).body.code).toBe(
      'connection-in-use',
    );

    const test = await call<ConnectionTestResult>(`${connections}/github/test`, 'POST');
    expect(test.body).toMatchObject({ ok: true, status: 200 });
    expect((await call<Problem>(`${connections}/playwright/test`, 'POST')).body.code).toBe('connection-not-testable');

    const detail = await call<Connection>(`${connections}/github`);
    expect(detail.body).toMatchObject({ status: 'ok', env: ['GH_HOST', 'GH_TOKEN'] });
  });
});
