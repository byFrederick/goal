// The only module that speaks HTTP to goald. Screens call these functions through TanStack Query
// (queries.ts); swapping to the generated @goal/api-client means reimplementing this file on top
// of it with the same exports.
import type {
  Board,
  EventPage,
  Factory,
  FactoryList,
  Item,
  ItemList,
  ItemState,
  MergeMethod,
  Metrics,
  NewItem,
  Problem,
  StreamEvent,
} from './types';

/** goald's origin; empty when the UI is served by goald itself or through the dev proxy. */
export const apiBase: string = import.meta.env.VITE_API_URL ?? '';

export class ApiError extends Error {
  readonly status: number;
  readonly detail: string | undefined;

  constructor(status: number, title: string, detail?: string) {
    super(detail ? `${title}: ${detail}` : title);
    this.name = 'ApiError';
    this.status = status;
    this.detail = detail;
  }
}

async function request<T>(path: string, init?: RequestInit & { json?: unknown }): Promise<T> {
  const { json, ...rest } = init ?? {};
  const headers = new Headers(rest.headers);
  headers.set('Accept', 'application/json');

  if (json !== undefined) {
    headers.set('Content-Type', 'application/json');
  }

  const res = await fetch(`${apiBase}${path}`, {
    ...rest,
    headers,
    ...(json === undefined ? {} : { body: JSON.stringify(json) }),
  });

  if (!res.ok) {
    let problem: Partial<Problem> = {};
    try {
      problem = (await res.json()) as Partial<Problem>;
    } catch {
      // Not problem details: fall back to the status line.
    }

    throw new ApiError(res.status, problem.title ?? res.statusText ?? 'Request failed', problem.detail);
  }

  return (await res.json()) as T;
}

const enc = encodeURIComponent;

export async function listFactories(signal?: AbortSignal): Promise<Factory[]> {
  const body = await request<FactoryList>('/v1/factories', { signal: signal ?? null });
  return body.factories;
}

export function getBoard(factory: string, signal?: AbortSignal): Promise<Board> {
  return request(`/v1/factories/${enc(factory)}/board`, { signal: signal ?? null });
}

export function getMetrics(factory: string, window: string, signal?: AbortSignal): Promise<Metrics> {
  return request(`/v1/factories/${enc(factory)}/metrics?window=${enc(window)}`, { signal: signal ?? null });
}

export interface ItemFilter {
  state?: ItemState | undefined;
  station?: string | undefined;
  /** 1 to 200; goald's default is 50. */
  limit?: number | undefined;
  after?: number | undefined;
}

export function listItems(factory: string, filter: ItemFilter, signal?: AbortSignal): Promise<ItemList> {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(filter)) {
    if (v !== undefined && v !== '') q.set(k, String(v));
  }
  const qs = q.size > 0 ? `?${q}` : '';
  return request(`/v1/factories/${enc(factory)}/items${qs}`, { signal: signal ?? null });
}

/** `item` is an id or a `<factory>#<number>` ref. */
export function getItem(item: string, signal?: AbortSignal): Promise<Item> {
  return request(`/v1/items/${enc(item)}`, { signal: signal ?? null });
}

export function listItemEvents(item: string, after: number, limit: number, signal?: AbortSignal): Promise<EventPage> {
  return request(`/v1/items/${enc(item)}/events?after=${after}&limit=${limit}`, { signal: signal ?? null });
}

export function submitItem(factory: string, item: NewItem): Promise<Item> {
  return request(`/v1/factories/${enc(factory)}/items`, { method: 'POST', json: item });
}

export function approveItem(item: string, by: string): Promise<Item> {
  return request(`/v1/items/${enc(item)}/approve`, { method: 'POST', json: { by } });
}

export function rejectItem(item: string, by: string, reason: string): Promise<Item> {
  return request(`/v1/items/${enc(item)}/reject`, { method: 'POST', json: { by, reason } });
}

export function mergeItem(item: string, by: string, method?: MergeMethod): Promise<Item> {
  return request(`/v1/items/${enc(item)}/merge`, { method: 'POST', json: method ? { by, method } : { by } });
}

export function retryItem(item: string, by: string): Promise<Item> {
  return request(`/v1/items/${enc(item)}/retry`, { method: 'POST', json: { by } });
}

export type StreamStatus = 'connecting' | 'live' | 'reconnecting';

/**
 * Subscribes to a factory's item events (GET /v1/stream). goald does not replay: after a
 * reconnect, and on `event: resync`, events may have been missed, so `onResync` asks the caller to
 * re-read what it shows. EventSource reconnects on its own (goald sends `retry: 3000`);
 * `onStatus` reports the connection so screens can say when they are live. Returns the
 * unsubscribe function.
 */
export function subscribeToFactory(
  factory: string,
  onEvent: (e: StreamEvent) => void,
  onResync: () => void,
  onStatus: (s: StreamStatus) => void,
): () => void {
  const source = new EventSource(`${apiBase}/v1/stream?factory=${enc(factory)}`);
  onStatus('connecting');

  let opened = false;
  source.addEventListener('open', () => {
    if (opened) onResync();
    opened = true;
    onStatus('live');
  });
  source.addEventListener('resync', onResync);
  source.addEventListener('error', () => onStatus('reconnecting'));
  source.addEventListener('item', (msg) => {
    try {
      onEvent(JSON.parse((msg as MessageEvent<string>).data) as StreamEvent);
    } catch {
      // A malformed event is skipped; the next one or a refetch catches up.
    }
  });

  return () => source.close();
}
