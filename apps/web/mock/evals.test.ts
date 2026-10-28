import type { ServerResponse } from 'node:http';
import { describe, expect, it } from 'vitest';
import { createEvals } from './evals.ts';

// A response that records what the handler wrote, so the routes are tested without a server.
function fakeRes() {
  const res = {
    statusCode: 200,
    body: '' as string,
    setHeader: () => {},
    end(chunk: string) {
      res.body = chunk;
    },
  };
  return res;
}

const org = { id: 'org-1' } as never;
const software = { id: 'factory-software', name: 'software' };
const evals = () =>
  createEvals({
    clock: () => Date.parse('2026-10-27T12:00:00Z'),
    factory: (_org, key) => (key === 'software' ? software : undefined),
  });

async function call(api: ReturnType<typeof evals>, rest: string, method: string, body: unknown = {}) {
  const res = fakeRes();
  await api.handle(org, rest, method, res as unknown as ServerResponse, async () => body as Record<string, unknown>);
  return { status: res.statusCode, body: JSON.parse(res.body || 'null') };
}

describe('the mock evals API', () => {
  it('lists the seeded five cases with the picture summary: 3 of 5 passing, 13 of 15 tries', async () => {
    const api = evals();
    const { status, body } = await call(api, '/factories/software/evals', 'GET');

    expect(status).toBe(200);
    expect(body.cases).toHaveLength(5);
    expect(body.summary).toMatchObject({ version: 21, cases: 5, cases_passing: 3, tries: 15, tries_passed: 13 });
  });

  it('prices a run of every case at the sum of the cases, $2.40 for the picture', async () => {
    const { body } = await call(evals(), '/factories/software/evals/estimate', 'POST');
    expect(body.usd).toBe(2.4);
  });

  it('refuses a case name that is taken, and accepts a new one as the next version', async () => {
    const api = evals();
    const taken = await call(api, '/factories/software/evals/cases', 'POST', {
      run: 'run-41',
      name: 'reviewer-approves-clean-change',
    });
    expect(taken.body.code).toBe('eval-case-exists');

    const added = await call(api, '/factories/software/evals/cases', 'POST', {
      run: 'run-41',
      name: 'reviewer-misses-tests',
    });
    expect(added.status).toBe(201);
    expect(added.body.number).toBe(22);
  });
});
