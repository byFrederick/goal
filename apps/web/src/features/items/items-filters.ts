// The Items list's saved views, filters and selection (MASTER.md "Items"). The server filters by
// state; the views and the Type, Lane and Assembly filters run in the browser over the items the
// list loaded, and every choice lives in the URL, so a linked view shows the same rows.
import type { ItemCard, ItemState } from '../../api/types';

/** A saved view: Open is the default (no view and no state), the others are a state or this view. */
export type ItemsView = 'expedite' | 'done-week';

export interface ItemsFilters {
  view?: ItemsView | undefined;
  type?: string | undefined;
  lane?: string | undefined;
  assembly?: string | undefined;
}

const dayMs = 86_400_000;

/** An assembly's own card (it has parts), not an item a person moves. */
export const isAssemblyCard = (item: Pick<ItemCard, 'parts'>): boolean => Boolean(item.parts);

/** Open: neither done nor cancelled. */
export const isOpen = (item: Pick<ItemCard, 'state'>): boolean => item.state !== 'done' && item.state !== 'cancelled';

/**
 * Done this week: done, and last updated in the seven days to `now`. The list carries no done time,
 * so the last update stands for it.
 */
export function isDoneThisWeek(item: Pick<ItemCard, 'state' | 'updated_at'>, now: number): boolean {
  return item.state === 'done' && Date.parse(item.updated_at) >= now - 7 * dayMs;
}

/** The counts the tabs and the line under the heading show, from the items the list loaded. */
export interface ItemsCounts {
  open: number;
  waiting: number;
  expedite: number;
  doneThisWeek: number;
}

export function countsOf(items: ItemCard[], now: number): ItemsCounts {
  const work = items.filter((it) => !isAssemblyCard(it));
  return {
    open: work.filter(isOpen).length,
    waiting: work.filter((it) => it.state === 'waiting_human').length,
    expedite: work.filter((it) => isOpen(it) && it.class_of_service === 'expedite').length,
    doneThisWeek: work.filter((it) => isDoneThisWeek(it, now)).length,
  };
}

/**
 * The items that pass the view and the Type, Lane and Assembly filters. Assembly cards pass through.
 * The Done this week view keeps the done items of the last seven days, the same rule its count uses.
 */
export function applyFilters(items: ItemCard[], filters: ItemsFilters, now: number = Date.now()): ItemCard[] {
  return items.filter((it) => {
    if (isAssemblyCard(it)) return true;
    if (filters.view === 'expedite' && it.class_of_service !== 'expedite') return false;
    if (filters.view === 'done-week' && !isDoneThisWeek(it, now)) return false;
    if (filters.type && it.type !== filters.type) return false;
    if (filters.lane && it.lane !== filters.lane) return false;
    if (filters.assembly && it.assembly?.id !== filters.assembly) return false;
    return true;
  });
}

/** The values a Type, Lane or Assembly menu offers: the ones the loaded items have, in order. */
export function optionsOf(items: ItemCard[], pick: (it: ItemCard) => string | null | undefined): string[] {
  const seen = new Set<string>();
  for (const it of items) {
    const value = pick(it);
    if (value) seen.add(value);
  }
  return [...seen].sort((a, b) => a.localeCompare(b));
}

/** The selection, which the URL keeps as a comma-separated list of item ids. */
export const selectedIds = (sel: string | undefined): string[] => (sel ? sel.split(',').filter(Boolean) : []);

export const selectionParam = (ids: readonly string[]): string | undefined => (ids.length ? ids.join(',') : undefined);

/** The State filter's choices, in the order the menu shows them (no state means every state). */
export const stateChoices: { state?: ItemState; label: string }[] = [
  { label: 'Any state' },
  { state: 'waiting_human', label: 'Waiting on you' },
  { state: 'in_progress', label: 'In progress' },
  { state: 'blocked', label: 'Blocked' },
  { state: 'done', label: 'Done' },
];
