import { readdirSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type {
  Connection,
  ConnectionKindList,
  ConnectionList,
  ConnectionTestResult,
  Problem,
  Validation,
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
    expect(list.body.connections.map((c) => c.name)).toEqual(['claude', 'github', 'playwright']);
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

  it("answers a revoked key's test as failed until the key is replaced", async () => {
    const failed = await call<ConnectionTestResult>(`${connections}/pagerduty/test`, 'POST');
    expect(failed.status).toBe(200);
    expect(failed.body).toMatchObject({ ok: false, status: 401 });
    expect((await call<Connection>(`${connections}/pagerduty`)).body.status).toBe('failing');

    await call<Connection>(`${connections}/pagerduty`, 'PATCH', { secrets: { token: 'a-new-key' } });
    const passed = await call<ConnectionTestResult>(`${connections}/pagerduty/test`, 'POST');
    expect(passed.body).toMatchObject({ ok: true, status: 200 });
  });

  it('refuses a kind change that makes a field secret while a connection holds it plain, naming both', async () => {
    const kind = (secret: boolean) => ({
      apiVersion: 'goal/v1',
      kind: 'ConnectionKind',
      metadata: { name: 'acme-api', title: 'Acme API' },
      spec: {
        fields: [{ name: 'base_url', title: 'Base URL', secret, required: true }],
        env: { ACME_API_URL: '{{base_url}}' },
      },
    });
    expect((await call('/v1/orgs/acme/connection-kinds/acme-api', 'PUT', kind(false))).status).toBe(200);
    const made = await call(connections, 'POST', {
      name: 'acme-api',
      kind: 'acme-api',
      fields: { base_url: 'https://api.example.com' },
    });
    expect(made.status).toBe(201);
    const refused = await call<Problem>('/v1/orgs/acme/connection-kinds/acme-api', 'PUT', kind(true));
    expect([refused.status, refused.body.code]).toEqual([409, 'connection-kind-in-use']);
    expect(refused.body.detail).toContain('acme-api holds base_url');
    expect(refused.body.detail).not.toContain('api.example.com');
  });

  it('says what uses a connection from the current versions, and a missing secret', async () => {
    const { body } = await call<ConnectionList>(connections);
    const by = (name: string) => body.connections.find((c) => c.name === name);
    expect(by('claude')?.used_by.factories.map((f) => f.name)).toEqual(['goal', 'sre', 'support']);
    expect(by('aws-prod')?.used_by.factories.map((f) => f.name)).toEqual(['sre']);
    expect(by('gitlab-infra')?.used_by.factories).toEqual([]);
    expect(by('status-api')?.status).toBe('missing-secret');
  });

  it("checks the connections a definition names, with goald's fixes", async () => {
    const yaml = `apiVersion: goal/v1
kind: Factory
metadata:
  name: probe
spec:
  runtime:
    modelConnection: github
    tools: [go@1.26.10, "rm -rf /"]
    repository: { url: "https://gitlab.example.com/acme/infra", connection: github }
  connections: [aws-prod, nothing]
  knowledge: { sources: [handbook, ghost] }
  agents:
    - { name: worker, connections: [aws-prod] }
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: done, kind: done }
`;
    const { body } = await call<Validation>('/v1/orgs/acme/definitions/validate', 'POST', {
      files: { 'factory.yaml': { text: yaml } },
    });
    const at = (code: string) =>
      body.problems.filter((p) => p.code === code).map((p) => [p.pointer, p.fixes.map((f) => f.label)]);
    expect(at('connection-not-model')).toEqual([['/basics/model_connection', ['Use claude']]]);
    expect(at('repository-host-mismatch')).toEqual([
      ['/basics/repository/connection', ['Use gitlab-infra', 'Clone without a connection']],
    ]);
    expect(at('connection-unknown')).toEqual([['/connections/1', ['Remove nothing']]]);
    expect(at('tool-spec-invalid')).toEqual([['/basics/tools/1', ['Remove rm -rf /']]]);
    expect(at('knowledge-source-unknown')).toEqual([['/basics/knowledge_sources/1', ['Remove ghost']]]);
    // The model connection (github) and aws-prod set no variable in common.
    expect(at('connection-env-conflict')).toEqual([]);
  });

  it('refuses a definition with agents that names no model connection, offering each that gives model access', async () => {
    const yaml = `apiVersion: goal/v1
kind: Factory
metadata:
  name: probe
spec:
  agents:
    - { name: worker }
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: done, kind: done }
`;
    const validate = async (text: string) =>
      (
        await call<Validation>('/v1/orgs/acme/definitions/validate', 'POST', { files: { 'factory.yaml': { text } } })
      ).body.problems
        .filter((p) => p.code === 'model-connection-missing')
        .map((p) => [p.pointer, p.severity, p.fixes.map((f) => f.label)]);
    expect(await validate(yaml)).toEqual([['/basics/model_connection', 'error', ['Use claude']]]);
    // Without agents no run needs a model.
    expect(await validate(yaml.replace('  agents:\n    - { name: worker }\n', ''))).toEqual([]);
  });
});
