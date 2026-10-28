import { describe, expect, it } from 'vitest';
import { acmeOrg, seed } from './fixtures.ts';
import { orgRunsOf, runsCsvOf } from './runs.ts';

// A window that holds every seeded run: the mock's runs are placed within hours of `now`.
const now = Date.parse('2026-10-27T15:00:00Z');
const state = seed(now);
const window = new URLSearchParams({ since: '2026-10-25T00:00:00Z', until: '2026-10-28T00:00:00Z' });

function runsFor(params: Record<string, string>) {
  const query = { limit: '200', ...params };
  const result = orgRunsOf(state, acmeOrg, new URLSearchParams({ ...Object.fromEntries(window), ...query }));
  if (!('body' in result)) throw new Error(result.detail);
  return result.body;
}

describe('the mock GET /v1/orgs/{org}/runs', () => {
  it('lists the runs newest first, each with its factory and its item', () => {
    const { runs } = runsFor({});
    const starts = runs.map((r) => Date.parse(r.started_at));

    expect(starts).toEqual([...starts].sort((a, b) => b - a));
    expect(runs[0]).toMatchObject({
      factory: { name: expect.any(String) },
      item: { ref: expect.stringMatching(/#\d+$/) },
    });
  });

  it('filters by status, and the summary ignores it', () => {
    const all = runsFor({});
    const failed = runsFor({ status: 'failed' });

    expect(failed.runs.length).toBeGreaterThan(0);
    expect(failed.runs.every((r) => r.status === 'failed')).toBe(true);
    expect(failed.summary).toEqual(all.summary);
    expect(all.summary.failed).toBe(failed.runs.length);
  });

  it('counts finished runs as succeeded and cancelled ones together', () => {
    const { summary, runs } = runsFor({});

    expect(summary.total).toBe(runs.length);
    expect(summary.finished).toBe(runs.filter((r) => r.status === 'succeeded' || r.status === 'cancelled').length);
    expect(summary.running).toBe(runs.filter((r) => r.status === 'running').length);
  });

  it('filters by factory, by name or id, and answers 404 for an unknown factory', () => {
    const sre = runsFor({ factory: 'sre' });

    expect(sre.runs.length).toBeGreaterThan(0);
    expect(sre.runs.every((r) => r.factory.name === 'sre')).toBe(true);
    expect(sre.summary.spend_by_factory.map((f) => f.name)).toEqual(['sre']);

    const unknown = orgRunsOf(state, acmeOrg, new URLSearchParams({ factory: 'nope' }));
    expect(unknown).toMatchObject({ status: 404 });
  });

  it('matches q on the item title, the agent, the model and the item number, case-insensitively', () => {
    expect(runsFor({ q: 'CHECKOUT' }).runs.every((r) => r.item.title.includes('Checkout'))).toBe(true);
    expect(runsFor({ q: 'opus-5-5' }).runs.every((r) => r.model === 'claude-opus-5-5')).toBe(true);

    const byNumber = runsFor({ q: 'goal#147' });
    expect(byNumber.runs.length).toBeGreaterThan(0);
    expect(byNumber.runs.every((r) => r.item.ref === 'goal#147')).toBe(true);
    expect(runsFor({ q: '#147' }).runs.map((r) => r.item.ref)).toEqual(byNumber.runs.map((r) => r.item.ref));
  });

  it('pages with a cursor, and the summary covers every page', () => {
    const first = runsFor({ limit: '2' });
    expect(first.runs).toHaveLength(2);
    expect(first.next_cursor).toBe('2');

    const second = runsFor({ limit: '2', cursor: first.next_cursor ?? '' });
    expect(second.runs[0]?.id).not.toBe(first.runs[1]?.id);
    expect(second.summary).toEqual(first.summary);
  });

  it('refuses a window that is empty or backwards, and an unknown status', () => {
    expect(orgRunsOf(state, acmeOrg, new URLSearchParams({ since: '2026-10-28', until: '2026-10-27' }))).toMatchObject({
      status: 400,
    });
    expect(orgRunsOf(state, acmeOrg, new URLSearchParams({ status: 'merged' }))).toMatchObject({ status: 400 });
  });
});

describe('the mock GET /v1/orgs/{org}/runs/export', () => {
  const csvLines = (params: URLSearchParams) => {
    const result = runsCsvOf(state, acmeOrg, params);
    if (!('csv' in result)) throw new Error(result.detail);
    return result.csv.split('\n').filter((line) => line !== '');
  };

  it('answers the list as CSV: the header, then one row per run the list would list, over every page', () => {
    const lines = csvLines(window);
    const { runs } = runsFor({});

    expect(lines[0]).toBe(
      'id,factory,item_ref,item_title,station,agent,model,status,started_at,ended_at,turns,cost_usd',
    );
    expect(lines).toHaveLength(runs.length + 1);
    expect(lines[1]?.split(',')[0]).toBe(runs[0]?.id);
  });

  it('takes the list filters and ignores the page', () => {
    const failed = csvLines(new URLSearchParams({ ...Object.fromEntries(window), status: 'failed', limit: '1' }));
    const listed = runsFor({ status: 'failed' }).runs;

    expect(failed).toHaveLength(listed.length + 1);
    expect(failed.slice(1).every((line) => line.split(',')[7] === 'failed')).toBe(true);
  });

  it('refuses a window the list refuses, with the same problem', () => {
    const result = runsCsvOf(state, acmeOrg, new URLSearchParams({ status: 'merged' }));
    expect(result).toMatchObject({ status: 400 });
  });
});
