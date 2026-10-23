import { createFileRoute } from '@tanstack/react-router';
import { boardQuery, metricsQuery } from '../api/queries';
import { FloorPage } from '../features/floor/floor-page';

export const Route = createFileRoute('/f/$factory/')({
  loader: ({ context: { queryClient, factory } }) =>
    Promise.all([
      queryClient.ensureQueryData(boardQuery(factory.name)),
      queryClient.ensureQueryData(metricsQuery(factory.name)),
    ]),
  component: FloorPage,
});
