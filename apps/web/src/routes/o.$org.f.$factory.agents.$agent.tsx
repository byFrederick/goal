import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { agentStatsQuery, ensureLoaded, versionModelQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen } from '../app/states';
import { AgentScreen } from '../features/agents/agent-screen';
import { type AgentSearch, agentViewOf } from '../features/agents/views';

// $agent is the agent's name as the factory's definition declares it. The page reads its statistics
// here; the definition's cards (skills, tool policy) read the version that runs and show their own
// errors, so they never block the page.
export const Route = createFileRoute('/o/$org/f/$factory/agents/$agent')({
  // ?view= picks the page's tab (Overview, Prompt, Tools and access, Runs); anything else is Overview.
  validateSearch: (search: Record<string, unknown>): AgentSearch => ({ view: agentViewOf(search.view) }),
  loader: async ({ context: { queryClient, org, factory }, params }) => {
    const running = queryClient.prefetchQuery(versionModelQuery(org.id, factory.id, 'current'));
    try {
      await ensureLoaded(queryClient, agentStatsQuery(org.id, factory.id, params.agent));
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
