// "Needs you first" on Home (ADR 0016, wave 2): what waits on a person across the organization, from
// GET /inbox, at most five rows. An alert (a stopped line, a rework warning, a blocked item) opens its
// factory's floor. An approval is decided here: Approve runs the item's approve decision, and Deny opens
// the item, where a send-back takes its reason. Kinds this page does not know get a Review link. There
// are no Question rows: questions are not built yet.
import { Link } from '@tanstack/react-router';
import { useId } from 'react';
import type { Factory, Inbox, InboxEntry, ItemCard, OrgSummary } from '../../api/types';
import { refParam } from '../../shared/factory';
import { humanize, plural } from '../../shared/format';
import { type DecidableItem, useDecision } from '../../shared/item-actions';
import { holds, reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';

/** At most this many rows on Home; the rest wait behind "Open all". */
const shown = 5;

export function NeedsYouFirst({
  org,
  inbox,
  factories,
  mark,
}: {
  org: OrgSummary;
  inbox: Inbox;
  /** The organization's factories, for what the caller may decide in each. */
  factories: readonly Factory[];
  mark: (factory: { name: string }) => string;
}) {
  const byId = new Map(factories.map((f) => [f.id, f]));
  const rows = inbox.entries.slice(0, shown);

  return (
    <section
      aria-labelledby="needs-you-first"
      className="@container flex min-w-0 flex-col rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex items-center gap-3 pb-2">
        <h2 id="needs-you-first" className="m-0 text-heading">
          Needs you first
        </h2>
        <span className="rounded-chip bg-you-tint px-2 text-meta text-you-ink tabular-nums">
          {inbox.entries.length}
        </span>
        <Link
          to="/o/$org/needs-you"
          params={{ org: org.slug }}
          className="ml-auto text-secondary font-control text-run-ink underline"
        >
          Open all
        </Link>
      </div>

      {rows.length === 0 ? (
        <p className="m-0 border-t border-line py-3 text-body text-muted">Nothing is waiting on you.</p>
      ) : (
        <ul className="m-0 list-none p-0">
          {rows.map((entry) => (
            <li key={entry.id}>
              <AskRow
                org={org}
                entry={entry}
                mark={mark(entry.factory)}
                canDecide={holds(byId.get(entry.factory.id), 'item:decide')}
              />
            </li>
          ))}
        </ul>
      )}
      {inbox.truncated ? (
        <p className="m-0 pt-2 text-meta text-muted">
          Showing the newest {plural(inbox.entries.length, 'entry', 'entries')}.
        </p>
      ) : null}
    </section>
  );
}

function AskRow({
  org,
  entry,
  mark,
  canDecide,
}: {
  org: OrgSummary;
  entry: InboxEntry;
  mark: string;
  canDecide: boolean;
}) {
  const floor = { to: '/o/$org/f/$factory' as const, params: { org: org.slug, factory: entry.factory.name } };
  const kind = describeEntry(entry);
  const item = entry.item;

  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-2 border-t border-line py-3 @[480px]:grid-cols-[auto_minmax(0,1fr)_auto]">
      <span
        aria-hidden="true"
        className="flex size-6 items-center justify-center rounded-control bg-run font-numeral text-meta font-semibold text-white"
      >
        {mark}
      </span>
      <div className="min-w-0">
        <span className={cn('block text-meta font-semibold', kind.tone)}>{kind.label}</span>
        <span className="block text-body break-words">{kind.title}</span>
      </div>
      <div className="col-span-2 flex flex-wrap gap-2 @[480px]:col-span-1 @[480px]:justify-end">
        {entry.kind === 'decision' && item ? (
          <Decision org={org} factory={entry.factory} item={item} canDecide={canDecide} />
        ) : (
          <Button asChild size="sm">
            <Link {...floor}>{kind.action}</Link>
          </Button>
        )}
      </div>
    </div>
  );
}

/** Approve in place, or Deny by opening the item where the send-back takes its reason. */
function Decision({
  org,
  factory,
  item,
  canDecide,
}: {
  org: OrgSummary;
  factory: InboxEntry['factory'];
  item: ItemCard;
  canDecide: boolean;
}) {
  const decide = useDecision(decidable(item), factory.id);
  const pending = decide.isPending;
  const reasonId = useId();

  return (
    <>
      <Button asChild size="sm">
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ org: org.slug, factory: factory.name, ref: refParam(item.ref) }}
        >
          Deny
        </Link>
      </Button>
      <Button
        variant="primary"
        size="sm"
        className="border-you bg-you hover:bg-you/90"
        disabled={!canDecide || pending}
        aria-describedby={canDecide ? undefined : reasonId}
        title={canDecide ? undefined : reasons.decide(factory.name)}
        onClick={() => decide.mutate({ kind: 'approve' })}
      >
        {pending ? 'Approving…' : 'Approve'}
      </Button>
      {canDecide ? null : (
        <span id={reasonId} className="sr-only">
          {reasons.decide(factory.name)}
        </span>
      )}
    </>
  );
}

/** The item as the decision hook takes it: the ref is the key the API accepts here. */
function decidable(item: ItemCard): DecidableItem {
  return { key: item.ref, ref: item.ref, title: item.title, state: item.state, pull_requests: item.pull_requests };
}

/** The type, its colour, the title and the action for each row. Unknown kinds say what they are. */
function describeEntry(entry: InboxEntry): { label: string; tone: string; title: string; action: string } {
  const item = entry.item;
  switch (entry.kind) {
    case 'andon':
      return {
        label: 'Alert',
        tone: 'text-rework-ink',
        title: `Line stopped: ${entry.andon?.reason ?? 'no reason given'}`,
        action: 'Open floor',
      };
    case 'rework_warning':
      return {
        label: 'Alert',
        tone: 'text-rework-ink',
        title: entry.rework
          ? `${humanize(entry.rework.station ?? entry.station ?? 'review')} sent back ${entry.rework.rejected} of the last ${entry.rework.verdicts}`
          : 'Rework is climbing',
        action: 'Open floor',
      };
    case 'blocked':
      return {
        label: 'Alert',
        tone: 'text-rework-ink',
        title: item ? `${item.title}: ${entry.blocked?.reason ?? 'blocked'}` : 'An item is blocked',
        action: 'Open floor',
      };
    case 'decision':
      return {
        label: 'Approval',
        tone: 'text-you-ink',
        title: item?.title ?? entry.approval?.summary ?? 'Waiting for a decision',
        action: 'Review',
      };
    default:
      return {
        label: humanize(entry.kind),
        tone: 'text-muted',
        title: item?.title ?? humanize(entry.kind),
        action: 'Review',
      };
  }
}
