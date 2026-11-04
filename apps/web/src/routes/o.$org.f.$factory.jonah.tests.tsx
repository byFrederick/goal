import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { JonahPending } from '../features/jonah/jonah-page';
import { TestsPage } from '../features/jonah/tests-page';

// Jonah's Tests tab (ADR 0039 decision 12): the past items kept as tests, by station. The page reads its own
// data and shows its own states.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/tests')({
  pendingComponent: JonahPending,
  head: ({ match: { context } }) => pageTitle('Tests', context.factory.name),
  component: TestsPage,
});
