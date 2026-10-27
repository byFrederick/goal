// Needs you on the Floor (Floor, Needs you): what waits on a person, one row per entry, from the inbox.
// Decisions get Deny and Approve (the same calls as the item page); the other rows get Decide, which
// opens the item. Questions are not built yet (agents cannot ask), so no question row is shown.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CircleAlert, RotateCcw, ShieldCheck } from 'lucide-react';
import { useState } from 'react';
import { inboxQuery } from '../../api/queries';
import type { ItemCard } from '../../api/types';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { humanize } from '../../shared/format';
import { type DecidableItem, RejectDialog, useDecision } from '../../shared/item-actions';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { cn } from '../../ui/cn';
import { Count } from '../../ui/count';
import { CardEmpty, CardFailed, CardLoading } from './card-states';
import { type InboxRow, inboxGroups } from './needs-you-groups';

function decidable(item: ItemCard): DecidableItem {
  return { key: item.ref, ref: item.ref, title: item.title, state: item.state, pull_requests: item.pull_requests };
}

/** The tile's icon and tone: a decision is violet, a sent-back warning or a stop is red. */
function Tile({ row }: { row: InboxRow }) {
  const kind = row.entry.kind;
  const tone = kind === 'decision' ? 'bg-you-tint text-you-ink' : 'bg-rework-tint text-rework-ink';
  const Icon = kind === 'decision' ? ShieldCheck : kind === 'rework_warning' ? RotateCcw : CircleAlert;
  return (
    <span className={cn('flex size-9 shrink-0 items-center justify-center rounded-control', tone)} aria-hidden="true">
      <Icon size={18} strokeWidth={1.75} />
    </span>
  );
}

/** A decision's Deny and Approve, on the row. Phones get Deny and Review instead (Approve is on the item). */
function DecisionActions({ item, factory }: { item: ItemCard; factory: string }) {
  const decide = useDecision(decidable(item), factory);
  const params = useFactoryParams();
  const [denying, setDenying] = useState(false);
  const pending = decide.isPending ? decide.variables?.kind : undefined;
  return (
    <>
      <Button size="sm" className="max-md:h-11 max-md:flex-1 max-md:justify-center" onClick={() => setDenying(true)}>
        Deny
      </Button>
      <Button
        size="sm"
        variant="primary"
        className="border-you bg-you hover:bg-you/90 max-md:hidden"
        disabled={decide.isPending}
        aria-label={`Approve ${item.ref}`}
        onClick={() => decide.mutate({ kind: 'approve' })}
      >
        {pending === 'approve' ? 'Approving…' : 'Approve'}
      </Button>
      <Link
        to="/o/$org/f/$factory/items/$ref"
        params={{ ...params, ref: item.ref }}
        className="inline-flex h-11 flex-1 items-center justify-center rounded-control border border-you bg-you px-4 text-body font-medium text-white no-underline md:hidden"
      >
        Review
      </Link>
      <RejectDialog
        item={decidable(item)}
        to={null}
        open={denying}
        onOpenChange={setDenying}
        pending={pending === 'reject'}
        onConfirm={(reason) => decide.mutate({ kind: 'reject', reason }, { onSuccess: () => setDenying(false) })}
      />
    </>
  );
}

/** Any other row: Decide opens the item, or the board where the entry is about no item. */
function DecideLink({ row }: { row: InboxRow }) {
  const params = useFactoryParams();
  const className =
    'inline-flex h-9 items-center rounded-control border border-rule bg-card px-3 text-secondary font-medium text-ink no-underline hover:bg-panel max-md:h-11 max-md:flex-1 max-md:justify-center';
  return row.item ? (
    <Link to="/o/$org/f/$factory/items/$ref" params={{ ...params, ref: row.item.ref }} className={className}>
      Decide
    </Link>
  ) : (
    <Link to="/o/$org/f/$factory/board" params={params} className={className}>
      Decide
    </Link>
  );
}

export function NeedsYouCard({ className }: { className?: string }) {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  const query = useQuery(inboxQuery(org.id, factory.id));
  const rows = query.data ? inboxGroups(query.data.entries).flatMap((g) => g.rows) : [];
  const count = rows.length;

  return (
    <SectionCard
      title="Needs you"
      aria-busy={query.isPending || undefined}
      className={className}
      aside={
        <span className="flex items-center gap-3">
          {query.data ? <Count value={count} label="waiting on you" tone="you" /> : null}
          <Link
            to="/o/$org/f/$factory/items"
            params={params}
            className="inline-flex h-11 items-center text-secondary font-medium text-ink no-underline max-md:-mr-2 max-md:px-2 md:h-auto"
          >
            <span className="md:hidden">See all</span>
            <span className="max-md:hidden">Open all</span>
          </Link>
        </span>
      }
    >
      {query.isPending ? <CardLoading /> : null}
      {query.isError ? <CardFailed what="what needs you" onRetry={() => void query.refetch()} /> : null}
      {query.data && rows.length === 0 ? <CardEmpty>Nothing waits on you.</CardEmpty> : null}
      {query.data && rows.length > 0 ? (
        <ul className="m-0 flex list-none flex-col p-0">
          {rows.map((row) => {
            const item = row.item;
            const decision = row.action === 'approve' && item !== null;
            const station = humanize(row.entry.station ?? row.item?.station ?? '');
            return (
              <li key={row.key} className="flex items-start gap-3 border-t border-line py-3 first:border-t-0">
                <Tile row={row} />
                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  <div className="min-w-0">
                    <p className="m-0 truncate text-body font-medium text-ink">{row.title}</p>
                    <p className="m-0 text-meta text-muted">{item ? `${item.ref} at ${station}` : row.why}</p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 max-md:flex-col max-md:items-stretch">
                    {decision ? <DecisionActions item={item} factory={factory.id} /> : <DecideLink row={row} />}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      ) : null}
    </SectionCard>
  );
}
