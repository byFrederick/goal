import { createFileRoute } from '@tanstack/react-router';
import { boardQuery, ensureLoaded, inboxQuery, itemsQuery } from '../api/queries';
import type { ItemState } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { ItemsPage } from '../features/items/items-page';
import { stateNames } from '../shared/item-state';

interface ItemsSearch {
  state?: ItemState;
  /** `flat` lists every item as a row; the default groups parts under their assembly. */
  show?: 'flat' | undefined;
}

const isState = (v: unknown): v is ItemState => typeof v === 'string' && v in stateNames;

export const Route = createFileRoute('/o/$org/f/$factory/items/')({
  validateSearch: (search: Record<string, unknown>): ItemsSearch => ({
    ...(isState(search.state) ? { state: search.state } : {}),
    ...(search.show === 'flat' ? { show: 'flat' as const } : {}),
  }),
  loaderDeps: ({ search }) => ({ state: search.state }),
  // The inbox and the board serve the phone's Needs you, above the list. A filter also reads the
  // assemblies, whose progress heads the parts it keeps (the page's listRows).
  loader: ({ context: { queryClient, org, factory }, deps }) =>
    Promise.all([
      ensureLoaded(queryClient, itemsQuery(org.id, factory.id, { state: deps.state, limit: 200 })),
      ...(deps.state
        ? [ensureLoaded(queryClient, itemsQuery(org.id, factory.id, { state: 'assembly', limit: 200 }))]
        : []),
      ensureLoaded(queryClient, inboxQuery(org.id, factory.id)),
      ensureLoaded(queryClient, boardQuery(org.id, factory.id)),
    ]),
  head: ({ match: { context } }) => pageTitle('Items', context.factory.name, context.org.name),
  component: ItemsPage,
});
