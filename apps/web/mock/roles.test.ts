import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Factory, Inbox, Org, OrgList } from '@goal/api-client';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';
import { grantsOf, mockRoles, readSources, scopeOf } from './roles.ts';

const { table, scopes } = readSources();

describe('grantsOf (contracts/api/permissions.yaml)', () => {
  it('gives each role what spec 3 §4.2 says', () => {
    const viewer = grantsOf(table, 'viewer');
    expect(viewer.factory).toEqual(['factory:read']);
    expect(viewer.org).not.toContain('org:manage');

    const approver = grantsOf(table, 'approver');
    expect(approver.factory).toEqual(
      expect.arrayContaining(['item:create', 'item:decide', 'item:operate', 'andon:manage']),
    );
    expect(approver.factory).not.toContain('definition:edit');

    const editor = grantsOf(table, 'editor');
    expect(editor.factory).toEqual(expect.arrayContaining(['definition:edit', 'definition:publish', 'factory:manage']));
    expect(editor.org).not.toContain('factory:create');

    const member = grantsOf(table, 'member');
    expect(member).toMatchObject({ orgRole: 'member', factoryRole: null, factory: [] });
    expect(member.org).toEqual(expect.arrayContaining(['org:read', 'member:read']));
  });

  it('makes owners and admins editors everywhere; only owners delete or transfer', () => {
    const owner = grantsOf(table, 'owner');
    const admin = grantsOf(table, 'admin');
    expect(owner.factoryRole).toBe('editor');
    expect(admin.factoryRole).toBe('editor');
    expect(admin.factory).toEqual(owner.factory);
    expect(owner.org).toEqual(expect.arrayContaining(['org:delete', 'org:transfer', 'factory:create']));
    expect(admin.org).toEqual(expect.arrayContaining(['org:manage', 'factory:create', 'member:manage']));
    expect(admin.org).not.toContain('org:delete');
    expect(admin.org).not.toContain('org:transfer');
  });
});

describe('scopeOf (contracts/api/openapi.yaml)', () => {
  it('names the permission of an operation and its path parameters', () => {
    expect(scopeOf(scopes, 'POST', '/v1/orgs/acme/items/goal%2312/approve')).toEqual({
      permission: 'item:decide',
      params: { org: 'acme', item: 'goal#12' },
    });
    expect(scopeOf(scopes, 'POST', '/v1/orgs/acme/factories/sre/andon')?.permission).toBe('andon:manage');
    expect(scopeOf(scopes, 'GET', '/v1/orgs/acme/factories/sre/andon')?.permission).toBe('factory:read');
    expect(scopeOf(scopes, 'PATCH', '/v1/orgs/acme/settings')?.permission).toBe('org:manage');
    expect(scopeOf(scopes, 'GET', '/v1/nothing')).toBeUndefined();
  });
});

describe('the mock in a role', () => {
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

  const reset = (role?: string) =>
    fetch(`${base}/__mock/reset`, { method: 'POST', body: JSON.stringify(role ? { role } : {}) });
  const get = async <T>(path: string) => (await fetch(`${base}${path}`)).json() as Promise<T>;
  const post = (path: string, body: unknown = {}) =>
    fetch(`${base}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });

  it('answers role and can as goald does for each role', async () => {
    for (const role of mockRoles) {
      await reset(role);
      const grants = grantsOf(table, role);
      const { orgs } = await get<OrgList>('/v1/orgs');
      expect(orgs.map((o) => o.role)).toEqual(orgs.map(() => grants.orgRole));
      expect((await get<Org>('/v1/orgs/acme')).can).toEqual(grants.org);
      const { factories } = await get<{ factories: Factory[] }>('/v1/orgs/acme/factories');
      for (const f of factories) expect(f).toMatchObject({ role: grants.factoryRole, can: grants.factory });
    }
  });

  it('shows a member without factory roles no factory', async () => {
    await reset('member');
    expect((await get<{ factories: Factory[] }>('/v1/orgs/acme/factories')).factories).toEqual([]);
    expect((await fetch(`${base}/v1/orgs/acme/factories/sre/board`)).status).toBe(404);
  });

  it('refuses what the role lacks with 403 forbidden, and lets the right role through', async () => {
    await reset('viewer');
    for (const [path, body] of [
      ['/v1/orgs/acme/items/goal%2312/approve', {}],
      ['/v1/orgs/acme/factories/sre/andon', { reason: 'Stop' }],
      ['/v1/orgs/acme/factories/sre/items', { title: 'A viewer’s item' }],
    ] as const) {
      const res = await post(path, body);
      expect(res.status, path).toBe(403);
      expect(await res.json()).toMatchObject({ code: 'forbidden' });
    }

    await reset('approver');
    expect((await post('/v1/orgs/acme/items/goal%2312/approve')).status).toBe(200);
    const patch = await fetch(`${base}/v1/orgs/acme/settings`, {
      method: 'PATCH',
      body: JSON.stringify({ name: 'X' }),
    });
    expect(patch.status).toBe(403);
  });

  it('leaves decisions out of a viewer’s inbox and attention', async () => {
    await reset('viewer');
    const inbox = await get<Inbox>('/v1/orgs/acme/inbox');
    expect(inbox.entries.filter((e) => e.kind === 'decision')).toEqual([]);
    expect((await get<{ waiting_on_you: number }>('/v1/orgs/acme/attention')).waiting_on_you).toBe(0);

    await reset();
    expect((await get<Inbox>('/v1/orgs/acme/inbox')).entries.some((e) => e.kind === 'decision')).toBe(true);
  });
});
