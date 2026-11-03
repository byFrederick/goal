import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, Factory, Org } from '@goal/api-client';
import { describe, expect, it } from 'vitest';
import { createStandingRules } from './standing-rules.ts';

const org = { id: 'org-1', slug: 'acme', name: 'Acme' } as unknown as Org;
const by: Actor = { kind: 'user', id: 'local', name: 'Local user' };

function mock(fresh = true) {
  const rules = createStandingRules({
    clock: () => Date.parse('2026-11-03T12:00:00Z'),
    actor: () => by,
    stale: (_req, res) => {
      if (fresh) return false;
      res.statusCode = 403;
      res.end(JSON.stringify({ code: 'reauth-required' }));
      return true;
    },
    factories: () => [{ id: 'f-goal', name: 'goal' }] as unknown as Factory[],
    acme: 'org-1',
  });
  const call = (rest: string, method: string, query = '') => {
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
    rules.handle(org, rest, method, new URL(`http://mock/v1/orgs/acme${rest}${query}`), {} as IncomingMessage, res);
    return { status, answer: answer as Record<string, unknown> };
  };
  return { call };
}

describe('the mock standing rules', () => {
  it('lists the active rules, keyed by server, and the revoked ones on request', () => {
    const { call } = mock();
    const active = call('/standing-rules', 'GET').answer.rules as Record<string, unknown>[];
    expect(active.map((r) => r.tool)).toEqual(['create_comment']);
    expect(active[0]).toMatchObject({
      factory_id: 'f-goal',
      environment: 'default',
      server: 'linear',
      connection_id: null,
    });
    expect((call('/standing-rules', 'GET', '?revoked=true').answer.rules as unknown[]).length).toBe(2);
  });

  it('revokes a rule once, then answers 409 standing-rule-revoked', () => {
    const { call } = mock();
    const first = call('/standing-rules/rule-linear-create-comment', 'DELETE');
    expect(first.status).toBe(200);
    expect(first.answer.revoked_at).toBe('2026-11-03T12:00:00.000Z');
    const again = call('/standing-rules/rule-linear-create-comment', 'DELETE');
    expect(again.status).toBe(409);
    expect(again.answer.code).toBe('standing-rule-revoked');
    expect((call('/standing-rules', 'GET').answer.rules as unknown[]).length).toBe(0);
  });

  it('answers 404 for an unknown rule and 409 for a revoked one', () => {
    const { call } = mock();
    expect(call('/standing-rules/nope', 'DELETE').status).toBe(404);
    expect(call('/standing-rules/rule-linear-update-issue', 'DELETE').status).toBe(409);
  });

  it('asks for a fresh sign-in before revoking', () => {
    expect(mock(false).call('/standing-rules/rule-linear-create-comment', 'DELETE').status).toBe(403);
  });
});
