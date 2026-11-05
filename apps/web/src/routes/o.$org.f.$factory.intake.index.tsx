import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { IntakePage } from '../features/intake/intake-page';
import { IntakeSkeleton } from '../features/intake/intake-skeleton';

// The Intake page (intake): what is coming up, where items came from, the follow-ups to triage, the triggers
// and the webhooks. Its states are the page's own: the route does not wait for the data, so loading and error
// show on the page.
export const Route = createFileRoute('/o/$org/f/$factory/intake/')({
  pendingComponent: IntakeSkeleton,
  head: ({ match: { context } }) => pageTitle('Intake', context.factory.name, context.org.name),
  component: IntakeRoute,
});

function IntakeRoute() {
  return <IntakePage />;
}
