// Query keys and options over client.ts. Keys are hierarchical, rooted at the organization by its
// id (['org', org, …], C §10.10, so a slug rename does not split the cache), then factory (by id,
// as stream events name it) → resource, so the stream can invalidate exactly what an event
// touches and a resync everything of one organization. Requests name the factory by id too.
import {
  type EnsureQueryDataOptions,
  isCancelledError,
  keepPreviousData,
  type QueryClient,
  type QueryKey,
  queryOptions,
} from '@tanstack/react-query';
import { getBuilderMeta, getSkillFiles, type SkillKey, searchSkills } from './builder';
import {
  ApiError,
  type FlowRange,
  getAgentStats,
  getAttention,
  getAuthConfig,
  getBoard,
  getConstraint,
  getFactoryOverview,
  getInbox,
  getItem,
  getLeadTime,
  getMe,
  getMetrics,
  getOrg,
  getOrgSettings,
  getTouchMetrics,
  getWeekly,
  type ItemFilter,
  listFactories,
  listInvitations,
  listItemEvents,
  listItemRuns,
  listItems,
  listMembers,
  listOrgs,
} from './client';
import {
  getDraft,
  getDraftFile,
  getDraftModel,
  getFactory,
  getTemplate,
  getVersion,
  getVersionFile,
  getVersionModel,
  listDraftFiles,
  listTemplates,
  listVersions,
  validateDraft,
} from './definitions';
import { listApiKeys, listMemberTokens, listPersonalTokens } from './keys';
import type { ItemEvent } from './types';

/**
 * The one way route loaders load (loaders.test.ts keeps it so): an ensureQueryData that survives
 * a fetch it shares being cancelled. When the page being left was the fetch's only observer (the
 * Line section reading the settings Organization settings needs, the wizard's Review reading the
 * board of the factory it just published), unmounting it cancels the request the next page's
 * loader awaits. Loading again starts a fresh one.
 */
export async function ensureLoaded<T, K extends QueryKey>(
  client: QueryClient,
  options: EnsureQueryDataOptions<T, Error, T, K>,
): Promise<T> {
  try {
    return await client.ensureQueryData(options);
  } catch (err) {
    if (isCancelledError(err)) return client.ensureQueryData(options);
    throw err;
  }
}

/**
 * A loader's lookup in a cached list (the organizations, a factory's list) by what the URL names:
 * when the cached list lacks it, the list is read again once before the route says Not found, so
 * an organization or factory made in another tab or with the CLI opens at once.
 */
export async function ensureFound<T, K extends QueryKey, R>(
  client: QueryClient,
  options: EnsureQueryDataOptions<T, Error, T, K>,
  find: (data: T) => R | undefined,
): Promise<R | undefined> {
  const found = find(await ensureLoaded(client, options));
  if (found !== undefined) return found;
  const again = { ...options, staleTime: 0 };
  try {
    return find(await client.fetchQuery(again));
  } catch (err) {
    if (isCancelledError(err)) return find(await client.fetchQuery(again));
    throw err;
  }
}

