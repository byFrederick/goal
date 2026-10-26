import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Board, Item, ItemCard } from '@goal/api-client';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

describe('the mock waiting items (Item.waiting, ItemWaiting)', () => {
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

  const get = async <T>(path: string) => (await fetch(`${base}${path}`)).json() as Promise<T>;

  it('serves a files wait on the board card and on the item, in the contract shape', async () => {
    const board = await get<Board>('/v1/orgs/acme/factories/goal/board');
    const cards = board.stations.flatMap((s) => s.items as ItemCard[]);
    const card = cards.find((c) => c.ref === 'goal#15');

    const expected = {
      kind: 'files',
      items: ['goal#13'],
      files: ['internal/knowledge/sync.go'],
      needs: [
        {
          ref: 'goal#13',
          title: 'Spec the knowledge sync retry',
          paths: ['internal/knowledge/**', 'internal/knowledge/sync.go'],
        },
      ],
    };
    expect(card?.waiting).toEqual(expected);

    const item = await get<Item>('/v1/orgs/acme/items/goal%2315');
    expect(item.waiting).toEqual(expected);
    expect(item).not.toHaveProperty('paths');
  });

  it('serves an after wait on a queued item whose need has no paths', async () => {
    const item = await get<Item>('/v1/orgs/acme/items/goal%2314');

    expect(item.waiting).toEqual({
      kind: 'after',
      items: ['goal#16'],
      files: [],
      needs: [{ ref: 'goal#16', title: 'goald exits when the bucket has no factories', paths: [] }],
    });
  });

  it('serves no wait for an item in progress', async () => {
    const item = await get<Item>('/v1/orgs/acme/items/goal%2313');

    expect(item).not.toHaveProperty('waiting');
  });
});
