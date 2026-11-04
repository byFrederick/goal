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
  it('reads the picture: four proposals waiting, $6.40 this month and the four observations', async () => {
    const { status, body } = await call(jonah(), '/factories/software/jonah', 'GET');

    expect(status).toBe(200);
    expect(body.waiting).toBe(4);
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
    expect(all.body.proposals.map((p: { number: number }) => p.number)).toEqual([10, 9, 8, 7, 6]);

    const waiting = await call(api, '/factories/software/jonah/proposals?state=proposed', 'GET');
    expect(waiting.body.proposals.map((p: { number: number }) => p.number)).toEqual([10, 9, 8, 7]);
  });

  it('serves proposal 9 as a memory proposal: memory.yaml only, three quoted facts and no tries', async () => {
    const api = jonah();
    const list = await call(api, '/factories/software/jonah/proposals', 'GET');
    const entry = list.body.proposals.find((p: { number: number }) => p.number === 9);
    expect(entry).toMatchObject({ paths: ['memory.yaml'], agents: [], evidence_count: 3 });
    expect(entry.estimate).toBeUndefined();

    const { status, body } = await call(api, '/factories/software/jonah/proposals/9', 'GET');
    expect(status).toBe(200);
    expect(body.tried).toBeUndefined();
    const count = (y: string) => (y.match(/^- text:/gm) ?? []).length;
    expect(count(body.files[0].base_content)).toBe(12);
    expect(count(body.files[0].content)).toBe(15);
    expect(body.evidence.map((e: { item_ref: string }) => e.item_ref)).toEqual([
      'software#493',
      'software#371',
      'software#308',
    ]);
    for (const e of body.evidence) expect(e.quote).toContain('“');
  });

  it('serves proposal 10 as a model proposal: three tries on each of seven items, each with its line result, and no estimate', async () => {
    const api = jonah();
    const list = await call(api, '/factories/software/jonah/proposals', 'GET');
    const entry = list.body.proposals.find((p: { number: number }) => p.number === 10);
    expect(entry).toMatchObject({
      title: 'frontend-engineer on claude-haiku-5-5',
      paths: ['factory.yaml'],
      agents: [],
    });
    expect(entry.estimate).toBeUndefined();

    const { status, body } = await call(api, '/factories/software/jonah/proposals/10', 'GET');
    expect(status).toBe(200);
    expect(body.files[0].content).toContain('model: claude-haiku-5-5');
    expect(body.tried).toMatchObject({ state: 'done', seconds: 3900, cost_usd: 4.4, kept_tests: [] });
    expect(body.tried.items).toHaveLength(7);
    const items = body.tried.items as { line_result: string; tries: { verdict: string }[] }[];
    for (const item of items) {
      expect(item.line_result).toMatch(/\$\d/);
      expect(item.tries).toHaveLength(3);
    }
    const tries = items.flatMap((i) => i.tries);
    expect(tries.filter((t) => t.verdict === 'fixed')).toHaveLength(11);
    expect(tries).toHaveLength(21);
    expect(items.filter((i) => i.line_result.startsWith('passed first time'))).toHaveLength(5);

    const again = await call(api, '/factories/software/jonah/proposals/10/try', 'POST');
    expect(again.status).toBe(409);
    expect(again.body.code).toBe('proposal-not-tried');
  });

  it('has no judge among the observations: the line observation replaced it', async () => {
    const { body } = await call(jonah(), '/factories/software/jonah', 'GET');
    expect(JSON.stringify(body)).not.toMatch(/judge/i);
    expect(body.observations.map((o: { id: string }) => o.id)).not.toContain('obs-plain-language');
  });

  it('queues a manual review with 202 and answers 409 review-in-flight to the next while it waits', async () => {
    const api = jonah();
    const review = await call(api, '/factories/software/jonah/reviews', 'POST');
    expect(review.status).toBe(202);
    expect(review.body).toMatchObject({ state: 'pending', trigger: 'manual' });
    const again = await call(api, '/factories/software/jonah/reviews', 'POST');
    expect(again.status).toBe(409);
    expect(again.body.code).toBe('review-in-flight');
  });

  it('lists the four kept tests, removes one, and answers 404 for it a second time', async () => {
    const api = jonah();
    const list = await call(api, '/factories/software/jonah/tests', 'GET');
    expect(list.body.tests).toHaveLength(4);
    const removed = await call(api, '/factories/software/jonah/tests/kept-2', 'DELETE');
    expect(removed.status).toBe(204);
    const after = await call(api, '/factories/software/jonah/tests', 'GET');
    expect(after.body.tests.map((t: { id: string }) => t.id)).toEqual(['kept-1', 'kept-3', 'kept-4']);
    const again = await call(api, '/factories/software/jonah/tests/kept-2', 'DELETE');
    expect(again.status).toBe(404);
    expect(again.body.code).toBe('kept-test-not-found');
  });

  it('has no version for a factory without a seed, and a review still queues', async () => {
    const api = jonah();
    const overview = await call(api, '/factories/goal/jonah', 'GET', 'goal');
    expect(overview.body.waiting).toBe(0);
    expect(overview.body.observations).toEqual([]);

    const review = await call(api, '/factories/goal/jonah/reviews', 'POST', 'goal');
    expect(review.status).toBe(202);
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
    expect(waiting.body.proposals.map((p: { number: number }) => p.number)).toEqual([10, 9, 8]);
  });

  it('answers Try again with an estimate, then with the new tries once confirmed, and refuses over budget', async () => {
    const api = jonah();
    const path = '/factories/software/jonah/proposals/7/try';
    const estimate = await call(api, path, 'POST');
    expect(estimate.status).toBe(200);
    expect(estimate.body.estimate.tries).toBe(2);
    expect(estimate.body.proposal).toBeUndefined();

    const first = await call(api, path, 'POST', 'software', { confirm: true });
    expect(first.status).toBe(201);
    expect(first.body.proposal.tried.items[0].tries).toHaveLength(2);
    const second = await call(api, path, 'POST', 'software', { confirm: true });
    expect(second.body.proposal.tried.items[0].tries).toHaveLength(3);
    const third = await call(api, path, 'POST', 'software', { confirm: true });
    expect(third.status).toBe(409);
    expect(third.body.code).toBe('learning-budget-exhausted');
    const shown = await call(api, '/factories/software/jonah/proposals/7', 'GET');
    expect(shown.body.tried.items[0].tries).toHaveLength(3);
  });

  it('gives the seeded kept tests passed, failed, error and no last result', async () => {
    const list = await call(jonah(), '/factories/software/jonah/tests', 'GET');
    expect(
      list.body.tests.map((t: { last_result: { result: string } | null }) => t.last_result?.result ?? null),
    ).toEqual(['passed', 'failed', 'error', null]);
  });
});
