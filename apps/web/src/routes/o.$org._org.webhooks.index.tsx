import { createFileRoute } from '@tanstack/react-router';
import { factoriesQuery, webhooksQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { PendingSkeleton } from '../app/route-skeletons';
import { WebhooksShape } from '../features/webhooks/parts';
import { WebhooksPage } from '../features/webhooks/webhooks-page';

// The organization's webhooks (ADR 0040 decision 20, canvas "Webhooks"). The list and the factories load
// with the page: its own loading and error states show in the page, under the header.
export const Route = createFileRoute('/o/$org/_org/webhooks/')({
  pendingComponent: () => <PendingSkeleton placeholder={<WebhooksShape />} />,
  loader: ({ context: { queryClient, org } }) => {
    void queryClient.prefetchQuery(webhooksQuery(org.id));
    void queryClient.prefetchQuery(factoriesQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('Webhooks', context.org.name),
  component: WebhooksPage,
});
