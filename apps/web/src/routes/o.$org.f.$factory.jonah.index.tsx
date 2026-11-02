import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { JonahOverviewPage, JonahPending, JonahProposalsPage } from '../features/jonah/jonah-page';
import { type JonahSearch, jonahSearchOf } from '../features/jonah/jonah-search';

// Jonah's overview (ADR 0034): what waits for a person, the policy and what Jonah noticed. ?tab=proposals
// opens the Proposals tab, filtered by ?state=. The pages read their own data and show their own states, so
// the route never waits on them.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/')({
  pendingComponent: JonahPending,
  pendingMs: 100,
  pendingMinMs: 300,
  validateSearch: (search: Record<string, unknown>): JonahSearch => jonahSearchOf(search),
  head: ({ match: { context } }) => pageTitle('Jonah', context.factory.name),
  component: JonahRoute,
});

function JonahRoute() {
  const { tab, state } = Route.useSearch();
  return tab === 'proposals' ? <JonahProposalsPage state={state} /> : <JonahOverviewPage />;
}
