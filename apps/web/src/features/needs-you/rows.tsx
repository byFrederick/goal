// The Needs you list (MASTER.md, "Needs you list"): one row per entry with its factory badge, its type
// in its colour, its title, its item and station, and its age. The selected row has the orange tint, no edge bar,
// and says so with aria-current. Rows are links, so the selection lives in the URL.
import { Link } from '@tanstack/react-router';
import { useRef } from 'react';
import type { InboxEntry, Watch } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { formatAge, formatUsd, formatUsdCompact, formatWeekdayTime, humanize } from '../../shared/format';
import { factoryMark } from '../../shared/marks';
import { useOrg } from '../../shared/org';
import { cn } from '../../ui/cn';
import { watchMetricLabel } from '../jonah/jonah-format';
import type { NeedsSearch } from './search';
import { typeLabel, typeOf } from './types';

/** How long an entry has waited, in seconds, from its `since`. */
export function ageOf(entry: InboxEntry, now = Date.now()): number {
  return Math.max(0, (now - Date.parse(entry.since)) / 1000);
}

/** The row's title: the asking role and its question, the item's title, or what the entry is when it is about no item. */
export function titleOf(entry: InboxEntry): string {
  if (entry.kind === 'question' && entry.question) return `${entry.question.agent}: ${entry.question.text}`;
  if (entry.kind === 'tool_approval' && entry.tool_approval) return approvalTitle(entry.tool_approval);
  if (entry.item) return entry.item.title;
  if (entry.kind === 'knowledge_source' && entry.knowledge_source) return syncFailureTitle(entry);
  if (entry.kind === 'conversion') return conversionTitle(entry);
  if (entry.kind === 'watch_worse' && entry.watch) return watchWorseTitle(entry.watch);
  if (entry.kind === 'andon') return 'Line stopped';
  if (entry.kind === 'budget' && entry.budget) return budgetTitle(entry.budget);
  if (entry.kind === 'rework_warning' && entry.rework) {
    return `${humanize(entry.rework.station)} sent back ${entry.rework.rejected} of the last ${entry.rework.verdicts}`;
  }
  return humanize(entry.kind);
}

/** A used-up weekly budget's title: "Weekly budget used up · $1,602 of $1,600". */
export function budgetTitle(b: { spent_usd: number; weekly_usd: number }): string {
  return `Weekly budget used up · ${formatUsd(b.spent_usd)} of ${formatUsdCompact(b.weekly_usd)}`;
}

/** A failing source's title: the source, and the factory that reads it, "product-specs, which Software reads, does not sync". */
export function syncFailureTitle(entry: InboxEntry): string {
  return `${entry.knowledge_source?.source ?? 'A source'}, which ${entry.factory.title ?? entry.factory.name} reads, does not sync`;
}

/** A conversion entry's title: the factory that could not move to secrets. */
export function conversionTitle(entry: InboxEntry): string {
  return `${entry.factory.title ?? entry.factory.name} could not move to secrets`;
}

/** A watch_worse entry's title: the proposal and the number that got worse, "Proposal 14 made sent-back rate worse". */
export function watchWorseTitle(watch: { proposal?: number | undefined; metric: Watch['metric'] }): string {
  const label = watchMetricLabel(watch.metric).toLowerCase();
  return watch.proposal === undefined
    ? `A change made ${label} worse`
    : `Proposal ${watch.proposal} made ${label} worse`;
}

/** An Approval's title: the agent role that asks and the tool it wants to call. */
export function approvalTitle(approval: { agent: string; tool: string }): string {
  return `${approval.agent} wants to call ${approval.tool}`;
}

