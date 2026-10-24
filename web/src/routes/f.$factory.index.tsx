import { createFileRoute } from '@tanstack/react-router';
import { boardQuery, metricsQuery } from '../api/queries';
import { FloorPage } from '../features/floor/floor-page';

export const Route = createFileRoute('/f/$factory/')({
  loader: ({ context: { queryClient, org, factory } }) =>
    Promise.all([
      queryClient.ensureQueryData(boardQuery(org.id, factory.name)),
      queryClient.ensureQueryData(metricsQuery(org.id, factory.name)),
    ]),
  component: FloorPage,
});
