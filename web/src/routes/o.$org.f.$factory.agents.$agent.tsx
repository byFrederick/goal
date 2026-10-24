import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { agentStatsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen } from '../app/states';
import { AgentPage } from '../features/agents/agent-page';

// $agent is the agent's name as the factory's definition declares it. E owns the page and its
// statistics; the definition's cards (connections, skills, tool policy) join it in D12.
export const Route = createFileRoute('/o/$org/f/$factory/agents/$agent')({
  loader: async ({ context: { queryClient, org, factory }, params }) => {
    try {
      await queryClient.ensureQueryData(agentStatsQuery(org.id, factory.id, params.agent));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  head: ({ match: { context }, params }) => pageTitle(params.agent, context.factory.name, context.org.name),
  component: AgentPage,
  notFoundComponent: () => <NotFoundScreen what="agent" />,
});