export const keys = {
  /** How the install signs people in (spec 3 §10.1). */
  authConfig: ['auth', 'config'] as const,
  /** Who the requests act for. */
  me: ['me'] as const,
  orgs: ['orgs'] as const,
  /** The prefix of everything of one organization. */
  org: (org: string) => ['org', org] as const,
  /** The organization with the caller's role and `can` (what screens offer). */
  detail: (org: string) => ['org', org, 'detail'] as const,
  /** What each factory needs from people now (the rail's badges). */
  attention: (org: string) => ['org', org, 'attention'] as const,
  settings: (org: string) => ['org', org, 'settings'] as const,
  apiKeys: (org: string) => ['org', org, 'api-keys'] as const,
  /** Its members (spec 3 §10.1); identity service changes invalidate members and invitations. */
  members: (org: string) => ['org', org, 'members'] as const,
  invitations: (org: string) => ['org', org, 'invitations'] as const,
  /** The caller's own personal tokens in the organization. */
  personalTokens: (org: string) => ['org', org, 'personal-tokens'] as const,
  /** Every member's personal tokens (owners and admins). */
  memberTokens: (org: string) => ['org', org, 'member-tokens'] as const,
  factories: (org: string) => ['org', org, 'factories'] as const,
  /** The prefix of every factory overview window. */
  overviews: (org: string) => ['org', org, 'overview'] as const,
  overview: (org: string, window: string) => ['org', org, 'overview', window] as const,
  /** The prefix of the organization's inbox and every factory's. */
  inboxes: (org: string) => ['org', org, 'inbox'] as const,
  inbox: (org: string, factory?: string) => ['org', org, 'inbox', factory ?? 'all'] as const,
  factory: (org: string, factory: string) => ['org', org, 'factory', factory] as const,
  board: (org: string, factory: string) => ['org', org, 'factory', factory, 'board'] as const,
  /** The mutation key of moving an item of the factory (the Move menu): the stream waits for it. */
  move: (org: string, factory: string) => ['org', org, 'factory', factory, 'move'] as const,
  metrics: (org: string, factory: string, range: FlowRange) =>
    ['org', org, 'factory', factory, 'metrics', range] as const,
  weekly: (org: string, factory: string, to: string | undefined, type: string | undefined) =>
    ['org', org, 'factory', factory, 'weekly', to ?? 'today', type ?? 'all'] as const,
  constraint: (org: string, factory: string, from: string | undefined, to: string | undefined) =>
    ['org', org, 'factory', factory, 'constraint', from ?? 'week', to ?? 'today'] as const,
  leadTime: (org: string, factory: string, range: FlowRange) =>
    ['org', org, 'factory', factory, 'lead-time', range] as const,
  /** Human touches of the items finished over a range (spec 4 §10); item events refresh them. */
  touches: (org: string, factory: string, range: FlowRange) =>
    ['org', org, 'factory', factory, 'touches', range] as const,
  /** One agent's statistics over a range (E12); the stream refreshes the factory's 'agent' keys. */
  agentStats: (org: string, factory: string, agent: string, from: string | undefined, to: string | undefined) =>
    ['org', org, 'factory', factory, 'agent', agent, from ?? 'default', to ?? 'today'] as const,
  items: (org: string, factory: string, filter: ItemFilter) =>
    ['org', org, 'factory', factory, 'items', filter] as const,
  item: (org: string, item: string) => ['org', org, 'item', item] as const,
  events: (org: string, item: string) => ['org', org, 'item', item, 'events'] as const,
  runs: (org: string, item: string) => ['org', org, 'item', item, 'runs'] as const,
};

/** Fixed while the app runs: goald's GOAL_AUTH changes only with a restart. */
export const authConfigQuery = queryOptions({
  queryKey: keys.authConfig,
  queryFn: ({ signal }) => getAuthConfig(signal),
  staleTime: Number.POSITIVE_INFINITY,
});

export const meQuery = queryOptions({
  queryKey: keys.me,
  queryFn: ({ signal }) => getMe(signal),
  staleTime: 60_000,
});

export const orgsQuery = queryOptions({
  queryKey: keys.orgs,
  queryFn: ({ signal }) => listOrgs(signal),
  staleTime: 60_000,
});

/** The organization's `can`: rarely changes; a refusal (403 forbidden) refetches it (main.tsx). */
export const orgQuery = (org: string) =>
  queryOptions({
    queryKey: keys.detail(org),
    queryFn: ({ signal }) => getOrg(org, signal),
    staleTime: 60_000,
  });

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

export const apiKeysQuery = (org: string) =>
  queryOptions({
    queryKey: keys.apiKeys(org),
    queryFn: ({ signal }) => listApiKeys(org, signal),
    staleTime: 30_000,
  });

export const personalTokensQuery = (org: string) =>
  queryOptions({
    queryKey: keys.personalTokens(org),
    queryFn: ({ signal }) => listPersonalTokens(org, signal),
    staleTime: 30_000,
  });

