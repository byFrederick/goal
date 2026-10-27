import { createFileRoute } from '@tanstack/react-router';
import { boardQuery, ensureLoaded, touchesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { type BoardGroup, type BoardView, statusChoices } from '../features/board/board-filters';
import { BoardPage } from '../features/board/board-page';
import type { StatusKind } from '../ui/status-icon';

interface BoardSearch extends BoardView {
  /** The station shown on phones; the constraint when absent. */
  station?: string | undefined;
  /** The assembly (the item id) the cards are filtered to; "Any" when absent. */
  assembly?: string | undefined;
  /** The lane role the cards are filtered to; "Any" when absent. */
  role?: string | undefined;
  /** The search: the cards whose title or number match; the search box is empty when absent. */
  q?: string | undefined;
  /** The work type the cards are filtered to; "Any" when absent. */
  type?: string | undefined;
  /** The work lane the cards are filtered to; "Any" when absent. */
  lane?: string | undefined;
  /** The status the cards are filtered to; "Any" when absent. */
  status?: StatusKind | undefined;
  /** How the cards of a station are grouped; "None" when absent. */
  group?: BoardGroup | undefined;
}

const text = (value: unknown): string | undefined => (typeof value === 'string' && value !== '' ? value : undefined);

export const Route = createFileRoute('/o/$org/f/$factory/board')({
  validateSearch: (search: Record<string, unknown>): BoardSearch => {
    const valid: BoardSearch = {};
    const station = text(search.station);
    const assembly = text(search.assembly);
    const role = text(search.role);
    const q = text(search.q);
    const type = text(search.type);
    const lane = text(search.lane);
    const status = statusChoices.find((s) => s.value === search.status)?.value;
    if (station) valid.station = station;
    if (assembly) valid.assembly = assembly;
    if (role) valid.role = role;
    if (q) valid.q = q;
    if (type) valid.type = type;
    if (lane) valid.lane = lane;
    if (status) valid.status = status;
    if (search.group === 'assembly') valid.group = 'assembly';
    return valid;
  },
  // The touch metrics only add each station's rate: fetched alongside, never waited for.
  loader: ({ context: { queryClient, org, factory } }) => {
    void queryClient.prefetchQuery(touchesQuery(org.id, factory.id));
    return ensureLoaded(queryClient, boardQuery(org.id, factory.id));
  },
  head: ({ match: { context } }) => pageTitle('Board', context.factory.name, context.org.name),
  component: BoardPage,
});
