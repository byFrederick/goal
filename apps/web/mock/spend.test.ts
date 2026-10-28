// The mock's spend answer (spend.ts): the windows it answers, the 400 for an unknown one, and that its
// days add up to the total and each day to its factories. The acme factories it budgets are Software,
// SRE and Support, as the spend screen is drawn.
import type { ServerResponse } from 'node:http';
import { describe, expect, it } from 'vitest';
import { createSpend, type SpendFactory } from './spend.ts';

const factories: SpendFactory[] = [
  { id: 'f-software', name: 'software', title: 'Software' },
  { id: 'f-sre', name: 'sre', title: 'SRE' },
  { id: 'f-support', name: 'support', title: 'Support' },
  { id: 'f-goal', name: 'goal', title: 'Goal' },
];

/** A ServerResponse that keeps what the handler wrote. */
function response() {
  const out = { statusCode: 0, body: '', headers: {} as Record<string, string> };
  const res = {
    setHeader: (name: string, value: string) => {
      out.headers[name] = value;
      return res;
    },
    end: (body: string) => {
      out.body = body;
    },
  };
  Object.defineProperty(res, 'statusCode', {
    get: () => out.statusCode,
    set: (v: number) => {
      out.statusCode = v;
    },
  });
  return { res: res as unknown as ServerResponse, out };
}

const spend = createSpend({
  clock: () => Date.UTC(2026, 9, 27, 9, 0),
  factoriesOf: () => factories,
  reasonOf: (_org, f) => (f.name === 'sre' ? 'Andon pulled, no runs today' : null),
});

async function answer(window: string | null) {
  const url = new URL(`http://mock.test/v1/orgs/acme/spend${window === null ? '' : `?window=${window}`}`);
  const { res, out } = response();
  const handled = await spend.handle({ id: 'org-1' }, '/spend', 'GET', url, res);
  expect(handled).toBe(true);
  return { status: out.statusCode, body: out.body ? JSON.parse(out.body) : null };
}

describe('the mock spend answer', () => {
  it('answers a window with its days, factories and budgets', async () => {
    const { status, body } = await answer('week');

    expect(status).toBe(200);
    expect(body.window).toBe('week');
    expect(body.complete).toBe(true);
    // Monday 2026-10-26 is the week's first day, the 27th (a Tuesday) its last.
    expect(body.days.map((d: { day: string }) => d.day)).toEqual(['2026-10-26', '2026-10-27']);
    expect(body.budgets.map((b: { factory: { name: string } }) => b.factory.name)).toEqual([
      'software',
      'sre',
      'support',
    ]);
    expect(body.budgets.find((b: { factory: { name: string } }) => b.factory.name === 'sre').state).toBe('stopped');
  });

  it('adds up: each day is the sum of its factories, and the window the sum of its days', async () => {
    const { body } = await answer('90d');

    expect(body.days).toHaveLength(90);
    for (const d of body.days) {
      const split = d.by_factory.reduce((n: number, f: { usd: number }) => n + f.usd, 0);
      expect(Math.round(split * 100) / 100).toBe(d.usd);
    }
    const total = body.days.reduce((n: number, d: { usd: number }) => n + d.usd, 0);
    expect(Math.round(total * 100) / 100).toBe(body.total_usd);
  });

  it('answers the month from the 1st, and refuses another window with 400', async () => {
    const month = await answer('month');
    expect(month.body.days[0].day).toBe('2026-10-01');

    const other = await answer('yesterday');
    expect(other.status).toBe(400);
  });
});

describe('the mock spend export', () => {
  async function exported(window: string) {
    const url = new URL(`http://mock.test/v1/orgs/acme/spend/export?window=${window}`);
    const { res, out } = response();
    expect(await spend.handle({ id: 'org-1' }, '/spend/export', 'GET', url, res)).toBe(true);
    return out;
  }

  it('answers the window as CSV with a filename for the day', async () => {
    const out = await exported('week');

    expect(out.statusCode).toBe(200);
    expect(out.headers['Content-Type']).toBe('text/csv; charset=utf-8');
    expect(out.headers['Content-Disposition']).toBe('attachment; filename="spend-2026-10-27.csv"');
    const [header, ...rows] = out.body.trim().split('\n');
    expect(header).toBe('factory,agent,model,cost_usd');
    expect(rows.length).toBeGreaterThan(0);
    expect(rows[0]?.split(',')).toHaveLength(4);
  });

  it('refuses another window with 400, as the spend answer does', async () => {
    const out = await exported('year');
    expect(out.statusCode).toBe(400);
  });
});
