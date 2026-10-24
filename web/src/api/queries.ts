// Query keys and options over client.ts. Keys are hierarchical, rooted at the organization by its
// id (['org', org, …], C §10.10, so a slug rename does not split the cache), then factory (by id,
// as stream events name it) → resource, so the stream can invalidate exactly what an event
// touches and a resync everything of one organization. Requests name the factory by id too.
import { keepPreviousData, type QueryClient, queryOptions } from '@tanstack/react-query';
import { getBuilderMeta, getSkillFiles, type SkillKey, searchSkills } from './builder';
import {
  ApiError,
  type FlowRange,
  getAttention,
  getBoard,
  getFactoryOverview,
  getInbox,
  getItem,
  getLeadTime,
  getMetrics,
  getOrg,
  getOrgSettings,
  getWeekly,
  type ItemFilter,
  listAudit,
  listFactories,
  listItemEvents,
  listItemRuns,
  listItems,
  listOrgs,
} from './client';
import {
  compareVersions,
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
import type { AuditFilter, ItemEvent } from './types';

export const keys = {
  orgs: ['orgs'] as const,
  /** The prefix of everything of one organization. */
  org: (org: string) => ['org', org] as const,
  orgDetail: (org: string) => ['org', org, 'detail'] as const,
  /** What each factory needs from people now (the rail's badges). */
  attention: (org: string) => ['org', org, 'attention'] as const,
  settings: (org: string) => ['org', org, 'settings'] as const,
  audit: (org: string, filter: AuditFilter) => ['org', org, 'audit', filter] as const,
  factories: (org: string) => ['org', org, 'factories'] as const,
  /** The prefix of every factory overview window. */
  overviews: (org: string) => ['org', org, 'overview'] as const,
  overview: (org: string, window: string) => ['org', org, 'overview', window] as const,
  /** The prefix of the organization's inbox and every factory's. */
  inboxes: (org: string) => ['org', org, 'inbox'] as const,
  inbox: (org: string, factory?: string) => ['org', org, 'inbox', factory ?? 'all'] as const,
  factory: (org: string, factory: string) => ['org', org, 'factory', factory] as const,
  board: (org: string, factory: string) => ['org', org, 'factory', factory, 'board'] as const,
  metrics: (org: string, factory: string, range: FlowRange) =>
    ['org', org, 'factory', factory, 'metrics', range] as const,
  weekly: (org: string, factory: string, to: string | undefined, type: string | undefined) =>
    ['org', org, 'factory', factory, 'weekly', to ?? 'today', type ?? 'all'] as const,
  leadTime: (org: string, factory: string, range: FlowRange) =>
    ['org', org, 'factory', factory, 'lead-time', range] as const,
  items: (org: string, factory: string, filter: ItemFilter) =>
    ['org', org, 'factory', factory, 'items', filter] as const,
  item: (org: string, item: string) => ['org', org, 'item', item] as const,
  events: (org: string, item: string) => ['org', org, 'item', item, 'events'] as const,
  runs: (org: string, item: string) => ['org', org, 'item', item, 'runs'] as const,
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

export const settingsQuery = (org: string) =>
  queryOptions({
    queryKey: keys.settings(org),
    queryFn: ({ signal }) => getOrgSettings(org, signal),
    staleTime: 30_000,
  });

export const auditQuery = (org: string, filter: AuditFilter = {}) =>
  queryOptions({ queryKey: keys.audit(org, filter), queryFn: ({ signal }) => listAudit(org, filter, signal) });

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

/** The lead-time bridge over a range. */
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
  compare: (org: string, f: string, from: string, to: string) =>
    ['org', org, 'factory', f, 'compare', from, to] as const,
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

export const draftFileQuery = (org: string, factory: string, path: string) =>
  queryOptions({
    queryKey: defKeys.draftFile(org, factory, path),
    queryFn: ({ signal }) => getDraftFile(org, factory, path, signal),
    retry: retryUnlessRefused,
  });

/** The draft validated at one revision: a result never goes stale, the next save is a new key. */
export const validationQuery = (org: string, factory: string, revision: number) =>
  queryOptions({
    queryKey: defKeys.validation(org, factory, revision),
    queryFn: ({ signal }) => validateDraft(org, factory, {}, signal),
    staleTime: Number.POSITIVE_INFINITY,
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

export const compareQuery = (org: string, factory: string, from = 'base', to = 'draft') =>
  queryOptions({
    queryKey: defKeys.compare(org, factory, from, to),
    queryFn: ({ signal }) => compareVersions(org, factory, from, to, signal),
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
