import { describe, expect, it } from 'vitest';
import { seed } from './fixtures.ts';
import { teardownOf } from './teardown.ts';

const now = Date.parse('2026-10-27T15:00:00Z');
const state = seed(now);

function teardownFor(number: number) {
  const it = state.items.find((i) => i.number === number && i.factory === 'goal');
  if (!it) throw new Error(`no item ${number}`);
  return teardownOf({
    spans: it.spans,
    runs: state.runs.filter((r) => r.item_id === it.id),
    events: state.events.filter((e) => e.item_id === it.id),
    touches: [],
    done: it.state === 'done',
    now,
    targetSeconds: 14_400,
    budgetUsd: 10,
  });
}

describe('the mock teardown', () => {
  it('breaks down a finished item with a failed check: runs, a loop, merged last', () => {
    const td = teardownFor(4);
    expect(td.open).toBe(false);
    expect(td.runs.map((r) => r.outcome)).toEqual(['finished', 'check_failed', 'finished', 'merged']);
    expect(td.rework.loops).toBe(1);
    expect(td.loops[0]).toMatchObject({ cause: 'check_failed', station: 'build', reason: 'go test -race ./...' });
    expect(td.cost_usd).toBeCloseTo(0.15 + 0.88 + 0.32 + 0.21, 2);
    expect(td.total.total_seconds).toBe(td.total.working_seconds + td.total.waiting_seconds);
    expect(td.target_seconds).toBe(14_400);
  });

  it('answers an item no agent picked up with no runs', () => {
    const td = teardownFor(20);
    expect(td.runs).toEqual([]);
    expect(td.run_total.runs).toBe(0);
    expect(td.open).toBe(true);
  });
});
