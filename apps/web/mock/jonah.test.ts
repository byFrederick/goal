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

async function call(
  api: ReturnType<typeof jonah>,
  path: string,
  method: string,
  factory = 'software',
  body: Record<string, unknown> = {},
) {
  const res = fakeRes();
  const url = new URL(path, 'http://mock');
  const rest = url.pathname.replace('{factory}', factory);
  await api.handle(org, rest, method, url, res as unknown as ServerResponse, async () => body);
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

  it('serves proposal 7 as the picture shows it: two files, the four runs as evidence and the versions it moves between', async () => {
    const { status, body } = await call(jonah(), '/factories/software/jonah/proposals/7', 'GET');

    expect(status).toBe(200);
    expect(body.number).toBe(7);
    expect(body.state).toBe('proposed');
    expect(body.made_against_version).toBe(21);
    expect(body.publishes_as_version).toBe(22);
    expect(body.files.map((f: { path: string }) => f.path)).toEqual([
      'agents/frontend-engineer.md',
      'rules/evidence.md',
    ]);
    expect(body.evidence.map((e: { item_ref: string }) => e.item_ref)).toEqual([
      'software#149',
      'software#138',
      'software#131',
      'software#127',
    ]);
  });

  it('answers 404 proposal-not-found for a number that is not a seeded proposal, and for another factory', async () => {
    const api = jonah();
    const missing = await call(api, '/factories/software/jonah/proposals/99', 'GET');
    expect(missing.status).toBe(404);
    expect(missing.body.code).toBe('proposal-not-found');

    const elsewhere = await call(api, '/factories/goal/jonah/proposals/7', 'GET', 'goal');
    expect(elsewhere.status).toBe(404);
    expect(elsewhere.body.code).toBe('proposal-not-found');
  });

  it('publishes proposal 7 as v22, and a second decision on it answers 409 proposal-decided', async () => {
    const api = jonah();
    const approved = await call(api, '/factories/software/jonah/proposals/7/approve', 'POST');
    expect(approved.status).toBe(200);
    expect(approved.body).toMatchObject({ number: 7, state: 'published', version: 22 });

    const shown = await call(api, '/factories/software/jonah/proposals/7', 'GET');
    expect(shown.body).toMatchObject({ state: 'published', published_version: 22 });

    const again = await call(api, '/factories/software/jonah/proposals/7/reject', 'POST', 'software', {
      reason: 'late',
    });
    expect(again.status).toBe(409);
    expect(again.body.code).toBe('proposal-decided');
  });

  it('answers 409 draft-open when proposal 8 is approved, the flagged case, and leaves it proposed', async () => {
    const api = jonah();
    const refused = await call(api, '/factories/software/jonah/proposals/8/approve', 'POST');
    expect(refused.status).toBe(409);
    expect(refused.body.code).toBe('draft-open');

    const shown = await call(api, '/factories/software/jonah/proposals/8', 'GET');
    expect(shown.body.state).toBe('proposed');
  });

  it('edit moves proposal 7 to applied, and a reject of a waiting proposal needs a reason that is not blank', async () => {
    const api = jonah();
    const edited = await call(api, '/factories/software/jonah/proposals/7/edit', 'POST');
    expect(edited.status).toBe(200);
    expect(edited.body).toMatchObject({ number: 7, state: 'applied' });

    const blank = await call(api, '/factories/software/jonah/proposals/8/reject', 'POST', 'software', { reason: '  ' });
    expect(blank.status).toBe(400);
    expect(blank.body.code).toBe('invalid-request');
  });

  it('rejects proposal 7 with the reason, which the list then shows as rejected and out of the waiting ones', async () => {
    const api = jonah();
    const blank = await call(api, '/factories/software/jonah/proposals/7/reject', 'POST', 'software', {
      reason: '   ',
    });
    expect(blank.status).toBe(400);
    expect(blank.body.code).toBe('invalid-request');

    const rejected = await call(api, '/factories/software/jonah/proposals/7/reject', 'POST', 'software', {
      reason: 'Put it in the pull request.',
    });
    expect(rejected.status).toBe(200);
    expect(rejected.body).toMatchObject({ number: 7, state: 'rejected' });

    const waiting = await call(api, '/factories/software/jonah/proposals?state=proposed', 'GET');
    expect(waiting.body.proposals.map((p: { number: number }) => p.number)).toEqual([8]);
  });
});
