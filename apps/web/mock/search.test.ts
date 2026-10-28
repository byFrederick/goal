import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { SearchItem } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';
import { search } from './search.ts';

const item = (number: number, title: string, station = 'build'): SearchItem => ({
  id: `item-${number}`,
  number,
  factory_id: 'goal-id',
  factory_name: 'goal',
  title,
  state: 'in_progress',
  station,
});

describe('search ranking', () => {
  const source = {
    items: [item(3, 'Spend tests for the tracker'), item(147, 'Show this week’s spend'), item(9, 'spend')],
    agents: [{ name: 'builder', stations: ['build'], factory_id: 'goal-id', factory_name: 'goal' }],
    connections: [{ name: 'spendwise', kind: 'github' }],
    factories: [{ id: 'goal-id', name: 'goal', title: 'Software' }],
  };

  it('ranks an exact match first, then a prefix, then the rest of a substring match', () => {
    const found = search(source, 'spend', 5);
    expect(found.items.map((i) => i.number)).toEqual([9, 3, 147]);
    expect(found.connections.map((c) => c.name)).toEqual(['spendwise']);
  });

  it('matches an item by its exact number, with or without the #, and ranks it first', () => {
    expect(search(source, '#147', 5).items.map((i) => i.number)).toEqual([147]);
    expect(search(source, '147', 5).items.map((i) => i.number)).toEqual([147]);
    // A number is exact: 14 is not 147.
    expect(search(source, '#14', 5).items).toEqual([]);
  });

  it('takes the title literally, so % and _ are not wildcards', () => {
    expect(search(source, '%', 5).items).toEqual([]);
    expect(search(source, '_', 5).items).toEqual([]);
  });

  it('answers each group up to limit results', () => {
    expect(search(source, 'spend', 2).items).toHaveLength(2);
  });

  it('matches a factory by its name or its title', () => {
    expect(search(source, 'software', 5).factories.map((f) => f.name)).toEqual(['goal']);
    expect(search(source, 'goal', 5).factories.map((f) => f.name)).toEqual(['goal']);
  });
});

describe('GET /v1/orgs/acme/search', () => {
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

  it('answers "spend" with acme’s three spend items first, then the one waiting on a person', async () => {
    const res = await fetch(`${base}/v1/orgs/acme/search?q=spend`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { items: SearchItem[]; agents: unknown[]; factories: unknown[] };
    const titles = body.items.map((i) => i.title);
    expect(titles).toHaveLength(4);
    expect(titles[3]).toBe('Show the sidebar spend for the organization');
    expect(titles.slice(0, 3).sort()).toEqual(
      [
        "Show this week's spend in the sidebar",
        'Check the spend against the budget',
        'S3 spend up 22% month over month',
      ].sort(),
    );
    expect(body.items.every((i) => i.factory_name && i.station)).toBe(true);
  });

  it('names an agent by its role and factory, never by a copy number', async () => {
    const res = await fetch(`${base}/v1/orgs/acme/search?q=builder`);
    const body = (await res.json()) as { agents: { name: string; factory_name: string }[] };
    expect(body.agents.length).toBeGreaterThan(0);
    for (const agent of body.agents) expect(agent.name).not.toMatch(/-\d+$/);
  });

  it('refuses an empty query and a limit out of range with a problem', async () => {
    const blank = await fetch(`${base}/v1/orgs/acme/search?q=%20%20`);
    expect(blank.status).toBe(400);
    const limit = await fetch(`${base}/v1/orgs/acme/search?q=spend&limit=21`);
    expect(limit.status).toBe(400);
  });
});
