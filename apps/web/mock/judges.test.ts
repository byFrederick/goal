import type { ServerResponse } from 'node:http';
import { describe, expect, it } from 'vitest';
import { createJudges } from './judges.ts';

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
const judges = () =>
  createJudges({
    factory: (_org, key) =>
      key === 'software' ? software : key === 'support' ? { id: 'f-2', name: 'support' } : undefined,
  });

async function call(
  api: ReturnType<typeof judges>,
  rest: string,
  method: string,
  body: unknown = {},
  factory = 'software',
) {
  const res = fakeRes();
  const path = rest.replace('{factory}', factory);
  await api.handle(org, path, method, res as unknown as ServerResponse, async () => body as Record<string, unknown>);
  return { status: res.statusCode, body: JSON.parse(res.body || 'null') };
}

describe('the mock judges API', () => {
  it('lists the four pictured judges at version 21 with the picture total: $11.60 in 30 days', async () => {
    const api = judges();
    const { status, body } = await call(api, '/factories/{factory}/judges', 'GET');

    expect(status).toBe(200);
    expect(body.version).toBe(21);
    expect(body.judges.map((j: { title: string }) => j.title)).toEqual([
      'Spec criteria testable',
      'Review reason actionable',
      'Plain language',
      'Efficiency',
    ]);
    expect(body.judge_cost_usd).toBe(11.6);
  });

  it('reads the review judge with its outcomes, rubric and newest scores, and 404s an unknown one', async () => {
    const api = judges();
    const judge = await call(api, '/factories/{factory}/judges/review-reason-actionable', 'GET');
    expect(judge.status).toBe(200);
    expect(judge.body).toMatchObject({ pass_mark: 0.7, sample: 0.25, version: 21 });
    expect(judge.body.outcomes).toHaveLength(3);
    expect(judge.body.recent).toHaveLength(4);
    expect(judge.body.summary).toMatchObject({ scored: 46, pass_rate: 0.81, cost_usd: 3.2 });

    const missing = await call(api, '/factories/{factory}/judges/nope', 'GET');
    expect(missing).toMatchObject({ status: 404, body: { code: 'judge-not-found' } });
  });

  it('publishes a saved judge as the next version and keeps its scores', async () => {
    const api = judges();
    const judge = await call(api, '/factories/{factory}/judges/efficiency', 'GET');
    const saved = await call(api, '/factories/{factory}/judges/efficiency', 'PUT', {
      ...judge.body,
      enabled: false,
      expected_current_version_id: 'v21',
    });

    expect(saved.status).toBe(200);
    expect(saved.body).toMatchObject({ enabled: false, version: 22 });
    const list = await call(api, '/factories/{factory}/judges', 'GET');
    expect(list.body.judges.find((j: { slug: string }) => j.slug === 'efficiency')).toMatchObject({ enabled: false });
  });

  it('adds a judge, and refuses a second one with the same name', async () => {
    const api = judges();
    const body = {
      slug: 'tone',
      title: 'Tone',
      criterion: 'Reads kindly',
      outcomes: [{ score: 1, label: 'Kind', meaning: 'Kind' }],
      pass_mark: 0.7,
      sample: 0.25,
      rubric: 'Read it.',
    };
    const created = await call(api, '/factories/{factory}/judges', 'POST', body);
    expect(created.status).toBe(201);
    expect(created.body).toMatchObject({ slug: 'tone', version: 22 });

    const again = await call(api, '/factories/{factory}/judges', 'POST', body);
    expect(again).toMatchObject({ status: 409, body: { code: 'judge-exists' } });
  });

  it('has no judges before a factory publishes one', async () => {
    const api = judges();
    const { body } = await call(api, '/factories/{factory}/judges', 'GET', {}, 'support');
    expect(body).toEqual({ version: null, judges: [], judge_cost_usd: 0 });
  });

  it('queues ten tries on a judge and never finishes them', async () => {
    const api = judges();
    const { status, body } = await call(api, '/factories/{factory}/judges/spec-criteria-testable/try', 'POST');
    expect(status).toBe(202);
    expect(body.tries).toHaveLength(10);
    expect(body.tries.every((t: { state: string }) => t.state === 'queued')).toBe(true);
  });
});
