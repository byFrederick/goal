// Query keys and options over client.ts. Keys are hierarchical (factory → resource) so the
// stream can invalidate exactly what an event touches.
import { type QueryClient, queryOptions } from '@tanstack/react-query';
import { getBoard, getItem, getMetrics, type ItemFilter, listFactories, listItemEvents, listItems } from './client';
import type { ItemEvent } from './types';

export const keys = {
  factories: ['factories'] as const,
  factory: (factory: string) => ['factory', factory] as const,
  board: (factory: string) => ['factory', factory, 'board'] as const,
  metrics: (factory: string, window: string) => ['factory', factory, 'metrics', window] as const,
  items: (factory: string, filter: ItemFilter) => ['factory', factory, 'items', filter] as const,
  item: (item: string) => ['item', item] as const,
  events: (item: string) => ['item', item, 'events'] as const,
};

export const factoriesQuery = queryOptions({
  queryKey: keys.factories,
  queryFn: ({ signal }) => listFactories(signal),
  staleTime: 60_000,
});

export const boardQuery = (factory: string) =>
  queryOptions({ queryKey: keys.board(factory), queryFn: ({ signal }) => getBoard(factory, signal) });

export const metricsQuery = (factory: string, window = '7d') =>
  queryOptions({
    queryKey: keys.metrics(factory, window),
    queryFn: ({ signal }) => getMetrics(factory, window, signal),
    staleTime: 30_000,
  });

export const itemsQuery = (factory: string, filter: ItemFilter) =>
  queryOptions({ queryKey: keys.items(factory, filter), queryFn: ({ signal }) => listItems(factory, filter, signal) });

export const itemQuery = (item: string) =>
  queryOptions({ queryKey: keys.item(item), queryFn: ({ signal }) => getItem(item, signal) });

const eventPage = 500;

/**
 * An item's whole history. A refetch asks only for events after the last one already cached,
 * so the stream can invalidate it on every tool call without reloading the trace.
 */
export const eventsQuery = (client: QueryClient, item: string) =>
  queryOptions({
    queryKey: keys.events(item),
    queryFn: async ({ signal }) => {
      const cached = client.getQueryData<ItemEvent[]>(keys.events(item)) ?? [];
      const events = [...cached];
      let after = events.at(-1)?.id ?? 0;

      for (;;) {
        const page = await listItemEvents(item, after, eventPage, signal);
        events.push(...page.events);
        if (page.events.length < eventPage || page.next_after === after) break;
        after = page.next_after;
      }

      return events;
    },
  });
