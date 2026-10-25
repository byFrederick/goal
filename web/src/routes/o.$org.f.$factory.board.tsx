import { createFileRoute } from '@tanstack/react-router';
import { boardQuery, ensureLoaded, touchesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { BoardPage } from '../features/board/board-page';

interface BoardSearch {
  /** The station shown on phones; the constraint when absent. */
  station?: string;
}

export const Route = createFileRoute('/o/$org/f/$factory/board')({
  validateSearch: (search: Record<string, unknown>): BoardSearch =>
    typeof search.station === 'string' && search.station !== '' ? { station: search.station } : {},
  // The touch metrics only add each station's rate: fetched alongside, never waited for.
  loader: ({ context: { queryClient, org, factory } }) => {
    void queryClient.prefetchQuery(touchesQuery(org.id, factory.id));
    return ensureLoaded(queryClient, boardQuery(org.id, factory.id));
  },
  head: ({ match: { context } }) => pageTitle('Board', context.factory.name, context.org.name),
  component: BoardPage,
});
