import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { EvalsPage } from '../features/jonah/evals-page';

// Jonah's Evals (ADR 0025): the factory's eval cases and how their last tries went. The page reads the
// cases itself and shows its own loading and error states, so the route never waits on them.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/evals')({
  head: ({ match: { context } }) => pageTitle('Evals', context.factory.name),
  component: EvalsPage,
});
