import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { JudgesPage } from '../features/jonah/judges-page';

// Jonah's Judges (ADR 0025): the factory's judges of the current version and their last 30 days. The page
// reads the judges itself and shows its own loading and error states, so the route never waits on them.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/judges/')({
  head: ({ match: { context } }) => pageTitle('Judges', context.factory.name),
  component: JudgesPage,
});
