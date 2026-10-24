import { createFileRoute } from '@tanstack/react-router';
import { itemsQuery } from '../api/queries';
import type { ItemState } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { ItemsPage } from '../features/items/items-page';
import { stateNames } from '../shared/item-state';

interface ItemsSearch {
  state?: ItemState;
}

const isState = (v: unknown): v is ItemState => typeof v === 'string' && v in stateNames;

export const Route = createFileRoute('/o/$org/f/$factory/items/')({
  validateSearch: (search: Record<string, unknown>): ItemsSearch =>
    isState(search.state) ? { state: search.state } : {},
  loaderDeps: ({ search }) => ({ state: search.state }),
  loader: ({ context: { queryClient, org, factory }, deps }) =>
    queryClient.ensureQueryData(itemsQuery(org.id, factory.id, { state: deps.state, limit: 200 })),
  head: ({ match: { context } }) => pageTitle('Items', context.factory.name, context.org.name),
  component: ItemsPage,
});
