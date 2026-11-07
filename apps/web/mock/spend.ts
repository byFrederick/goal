// The organization's spend for the mock API (spec 4 §11): GET /v1/orgs/{org}/spend?window=week|month|90d,
// with the figures goald computes from the runs' costs and each factory's budget (the same caps the
// factory definition sets). The mock keeps three factories in the acme fixtures budgeted (Software, SRE
// and Support, as the screen is drawn); the other factories spend nothing here and are left out. A window
// other than week, month or 90d is 400 bad-request, as goald answers it.
import type { ServerResponse } from 'node:http';
import type { components } from '@goal/api-client';

type Schemas = components['schemas'];
type OrgSpend = Schemas['OrgSpend'];
type SpendBudget = Schemas['SpendBudget'];
type SpendWindow = Schemas['SpendWindow'];

const dayMs = 86_400_000;
const windows: readonly SpendWindow[] = ['week', 'month', '90d'];

/** A factory as the mock keeps it: its id, name and title. */
export interface SpendFactory {
  id: string;
  name: string;
  title?: string | undefined;
}

export interface SpendOptions<F extends SpendFactory = SpendFactory> {
  clock: () => number;
  /** The factories the caller sees in an organization, as the mock keeps them. */
  factoriesOf: (org: string) => F[];
  /** The reason of a factory's open andon pull, or null while its line runs. */
  reasonOf: (org: string, factory: SpendFactory) => string | null;
  /** The weekly budget a factory's definition sets, when the mock keeps one that Home also shows. */
  weeklyOf?: (factory: SpendFactory) => number | null | undefined;
  /** A factory's spend over UTC days `from` through `to`, from the source Home reads. */
  spendOf?: (factory: F, from: number, to: number) => number;
}

/** The budgets the acme fixtures set in each factory's definition, and the agents that spend. */
const budgeted: Record<string, { daily: number; weekly: number; agents: [string, number][] }> = {
  software: {
    daily: 150,
    weekly: 800,
    agents: [
      ['backend-engineer', 0.4],
      ['reviewer', 0.24],
      ['infra-engineer', 0.14],
      ['frontend-engineer', 0.12],
      ['architect', 0.1],
    ],
  },
  sre: {
    daily: 40,
    weekly: 250,
    agents: [
      ['infra-engineer', 0.6],
      ['reviewer', 0.4],
    ],
  },
  support: {
    daily: 30,
    weekly: 350,
    agents: [['support-agent', 1]],
  },
};

/** The models the runs use, and their share of the spend. */
const models: [string, number][] = [
  ['claude-opus-5-5', 0.82],
  ['claude-haiku-5-5', 0.1],
  ['claude-sonnet-5-5', 0.08],
];

const connection = { name: 'claude', kind: 'claude' };

const cents = (n: number) => Math.round(n * 100) / 100;
const utcDay = (ms: number) => new Date(ms).toISOString().slice(0, 10);
const startOfDay = (ms: number) => Math.floor(ms / dayMs) * dayMs;

/** A day's spend wobbles around the factory's typical day, and weekends are quiet. */
function spendOn(ms: number, budget: { daily: number }): number {
  const day = startOfDay(ms) / dayMs;
  const wobble = 0.6 + (((day * 37) % 10) / 10) * 0.8;
  const weekend = [0, 6].includes(new Date(ms).getUTCDay()) ? 0.3 : 1;
  return cents(budget.daily * 0.55 * wobble * weekend);
}

/** Every UTC day from `from` through `to`, both included, as start-of-day times. */
function daysBetween(from: number, to: number): number[] {
  const days: number[] = [];
  for (let t = startOfDay(from); t <= to; t += dayMs) days.push(t);
  return days;
}

function windowStart(window: SpendWindow, now: number): number {
  const today = startOfDay(now);
  if (window === '90d') return today - 89 * dayMs;
  if (window === 'month') {
    const d = new Date(now);
    return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1);
  }
  const mondayOffset = (new Date(today).getUTCDay() + 6) % 7;
  return today - mondayOffset * dayMs;
}

