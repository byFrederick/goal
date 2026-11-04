import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { JonahOverviewPage, JonahPending } from '../features/jonah/jonah-page';

// Jonah's Overview (ADR 0039): what waits for a person, what happened after each publish, what Jonah noticed and
// how it works here. Proposals and Tests are the tabs beside it, each at its own URL. The page reads its own data
// and shows its own states, so the route never waits on it.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/')({
  pendingComponent: JonahPending,
  head: ({ match: { context } }) => pageTitle('Jonah', context.factory.name),
  component: JonahOverviewPage,
});
