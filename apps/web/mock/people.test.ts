import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Org, OrgRole } from '@goal/api-client';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';
import { orgCanOf } from './people.ts';
import { readSources } from './roles.ts';

const { table } = readSources();
const orgRoles: OrgRole[] = ['owner', 'admin', 'member'];

describe('orgCanOf (contracts/api/permissions.yaml)', () => {
  const orgRules = table.rules.filter((r) => r.level === 'org');

  it('answers the org-level rules whose org_roles include the role, sorted', () => {
    for (const role of orgRoles) {
      const expected = orgRules
        .filter((r) => r.org_roles?.includes(role))
        .map((r) => r.name)
        .sort();
      expect(orgCanOf(table, role), role).toEqual(expected);
    }
  });

  it('gives a member exactly the nine reads and token rights', () => {
    expect(orgCanOf(table, 'member')).toEqual(
      [
        'org:read',
        'member:read',
        'personal_token:read',
        'personal_token:manage',
        'standing_rule:read',
        'secret:read',
        'access:read',
        'knowledge:read',
      ].sort(),
    );
  });

  it('keeps the owner-only rules from admins and gives owners the factory ones', () => {
    const admin = orgCanOf(table, 'admin');
    for (const p of ['org:delete', 'org:transfer', 'org:signup_domain']) expect(admin).not.toContain(p);
    expect(orgCanOf(table, 'owner')).toEqual(expect.arrayContaining(['factory:archive', 'factory:delete']));
  });
});

describe('a signed-in caller’s Org.can', () => {
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

  async function canOf(email: string): Promise<string[]> {
    const reset = await fetch(`${base}/__mock/reset`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ auth: { mode: 'session', signed_in: 'fresh', email } }),
    });
    const cookie = (reset.headers.get('set-cookie') ?? '').split(';')[0] ?? '';
    const res = await fetch(`${base}/v1/orgs/acme`, { headers: { Cookie: cookie } });
    return ((await res.json()) as Org).can ?? [];
  }

  it('matches the table for a member, an admin and an owner', async () => {
    expect(await canOf('a.ortiz@example.com')).toEqual(orgCanOf(table, 'member'));
    expect(await canOf('m.okafor@example.com')).toEqual(orgCanOf(table, 'admin'));
    expect(await canOf('f.cruz@example.com')).toEqual(orgCanOf(table, 'owner'));
  });
});
