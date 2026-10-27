// The Needs you list (MASTER.md, "Needs you list"): one row per entry with its factory badge, its type
// in its colour, its title, its item and station, and its age. The selected row is violet-tinted with
// a violet edge and says so with aria-current. Rows are links, so the selection lives in the URL.
import { Link } from '@tanstack/react-router';
import type { InboxEntry } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { formatAge, humanize } from '../../shared/format';
import { factoryMark } from '../../shared/marks';
import { useOrg } from '../../shared/org';
import { cn } from '../../ui/cn';
import type { NeedsSearch } from './search';
import { typeLabel, typeOf } from './types';

/** How long an entry has waited, in seconds, from its `since`. */
export function ageOf(entry: InboxEntry, now = Date.now()): number {
  return Math.max(0, (now - Date.parse(entry.since)) / 1000);
}

/** The row's title: the item's title, or what the entry is when it is about no item. */
export function titleOf(entry: InboxEntry): string {
  if (entry.item) return entry.item.title;
  if (entry.kind === 'andon') return 'Line stopped';
  if (entry.kind === 'rework_warning' && entry.rework) {
    return `${humanize(entry.rework.station)} sent back ${entry.rework.rejected} of the last ${entry.rework.verdicts}`;
  }
  return humanize(entry.kind);
}

/** The line under the title: the item and its station, or the factory when no item is involved. */
function whereOf(entry: InboxEntry): string {
  const station = humanize(entry.station ?? entry.item?.station ?? '');
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
  return (
    <ul aria-label={label} className="m-0 flex list-none flex-col p-0">
      {entries.map((entry) => (
        <NeedsRow
          key={entry.id}
          entry={entry}
          current={entry.id === selected}
          search={search}
          mark={markOf(entry, siblings)}
        />
      ))}
    </ul>
  );
}

function NeedsRow({
  entry,
  current,
  search,
  mark,
}: {
  entry: InboxEntry;
  current: boolean;
  search: NeedsSearch;
  mark: string;
}) {
  const org = useOrg();
  const type = typeOf(entry);
  const typeClass = type === 'decision' ? 'text-you-ink' : type === 'alert' ? 'text-rework-ink' : 'text-ink-soft';
  return (
    <li className="border-b border-line">
      <Link
        to="/o/$org/needs-you"
        params={{ org: org.slug }}
        search={{ ...search, entry: entry.id }}
        activeOptions={{ exact: true, includeSearch: true }}
        className={cn(
          'relative flex items-start gap-3 px-4 py-3 text-ink no-underline hover:bg-panel hover:text-ink',
          current && 'bg-you-tint hover:bg-you-tint',
        )}
      >
        {current ? (
          <span aria-hidden="true" className="absolute inset-y-2 left-0 w-0.75 rounded-r-chip bg-you" />
        ) : null}
        <FactoryBadge mark={mark} />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className={cn('text-meta font-semibold', typeClass)}>{typeLabel[type]}</span>
          <span className="truncate text-body font-medium text-ink">{titleOf(entry)}</span>
          <span className="truncate text-secondary text-ink-soft">{whereOf(entry)}</span>
        </span>
        <span className="flex-none text-meta text-muted">{formatAge(ageOf(entry))}</span>
      </Link>
    </li>
  );
}
