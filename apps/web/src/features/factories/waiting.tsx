// What waits on people across the organization (C §10.7): the "Waiting on you" list (newest
// first, from GET /inbox) and, on a phone, the violet banner above the cards that leads to it.
import { Link } from '@tanstack/react-router';
import { ChevronRight, CircleCheck, CircleDot } from 'lucide-react';
import type { FactoryOverviewList, Inbox, InboxEntry, OrgSummary } from '../../api/types';
import { refParam } from '../../shared/factory';
import { formatAge, humanize, plural } from '../../shared/format';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { ListRow } from '../../ui/list-row';
import { bannerDetail, possessive } from './sentences';

/** The list's anchor: the phone banner links to it. */
export const waitingId = 'waiting';

const iconProps = { size: 18, strokeWidth: 1.5 } as const;

/**
 * The inbox's kinds that wait on no one's decision: a stopped line, a rework warning, blocked and
 * expedited work show on each factory's Floor, not in this list of what waits on you.
 */
const notDecisions: ReadonlySet<InboxEntry['kind']> = new Set(['andon', 'rework_warning', 'blocked', 'expedite']);

const newestFirst = (entries: readonly InboxEntry[]) =>
  entries.filter((e) => !notDecisions.has(e.kind)).toSorted((a, b) => b.since.localeCompare(a.since));

export function WaitingList({ org, inbox, className }: { org: OrgSummary; inbox: Inbox; className?: string }) {
  const entries = newestFirst(inbox.entries);
  const newest = entries.find((e) => e.item !== null);
  const now = Date.now();

  return (
    <SectionCard
      id={waitingId}
      title="Waiting on you"
      description={`Across ${possessive(org.name)} factories, newest first`}
      aside={
        newest?.item ? (
          <Link
            to="/o/$org/f/$factory/items/$ref"
            params={{ org: org.slug, factory: newest.factory.name, ref: refParam(newest.item.ref) }}
            className="shrink-0 text-sm whitespace-nowrap underline transition-colors duration-200 hover:text-run"
          >
            Open {newest.item.ref}
          </Link>
        ) : null
      }
      className={className}
    >
      {entries.length === 0 ? (
        <p className="m-0 border-t border-line pt-3 text-[15px] text-muted">Nothing is waiting on you.</p>
      ) : (
        <ul className="@container m-0 -mt-1 -mb-2 list-none p-0">
          {entries.map((entry) => (
            <li key={entry.id}>
              <WaitingRow org={org.slug} entry={entry} now={now} />
            </li>
          ))}
        </ul>
      )}
      {inbox.truncated ? (
        <p className="m-0 text-[13px] text-muted">Showing the newest {plural(entries.length, 'entry', 'entries')}.</p>
      ) : null}
    </SectionCard>
  );
}

/** A decision about an item links to the item; a kind this build does not know is a generic row. */
function WaitingRow({ org, entry, now }: { org: string; entry: InboxEntry; now: number }) {
  const age = formatAge(Math.max(0, (now - Date.parse(entry.since)) / 1000));
  const aside = (
    <>
      <Chip>{entry.factory.name}</Chip>
      <time dateTime={entry.since} className="text-[13px] text-muted @[480px]:min-w-11 @[480px]:text-right">
        {age}
      </time>
    </>
  );
  const item = entry.kind === 'decision' ? entry.item : null;

  if (!item) {
    return (
      <ListRow
        icon={<CircleDot {...iconProps} />}
        tone="plain"
        title={humanize(entry.kind)}
        sub={entry.station ? `At ${humanize(entry.station)}` : undefined}
        aside={aside}
      >
        <Link to="/o/$org/f/$factory" params={{ org, factory: entry.factory.name }} />
      </ListRow>
    );
  }

  return (
    <ListRow
      icon={<CircleCheck {...iconProps} />}
      title={item.title}
      sub={`${item.ref}, at ${humanize(entry.station ?? item.station)}`}
      aside={aside}
    >
      <Link to="/o/$org/f/$factory/items/$ref" params={{ org, factory: entry.factory.name, ref: refParam(item.ref) }} />
    </ListRow>
  );
}

/**
 * On a phone, above the cards: how much waits and where. It leads to the list below the cards,
 * or straight to the item when one thing waits. Nothing shows when nothing waits.
 */
export function WaitingBanner({
  org,
  overview,
  inbox,
}: {
  org: OrgSummary;
  overview: FactoryOverviewList;
  inbox: Inbox;
}) {
  const total = overview.waiting_on_you;
  if (total === 0) return null;

  const only = total === 1 ? newestFirst(inbox.entries).find((e) => e.item !== null) : undefined;
  const className =
    'grid min-h-14 grid-cols-[36px_minmax(0,1fr)_12px] items-center gap-3 rounded-card bg-you-tint px-3.5 py-2 text-ink no-underline md:hidden';
  const content = (
    <>
      <span aria-hidden="true" className="flex size-9 items-center justify-center rounded-control bg-surface text-you">
        <CircleCheck {...iconProps} />
      </span>
      <span className="min-w-0">
        <span className="block text-[15px] font-medium">{total} waiting on you</span>
        <span className="block text-[13px] text-you-ink">{bannerDetail(overview.factories)}</span>
      </span>
      <ChevronRight size={14} strokeWidth={1.5} aria-hidden="true" className="text-you-ink" />
    </>
  );

  return only?.item ? (
    <Link
      to="/o/$org/f/$factory/items/$ref"
      params={{ org: org.slug, factory: only.factory.name, ref: refParam(only.item.ref) }}
      className={className}
    >
      {content}
    </Link>
  ) : (
    <a href={`#${waitingId}`} className={className}>
      {content}
    </a>
  );
}
