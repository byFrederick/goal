import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { JonahPending, JonahProposalsPage } from '../features/jonah/jonah-page';
import { type JonahSearch, jonahSearchOf } from '../features/jonah/jonah-search';
import { factoryTitle } from '../shared/factory';

// Jonah's Proposals tab (ADR 0039): the factory's proposals, filtered by ?state=. The page reads its own data and
// shows its own states, so the route never waits on it.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/proposals/')({
  pendingComponent: JonahPending,
  validateSearch: (search: Record<string, unknown>): JonahSearch => jonahSearchOf(search),
  head: ({ match: { context } }) => pageTitle('Proposals', factoryTitle(context.factory)),
  component: ProposalsRoute,
});

function ProposalsRoute() {
  const { state } = Route.useSearch();
  return <JonahProposalsPage state={state} />;
}
