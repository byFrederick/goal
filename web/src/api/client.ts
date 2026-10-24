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
import { sessionHandlers, withSession } from './session';
import type {
  AgentStats,
  AndonPull,
  AuthConfig,
  Board,
  ConstraintReport,
  DeviceRequest,
  EventPage,
  Factory,
  FactoryOverviewList,
  Inbox,
  Item,
  ItemList,
  ItemState,
  LeadTimeBridge,
  Me,
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
  WeeklyThroughput,
} from './types';

export { ApiError, type StreamStatus };

/** goald's origin; empty when the UI is served by goald itself or through the dev proxy. */
const apiBase: string = import.meta.env.VITE_API_URL ?? '';

/**
 * The one client of goald. The other request modules of api/ (definitions.ts) use it; nothing
 * outside api/ does.
 */
export const goal = createGoalClient(apiBase, {
  headers: { Accept: 'application/json' },
  // 401 opens the sign-in page; 403 reauth-required asks the person to confirm it's them, then
  // sends the request once more (session.ts).
  fetch: withSession((request) => fetch(request), sessionHandlers),
});

/** Request options openapi-fetch takes beside the parameters; `signal` cancels a query. */
export const cancel = (signal: AbortSignal | undefined) => (signal ? { signal } : {});

/** How this install signs people in (session, with its methods) or that sign-in is off (none). */
export function getAuthConfig(signal?: AbortSignal): Promise<AuthConfig> {
  return unwrap(goal.GET('/v1/auth/config', cancel(signal)));
}

/**
 * A pending CLI sign-in, by the code the person typed (Device-Approve): the client, when and where
 * it asked from. 404 device-code-not-found when the code is not pending.
 */
export function getDeviceRequest(userCode: string): Promise<DeviceRequest> {
  return unwrap(goal.GET('/v1/auth/device/{user_code}', { params: { path: { user_code: userCode } } }));
}

/** Who the requests act for: the signed-in person, or the local user when sign-in is off. */
export function getMe(signal?: AbortSignal): Promise<Me> {
  return unwrap(goal.GET('/v1/me', cancel(signal)));
}

export async function listOrgs(signal?: AbortSignal): Promise<OrgSummary[]> {
  const body = await unwrap(goal.GET('/v1/orgs', cancel(signal)));
  return body.orgs;
}

export function createOrg(org: NewOrg): Promise<Org> {
  return unwrap(goal.POST('/v1/orgs', { body: org }));
}

/** One organization with the caller's role and what they may do in it (`can`). */
export function getOrg(org: string, signal?: AbortSignal): Promise<Org> {
  return unwrap(goal.GET('/v1/orgs/{org}', { params: { path: { org } }, ...cancel(signal) }));
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

export async function listFactories(org: string, signal?: AbortSignal): Promise<Factory[]> {
  const body = await unwrap(goal.GET(orgPath('/factories'), { params: { path: { org } }, ...cancel(signal) }));
  return body.factories;
}

export function getBoard(org: string, factory: string, signal?: AbortSignal): Promise<Board> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/board'), { params: { path: { org, factory } }, ...cancel(signal) }),
  );
}

/**
 * A range of UTC days and a work type, as the Floor's URL holds them: absent `to` is today,
 * absent `from` six days before `to`, absent `type` every type.
 */
export interface FlowRange {
  from?: string | undefined;
  to?: string | undefined;
  type?: string | undefined;
}

/** A range's query parameters: the ones it sets. */
function rangeQuery({ from, to, type }: FlowRange): { from?: string; to?: string; type?: string } {
  return { ...(from ? { from } : {}), ...(to ? { to } : {}), ...(type ? { type } : {}) };
}

/** The flow over a range, with the range of the same length before it. */
export function getMetrics(org: string, factory: string, range: FlowRange, signal?: AbortSignal): Promise<Metrics> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/metrics'), {
      params: { path: { org, factory }, query: { ...rangeQuery(range), compare: 'previous' } },
      ...cancel(signal),
    }),
  );
}

/** The constraint over a range, with the top candidates. */
export function getConstraint(
  org: string,
  factory: string,
  { from, to }: Pick<FlowRange, 'from' | 'to'>,
  signal?: AbortSignal,
): Promise<ConstraintReport> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/constraint'), {
      params: { path: { org, factory }, query: rangeQuery({ from, to }) },
      ...cancel(signal),
    }),
  );
}

/** Why the p85 lead time over a range is over its target: the lead-time bridge. */
export function getLeadTime(
  org: string,
  factory: string,
  range: FlowRange,
  signal?: AbortSignal,
): Promise<LeadTimeBridge> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/metrics/lead-time'), {
      params: { path: { org, factory }, query: rangeQuery(range) },
      ...cancel(signal),
    }),
  );
}

/** Items finished in each of the `weeks` weeks ending on `to` (today when absent). */
export function getWeekly(
  org: string,
  factory: string,
  { to, type }: Pick<FlowRange, 'to' | 'type'>,
  signal?: AbortSignal,
): Promise<WeeklyThroughput> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/metrics/weekly'), {
      params: { path: { org, factory }, query: rangeQuery({ to, type }) },
      ...cancel(signal),
    }),
  );
}

/** Pulls the andon cord: the line releases and claims nothing until it is cleared. */
export function pullAndon(org: string, factory: string, reason: string): Promise<AndonPull> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/andon'), { params: { path: { org, factory } }, body: { reason } }),
  );
}

/** Clears the andon cord with an optional note: the line runs again. */
export function clearAndon(org: string, factory: string, note?: string): Promise<AndonPull> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/andon/clear'), {
      params: { path: { org, factory } },
      body: note ? { note } : {},
    }),
  );
}

/** One agent's work over a range (the last 7 days when it sets none): E12's agent statistics. */
export function getAgentStats(
  org: string,
  factory: string,
  agent: string,
  { from, to }: Pick<FlowRange, 'from' | 'to'> = {},
  signal?: AbortSignal,
): Promise<AgentStats> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/agents/{agent}/stats'), {
      params: { path: { org, factory, agent }, query: rangeQuery({ from, to }) },
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

/** Puts an item at another station; upstream moves need a reason. A run in progress is stopped. */
export function moveItem(org: string, item: string, to: string, reason?: string): Promise<Item> {
  return unwrap(
    goal.POST(orgPath('/items/{item}/move'), {
      params: { path: { org, item } },
      body: reason ? { to, reason } : { to },
    }),
  );
}

/** Stops an open item where it is (blocked) until a retry. A run in progress is stopped. */
export function pauseItem(org: string, item: string, reason?: string): Promise<Item> {
  return unwrap(
    goal.POST(orgPath('/items/{item}/pause'), { params: { path: { org, item } }, body: reason ? { reason } : {} }),
  );
}

/**
 * Subscribes to an organization's events (every factory's). goald does not replay: after a
 * reconnect (not the first open), and on `event: resync`, events may have been missed, so
 * `onResync` asks the caller to re-read what it shows. `onStatus` reports the connection so the
 * shell can say when it is reconnecting; `onFactory` hears a factory's line change (its andon);
 * `events` names further event kinds to pass to `onEvent`.
 * Returns the unsubscribe function.
 */
export function subscribeToOrg(org: string, handlers: StreamHandlers): () => void {
  return subscribe(apiBase, { org }, handlers);
}
