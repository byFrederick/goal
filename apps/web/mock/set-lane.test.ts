import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Item, Problem } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// The mock answers POST …/items/{item}/lane the way goald does: a 200 with the item, or the problem
// type of the refusal. The goal factory's current version routes the frontend and backend lanes.

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

beforeEach(async () => {
  await fetch(`${base}/__mock/reset`, { method: 'POST' });
});

const goal = '/v1/orgs/acme/factories/goal';

async function call<T>(path: string, init: RequestInit = {}): Promise<{ status: number; body: T }> {
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
  });
  const text = await res.text();
  return { status: res.status, body: (text ? JSON.parse(text) : undefined) as T };
}

const post = (body: unknown) => ({ method: 'POST', body: JSON.stringify(body) });

/** A new open item in the goal factory, with no lane. */
async function newItem(title: string): Promise<Item> {
  const created = await call<Item>(`${goal}/items`, post({ title }));
  return created.body;
}

const lane = (id: string, body: unknown) => call<Item | Problem>(`/v1/orgs/acme/items/${id}/lane`, post(body));

describe('mock set lane', () => {
  it('sets an open item’s lane and records the change', async () => {
    const item = await newItem('Give the search box focus');

    const set = await lane(item.id, { lane: 'frontend' });
    expect(set.status).toBe(200);
    expect((set.body as Item).lane).toBe('frontend');

    const events = await call<{ events: { kind: string; data: { from: unknown; to: string } }[] }>(
      `/v1/orgs/acme/items/${item.id}/events`,
    );
    expect(events.body.events.find((e) => e.kind === 'lane_set')?.data).toMatchObject({ from: null, to: 'frontend' });
  });

  it('refuses the lane the item already has as same-lane', async () => {
    const item = await newItem('Keep the lane');
    await lane(item.id, { lane: 'frontend' });

    const again = await lane(item.id, { lane: 'frontend' });
    expect(again.status).toBe(409);
    expect((again.body as Problem).code).toBe('same-lane');
  });

  it('refuses a lane no station routes as unknown-lane, and a missing lane as lane-required', async () => {
    const item = await newItem('Pick a lane');

    const unknown = await lane(item.id, { lane: 'nowhere' });
    expect(unknown.status).toBe(400);
    expect((unknown.body as Problem).code).toBe('unknown-lane');

    const missing = await lane(item.id, {});
    expect(missing.status).toBe(400);
    expect((missing.body as Problem).code).toBe('lane-required');
  });
});
