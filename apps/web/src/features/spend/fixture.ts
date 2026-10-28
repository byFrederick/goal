// The spend answer the tests read: the acme organization as the screen is drawn (Software, SRE and
// Support, one of them stopped, one connection used by all three). Used by tests only.
import type { OrgSpend, SpendWindow } from '../../api/types';

const software = { id: 'f-software', name: 'software', title: 'Software' };
const sre = { id: 'f-sre', name: 'sre', title: 'SRE' };
const support = { id: 'f-support', name: 'support', title: 'Support' };
const claude = { name: 'claude', kind: 'claude' };

const week = ['2026-10-20', '2026-10-21', '2026-10-22', '2026-10-23', '2026-10-24', '2026-10-25', '2026-10-26'];

/** Days of the window: each day's total and its split by factory. */
const days = week.map((day, i) => {
  const s = [70, 62, 48, 80, 60, 25, 0][i] ?? 0;
  const r = [9, 8, 12, 10, 7, 6, 0][i] ?? 0;
  const p = [4, 5, 4, 6, 3, 2, 0][i] ?? 0;
  return {
    day,
    usd: s + r + p,
    by_factory: [
      { factory_id: software.id, usd: s },
      { factory_id: sre.id, usd: r },
      { factory_id: support.id, usd: p },
    ],
  };
});

/** The acme answer for the week, complete. Override any field for a state a test needs. */
export function spendFixture(patch: Partial<OrgSpend> = {}): OrgSpend {
  return {
    window: 'week',
    since: '2026-10-20T00:00:00Z',
    until: '2026-10-27T09:00:00Z',
    complete: true,
    total_usd: 512,
    this_month_usd: 1940,
    last_month_usd: 2310,
    days,
    by_agent: [
      { factory_id: software.id, agent: 'backend-engineer', usd: 168 },
      { factory_id: software.id, agent: 'reviewer', usd: 94 },
      { factory_id: sre.id, agent: 'infra-engineer', usd: 64 },
    ],
    by_model: [
      { model: 'claude-opus-5-5', usd: 398 },
      { model: 'claude-haiku-5-5', usd: 47 },
    ],
    merged: 150,
    cost_per_merged_usd: 3.4,
    weekly: [4.9, 4.6, 4.5, 4.2, 4.0, 3.9, 3.7, 3.4].map((cost, i) => ({
      week_start: `2026-09-${String(1 + i * 7).padStart(2, '0')}`,
      spend_usd: 500,
      merged: 100,
      cost_per_merged_usd: cost,
    })),
    rework_usd: 61,
    rework_share: 0.12,
    budgets: [
      {
        factory: software,
        daily_usd: 150,
        weekly_usd: 800,
        today_usd: 96,
        week_usd: 412,
        state: 'running',
        reason: null,
        model_connections: [claude],
      },
      {
        factory: sre,
        daily_usd: 40,
        weekly_usd: 250,
        today_usd: 0,
        week_usd: 71,
        state: 'stopped',
        reason: 'Andon pulled, no runs today',
        model_connections: [claude],
      },
      {
        factory: support,
        daily_usd: 30,
        weekly_usd: 350,
        today_usd: 6.3,
        week_usd: 29,
        state: 'running',
        reason: null,
        model_connections: [claude],
      },
    ],
    ...patch,
  };
}

/** A window's answer: the fixture, labelled with the window asked for. */
export const answerFor = (window: SpendWindow): OrgSpend => spendFixture({ window });
