import { createFileRoute } from '@tanstack/react-router';
import { boardQuery, metricsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { FloorPage } from '../features/floor/floor-page';

export const Route = createFileRoute('/o/$org/f/$factory/')({
  loader: ({ context: { queryClient, org, factory } }) =>
    Promise.all([
      queryClient.ensureQueryData(boardQuery(org.id, factory.id)),
      queryClient.ensureQueryData(metricsQuery(org.id, factory.id)),
    ]),
  head: ({ match: { context } }) => pageTitle('Floor', context.factory.name, context.org.name),
  component: FloorPage,
});
