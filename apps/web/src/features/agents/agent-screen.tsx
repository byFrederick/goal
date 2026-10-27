// The Agent page as the route shows it: the agent and the tab its ?view= names (validated in the
// route). The page itself reads the statistics and the running version (agent-page.tsx).
import { useParams, useSearch } from '@tanstack/react-router';
import { AgentPage } from './agent-page';

export function AgentScreen() {
  const { agent } = useParams({ from: '/o/$org/f/$factory/agents/$agent' });
  const { view } = useSearch({ from: '/o/$org/f/$factory/agents/$agent' });
  return <AgentPage agent={agent} view={view ?? 'overview'} />;
}
