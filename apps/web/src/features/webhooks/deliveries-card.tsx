// The delivery log (ADR 0040 decision 13): each delivery's receipt (Accepted, Duplicate, Refused with its
// reason) and, under it, one line per factory whose triggers name it: Created with its item, Skipped with the
// reason, Failed. A delivery accepted that no factory names is counted, not listed. Pages of the newest first;
// webhook:manage adds View request, which opens the headers and the body.
import { useInfiniteQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { webhookDeliveriesQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import type { WebhookDelivery } from '../../api/webhooks';
import { formatClock } from '../../shared/format';
import { useArrivals } from '../../ui/arrivals';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NumberRoll } from '../../ui/number-roll';
import { Skeleton, SkeletonRows } from '../../ui/skeleton';
import { Card, FactoryChip } from './parts';
import type { RequestTarget } from './request-dialog';
import { evaluationText, receiptNote, receiptOf } from './words';

/** Whether the log lists the delivery: one a factory names, a refusal or a repeat. The rest is only counted. */
const listed = (d: WebhookDelivery) => d.evaluations.length > 0 || receiptOf(d).label !== 'Accepted';

export function DeliveriesCard({
  org,
  orgSlug,
  webhook,
  factories,
  canManage,
  onView,
}: {
  org: string;
  orgSlug: string;
  webhook: string;
  factories: readonly Factory[];
  canManage: boolean;
  onView: (target: RequestTarget) => void;
}) {
  const log = useInfiniteQuery(webhookDeliveriesQuery(org, webhook));
  const all = log.data?.pages.flatMap((p) => p.deliveries) ?? [];
  const rows = all.filter(listed);
  // Every page carries the same unnamed_days, so only the first page's counts are summed.
  const unnamed = (log.data?.pages[0]?.unnamed_days ?? []).reduce((n, d) => n + d.count, 0);
  const counted = all.length - rows.length + unnamed;
  // A delivery that arrives after the log first had rows fades in (MOTION.md, A new item appears in a list).
  const arrived = useArrivals(rows.map((d) => d.id));

  return (
    <Card
      title="Deliveries"
      tip={{
        about: 'deliveries',
        text: 'Each delivery’s receipt, and what it led to in each factory whose triggers name it. Kept 30 days; only owners and admins see bodies.',
      }}
    >
      {log.isPending ? (
        <div className="p-4">
          <Skeleton ready={false} placeholder={<SkeletonRows rows={4} />}>
            <span />
          </Skeleton>
        </div>
      ) : log.isError ? (
        <div role="alert" className="flex flex-col items-start gap-3 p-4">
          <Callout tone="rework">Couldn’t load the deliveries. Check your connection and try again.</Callout>
          <Button variant="secondary" size="sm" onClick={() => void log.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <>
          {rows.length === 0 ? (
            <p className="m-0 px-4 py-3 text-secondary text-muted">No deliveries to show yet.</p>
          ) : (
            <ul className="m-0 flex list-none flex-col p-0">
              {rows.map((d) => (
                <DeliveryRow
                  key={d.id}
                  delivery={d}
                  arrived={arrived(d.id)}
                  orgSlug={orgSlug}
                  factories={factories}
                  onView={
                    canManage
                      ? () => onView({ org, webhook, delivery: d.id, receivedAt: formatClock(d.received_at) })
                      : undefined
                  }
                />
              ))}
            </ul>
          )}
          {counted > 0 ? (
            <p className={cn('m-0 px-4 py-3 text-secondary text-muted', rows.length > 0 && 'border-t border-line')}>
              <NumberRoll value={counted.toLocaleString('en-US')} /> more {counted === 1 ? 'delivery' : 'deliveries'} no
              trigger names in the last 30 days
            </p>
          ) : null}
          {log.hasNextPage ? (
            <div className="border-t border-line px-4 py-3">
              <Button
                variant="secondary"
                size="sm"
                disabled={log.isFetchingNextPage}
                onClick={() => void log.fetchNextPage()}
              >
                {log.isFetchingNextPage ? 'Loading…' : 'Show older'}
              </Button>
            </div>
          ) : null}
        </>
      )}
    </Card>
  );
}

function DeliveryRow({
  delivery,
  arrived,
  orgSlug,
  factories,
  onView,
}: {
  delivery: WebhookDelivery;
  arrived: boolean;
  orgSlug: string;
  factories: readonly Factory[];
  onView: (() => void) | undefined;
}) {
  const receipt = receiptOf(delivery);
  const note = receiptNote(delivery);
  const time = formatClock(delivery.received_at);
  return (
    <li
      className={cn(
        'grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-3 gap-y-1 border-t border-line px-4 py-3 text-secondary first:border-t-0',
        arrived && 'motion-safe:animate-item-in motion-reduce:animate-fade-in',
      )}
    >
      <time dateTime={delivery.received_at} className="text-meta text-muted">
        {time}
      </time>
      <span className="flex min-w-0 flex-col">
        <span title={delivery.event} className="truncate font-mono text-ink">
          {delivery.event || 'No event'}
        </span>
        {note ? (
          <span title={note} className={cn('truncate text-meta', receipt.refused ? 'text-rework-ink' : 'text-muted')}>
            {note}
          </span>
        ) : null}
      </span>
      <span className="flex items-center gap-2">
        <Chip tone={receipt.refused ? 'rework' : 'quiet'}>{receipt.label}</Chip>
        {onView ? (
          <Button variant="secondary" size="sm" onClick={onView} aria-label={`View request ${time}`}>
            View request
          </Button>
        ) : null}
      </span>
      {delivery.evaluations.length > 0 ? (
        <ul className="col-start-2 col-end-4 m-0 flex list-none flex-col gap-1 p-0">
          {delivery.evaluations.map((e) => {
            const said = evaluationText(e);
            return (
              <li key={e.factory} className="flex min-w-0 flex-wrap items-center gap-x-2 text-secondary">
                <FactoryChip id={e.factory} factories={factories} name={e.factory_name} />
                {said.kind === 'created' && e.item ? (
                  <span className="text-ink-soft">
                    Created{' '}
                    <Link
                      to="/o/$org/f/$factory/items/$ref"
                      params={{ org: orgSlug, factory: e.factory_name, ref: String(e.item.number) }}
                      className="font-medium text-link underline-offset-2 hover:underline"
                    >
                      {e.item.ref}
                    </Link>
                  </span>
                ) : (
                  <span className={cn(said.kind === 'failed' ? 'text-rework-ink' : 'text-ink-soft')}>{said.text}</span>
                )}
              </li>
            );
          })}
        </ul>
      ) : null}
    </li>
  );
}