/** The line under the title: the item and its station, or the factory when no item is involved. */
function whereOf(entry: InboxEntry): string {
  const station = humanize(entry.station ?? entry.item?.station ?? '');
  if (entry.kind === 'knowledge_source' && entry.knowledge_source) {
    const k = entry.knowledge_source;
    return `${k.error || 'The sync failed'} ${k.failed_syncs} times`;
  }
  if (entry.kind === 'conversion' && entry.conversion) return entry.conversion.reason;
  if (entry.kind === 'budget' && entry.budget) {
    return `${entry.factory.title ?? entry.factory.name} · Resumes about ${formatWeekdayTime(entry.budget.resumes_at)}`;
  }
  if ((entry.kind === 'question' || entry.kind === 'tool_approval') && entry.item) {
    return `#${entry.item.number} at ${station}, run paused`;
  }
  const about = entry.item ? entry.item.ref : (entry.factory.title ?? entry.factory.name);
  return [about, station].filter(Boolean).join(' at ');
}

/** The factory's badge letter, as the sidebar gives it: its name's, against the other factories listed. */
function markOf(entry: InboxEntry, siblings: readonly { name: string }[]): string {
  return factoryMark(entry.factory, siblings);
}

export function NeedsList({
  entries,
  selected,
  search,
  siblings,
  label,
}: {
  entries: readonly InboxEntry[];
  selected: string | undefined;
  /** The search params every row link keeps (the tab and the factory), with its own entry. */
  search: NeedsSearch;
  /** The factories the list shows, so each badge takes its letter as the sidebar gives it. */
  siblings: readonly { name: string }[];
  label: string;
}) {
  const arrived = useArrivals(entries.map((e) => e.id));
  return (
    <ul aria-label={label} className="m-0 flex list-none flex-col p-0">
      {entries.map((entry) => (
        <NeedsRow
          key={entry.id}
          arrived={arrived.has(entry.id)}
          entry={entry}
          current={entry.id === selected}
          search={search}
          mark={markOf(entry, siblings)}
        />
      ))}
    </ul>
  );
}

/**
 * The ids that appeared after the list first drew: a new entry fades in and rises (MOTION.md, A new item appears in
 * a list). The entries there when the page opened, and those a tab or the factory filter brings back, do not move.
 */
function useArrivals(ids: readonly string[]): ReadonlySet<string> {
  const known = useRef<Set<string> | null>(null);
  const arrived = useRef(new Set<string>());
  if (known.current === null) known.current = new Set(ids);
  else {
    for (const id of ids) {
      if (!known.current.has(id)) {
        known.current.add(id);
        arrived.current.add(id);
      }
    }
  }
  return arrived.current;
}

function NeedsRow({
  arrived,
  entry,
  current,
  search,
  mark,
}: {
  arrived: boolean;
  entry: InboxEntry;
  current: boolean;
  search: NeedsSearch;
  mark: string;
}) {
  const org = useOrg();
  const type = typeOf(entry);
  const typeClass =
    type === 'approval' || type === 'decision' || type === 'question'
      ? 'text-you-ink'
      : type === 'alert'
        ? 'text-rework-ink'
        : 'text-ink-soft';
  return (
    <li className={cn('border-b border-line', arrived && 'motion-safe:animate-row-in')}>
      <Link
        to="/o/$org/needs-you"
        params={{ org: org.slug }}
        search={{ ...search, entry: entry.id }}
        activeOptions={{ exact: true, includeSearch: true }}
        className={cn(
          'flex items-start gap-3 px-4 py-3 text-ink no-underline transition-colors duration-(--duration-quick) hover:bg-panel hover:text-ink',
          current && 'bg-you-tint hover:bg-you-tint',
        )}
      >
        <FactoryBadge mark={mark} />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className={cn('text-meta font-semibold', typeClass)}>{typeLabel[type]}</span>
          <span className="truncate text-body font-medium text-ink" title={titleOf(entry)}>
            {titleOf(entry)}
          </span>
          <span className="truncate text-secondary text-ink-soft" title={whereOf(entry)}>
            {whereOf(entry)}
          </span>
        </span>
        <span className="flex-none text-meta text-muted">{formatAge(ageOf(entry))}</span>
      </Link>
    </li>
  );
}
