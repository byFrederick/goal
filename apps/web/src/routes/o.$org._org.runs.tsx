import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { RunsPage } from '../features/runs/runs-page';
import { type RunsSearch, runsSearch } from '../features/runs/search';

// The organization's agent runs across its factories (the Runs page, ADR 0016): ?factory=, ?status=,
// ?range= and ?q= filter the list, and the page reads them from the URL on every visit.
export const Route = createFileRoute('/o/$org/_org/runs')({
  validateSearch: (search: Record<string, unknown>): RunsSearch => runsSearch(search),
  head: ({ match: { context } }) => pageTitle('Runs', context.org.name),
  component: RunsPage,
});
