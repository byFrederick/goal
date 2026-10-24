// The only module that speaks HTTP to goald, over the generated @goal/api-client. Screens call
// these functions through TanStack Query (queries.ts). Every failure throws ApiError, which
// carries goald's Problem. Every route but the organization list belongs to an organization:
// those functions take its slug or id first (`org`), and their paths are written relative to it
// (orgPath). Decisions carry no actor: goald takes the request's (the local user in phase 2.2).
import {
  ApiError,
  createGoalClient,
  orgPath,
  type StreamEvent,
  type StreamStatus,
  subscribe,
  unwrap,
} from '@goal/api-client';
import type {
  Board,
  EventPage,
  Factory,
  Item,
  ItemList,
  ItemState,
  MergeMethod,
  Metrics,
  NewItem,
  NewOrg,
  Org,
  OrgSummary,
} from './types';

export { ApiError, type StreamStatus };

/** goald's origin; empty when the UI is served by goald itself or through the dev proxy. */
const apiBase: string = import.meta.env.VITE_API_URL ?? '';

const goal = createGoalClient(apiBase, { headers: { Accept: 'application/json' } });

/** Request options openapi-fetch takes beside the parameters; `signal` cancels a query. */
const cancel = (signal: AbortSignal | undefined) => (signal ? { signal } : {});

export async function listOrgs(signal?: AbortSignal): Promise<OrgSummary[]> {
  const body = await unwrap(goal.GET('/v1/orgs', cancel(signal)));
  return body.orgs;
}

export function getOrg(org: string, signal?: AbortSignal): Promise<Org> {
  return unwrap(goal.GET('/v1/orgs/{org}', { params: { path: { org } }, ...cancel(signal) }));
}

export function createOrg(org: NewOrg): Promise<Org> {
  return unwrap(goal.POST('/v1/orgs', { body: org }));
}

export async function listFactories(org: string, signal?: AbortSignal): Promise<Factory[]> {
  const body = await unwrap(goal.GET(orgPath('/factories'), { params: { path: { org } }, ...cancel(signal) }));
  return body.factories;
}

export function getBoard(org: string, factory: string, signal?: AbortSignal): Promise<Board> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/board'), { params: { path: { org, factory } }, ...cancel(signal) }),
  );
}

export function getMetrics(org: string, factory: string, window: string, signal?: AbortSignal): Promise<Metrics> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/metrics'), {
      params: { path: { org, factory }, query: { window } },
      ...cancel(signal),
    }),
  );
}

export interface ItemFilter {
  state?: ItemState | undefined;
  station?: string | undefined;
  /** 1 to 200; goald's default is 50. */
  limit?: number | undefined;
  after?: number | undefined;
}

/** The filter as query parameters: unset and empty values are left out. */
function itemQuery({ state, station, limit, after }: ItemFilter) {
  return {
    ...(state ? { state } : {}),
    ...(station ? { station } : {}),
    ...(limit !== undefined ? { limit } : {}),
    ...(after !== undefined ? { after } : {}),
  };
}

export function listItems(org: string, factory: string, filter: ItemFilter, signal?: AbortSignal): Promise<ItemList> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/items'), {
      params: { path: { org, factory }, query: itemQuery(filter) },
      ...cancel(signal),
    }),
  );
}

/** `item` is an id or a `<factory>#<number>` ref. */
export function getItem(org: string, item: string, signal?: AbortSignal): Promise<Item> {
  return unwrap(goal.GET(orgPath('/items/{item}'), { params: { path: { org, item } }, ...cancel(signal) }));
}

export function listItemEvents(
  org: string,
  item: string,
  after: number,
  limit: number,
  signal?: AbortSignal,
): Promise<EventPage> {
  return unwrap(
    goal.GET(orgPath('/items/{item}/events'), {
      params: { path: { org, item }, query: { after, limit } },
      ...cancel(signal),
    }),
  );
}

export function submitItem(org: string, factory: string, item: NewItem): Promise<Item> {
  return unwrap(goal.POST(orgPath('/factories/{factory}/items'), { params: { path: { org, factory } }, body: item }));
}

export function approveItem(org: string, item: string): Promise<Item> {
  return unwrap(goal.POST(orgPath('/items/{item}/approve'), { params: { path: { org, item } } }));
}

export function rejectItem(org: string, item: string, reason: string): Promise<Item> {
  return unwrap(goal.POST(orgPath('/items/{item}/reject'), { params: { path: { org, item } }, body: { reason } }));
}

export function mergeItem(org: string, item: string, method?: MergeMethod): Promise<Item> {
  return unwrap(
    goal.POST(orgPath('/items/{item}/merge'), { params: { path: { org, item } }, body: method ? { method } : {} }),
  );
}

export function retryItem(org: string, item: string): Promise<Item> {
  return unwrap(goal.POST(orgPath('/items/{item}/retry'), { params: { path: { org, item } } }));
}

/**
 * Subscribes to a factory's item events in an organization. goald does not replay: after a
 * reconnect (not the first open), and on `event: resync`, events may have been missed, so
 * `onResync` asks the caller to re-read what it shows. `onStatus` reports the connection so
 * screens can say when they are live. Returns the unsubscribe function.
 */
export function subscribeToFactory(
  org: string,
  factory: string,
  onEvent: (e: StreamEvent) => void,
  onResync: () => void,
  onStatus: (s: StreamStatus) => void,
): () => void {
  return subscribe(apiBase, { org, factory }, { onItem: onEvent, onResync, onStatus });
}
