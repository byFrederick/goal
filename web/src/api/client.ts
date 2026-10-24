// The only module that speaks HTTP to goald, over the generated @goal/api-client. Screens call
// these functions through TanStack Query (queries.ts). Every failure throws ApiError, which
// carries goald's Problem. Routes are written relative to the organization (orgPath), so their
// move under /v1/orgs/{org} happens in @goal/api-client's routes.ts.
import {
  ApiError,
  createGoalClient,
  orgPath,
  type StreamEvent,
  type StreamStatus,
  subscribe,
  unwrap,
} from '@goal/api-client';
import type { Board, EventPage, Factory, Item, ItemList, ItemState, MergeMethod, Metrics, NewItem } from './types';

export { ApiError, type StreamStatus };

/** goald's origin; empty when the UI is served by goald itself or through the dev proxy. */
const apiBase: string = import.meta.env.VITE_API_URL ?? '';

const goal = createGoalClient(apiBase, { headers: { Accept: 'application/json' } });

/** Request options openapi-fetch takes beside the parameters; `signal` cancels a query. */
const cancel = (signal: AbortSignal | undefined) => (signal ? { signal } : {});

export async function listFactories(signal?: AbortSignal): Promise<Factory[]> {
  const body = await unwrap(goal.GET(orgPath('/factories'), cancel(signal)));
  return body.factories;
}

export function getBoard(factory: string, signal?: AbortSignal): Promise<Board> {
  return unwrap(goal.GET(orgPath('/factories/{factory}/board'), { params: { path: { factory } }, ...cancel(signal) }));
}

export function getMetrics(factory: string, window: string, signal?: AbortSignal): Promise<Metrics> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/metrics'), {
      params: { path: { factory }, query: { window } },
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

export function listItems(factory: string, filter: ItemFilter, signal?: AbortSignal): Promise<ItemList> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/items'), {
      params: { path: { factory }, query: itemQuery(filter) },
      ...cancel(signal),
    }),
  );
}

/** `item` is an id or a `<factory>#<number>` ref. */
export function getItem(item: string, signal?: AbortSignal): Promise<Item> {
  return unwrap(goal.GET(orgPath('/items/{item}'), { params: { path: { item } }, ...cancel(signal) }));
}

export function listItemEvents(item: string, after: number, limit: number, signal?: AbortSignal): Promise<EventPage> {
  return unwrap(
    goal.GET(orgPath('/items/{item}/events'), {
      params: { path: { item }, query: { after, limit } },
      ...cancel(signal),
    }),
  );
}

export function submitItem(factory: string, item: NewItem): Promise<Item> {
  return unwrap(goal.POST(orgPath('/factories/{factory}/items'), { params: { path: { factory } }, body: item }));
}

export function approveItem(item: string, by: string): Promise<Item> {
  return unwrap(goal.POST(orgPath('/items/{item}/approve'), { params: { path: { item } }, body: { by } }));
}

export function rejectItem(item: string, by: string, reason: string): Promise<Item> {
  return unwrap(goal.POST(orgPath('/items/{item}/reject'), { params: { path: { item } }, body: { by, reason } }));
}

export function mergeItem(item: string, by: string, method?: MergeMethod): Promise<Item> {
  return unwrap(
    goal.POST(orgPath('/items/{item}/merge'), {
      params: { path: { item } },
      body: method ? { by, method } : { by },
    }),
  );
}

export function retryItem(item: string, by: string): Promise<Item> {
  return unwrap(goal.POST(orgPath('/items/{item}/retry'), { params: { path: { item } }, body: { by } }));
}

/**
 * Subscribes to a factory's item events. goald does not replay: after a reconnect (not the first
 * open), and on `event: resync`, events may have been missed, so `onResync` asks the caller to
 * re-read what it shows. `onStatus` reports the connection so screens can say when they are
 * live. Returns the unsubscribe function.
 */
export function subscribeToFactory(
  factory: string,
  onEvent: (e: StreamEvent) => void,
  onResync: () => void,
  onStatus: (s: StreamStatus) => void,
): () => void {
  return subscribe(apiBase, { factory }, { onItem: onEvent, onResync, onStatus });
}
