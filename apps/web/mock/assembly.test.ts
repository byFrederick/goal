import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { AssemblyParts, Item } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

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

describe('mock assembly parts', () => {
  it('returns a done assembly’s parts, all merged', async () => {
    const items = (await (await fetch(`${base}/v1/orgs/acme/factories/goal/items?limit=200`)).json()) as {
      items?: Item[];
    };
    const done = items.items?.find((i) => i.number === 21);
    expect(done, 'fixture item 21 is listed').toBeDefined();
    const res = await fetch(`${base}/v1/orgs/acme/items/${done?.id}/parts`);
    const body = (await res.json()) as AssemblyParts;
    expect(body.total).toBe(2);
    expect(body.done).toBe(2);
  });
});
