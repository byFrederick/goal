import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { JudgePage } from '../features/jonah/judge-page';

// One judge of the current version (ADR 0025): its file, its 30 days and its newest scores, edited and saved
// as a new version. The page reads the judge itself and shows its own states.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/judges/$judge')({
  head: ({ match: { context } }) => pageTitle('Judge', context.factory.name),
  component: JudgeRoute,
});

function JudgeRoute() {
  const { judge } = Route.useParams();
  return <JudgePage slug={judge} />;
}