export const memberTokensQuery = (org: string) =>
  queryOptions({
    queryKey: keys.memberTokens(org),
    queryFn: ({ signal }) => listMemberTokens(org, signal),
    staleTime: 30_000,
  });

export const settingsQuery = (org: string) =>
  queryOptions({
    queryKey: keys.settings(org),
    queryFn: ({ signal }) => getOrgSettings(org, signal),
    staleTime: 30_000,
  });

export const membersQuery = (org: string) =>
  queryOptions({
    queryKey: keys.members(org),
    queryFn: ({ signal }) => listMembers(org, signal),
    staleTime: 30_000,
  });

export const invitationsQuery = (org: string) =>
  queryOptions({
    queryKey: keys.invitations(org),
    queryFn: ({ signal }) => listInvitations(org, signal),
    staleTime: 30_000,
  });

export const factoriesQuery = (org: string) =>
  queryOptions({
    queryKey: keys.factories(org),
    queryFn: ({ signal }) => listFactories(org, signal),
    staleTime: 60_000,
  });

export const boardQuery = (org: string, factory: string) =>
  queryOptions({ queryKey: keys.board(org, factory), queryFn: ({ signal }) => getBoard(org, factory, signal) });

/**
 * A factory's flow over a range, with the range before it. Flow statistics refetch at most every
 * 30 s however many events arrive (E §10.3).
 */
export const agentStatsQuery = (
  org: string,
  factory: string,
  agent: string,
  range: Pick<FlowRange, 'from' | 'to'> = {},
) =>
  queryOptions({
    queryKey: keys.agentStats(org, factory, agent, range.from, range.to),
    queryFn: ({ signal }) => getAgentStats(org, factory, agent, range, signal),
    staleTime: 30_000,
    retry: retryUnlessRefused,
  });

export const metricsQuery = (org: string, factory: string, range: FlowRange = {}) =>
  queryOptions({
    queryKey: keys.metrics(org, factory, range),
    queryFn: ({ signal }) => getMetrics(org, factory, range, signal),
    staleTime: 30_000,
  });

/** Throughput by week for the 12 weeks ending on the range's last day. */
export const weeklyQuery = (org: string, factory: string, { to, type }: FlowRange = {}) =>
  queryOptions({
    queryKey: keys.weekly(org, factory, to, type),
    queryFn: ({ signal }) => getWeekly(org, factory, { to, type }, signal),
    staleTime: 30_000,
  });

/** The constraint over a range (the work type does not change it). */
export const constraintQuery = (org: string, factory: string, { from, to }: FlowRange = {}) =>
  queryOptions({
    queryKey: keys.constraint(org, factory, from, to),
    queryFn: ({ signal }) => getConstraint(org, factory, { from, to }, signal),
    staleTime: 30_000,
  });

/** The lead-time bridge over a range. */
/** Human touches over a range: the Floor's panel and, over the last 7 days, the board and the item. */
export const touchesQuery = (org: string, factory: string, range: FlowRange = {}) =>
  queryOptions({
    queryKey: keys.touches(org, factory, range),
    queryFn: ({ signal }) => getTouchMetrics(org, factory, range, signal),
    staleTime: 30_000,
  });

export const leadTimeQuery = (org: string, factory: string, range: FlowRange = {}) =>
  queryOptions({
    queryKey: keys.leadTime(org, factory, range),
    queryFn: ({ signal }) => getLeadTime(org, factory, range, signal),
    staleTime: 30_000,
  });

export const itemsQuery = (org: string, factory: string, filter: ItemFilter) =>
  queryOptions({
    queryKey: keys.items(org, factory, filter),
    queryFn: ({ signal }) => listItems(org, factory, filter, signal),
  });

export const itemQuery = (org: string, item: string) =>
  queryOptions({ queryKey: keys.item(org, item), queryFn: ({ signal }) => getItem(org, item, signal) });

