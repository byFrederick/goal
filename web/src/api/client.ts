// The module that speaks HTTP to goald, over the generated @goal/api-client, with definitions.ts
// (the definitions routes) over the same client. Screens call these functions through TanStack
// Query (queries.ts). Every failure throws ApiError, which carries goald's Problem. Every route but the organization list belongs to an organization:
// those functions take its slug or id first (`org`), and their paths are written relative to it
// (orgPath); factories are named by id. Decisions carry no actor: goald takes the request's (the local user in phase 2.2).
import {
  ApiError,
  createGoalClient,
  orgPath,
  type StreamHandlers,
  type StreamStatus,
  subscribe,
  unwrap,
} from '@goal/api-client';
import type {
  AuditFilter,
  AuditPage,
  Board,
  EventPage,
  Factory,
  FactoryOverviewList,
  Inbox,
  Item,
  ItemList,
  ItemState,
  MergeMethod,
  Metrics,
  NewItem,
  NewOrg,
  NoteReceipt,
  Org,
  OrgAttention,
  OrgDeleted,
  OrgSettings,
  OrgSettingsPatch,
  OrgSummary,
  RunList,
} from './types';

export { ApiError, type StreamStatus };

/** goald's origin; empty when the UI is served by goald itself or through the dev proxy. */
const apiBase: string = import.meta.env.VITE_API_URL ?? '';

/**
 * The one client of goald. The other request modules of api/ (definitions.ts) use it; nothing
 * outside api/ does.
 */
export const goal = createGoalClient(apiBase, { headers: { Accept: 'application/json' } });

/** Request options openapi-fetch takes beside the parameters; `signal` cancels a query. */
export const cancel = (signal: AbortSignal | undefined) => (signal ? { signal } : {});

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

/** What each factory needs from people now: the rail's badges and dots. */
export function getAttention(org: string, signal?: AbortSignal): Promise<OrgAttention> {
  return unwrap(goal.GET(orgPath('/attention'), { params: { path: { org } }, ...cancel(signal) }));
}

/** Every factory summarized over `window` (Nd, 1d to 90d) for the Factories home. */
export function getFactoryOverview(org: string, window: string, signal?: AbortSignal): Promise<FactoryOverviewList> {
  return unwrap(goal.GET(orgPath('/overview'), { params: { path: { org }, query: { window } }, ...cancel(signal) }));
}

export interface InboxFilter {
  /** Only this factory's entries (id or name). */
  factory?: string | undefined;
  /** 1 to 500; goald's default is 20. */
  limit?: number | undefined;
}

/** What waits for a person across the organization, or in one factory. */
export function getInbox(org: string, { factory, limit }: InboxFilter = {}, signal?: AbortSignal): Promise<Inbox> {
  return unwrap(
    goal.GET(orgPath('/inbox'), {
      params: {
        path: { org },
        query: { ...(factory ? { factory } : {}), ...(limit !== undefined ? { limit } : {}) },
      },
      ...cancel(signal),
    }),
  );
}

export function getOrgSettings(org: string, signal?: AbortSignal): Promise<OrgSettings> {
  return unwrap(goal.GET(orgPath('/settings'), { params: { path: { org } }, ...cancel(signal) }));
}

/** Changes only the fields patch names; `slots.max: null` clears a cap. Answers the settings after it. */
export function patchOrgSettings(org: string, patch: OrgSettingsPatch): Promise<OrgSettings> {
  return unwrap(goal.PATCH(orgPath('/settings'), { params: { path: { org } }, body: patch }));
}

/** Deletes the organization; `confirm` must be its name. It 404s at once and is purged after purge_after. */
export function deleteOrg(org: string, confirm: string): Promise<OrgDeleted> {
  return unwrap(goal.DELETE('/v1/orgs/{org}', { params: { path: { org } }, body: { confirm } }));
}

/** One page of the organization's audit log, newest first; pass next_before as `before` for the next. */
export function listAudit(org: string, filter: AuditFilter = {}, signal?: AbortSignal): Promise<AuditPage> {
  return unwrap(goal.GET(orgPath('/audit'), { params: { path: { org }, query: filter }, ...cancel(signal) }));
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

/** An item's agent runs, oldest first (at most 200; `truncated` says there are more). */
export function listItemRuns(org: string, item: string, signal?: AbortSignal): Promise<RunList> {
  return unwrap(goal.GET(orgPath('/items/{item}/runs'), { params: { path: { org, item } }, ...cancel(signal) }));
}

/** Leaves the local user's note for the agents working an item; the receipt says when one reads it. */
export function noteItem(org: string, item: string, text: string): Promise<NoteReceipt> {
  return unwrap(goal.POST(orgPath('/items/{item}/notes'), { params: { path: { org, item } }, body: { text } }));
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
 * Subscribes to an organization's events (every factory's). goald does not replay: after a
 * reconnect (not the first open), and on `event: resync`, events may have been missed, so
 * `onResync` asks the caller to re-read what it shows. `onStatus` reports the connection so the
 * shell can say when it is reconnecting; `events` names further event kinds to pass to `onEvent`.
 * Returns the unsubscribe function.
 */
export function subscribeToOrg(org: string, handlers: StreamHandlers): () => void {
  return subscribe(apiBase, { org }, handlers);
}
