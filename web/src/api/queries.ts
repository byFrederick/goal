// Query keys and options over client.ts. Keys are hierarchical, rooted at the organization by its
// id (['org', org, …], C §10.10, so a slug rename does not split the cache), then factory →
// resource, so the stream can invalidate exactly what an event touches and a resync everything
// of one organization.
import { type QueryClient, queryOptions } from '@tanstack/react-query';
import {
  getAttention,
  getBoard,
  getFactoryOverview,
  getInbox,
  getItem,
  getMetrics,
  getOrg,
  type ItemFilter,
  listFactories,
  listItemEvents,
  listItems,
  listOrgs,
} from './client';
import type { ItemEvent } from './types';

export const keys = {
  orgs: ['orgs'] as const,
  /** The prefix of everything of one organization. */
  org: (org: string) => ['org', org] as const,
  orgDetail: (org: string) => ['org', org, 'detail'] as const,
  attention: (org: string) => ['org', org, 'attention'] as const,
  overview: (org: string, window: string) => ['org', org, 'overview', window] as const,
  /** One factory's inbox (by the id or name the query was made with), or the organization's ('all'). */
  inbox: (org: string, factory?: string) => ['org', org, 'inbox', factory ?? 'all'] as const,
  factories: (org: string) => ['org', org, 'factories'] as const,
  factory: (org: string, factory: string) => ['org', org, 'factory', factory] as const,
  board: (org: string, factory: string) => ['org', org, 'factory', factory, 'board'] as const,
  metrics: (org: string, factory: string, window: string) =>
    ['org', org, 'factory', factory, 'metrics', window] as const,
  items: (org: string, factory: string, filter: ItemFilter) =>
    ['org', org, 'factory', factory, 'items', filter] as const,
  item: (org: string, item: string) => ['org', org, 'item', item] as const,
  events: (org: string, item: string) => ['org', org, 'item', item, 'events'] as const,
};

export const orgsQuery = queryOptions({
  queryKey: keys.orgs,
  queryFn: ({ signal }) => listOrgs(signal),
  staleTime: 60_000,
});

export const orgQuery = (org: string) =>
  queryOptions({ queryKey: keys.orgDetail(org), queryFn: ({ signal }) => getOrg(org, signal), staleTime: 60_000 });

/** The rail's badges: cheap enough to refetch on every stream event. */
export const attentionQuery = (org: string) =>
  queryOptions({
    queryKey: keys.attention(org),
    queryFn: ({ signal }) => getAttention(org, signal),
    staleTime: 10_000,
  });

export const overviewQuery = (org: string, window = '7d') =>
  queryOptions({
    queryKey: keys.overview(org, window),
    queryFn: ({ signal }) => getFactoryOverview(org, window, signal),
    staleTime: 30_000,
  });

/** The inbox at goald's default limit; `factory` narrows it to one factory. */
export const inboxQuery = (org: string, factory?: string) =>
  queryOptions({ queryKey: keys.inbox(org, factory), queryFn: ({ signal }) => getInbox(org, { factory }, signal) });

export const factoriesQuery = (org: string) =>
  queryOptions({
    queryKey: keys.factories(org),
    queryFn: ({ signal }) => listFactories(org, signal),
    staleTime: 60_000,
  });

export const boardQuery = (org: string, factory: string) =>
  queryOptions({ queryKey: keys.board(org, factory), queryFn: ({ signal }) => getBoard(org, factory, signal) });

export const metricsQuery = (org: string, factory: string, window = '7d') =>
  queryOptions({
    queryKey: keys.metrics(org, factory, window),
    queryFn: ({ signal }) => getMetrics(org, factory, window, signal),
    staleTime: 30_000,
  });

export const itemsQuery = (org: string, factory: string, filter: ItemFilter) =>
  queryOptions({
    queryKey: keys.items(org, factory, filter),
    queryFn: ({ signal }) => listItems(org, factory, filter, signal),
  });

export const itemQuery = (org: string, item: string) =>
  queryOptions({ queryKey: keys.item(org, item), queryFn: ({ signal }) => getItem(org, item, signal) });

const eventPage = 500;

/**
 * An item's whole history. A refetch asks only for events after the last one already cached,
 * so the stream can invalidate it on every tool call without reloading the trace.
 */
export const eventsQuery = (client: QueryClient, org: string, item: string) =>
  queryOptions({
    queryKey: keys.events(org, item),
    queryFn: async ({ signal }) => {
      const cached = client.getQueryData<ItemEvent[]>(keys.events(org, item)) ?? [];
      const events = [...cached];
      let after = events.at(-1)?.id ?? 0;

      for (;;) {
        const page = await listItemEvents(org, item, after, eventPage, signal);
        events.push(...page.events);
        if (page.events.length < eventPage || page.next_after === after) break;
        after = page.next_after;
      }

      return events;
    },
  });
