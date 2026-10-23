import { createFileRoute } from '@tanstack/react-router';
import { itemsQuery } from '../api/queries';
import type { ItemState } from '../api/types';
import { ItemsPage } from '../features/items/items-page';
import { stateNames } from '../shared/item-state';

interface ItemsSearch {
  state?: ItemState;
}

const isState = (v: unknown): v is ItemState => typeof v === 'string' && v in stateNames;

export const Route = createFileRoute('/f/$factory/items/')({
  validateSearch: (search: Record<string, unknown>): ItemsSearch =>
    isState(search.state) ? { state: search.state } : {},
  loaderDeps: ({ search }) => ({ state: search.state }),
  loader: ({ context: { queryClient, factory }, deps }) =>
    queryClient.ensureQueryData(itemsQuery(factory.name, { state: deps.state, limit: 200 })),
  component: ItemsPage,
});
