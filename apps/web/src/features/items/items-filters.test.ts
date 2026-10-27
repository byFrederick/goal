import { describe, expect, it } from 'vitest';
import type { ItemCard } from '../../api/types';
import { applyFilters, countsOf, isDoneThisWeek, selectedIds, selectionParam } from './items-filters';

const now = Date.parse('2026-10-27T12:00:00Z');
const card = (over: Partial<ItemCard>): ItemCard =>
  ({
    id: 'x',
    ref: 'sre#1',
    title: 'Item',
    type: 'task',
    lane: undefined,
    state: 'queued',
    class_of_service: 'standard',
    updated_at: '2026-10-27T10:00:00Z',
    ...over,
  }) as ItemCard;

describe('Items counts', () => {
  it('counts open work, waiting, expedite and done this week, leaving assembly cards out', () => {
    const counts = countsOf(
      [
        card({ id: 'a', state: 'queued' }),
        card({ id: 'b', state: 'waiting_human' }),
        card({ id: 'c', state: 'in_progress', class_of_service: 'expedite' }),
        card({ id: 'd', state: 'done', updated_at: '2026-10-27T10:00:00Z' }),
        card({ id: 'e', state: 'done', updated_at: '2026-10-01T10:00:00Z' }),
        card({ id: 'f', state: 'cancelled' }),
        card({
          id: 'asm',
          state: 'assembly',
          parts: { total: 2, merged: 0, in_progress: 0, waiting: 0, stations: [] },
        }),
      ],
      now,
    );
    expect(counts).toEqual({ open: 3, waiting: 1, expedite: 1, doneThisWeek: 1 });
  });

  it('counts a done item as done this week only within seven days', () => {
    expect(isDoneThisWeek(card({ state: 'done', updated_at: '2026-10-21T12:00:00Z' }), now)).toBe(true);
    expect(isDoneThisWeek(card({ state: 'done', updated_at: '2026-10-20T11:59:00Z' }), now)).toBe(false);
  });
});

describe('Items filters', () => {
  const items = [
    card({ id: 'a', type: 'incident', lane: 'backend' }),
    card({ id: 'b', type: 'task', lane: 'frontend', class_of_service: 'expedite' }),
  ];

  it('keeps the items that match the type, the lane and the expedite view', () => {
    expect(applyFilters(items, { type: 'task' }).map((it) => it.id)).toEqual(['b']);
    expect(applyFilters(items, { lane: 'backend' }).map((it) => it.id)).toEqual(['a']);
    expect(applyFilters(items, { view: 'expedite' }, now).map((it) => it.id)).toEqual(['b']);
  });

  it('keeps only the done items updated within seven days for the Done this week view', () => {
    const done = [
      card({ id: 'recent', state: 'done', updated_at: '2026-10-21T12:00:00Z' }),
      card({ id: 'old', state: 'done', updated_at: '2026-10-01T10:00:00Z' }),
      card({ id: 'open', state: 'in_progress', updated_at: '2026-10-26T10:00:00Z' }),
    ];
    expect(applyFilters(done, { view: 'done-week' }, now).map((it) => it.id)).toEqual(['recent']);
  });

  it('lets an assembly card pass through the filters, so its parts can still group under it', () => {
    const assembly = card({
      id: 'asm',
      type: 'feature',
      parts: { total: 1, merged: 0, in_progress: 0, waiting: 0, stations: [] },
    });
    expect(applyFilters([assembly], { type: 'task' }).map((it) => it.id)).toEqual(['asm']);
  });
});

describe('Items selection in the URL', () => {
  it('round-trips the checked ids and drops the param when none are checked', () => {
    expect(selectedIds(selectionParam(['a', 'b']))).toEqual(['a', 'b']);
    expect(selectionParam([])).toBeUndefined();
    expect(selectedIds(undefined)).toEqual([]);
  });
});
