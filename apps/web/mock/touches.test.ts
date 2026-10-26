import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Item, ItemEvent, OrgTouchMetrics, TouchMetrics } from '@goal/api-client';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';
import { touchesOf, touchStatsOf } from './touches.ts';

const at = (m: number) => new Date(Date.UTC(2026, 9, 23, 9, m)).toISOString();
let ids = 0;
const ev = (kind: string, m: number, data: Record<string, unknown> = {}, actor = 'Ada'): ItemEvent => ({
  id: ++ids,
  item_id: 'i',
  at: at(m),
  kind,
  station: 'work',
  actor,
  data,
});
const ada = { actor: 'user:ada' };

describe('touchesOf (goald’s rule, spec 4 §4.9)', () => {
  it('counts one action by a person once, by its most specific event, and nothing else', () => {
    const touches = touchesOf(
      [
        ev('created', 0, ada),
        ev('gate_rejected', 1, {}, 'reviewer'),
        ev('approved', 2, { actor: 'service:ci' }),
        ev('lease_taken', 3, ada),
        ev('rejected', 3, { ...ada, planned: false }),
        ev('merged', 4, { ...ada, planned: true }),
        ev('approved', 4, { ...ada, planned: true }),
        ev('approved', 5, { actor: 'user:bob', planned: false }),
        ev('approved', 6, { actor: 'user:bob' }),
      ],
      {},
    );
    expect(touches.map((t) => [t.kind, t.planned, t.reason])).toEqual([
      ['rejected', false, null],
      ['merged', true, 'required_gate'],
      ['approved', false, null],
      ['approved', true, 'required_gate'],
    ]);
  });

  it('measures touches per finished item, the touchless share and the versions newest first', () => {
    const note = { kind: 'note', station: 'work', planned: false, reason: null };
    const s = touchStatsOf(
      [
        { type: 't', done_at: 1, version: { id: 'v1', number: 1 }, touches: [] },
        { type: 't', done_at: 2, version: { id: 'v2', number: 2 }, touches: [note, note] },
        { type: 't', done_at: 3, version: null, touches: [note] },
      ],
      2,
    );
    expect(s).toMatchObject({ finished: 3, touches: 3, per_item_mean: 1, per_item_median: 1, line_stops: 2 });
    expect(s.touchless_rate).toBeCloseTo(1 / 3);
    expect(s.by_version.map((v) => v.number)).toEqual([2, 1, null]);
  });
});

describe('the mock’s touch routes', () => {
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

  const send = (method: string, path: string, body?: unknown) =>
    fetch(`${base}${path}`, {
      method,
      headers: { 'Content-Type': 'application/json' },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
  const get = async <T>(path: string) => (await fetch(`${base}${path}`)).json() as Promise<T>;

  it('lists an item’s touches, sets a reason and refuses an event that is no touch', async () => {
    await send('POST', '/__mock/reset');
    expect((await send('POST', '/v1/orgs/acme/items/goal%2312/notes', { text: 'see the docs' })).status).toBe(202);

    const item = await get<Item>('/v1/orgs/acme/items/goal%2312');
    const note = item.touches?.list.findLast((t) => t.kind === 'note');
    expect(note).toMatchObject({ planned: false, reason: null, actor: { kind: 'user', id: 'local' } });

    const reason = `/v1/orgs/acme/items/goal%2312/touches/${note?.event_id}/reason`;
    expect((await send('PUT', reason, { reason: 'missing_knowledge' })).status).toBe(204);
    const after = await get<Item>('/v1/orgs/acme/items/goal%2312');
    expect(after.touches?.list.find((t) => t.event_id === note?.event_id)).toMatchObject({
      reason: 'missing_knowledge',
      reason_by: 'user:local',
    });

    expect((await send('PUT', '/v1/orgs/acme/items/goal%2312/touches/999999/reason', { reason: 'other' })).status).toBe(
      404,
    );
    expect((await send('PUT', reason, { reason: 'bad luck' })).status).toBe(400);
  });

  it('measures the line and the organization over a range', async () => {
    await send('POST', '/__mock/reset');
    const line = await get<TouchMetrics>('/v1/orgs/acme/factories/goal/metrics/touches?weeks=4');
    expect(line.stats.finished).toBeGreaterThan(0);
    expect(line.weekly).toHaveLength(4);
    expect(line.stats.by_version.length).toBeGreaterThan(0);

    const org = await get<OrgTouchMetrics>('/v1/orgs/acme/metrics/touches');
    expect(org.total.finished).toBe(org.factories.reduce((n, f) => n + f.stats.finished, 0));
    expect((await send('GET', '/v1/orgs/acme/factories/goal/metrics/touches?version=draft')).status).toBe(400);
  });
});
