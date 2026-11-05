import { createFileRoute } from '@tanstack/react-router';
import { secretsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { PendingScreen } from '../app/states';
import { NewWebhookPage } from '../features/webhooks/new-webhook-page';

// New webhook (ADR 0040 decision 20, canvas "Webhook-New"): a sender, a name, a secret and the factories to
// open it to. The organization's secrets load beside the page, for "Use a secret".
export const Route = createFileRoute('/o/$org/_org/webhooks/new')({
  pendingComponent: PendingScreen,
  loader: ({ context: { queryClient, org } }) => {
    void queryClient.prefetchQuery(secretsQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('New webhook', 'Webhooks', context.org.name),
  component: NewWebhookPage,
});
