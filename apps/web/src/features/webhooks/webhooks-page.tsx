// Webhooks (docs/design/screens/intake/webhooks.html, ADR 0016 and ADR 0026): the addresses other systems
// post to, one card each, with the triggers they feed, their last week and their newest deliveries; a
// Send a test panel beside them. New webhook and Rotate show the signing secret once (secret-dialog.tsx).
// The page reads the factory's endpoints; a delivery arrives as an item event or on a refocus, so the
// stream's item events refresh this page (invalidation.ts).
import { useQuery } from '@tanstack/react-query';
import { Plus, Webhook } from 'lucide-react';
import { useMemo, useState } from 'react';
import { webhookDeliveriesQuery, webhooksQuery } from '../../api/queries';
import type { WebhookEndpoint } from '../../api/webhooks';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { EmptyState } from '../../ui/empty-state';
import type { DeliveriesScope } from './deliveries';
import { EndpointCard } from './endpoint-card';
import { NewWebhookDialog } from './new-webhook-dialog';
import { RequestDialog, type RequestTarget } from './request-dialog';
import { SecretDialog, type ShownSecret } from './secret-dialog';
import { TestPanel } from './test-panel';

export function WebhooksPage() {
  const org = useOrg();
  const factory = useFactory();
  const canManage = factory.can.includes('webhook:manage');
  const webhooks = useQuery(webhooksQuery(org.id, factory.id));
  const [creating, setCreating] = useState(false);
  const [shown, setShown] = useState<ShownSecret | null>(null);
  const [target, setTarget] = useState<RequestTarget | null>(null);

  const actions = useMemo(
    () =>
      canManage ? (
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus size={15} strokeWidth={1.5} aria-hidden="true" />
          New webhook
        </Button>
      ) : null,
    [canManage],
  );

  const endpoints = webhooks.data ?? [];
  const first = endpoints[0];
  const newestAccepted = useNewestAccepted(org.id, factory.id, first?.id);

  return (
    <div className="flex flex-col gap-5">
      <TopBarSlot crumb="Webhooks" actions={actions} />
      <header className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title tracking-display">Webhooks</h1>
        <p className="m-0 text-body text-ink-soft">
          Addresses other systems post to. Each one feeds a trigger; without a trigger, a delivery creates nothing.
        </p>
      </header>

      {webhooks.isPending ? (
        <div aria-busy="true">
          <span role="status" className="sr-only">
            Loading webhooks
          </span>
        </div>
      ) : webhooks.isError ? (
        <div role="alert" className="flex flex-col items-start gap-3 rounded-card border border-line bg-card p-5">
          <Callout tone="rework">Couldn’t load the webhooks. Check your connection and try again.</Callout>
          <Button variant="secondary" onClick={() => void webhooks.refetch()}>
            Try again
          </Button>
        </div>
      ) : endpoints.length === 0 ? (
        <EmptyState
          icon={<Webhook size={18} strokeWidth={1.5} aria-hidden="true" />}
          title="No webhooks yet"
          actions={
            canManage ? (
              <Button variant="primary" onClick={() => setCreating(true)}>
                <Plus size={15} strokeWidth={1.5} aria-hidden="true" />
                New webhook
              </Button>
            ) : null
          }
        >
          Add a webhook, then give its address and signing secret to the system that posts to it.
        </EmptyState>
      ) : (
        <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_400px] xl:items-start">
          <div className="flex min-w-0 flex-col gap-4">
            {endpoints.map((endpoint) => (
              <EndpointCard
                key={endpoint.id}
                scope={scopeOf(org.id, org.slug, factory.id, factory.name, endpoint)}
                endpoint={endpoint}
                orgSlug={org.slug}
                factoryName={factory.name}
                canManage={canManage}
                onShown={setShown}
                onView={setTarget}
              />
            ))}
          </div>
          {first ? (
            <TestPanel
              scope={scopeOf(org.id, org.slug, factory.id, factory.name, first)}
              endpoint={first}
              newestAccepted={newestAccepted}
            />
          ) : null}
        </div>
      )}

      {creating ? (
        <NewWebhookDialog
          org={org.id}
          factory={factory.id}
          onClose={() => setCreating(false)}
          onCreated={(created) => {
            setCreating(false);
            setShown(created);
          }}
        />
      ) : null}
      <SecretDialog shown={shown} onDone={() => setShown(null)} />
      <RequestDialog target={target} onClose={() => setTarget(null)} />
    </div>
  );
}

function scopeOf(
  org: string,
  orgSlug: string,
  factory: string,
  factoryName: string,
  endpoint: WebhookEndpoint,
): DeliveriesScope {
  return { org, orgSlug, factory, factoryName, endpoint: endpoint.id, endpointName: endpoint.name };
}

/** The id of the newest delivery the endpoint accepted, from the list its card already reads. */
function useNewestAccepted(org: string, factory: string, endpoint: string | undefined): string | undefined {
  const deliveries = useQuery({
    ...webhookDeliveriesQuery(org, factory, endpoint ?? ''),
    enabled: endpoint !== undefined,
  });
  return deliveries.data?.deliveries.find((d) => d.outcome !== 'refused')?.id;
}
