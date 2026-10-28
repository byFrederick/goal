import type { ServerResponse } from 'node:http';
import { describe, expect, it } from 'vitest';
import { createJonah } from './jonah.ts';

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

const org = { id: 'org-1' };
const software = { id: 'factory-software', name: 'software' };
const other = { id: 'factory-goal', name: 'goal' };
const now = Date.parse('2026-10-28T12:00:00Z');
const jonah = () =>
  createJonah({
    factory: (_org, key) => (key === 'software' ? software : key === 'goal' ? other : undefined),
    clock: () => now,
  });

async function call(api: ReturnType<typeof jonah>, path: string, method: string, factory = 'software') {
  const res = fakeRes();
  const url = new URL(path, 'http://mock');
  const rest = url.pathname.replace('{factory}', factory);
  await api.handle(org, rest, method, url, res as unknown as ServerResponse, async () => ({}));
  return { status: res.statusCode, body: JSON.parse(res.body || 'null') };
}

describe('the mock Jonah API', () => {
  it('reads the picture: two proposals waiting, $6.40 this month and the four observations', async () => {
    const { status, body } = await call(jonah(), '/factories/software/jonah', 'GET');

    expect(status).toBe(200);
    expect(body.waiting).toBe(2);
    expect(body.month_cost_usd).toBe(6.4);
    expect(body.policy.approver_roles).toEqual(['owner', 'admin']);
    expect(body.observations).toHaveLength(4);
    expect(body.observations.map((o: { state: string }) => o.state)).toEqual([
      'proposed',
      'watching',
      'watching',
      'watching',
    ]);
  });

  it('lists the proposals newest first, and only those in the state the query names', async () => {
    const api = jonah();
    const all = await call(api, '/factories/software/jonah/proposals', 'GET');
    expect(all.body.proposals.map((p: { number: number }) => p.number)).toEqual([8, 7, 6]);

    const waiting = await call(api, '/factories/software/jonah/proposals?state=proposed', 'GET');
    expect(waiting.body.proposals.map((p: { number: number }) => p.number)).toEqual([8, 7]);
  });

  it('answers 409 review-in-flight to a second manual review until a reset', async () => {
    const api = jonah();
    const first = await call(api, '/factories/software/jonah/reviews', 'POST');
    expect(first.status).toBe(202);
    expect(first.body.trigger).toBe('manual');

    const second = await call(api, '/factories/software/jonah/reviews', 'POST');
    expect(second.status).toBe(409);
    expect(second.body.code).toBe('review-in-flight');

    api.reset();
    const after = await call(api, '/factories/software/jonah/reviews', 'POST');
    expect(after.status).toBe(202);
  });

  it('has no version for a factory without a seed: nothing waits and a review is version-not-found', async () => {
    const api = jonah();
    const overview = await call(api, '/factories/goal/jonah', 'GET', 'goal');
    expect(overview.body.waiting).toBe(0);
    expect(overview.body.observations).toEqual([]);

    const review = await call(api, '/factories/goal/jonah/reviews', 'POST', 'goal');
    expect(review.status).toBe(404);
    expect(review.body.code).toBe('version-not-found');
  });
});