/**
 * The definitions keys (B §10): under the factory's prefix (by id), beside its work keys, which
 * the stream names one by one, so agents working never refetch a draft. A version file is keyed
 * by its sha256 alone: its content never changes.
 */
export const defKeys = {
  factories: (org: string) => keys.factories(org),
  factory: (org: string, f: string) => ['org', org, 'factory', f, 'detail'] as const,
  draft: (org: string, f: string) => ['org', org, 'factory', f, 'draft'] as const,
  draftModel: (org: string, f: string) => ['org', org, 'factory', f, 'draft', 'model'] as const,
  draftFiles: (org: string, f: string) => ['org', org, 'factory', f, 'draft', 'files'] as const,
  draftFile: (org: string, f: string, path: string) => ['org', org, 'factory', f, 'draft', 'file', path] as const,
  validation: (org: string, f: string, revision: number) =>
    ['org', org, 'factory', f, 'draft', 'validation', revision] as const,
  versions: (org: string, f: string) => ['org', org, 'factory', f, 'versions'] as const,
  /** The prefix of every version of the factory and its model. */
  versionsOf: (org: string, f: string) => ['org', org, 'factory', f, 'version'] as const,
  version: (org: string, f: string, v: string) => ['org', org, 'factory', f, 'version', v] as const,
  versionModel: (org: string, f: string, v: string) => ['org', org, 'factory', f, 'version', v, 'model'] as const,
  versionFile: (org: string, sha256: string) => ['org', org, 'blob', sha256] as const,
  /** The prefix of every comparison of the factory. */
  compares: (org: string, f: string) => ['org', org, 'factory', f, 'compare'] as const,
  /** A comparison involving the draft changes with its revision (0 when neither side is the draft). */
  compare: (org: string, f: string, from: string, to: string, revision: number) =>
    ['org', org, 'factory', f, 'compare', from, to, revision] as const,
  templates: (org: string) => ['org', org, 'templates'] as const,
  template: (org: string, name: string, version?: number) =>
    ['org', org, 'template', name, version ?? 'latest'] as const,
};

export const factoryQuery = (org: string, factory: string) =>
  queryOptions({ queryKey: defKeys.factory(org, factory), queryFn: ({ signal }) => getFactory(org, factory, signal) });

/** The open draft; its absence is the ApiError `no-draft` (404), which is not retried. */
export const draftQuery = (org: string, factory: string) =>
  queryOptions({
    queryKey: defKeys.draft(org, factory),
    queryFn: ({ signal }) => getDraft(org, factory, signal),
    retry: retryUnlessRefused,
  });

export const draftModelQuery = (org: string, factory: string) =>
  queryOptions({
    queryKey: defKeys.draftModel(org, factory),
    queryFn: ({ signal }) => getDraftModel(org, factory, signal),
    retry: retryUnlessRefused,
  });

export const draftFilesQuery = (org: string, factory: string) =>
  queryOptions({
    queryKey: defKeys.draftFiles(org, factory),
    queryFn: ({ signal }) => listDraftFiles(org, factory, signal),
  });

/**
 * One file of the draft, with when the read started (`readAt`, epoch ms): the draft session
 * leaves a read that started before its last save for the one the save's refresh starts.
 */
export const draftFileQuery = (org: string, factory: string, path: string) =>
  queryOptions({
    queryKey: defKeys.draftFile(org, factory, path),
    queryFn: async ({ signal }) => {
      const readAt = Date.now();
      return { ...(await getDraftFile(org, factory, path, signal)), readAt };
    },
    retry: retryUnlessRefused,
  });

/** The draft validated at one revision: a result of that revision never goes stale, the next save is a new key. */
export const validationQuery = (org: string, factory: string, revision: number) =>
  queryOptions({
    queryKey: defKeys.validation(org, factory, revision),
    // goald validates the draft as it is when the request arrives (the request names no revision):
    // a later revision's result is cached under its own revision, and is stale under this one.
    queryFn: async ({ signal, client }) => {
      const result = await validateDraft(org, factory, {}, signal);
      if (result.revision !== revision) client.setQueryData(defKeys.validation(org, factory, result.revision), result);
      return result;
    },
    staleTime: (query) => (query.state.data?.revision === revision ? Number.POSITIVE_INFINITY : 0),
  });

