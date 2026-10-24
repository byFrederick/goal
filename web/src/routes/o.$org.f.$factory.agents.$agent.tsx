import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { agentStatsQuery, versionModelQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen } from '../app/states';
import { AgentScreen } from '../features/agents/agent-screen';

// $agent is the agent's name as the factory's definition declares it. E owns the page and its
// statistics; D12 adds the definition's cards (skills, tool policy), read from the version that
// runs, which loads beside the statistics (a failure shows in its cards, never blocks the page).
export const Route = createFileRoute('/o/$org/f/$factory/agents/$agent')({
  loader: async ({ context: { queryClient, org, factory }, params }) => {
    const running = queryClient.prefetchQuery(versionModelQuery(org.id, factory.id, 'current'));
    try {
      await queryClient.ensureQueryData(agentStatsQuery(org.id, factory.id, params.agent));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
    await running;
  },
  head: ({ match: { context }, params }) => pageTitle(params.agent, context.factory.name, context.org.name),
  component: AgentScreen,
  notFoundComponent: () => <NotFoundScreen what="agent" />,
});
