// Webhooks (ADR 0040 decision 20, canvas docs/design/screens/organization/webhooks.jpg): the organization's
// webhooks, each with its sender, address, the name of the secret that signs it and the factories it is
// open to. Owners and admins read them (webhook:read); webhook:manage adds New webhook. Nobody reads a
// secret's value here.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { factoriesQuery, webhooksQuery } from '../../api/queries';
import type { Webhook } from '../../api/webhooks';
import { TopBarSlot } from '../../app/top-bar';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { EmptyState } from '../../ui/empty-state';
import { InfoTip } from '../../ui/info-tip';
import { Skeleton } from '../../ui/skeleton';
import { Card, FactoryChip, StatusChip, WebhooksShape } from './parts';
import { senderLabel, statusOf, weekText } from './words';

export function WebhooksPage() {
  const org = useOrg();
  const manage = useOrgHolds('webhook:manage');
  const list = useQuery(webhooksQuery(org.id));
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const webhooks = list.data ?? [];

  const create = manage ? (
    <Button asChild variant="primary">
      <Link to="/o/$org/webhooks/new" params={{ org: org.slug }}>
        <Plus size={15} strokeWidth={1.75} aria-hidden="true" />
        New webhook
      </Link>
    </Button>
  ) : undefined;

  return (
    <>
      <TopBarSlot actions={create} />
      <div className="flex min-w-0 flex-col gap-5">
        <div className="flex items-center gap-1">
          <h1 className="m-0 text-page-title tracking-display">Webhooks</h1>
          <InfoTip about="webhooks">
            Each one is connected once for the organization and opened to the factories that may use it. Triggers in
            those factories name it, for example github-acme.check_run.
          </InfoTip>
        </div>

        {list.isPending ? (
          <Skeleton ready={false} placeholder={<WebhooksShape />}>
            <span />
          </Skeleton>
        ) : list.isError ? (
          <div
            role="alert"
            className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          >
            <Callout tone="rework">Couldn’t load the webhooks. Check your connection and try again.</Callout>
            <Button variant="secondary" onClick={() => void list.refetch()}>
              Try again
            </Button>
          </div>
        ) : webhooks.length === 0 ? (
          <EmptyState title="No webhooks yet" actions={create}>
            {manage
              ? 'Connect GitHub, PagerDuty or any sender that posts events, then open it to the factories that use them.'
              : `Owners and admins of ${org.name} connect webhooks.`}
          </EmptyState>
        ) : (
          <Card title={`${webhooks.length} ${webhooks.length === 1 ? 'webhook' : 'webhooks'}`}>
            <ul className="m-0 flex list-none flex-col p-0">
              {webhooks.map((webhook) => (
                <Row key={webhook.id} webhook={webhook} orgSlug={org.slug} factories={factories} />
              ))}
            </ul>
          </Card>
        )}
      </div>
    </>
  );
}

function Row({
  webhook,
  orgSlug,
  factories,
}: {
  webhook: Webhook;
  orgSlug: string;
  factories: Parameters<typeof FactoryChip>[0]['factories'];
}) {
  return (
    <li className="relative border-t border-line transition-colors duration-(--duration-quick) first:border-t-0 hover:bg-panel motion-reduce:transition-none">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2 px-4 py-3 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_auto_auto]">
        <Link
          to="/o/$org/webhooks/$webhook"
          params={{ org: orgSlug, webhook: webhook.name }}
          className="flex min-w-0 flex-col text-ink no-underline after:absolute after:inset-0 after:content-['']"
        >
          <span title={webhook.name} className="truncate text-body font-medium">
            {webhook.name}
          </span>
          <span title={`${senderLabel(webhook.sender)}, ${webhook.address}`} className="truncate text-meta text-muted">
            {senderLabel(webhook.sender)}, <span className="font-mono">{webhook.address}</span>
          </span>
          <span title={webhook.secret} className="truncate text-meta text-muted">
            Secret <span className="font-mono">{webhook.secret}</span>
          </span>
        </Link>
        <span className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 max-lg:order-last max-lg:col-span-2">
          {webhook.openings.length === 0 ? (
            <span className="text-secondary text-muted">Open to no factory</span>
          ) : (
            webhook.openings.map((o) => <FactoryChip key={o.factory} id={o.factory} factories={factories} />)
          )}
        </span>
        <StatusChip status={statusOf(webhook)} />
        <span className="text-secondary text-muted max-lg:hidden">{weekText(webhook)}</span>
      </div>
    </li>
  );
}
