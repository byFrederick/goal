// The mock's Access read model: the picture of the Access screen, grouped three ways.
import type { ServerResponse } from 'node:http';
import type { Org } from '@goal/api-client';
import { describe, expect, it } from 'vitest';
import type { Access } from '../src/api/types.ts';
import { createAccess } from './access.ts';

const acme = { id: 'org-1', slug: 'acme', name: 'Acme' } as unknown as Org;
const NOW = Date.parse('2026-10-31T12:00:00Z');

function ask(query = '', org: Org = acme) {
  const access = createAccess({ clock: () => NOW, acme: 'org-1' });
  let status = 0;
  let answer: unknown;
  const res = {
    setHeader() {},
    end(chunk?: string) {
      if (chunk) answer = JSON.parse(chunk);
    },
    set statusCode(code: number) {
      status = code;
    },
  } as unknown as ServerResponse;
  const url = new URL(`http://mock/v1/orgs/acme/access${query}`);
  const handled = access.handle(org, '/access', 'GET', url, res);
  return { handled, status, answer: answer as Access };
}

describe('the mock access', () => {
  it('serves the picture: four MCP servers, five keys, four environments, one expiring and one failed', () => {
    const { handled, status, answer } = ask();
    expect(handled).toBe(true);
    expect(status).toBe(200);
    const rows = answer.groups.flatMap((g) => g.rows);
    expect(answer.by).toBe('reach');
    expect(rows.filter((r) => r.kind === 'mcp')).toHaveLength(4);
    expect(rows.filter((r) => r.kind !== 'mcp')).toHaveLength(5);
    expect(new Set(rows.flatMap((r) => r.uses.map((u) => `${u.factory.name}/${u.environment}`))).size).toBe(4);
    expect(answer.reached).toBe(9);
    expect(answer.need_attention).toBe(2);
    expect(rows.find((r) => r.status === 'expiring')?.expires_at).toBe(new Date(NOW + 3 * 86_400_000).toISOString());
    expect(rows.find((r) => r.status === 'failed')?.test_error).toBe('401');
  });

  it('groups by factory and by secret, and narrows to one factory', () => {
    expect(ask('?by=factory').answer.groups.map((g) => g.title)).toEqual(['software', 'sre', 'support']);
    expect(ask('?by=secret').answer.groups.map((g) => g.title)).toContain('github-token');
    const sre = ask('?factory=sre').answer;
    expect(sre.groups.flatMap((g) => g.rows).every((r) => r.uses.every((u) => u.factory.name === 'sre'))).toBe(true);
  });

  it('answers nothing for another organization and leaves other paths alone', () => {
    expect(ask('', { ...acme, id: 'org-2' } as Org).answer.groups).toEqual([]);
    const access = createAccess({ clock: () => NOW, acme: 'org-1' });
    expect(access.handle(acme, '/secrets', 'GET', new URL('http://mock/'), {} as ServerResponse)).toBe(false);
  });
});
