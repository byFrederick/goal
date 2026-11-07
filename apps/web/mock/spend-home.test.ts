import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { components } from '@goal/api-client';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// Home's factory overview and Spend's budget rows read one per-factory spend and one weekly budget.

type FactoryOverviewList = components['schemas']['FactoryOverviewList'];
type OrgSpend = components['schemas']['OrgSpend'];

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

const get = async <T>(path: string) => (await (await fetch(`${base}/v1/orgs/acme${path}`)).json()) as T;

describe('spend budgets and Home', () => {
  it('show each factory the same day and 7-day spend and the same weekly budget', async () => {
    const [day, week, spend] = await Promise.all([
      get<FactoryOverviewList>('/overview?window=1d'),
      get<FactoryOverviewList>('/overview?window=7d'),
      get<OrgSpend>('/spend?window=week'),
    ]);
    expect(spend.budgets.length).toBeGreaterThan(0);
    for (const b of spend.budgets) {
      const d = day.factories.find((o) => o.factory.id === b.factory.id);
      const w = week.factories.find((o) => o.factory.id === b.factory.id);
      expect(b.today_usd).toBeCloseTo(d?.spend_usd ?? NaN, 2);
      expect(b.week_usd).toBeCloseTo(w?.spend_usd ?? NaN, 2);
      expect(b.weekly_usd).toBe(w?.budget_usd ?? null);
    }
  });
});