export function createSpend<F extends SpendFactory>(options: SpendOptions<F>) {
  /** The spend of one factory on one UTC day, from its budget. */
  const factorySpend = (f: F, ms: number): number => {
    if (options.spendOf) return options.spendOf(f, startOfDay(ms), startOfDay(ms));
    const budget = budgeted[f.name];
    return budget ? spendOn(ms, budget) : 0;
  };
  const daySpend = (org: string, ms: number) =>
    budgetedFactories(org).map((f) => ({ factory_id: f.id, usd: factorySpend(f, ms) }));
  const sumOfDay = (org: string, ms: number) => cents(daySpend(org, ms).reduce((n, f) => n + f.usd, 0));
  const budgetedFactories = (org: string) => options.factoriesOf(org).filter((f) => budgeted[f.name] !== undefined);
  const sumOver = (org: string, days: number[]) => cents(days.reduce((n, d) => n + sumOfDay(org, d), 0));

  function build(org: string, window: SpendWindow): OrgSpend {
    const now = options.clock();
    const today = startOfDay(now);
    const from = windowStart(window, now);
    const days = daysBetween(from, now);
    const factories = budgetedFactories(org);
    const total = sumOver(org, days);

    const byAgent = factories.flatMap((f) =>
      (budgeted[f.name]?.agents ?? []).map(([agent, share]) => ({
        factory_id: f.id,
        agent,
        usd: cents(spendOverFactory(f, days) * share),
      })),
    );
    const byModel = models.map(([model, share]) => ({ model, usd: cents(total * share) }));

    const monthStart = Date.UTC(new Date(now).getUTCFullYear(), new Date(now).getUTCMonth(), 1);
    const lastMonthStart = Date.UTC(new Date(monthStart).getUTCFullYear(), new Date(monthStart).getUTCMonth() - 1, 1);
    const thisMonth = sumOver(org, daysBetween(monthStart, now));
    const lastMonth = sumOver(org, daysBetween(lastMonthStart, monthStart - dayMs));

    const merged = Math.max(1, Math.round(total / 4));
    const weekly = Array.from({ length: 8 }, (_, k) => {
      const weekStart = today - ((new Date(today).getUTCDay() + 6) % 7) * dayMs - (7 - k) * 7 * dayMs;
      const spend = cents(total * (0.8 + k * 0.03));
      const items = Math.max(1, Math.round(spend / (4.9 - k * 0.2)));
      return {
        week_start: utcDay(weekStart),
        spend_usd: spend,
        merged: items,
        cost_per_merged_usd: cents(spend / items),
      };
    });

    const rework = cents(total * 0.12);
    const budgets: SpendBudget[] = factories.map((f) => {
      const cap = budgeted[f.name] ?? { daily: 0, weekly: 0, agents: [] };
      const weekly = options.weeklyOf ? (options.weeklyOf(f) ?? null) : cap.weekly;
      const today_usd = factorySpend(f, today);
      const week_usd = cents(daysBetween(today - 6 * dayMs, now).reduce((n, d) => n + factorySpend(f, d), 0));
      const reason = options.reasonOf(org, f);
      const state = reason !== null ? 'stopped' : weekly !== null && week_usd >= weekly ? 'over_budget' : 'running';
      return {
        factory: { id: f.id, name: f.name, title: f.title ?? f.name },
        daily_usd: cap.daily,
        weekly_usd: weekly,
        today_usd,
        week_usd,
        state,
        reason: state === 'stopped' ? reason : state === 'over_budget' ? `$${week_usd} of $${weekly} this week` : null,
        model_connections: [connection],
      };
    });

    return {
      window,
      since: new Date(from).toISOString(),
      until: new Date(now).toISOString(),
      complete: true,
      total_usd: total,
      this_month_usd: thisMonth,
      last_month_usd: lastMonth,
      days: days.map((d) => ({
        day: utcDay(d),
        usd: sumOfDay(org, d),
        by_factory: daySpend(org, d).map((f) => ({ factory_id: f.factory_id, usd: f.usd })),
      })),
      by_agent: byAgent,
      by_model: byModel,
      merged,
      cost_per_merged_usd: cents(total / merged),
      weekly,
      rework_usd: rework,
      rework_share: total > 0 ? 0.12 : 0,
      budgets,
      usage: usageOf(factories, window),
    };
  }

  /** Sample box usage: the factory's agents and stations, one with no box size and one with a missing percentile. */
  function usageOf(factories: F[], window: SpendWindow): OrgSpend['usage'] {
    const scale = window === 'week' ? 1 : window === 'month' ? 4 : 12;
    const GB = 1024 ** 3;
    const row = (i: number) => ({
      box_size: (i % 3 === 2 ? null : (['large', 'medium', 'small'] as const)[i % 3]) ?? null,
      runs: (12 - i * 2) * scale,
      cpu_seconds_p50: 200 / (i + 1),
      cpu_seconds_p90: i === 3 ? null : 450 / (i + 1),
      peak_memory_bytes_p50: (1.6 / (i + 1)) * GB,
      peak_memory_bytes_p90: (2.6 / (i + 1)) * GB,
    });
    const agents = factories.flatMap((f) =>
      (budgeted[f.name]?.agents ?? []).slice(0, 3).map(([agent]) => ({ f, agent })),
    );
    return {
      by_agent: agents.map(({ f, agent }, i) => ({ factory_id: f.id, agent, ...row(i % 5) })),
      by_station: factories.flatMap((f, fi) =>
        ['triage', 'build'].map((station, i) => ({ factory_id: f.id, station, ...row(fi + i) })),
      ),
    };
  }

  /** The spend of one factory over the days, the sum its agents split. */
  function spendOverFactory(f: F, days: number[]): number {
    return cents(days.reduce((n, d) => n + factorySpend(f, d), 0));
  }

  /** The window as CSV (ADR 0020, decision 3): one row per factory, agent and model, each agent's cost split by the models' shares. */
  function csvOf(org: string, window: SpendWindow): string {
    const names = new Map(options.factoriesOf(org).map((f) => [f.id, f.name]));
    const rows = build(org, window).by_agent.flatMap((a) =>
      models.map(([model, share]) => [
        names.get(a.factory_id) ?? a.factory_id,
        a.agent,
        model,
        cents(a.usd * share).toFixed(2),
      ]),
    );
    return `${['factory,agent,model,cost_usd', ...rows.map((row) => row.join(','))].join('\n')}\n`;
  }

  async function handle(
    org: { id: string },
    rest: string,
    method: string,
    url: URL,
    res: ServerResponse,
  ): Promise<boolean> {
    const exporting = rest === '/spend/export';
    if ((rest !== '/spend' && !exporting) || method !== 'GET') return false;
    const window = url.searchParams.get('window') ?? 'week';
    if (!windows.includes(window as SpendWindow)) {
      res.statusCode = 400;
      res.setHeader('Content-Type', 'application/problem+json');
      res.end(
        JSON.stringify({ title: 'Bad Request', status: 400, detail: `window "${window}" is not week, month or 90d` }),
      );
      return true;
    }
    if (exporting) {
      const day = new Date(options.clock()).toISOString().slice(0, 10);
      res.statusCode = 200;
      res.setHeader('Content-Type', 'text/csv; charset=utf-8');
      res.setHeader('Content-Disposition', `attachment; filename="spend-${day}.csv"`);
      res.end(csvOf(org.id, window as SpendWindow));
      return true;
    }
    res.statusCode = 200;
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify(build(org.id, window as SpendWindow)));
    return true;
  }

  return { handle };
}
