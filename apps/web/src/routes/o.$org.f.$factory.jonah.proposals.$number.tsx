import { createFileRoute, notFound } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { ProposalPage } from '../features/jonah/proposal-page';

// One of Jonah's proposals (ADR 0017): its files as diffs, the sent-back runs behind it, and Approve, Edit and
// Reject for a person to decide. The page reads the proposal itself and shows its own states. A number that is
// not a whole number is not a proposal, so the route answers not found.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/proposals/$number')({
  beforeLoad: ({ params }) => {
    if (!/^\d+$/.test(params.number)) throw notFound();
  },
  head: ({ match: { context } }) => pageTitle('Proposal', context.factory.name),
  component: ProposalRoute,
});

function ProposalRoute() {
  const { number } = Route.useParams();
  return <ProposalPage number={Number(number)} />;
}
