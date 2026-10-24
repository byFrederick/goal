import { createFileRoute } from '@tanstack/react-router';
import { boardQuery } from '../api/queries';
import { BoardPage } from '../features/board/board-page';

interface BoardSearch {
  /** The station shown on phones; the constraint when absent. */
  station?: string;
}

export const Route = createFileRoute('/f/$factory/board')({
  validateSearch: (search: Record<string, unknown>): BoardSearch =>
    typeof search.station === 'string' && search.station !== '' ? { station: search.station } : {},
  loader: ({ context: { queryClient, org, factory } }) => queryClient.ensureQueryData(boardQuery(org.id, factory.name)),
  component: BoardPage,
});
