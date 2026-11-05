import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { ensureLoaded, factoriesQuery, webhookDeliveriesQuery, webhookQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { PendingSkeleton } from '../app/route-skeletons';
import { NotFoundScreen } from '../app/states';
import { WebhookShape } from '../features/webhooks/parts';
import { WebhookPage } from '../features/webhooks/webhook-page';

// One webhook (ADR 0040 decision 20, canvas "Webhook"): the webhook loads before the page; a name that does
// not exist is the not-found screen. Its delivery log loads in the page, under its own skeleton.
export const Route = createFileRoute('/o/$org/_org/webhooks/$webhook')({
  pendingComponent: () => <PendingSkeleton placeholder={<WebhookShape />} />,
  loader: async ({ context: { queryClient, org }, params }) => {
    void queryClient.prefetchQuery(factoriesQuery(org.id));
    void queryClient.prefetchInfiniteQuery(webhookDeliveriesQuery(org.id, params.webhook));
    try {
      await ensureLoaded(queryClient, webhookQuery(org.id, params.webhook));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  head: ({ match: { context }, params }) => pageTitle(params.webhook, 'Webhooks', context.org.name),
  component: WebhookPage,
  notFoundComponent: () => <NotFoundScreen what="webhook" />,
});