export const versionsQuery = (org: string, factory: string) =>
  queryOptions({
    queryKey: defKeys.versions(org, factory),
    queryFn: ({ signal }) => listVersions(org, factory, {}, signal),
  });

export const versionQuery = (org: string, factory: string, version: string) =>
  queryOptions({
    queryKey: defKeys.version(org, factory, version),
    queryFn: ({ signal }) => getVersion(org, factory, version, signal),
  });

export const versionModelQuery = (org: string, factory: string, version: string) =>
  queryOptions({
    queryKey: defKeys.versionModel(org, factory, version),
    queryFn: ({ signal }) => getVersionModel(org, factory, version, signal),
  });

/** A published version's file by its sha256 (from the version's files): its content never changes. */
export const versionFileQuery = (org: string, factory: string, version: string, path: string, sha256: string) =>
  queryOptions({
    queryKey: defKeys.versionFile(org, sha256),
    queryFn: ({ signal }) => getVersionFile(org, factory, version, path, signal),
    staleTime: Number.POSITIVE_INFINITY,
  });

/** The template catalog changes when an operator pushes a version: a few minutes are fresh enough. */
export const templatesQuery = (org: string) =>
  queryOptions({
    queryKey: defKeys.templates(org),
    queryFn: ({ signal }) => listTemplates(org, signal),
    staleTime: 300_000,
  });

/** A template version never changes; the latest (no `version`) may move when a new one is pushed. */
export const templateQuery = (org: string, name: string, version?: number) =>
  queryOptions({
    queryKey: defKeys.template(org, name, version),
    queryFn: ({ signal }) => getTemplate(org, name, version, signal),
    staleTime: version === undefined ? 300_000 : Number.POSITIVE_INFINITY,
  });

/**
 * The builder keys (D §10.4), under the organization: the editor's metadata, catalog searches by
 * their text, and catalog skill folders by key.
 */
export const builderKeys = {
  meta: (org: string) => ['org', org, 'builder', 'meta'] as const,
  skills: (org: string, q: string) => ['org', org, 'builder', 'skills', q] as const,
  skillFiles: (org: string, key: string) => ['org', org, 'builder', 'skill', key] as const,
};

/** Models and capabilities change only when goald restarts. */
export const builderMetaQuery = (org: string) =>
  queryOptions({
    queryKey: builderKeys.meta(org),
    queryFn: ({ signal }) => getBuilderMeta(org, signal),
    staleTime: Number.POSITIVE_INFINITY,
  });

/** The catalog's first page for q; the previous answer stays on screen while the next loads. */
export const skillsQuery = (org: string, q: string) =>
  queryOptions({
    queryKey: builderKeys.skills(org, q),
    queryFn: ({ signal }) => searchSkills(org, { q }, signal),
    staleTime: 300_000,
    placeholderData: keepPreviousData,
  });

/** A catalog skill is pinned at a commit: its files never change. */
export const skillFilesQuery = (org: string, key: SkillKey) =>
  queryOptions({
    queryKey: builderKeys.skillFiles(org, key),
    queryFn: ({ signal }) => getSkillFiles(org, key, signal),
    staleTime: Number.POSITIVE_INFINITY,
    retry: retryUnlessRefused,
  });

/** Retries a failed read, but not goald's refusals (4xx): asking again gets the same answer. */
function retryUnlessRefused(failures: number, error: Error): boolean {
  return failures < 3 && !(error instanceof ApiError && error.status >= 400 && error.status < 500);
}

/** An item's runs: under the item's key, so every event of the item refreshes them. */
export const runsQuery = (org: string, item: string) =>
  queryOptions({ queryKey: keys.runs(org, item), queryFn: ({ signal }) => listItemRuns(org, item, signal) });

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
