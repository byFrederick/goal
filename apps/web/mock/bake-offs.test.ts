import { createServer, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';
import { createBakeOffs } from './bake-offs.ts';

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
const bakeOffs = () =>
  createBakeOffs({
    clock: () => Date.parse('2026-10-28T09:00:00Z'),
    factory: (_org, key) =>
      key === 'software' ? software : key === 'support' ? { id: 'f-2', name: 'support' } : undefined,
  });

async function call(
  api: ReturnType<typeof bakeOffs>,
  rest: string,
  method: string,
  body: Record<string, unknown> = {},
  factory = 'software',
) {
  const res = fakeRes();
  const path = rest.replace('{factory}', factory);
  await api.handle(org, path, method, res as unknown as ServerResponse, async () => body);
  return { status: res.statusCode, body: JSON.parse(res.body || 'null') };
}

const base = '/factories/{factory}/evals/bake-offs';

describe('the mock bake-offs API', () => {
  it('lists the picture’s done bake-off for the software factory, and none for another', async () => {
    const api = bakeOffs();
    const list = await call(api, base, 'GET');
    expect(list.status).toBe(200);
    expect(list.body.bake_offs).toHaveLength(1);
    const [only] = list.body.bake_offs;
    expect(only).toMatchObject({ state: 'done', agent: 'frontend-engineer', trials: 3, cases: 5, cost_usd: 14.2 });

    const other = await call(api, base, 'GET', {}, 'support');
    expect(other.body.bake_offs).toEqual([]);
  });

  it('answers the picture’s three arms, with haiku recommended and opus as the current model', async () => {
    const api = bakeOffs();
    const { body } = await call(api, `${base}/bake-off-1`, 'GET');
    expect(body.arms.map((a: { model: string }) => a.model)).toEqual([
      'claude-haiku-5-5',
      'claude-sonnet-5-5',
      'claude-opus-5-5',
    ]);
    expect(body.arms.find((a: { is_current: boolean }) => a.is_current).model).toBe('claude-opus-5-5');
    expect(body.recommended_arm).toBe(1);
    expect(body.arms[0].summary).toMatchObject({ tries: 15, passed: 14, cost_per_item_usd: 0.24, turns: 38 });
    expect(body.arms[2].summary.judged).toEqual({ judge: 'plain-language', title: 'Plain language', share: 0.97 });
  });

  it('answers the five pictured cases with their passed counts per arm', async () => {
    const api = bakeOffs();
    const { body } = await call(api, `${base}/bake-off-1`, 'GET');
    const cases = body.per_case.map((c: { case: string; arms: { passed: number }[] }) => [
      c.case,
      c.arms.map((a) => a.passed),
    ]);
    expect(cases).toEqual([
      ['empty-state-matches-design', [3, 3, 3]],
      ['dark-theme-contrast', [3, 2, 3]],
      ['board-card-move-menu', [2, 2, 3]],
      ['form-errors-near-field', [3, 3, 2]],
      ['usage-bar-thresholds', [3, 1, 3]],
    ]);
  });

  it('answers 404 eval-run-not-found for a bake-off the factory does not have', async () => {
    const api = bakeOffs();
    const res = await call(api, `${base}/nope`, 'GET');
    expect(res.status).toBe(404);
    expect(res.body.code).toBe('eval-run-not-found');
  });

  it('queues a re-run at the front of the list, with no summaries yet', async () => {
    const api = bakeOffs();
    const started = await call(api, `${base}/bake-off-1/rerun`, 'POST');
    expect(started.status).toBe(202);
    expect(started.body.bake_off).toMatchObject({ state: 'queued', agent: 'frontend-engineer', recommended_arm: null });
    expect(started.body.bake_off.arms.every((a: { summary: unknown }) => a.summary === null)).toBe(true);
    expect(started.body.estimate.usd).toBe(15.6);

    const list = await call(api, base, 'GET');
    expect(list.body.bake_offs.map((b: { id: string }) => b.id)).toEqual([started.body.bake_off.id, 'bake-off-1']);
  });

  it('writes an arm into the draft, and refuses an arm the bake-off does not have', async () => {
    const api = bakeOffs();
    const applied = await call(api, `${base}/bake-off-1/apply`, 'POST', { arm: 1 });
    expect(applied.status).toBe(200);

    const refused = await call(api, `${base}/bake-off-1/apply`, 'POST', { arm: 9 });
    expect(refused.status).toBe(422);
    expect(refused.body.code).toBe('invalid-bake-off');
  });

  it('answers 404 factory-not-found for a factory the organization does not have', async () => {
    const api = bakeOffs();
    const res = await call(api, base, 'GET', {}, 'missing');
    expect(res.status).toBe(404);
  });
});

// The whole mock API, the way the screen reaches it: evals answers every sub-path it does not
// know, so the bake-off routes must win over it.
describe('GET /v1/orgs/acme/factories/software/evals/bake-offs through createMockApi', () => {
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
    base = `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1/orgs/acme/factories/software/evals/bake-offs`;
  });

  afterAll(async () => {
    api.close();
    await new Promise((resolve) => server.close(resolve));
  });

  it('lists the picture’s bake-off and serves its detail, not a 404 from the evals mock', async () => {
    const list = await fetch(base);
    expect(list.status).toBe(200);
    const body = (await list.json()) as { bake_offs: { id: string }[] };
    expect(body.bake_offs.map((b) => b.id)).toEqual(['bake-off-1']);

    const detail = await fetch(`${base}/bake-off-1`);
    expect(detail.status).toBe(200);
    expect(await detail.json()).toMatchObject({ id: 'bake-off-1', recommended_arm: 1 });
  });
});
