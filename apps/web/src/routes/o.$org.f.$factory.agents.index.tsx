import { createFileRoute } from '@tanstack/react-router';
import { agentStatsQuery, ensureLoaded, versionModelQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { agentNamesOf } from '../features/agents/agents-line';
import { AgentsPage } from '../features/agents/agents-page';

// The Agents page: the running version's line, with each agent's statistics loaded beside it. Both are
// awaited, so the page renders whole; a failure shows the route's error screen with its retry.
export const Route = createFileRoute('/o/$org/f/$factory/agents/')({
  loader: async ({ context: { queryClient, org, factory } }) => {
    const version = await ensureLoaded(queryClient, versionModelQuery(org.id, factory.id, 'current'));
    await Promise.all(
      agentNamesOf(version.model).map((agent) => ensureLoaded(queryClient, agentStatsQuery(org.id, factory.id, agent))),
    );
  },
  head: ({ match: { context } }) => pageTitle('Agents', context.factory.name, context.org.name),
  component: AgentsPage,
});
