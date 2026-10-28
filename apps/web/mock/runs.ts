// GET /v1/orgs/{org}/runs for the mock, as goald answers it: the organization's runs across its
// factories in one window, newest first, one page at a time, with the window's summary. The filters
// are factory, status, since and until (the UTC day when absent), q and the page's limit and cursor;
// the summary ignores status and q, as the contract says.
import type { OrgRun, OrgRunList, RunStatus, RunSummary } from '@goal/api-client';
import type { MockState } from './fixtures.ts';

export type OrgRunsError = { status: number; title: string; detail: string };
export type OrgRunsResult = { body: OrgRunList } | OrgRunsError;

const statuses: readonly RunStatus[] = ['running', 'succeeded', 'failed', 'cancelled'];
const dayMs = 86_400_000;

const cents = (usd: number) => Math.round(usd * 100) / 100;

const bad = (detail: string): OrgRunsResult => ({ status: 400, title: 'Bad Request', detail });

/** Parses an ISO time from a query value; undefined when it is not one. */
function instant(value: string | null): number | null | undefined {
  if (value === null) return undefined;
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? null : ms;
}

/** The item a q names: "147" or "#147" is any factory's item 147, "goal#147" is that factory's. Else null. */
function itemNamed(q: string): { factory?: string | undefined; number: number } | null {
  const bare = /^#?(\d+)$/.exec(q);
  if (bare) return { number: Number(bare[1]) };
  const named = /^([a-z][a-z0-9-]*)#(\d+)$/i.exec(q);
  return named ? { factory: named[1]?.toLowerCase(), number: Number(named[2]) } : null;
}

/** The CSV export of the list (ADR 0020, decision 3): every run the list would list, over its pages, one row each. */
export function runsCsvOf(state: MockState, org: string, params: URLSearchParams): { csv: string } | OrgRunsError {
  const next = new URLSearchParams(params);
  next.delete('cursor');
  next.set('limit', '200');
  const runs: OrgRun[] = [];
  for (;;) {
    const result = orgRunsOf(state, org, next);
    if (!('body' in result)) return result;
    runs.push(...result.body.runs);
    if (result.body.next_cursor === null) break;
    next.set('cursor', result.body.next_cursor);
  }
  const cell = (value: string | number | null) => {
    const text = value === null ? '' : String(value);
    return /[",\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
  };
  const header = 'id,factory,item_ref,item_title,station,agent,model,status,started_at,ended_at,turns,cost_usd';
  const lines = runs.map((run) =>
    [
      run.id,
      run.factory.name,
      run.item.ref,
      run.item.title,
      run.station,
      run.agent,
      run.model,
      run.status,
      run.started_at,
      run.ended_at,
      run.turns,
      run.cost_usd.toFixed(2),
    ]
      .map(cell)
      .join(','),
  );
  return { csv: `${[header, ...lines].join('\n')}\n` };
}

export function orgRunsOf(state: MockState, org: string, params: URLSearchParams): OrgRunsResult {
  const factories = state.factories.filter((f) => f.org_id === org);
  const key = params.get('factory');
  const only = key ? factories.find((f) => f.id === key || f.name === key) : undefined;
  if (key && !only) return { status: 404, title: 'Factory not found', detail: key };
  const inOrg = only ? [only] : factories;

  const now = Date.now();
  const today = Math.floor(now / dayMs) * dayMs;
  const since = instant(params.get('since')) ?? today;
  const until = instant(params.get('until')) ?? today + dayMs;
  if (since === null || until === null) return bad('since and until must be times');
  if (since >= until) return bad('since must be before until');

  const status = params.get('status');
  if (status !== null && !statuses.includes(status as RunStatus)) return bad(`status "${status}" is not a run status`);

  const limitParam = params.get('limit');
  const limit = limitParam === null ? 50 : Number(limitParam);
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) return bad('limit must be in [1, 200]');

  const cursorParam = params.get('cursor');
  const offset = cursorParam === null ? 0 : Number(cursorParam);
  if (!Number.isInteger(offset) || offset < 0) return bad('cursor is not one of the cursors this API gave');

  const q = (params.get('q') ?? '').trim().toLowerCase();
  if (q.length > 200) return bad('q must be at most 200 characters');

  // Every run in the window of the factories, with the factory and the item it worked.
  const windowed: OrgRun[] = [];
  for (const run of state.runs) {
    const item = state.items.find((it) => it.id === run.item_id);
    const factory = item && inOrg.find((f) => f.name === item.factory);
    if (!item || !factory) continue;
    const started = Date.parse(run.started_at);
    if (started < since || started >= until) continue;
    windowed.push({
      ...run,
      factory: { id: factory.id, name: factory.name, title: factory.title ?? factory.name },
      item: { id: item.id, ref: `${factory.name}#${item.number}`, title: item.title },
    });
  }
  windowed.sort(
    (a, b) => Date.parse(b.started_at) - Date.parse(a.started_at) || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0),
  );

  const summary: RunSummary = {
    total: windowed.length,
    running: windowed.filter((r) => r.status === 'running').length,
    failed: windowed.filter((r) => r.status === 'failed').length,
    finished: windowed.filter((r) => r.status === 'succeeded' || r.status === 'cancelled').length,
    spend_usd: cents(windowed.reduce((n, r) => n + r.cost_usd, 0)),
    spend_by_factory: spendByFactory(windowed),
  };

  const named = q ? itemNamed(q) : null;
  const matching = windowed.filter((r) => {
    if (status !== null && r.status !== status) return false;
    if (!q) return true;
    if (named && r.item.ref === `${named.factory ?? r.factory.name}#${named.number}`) return true;
    return [r.item.title, r.agent, r.model].some((text) => text.toLowerCase().includes(q));
  });

  const runs = matching.slice(offset, offset + limit);
  const next = offset + limit < matching.length ? String(offset + limit) : null;
  return { body: { runs, next_cursor: next, summary } };
}

/** The factories that ran anything in the window, the highest spend first, then by name. */
function spendByFactory(runs: readonly OrgRun[]): RunSummary['spend_by_factory'] {
  const byId = new Map<string, { id: string; name: string; spend_usd: number }>();
  for (const r of runs) {
    const entry = byId.get(r.factory.id) ?? { id: r.factory.id, name: r.factory.name, spend_usd: 0 };
    entry.spend_usd += r.cost_usd;
    byId.set(r.factory.id, entry);
  }
  return [...byId.values()]
    .map((f) => ({ ...f, spend_usd: cents(f.spend_usd) }))
    .sort((a, b) => b.spend_usd - a.spend_usd || a.name.localeCompare(b.name));
}
