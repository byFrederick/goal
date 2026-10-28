// The newest deliveries of one endpoint, one row each: when it came, what became of it, and View
// request. A refused row is highlighted and says why; a created one links its item. Only the first
// page is read (the newest five); paging is not built.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { webhookDeliveriesQuery } from '../../api/queries';
import type { WebhookDelivery } from '../../api/webhooks';
import { formatClock } from '../../shared/format';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import type { RequestTarget } from './request-dialog';
import { outcomeOf } from './words';

export interface DeliveriesScope {
  org: string;
  orgSlug: string;
  factory: string;
  factoryName: string;
  endpoint: string;
  endpointName: string;
}

export function Deliveries({ scope, onView }: { scope: DeliveriesScope; onView: (target: RequestTarget) => void }) {
  const deliveries = useQuery(webhookDeliveriesQuery(scope.org, scope.factory, scope.endpoint));

  if (deliveries.isPending) {
    return (
      <div aria-busy="true" className="border-t border-line px-4 py-3">
        <span role="status" className="text-secondary text-muted">
          Loading deliveries
        </span>
      </div>
    );
  }
  if (deliveries.isError) {
    return (
      <div className="border-t border-line p-4">
        <Callout tone="rework">Couldn’t load the deliveries. Check your connection and try again.</Callout>
        <Button variant="secondary" size="sm" className="mt-3" onClick={() => void deliveries.refetch()}>
          Try again
        </Button>
      </div>
    );
  }
  const rows = deliveries.data.deliveries;
  if (rows.length === 0) {
    return <p className="m-0 border-t border-line px-4 py-3 text-secondary text-muted">No deliveries yet.</p>;
  }
  return (
    <ul className="m-0 list-none p-0">
      {rows.map((delivery) => (
        <DeliveryRow
          key={delivery.id}
          delivery={delivery}
          scope={scope}
          onView={() =>
            onView({
              org: scope.org,
              factory: scope.factory,
              endpoint: scope.endpoint,
              endpointName: scope.endpointName,
              delivery: delivery.id,
              receivedAt: formatClock(delivery.received_at),
            })
          }
        />
      ))}
    </ul>
  );
}

function DeliveryRow({
  delivery,
  scope,
  onView,
}: {
  delivery: WebhookDelivery;
  scope: DeliveriesScope;
  onView: () => void;
}) {
  const outcome = outcomeOf(delivery);
  const refused = outcome.kind === 'refused';
  return (
    <li
      className={cn(
        'grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 border-t border-line px-4 py-3 text-secondary sm:grid-cols-[auto_minmax(0,1fr)_auto]',
        refused && 'bg-rework-tint',
      )}
    >
      <time dateTime={delivery.received_at} className="col-span-2 text-meta text-muted sm:col-span-1">
        {formatClock(delivery.received_at)}
      </time>
      <span className={cn('min-w-0', refused ? 'font-medium text-rework-ink' : 'text-ink')}>
        {outcome.kind === 'created' && delivery.item ? (
          <>
            {outcome.text}{' '}
            <Link
              to="/o/$org/f/$factory/items/$ref"
              params={{ org: scope.orgSlug, factory: scope.factoryName, ref: String(delivery.item.number) }}
              className="font-medium text-run underline-offset-2 hover:underline"
            >
              #{delivery.item.number}
            </Link>
          </>
        ) : (
          outcome.text
        )}
      </span>
      <Button
        variant="secondary"
        size="sm"
        onClick={onView}
        aria-label={`View request ${scope.endpointName} ${formatClock(delivery.received_at)}`}
      >
        View request
      </Button>
    </li>
  );
}
