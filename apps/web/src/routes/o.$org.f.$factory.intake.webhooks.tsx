import { createFileRoute } from '@tanstack/react-router';
import { webhooksQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { WebhooksPage } from '../features/webhooks/webhooks-page';

// The factory's Webhooks page (docs/design/screens/intake/webhooks.html): the endpoints other systems post
// to, with their triggers, their last week and their newest deliveries. The list loads with the page:
// its own loading and error states show in the page, under the header.
export const Route = createFileRoute('/o/$org/f/$factory/intake/webhooks')({
  loader: ({ context: { queryClient, org, factory } }) => {
    void queryClient.prefetchQuery(webhooksQuery(org.id, factory.id));
  },
  head: ({ match: { context } }) => pageTitle('Webhooks', context.factory.name, context.org.name),
  component: WebhooksPage,
});
