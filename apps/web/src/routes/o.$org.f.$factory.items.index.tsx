import { createFileRoute } from '@tanstack/react-router';
import { boardQuery, ensureLoaded, itemsQuery } from '../api/queries';
import type { ItemState } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { ItemsPage } from '../features/items/items-page';
import { stateNames } from '../shared/item-state';

interface ItemsSearch {
  state?: ItemState | undefined;
  /** `flat` lists every item as a row; the default groups parts under their assembly. */
  show?: 'flat' | undefined;
  /** A saved view: `expedite`. Open, Waiting on you and Done this week are the state filter. */
  view?: 'expedite' | undefined;
  /** The Type, Lane and Assembly filters, by the values the list shows. */
  type?: string | undefined;
  lane?: string | undefined;
  assembly?: string | undefined;
  /** The checked rows, by item id, comma-separated. */
  sel?: string | undefined;
}

const isState = (v: unknown): v is ItemState => typeof v === 'string' && v in stateNames;
const nonEmpty = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);

export const Route = createFileRoute('/o/$org/f/$factory/items/')({
  validateSearch: (search: Record<string, unknown>): ItemsSearch => ({
    ...(isState(search.state) ? { state: search.state } : {}),
    ...(search.show === 'flat' ? { show: 'flat' as const } : {}),
    ...(search.view === 'expedite' ? { view: 'expedite' as const } : {}),
    ...(nonEmpty(search.type) ? { type: nonEmpty(search.type) } : {}),
    ...(nonEmpty(search.lane) ? { lane: nonEmpty(search.lane) } : {}),
    ...(nonEmpty(search.assembly) ? { assembly: nonEmpty(search.assembly) } : {}),
    ...(nonEmpty(search.sel) ? { sel: nonEmpty(search.sel) } : {}),
  }),
  loaderDeps: ({ search }) => ({ state: search.state }),
  // The saved views' counts and the filter menus read every item, so the unfiltered list is always
  // loaded; it holds the assemblies too, whose progress heads the parts a state filter keeps (the
  // page's listRows). The board is loaded for the Move menu's optimistic update.
  loader: ({ context: { queryClient, org, factory }, deps }) =>
    Promise.all([
      ensureLoaded(queryClient, itemsQuery(org.id, factory.id, { limit: 200 })),
      ...(deps.state
        ? [ensureLoaded(queryClient, itemsQuery(org.id, factory.id, { state: deps.state, limit: 200 }))]
        : []),
      ensureLoaded(queryClient, boardQuery(org.id, factory.id)),
    ]),
  head: ({ match: { context } }) => pageTitle('Items', context.factory.name, context.org.name),
  component: ItemsPage,
});
