// The Agent page as the route shows it: E12's page with the definition's cards (D12), read from
// the version that runs. "Blocked this week" sits under the Tool policy card (Agent board).
import { useSuspenseQuery } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { agentStatsQuery } from '../../api/queries';
import {
  DraftChangesNote,
  EditInDefinition,
  SkillsCard,
  ToolPolicyCard,
  useRunningModel,
} from '../../shared/definition/agent-cards';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { AgentPage, BlockedCalls } from './agent-page';

export function AgentScreen() {
  const org = useOrg();
  const factory = useFactory();
  const { agent } = useParams({ from: '/o/$org/f/$factory/agents/$agent' });
  const { data: stats } = useSuspenseQuery(agentStatsQuery(org.id, factory.id, agent));
  const role = useRunningModel().data?.agents.agents?.find((a) => a.name === agent)?.description;

  return (
    <AgentPage
      role={role}
      actions={<EditInDefinition agent={agent} />}
      cards={
        <>
          <DraftChangesNote agent={agent} className="col-span-full" />
          <SkillsCard agent={agent} />
          <ToolPolicyCard agent={agent}>
            <BlockedCalls stats={stats} />
          </ToolPolicyCard>
        </>
      }
    />
  );
}
