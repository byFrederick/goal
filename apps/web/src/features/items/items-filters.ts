// The Items list's saved views, filters and selection (MASTER.md "Items"). The server filters by
// state; the views and the Type, Lane and Assembly filters run in the browser over the items the
// list loaded, and every choice lives in the URL, so a linked view shows the same rows.
import type { ItemCard, ItemState, SavedViewFilters } from '../../api/types';
import { stateNames } from '../../shared/item-state';

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

/** The URL keys a saved view keeps: the page's filters, never the selection (a selection is not a filter). */
export const savedViewKeys = ['state', 'show', 'view', 'type', 'lane', 'assembly'] as const;
export type SavedViewKey = (typeof savedViewKeys)[number];

/** The page's search as a saved view holds it: each key with its one value, as the URL has it. */
export type FilterSearch = { [K in SavedViewKey]?: string | undefined };

/** A saved view's filters from the page's search: the keys it keeps, each with its value, the empty ones left out. */
export function filtersOf(search: FilterSearch): SavedViewFilters {
  const filters: SavedViewFilters = {};
  for (const key of savedViewKeys) {
    const value = search[key];
    if (value) filters[key] = [value];
  }
  return filters;
}

/** The Items page's search keys a saved view opens with; the route's validateSearch reads the same keys. */
export interface SavedViewSearch {
  state?: ItemState | undefined;
  show?: 'flat' | undefined;
  view?: ItemsView | undefined;
  type?: string | undefined;
  lane?: string | undefined;
  assembly?: string | undefined;
}

const isItemState = (v: unknown): v is ItemState => typeof v === 'string' && v in stateNames;

/**
 * The search a saved view opens: its first value of each key, and only the values the page reads, as
 * the route's validateSearch keeps them (an unknown key or value is dropped, so the view opens with it gone).
 */
export function searchOf(filters: SavedViewFilters): SavedViewSearch {
  const first = (key: SavedViewKey) => filters[key]?.[0];
  const state = first('state');
  const rawView = first('view');
  const view = rawView === 'expedite' || rawView === 'done-week' ? rawView : undefined;
  const type = first('type');
  const lane = first('lane');
  const assembly = first('assembly');
  return {
    ...(isItemState(state) ? { state } : {}),
    ...(first('show') === 'flat' ? { show: 'flat' as const } : {}),
    ...(view ? { view } : {}),
    ...(type ? { type } : {}),
    ...(lane ? { lane } : {}),
    ...(assembly ? { assembly } : {}),
  };
}

/** The same key for two filter sets: the same keys in any order, each with the same values in order. */
export const filtersKey = (filters: SavedViewFilters): string =>
  JSON.stringify(
    Object.keys(filters)
      .sort()
      .map((key) => [key, filters[key]]),
  );
