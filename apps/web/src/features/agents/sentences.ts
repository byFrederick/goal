// What the agent page says in words (E §10.2), from AgentStats alone: never an agent's or a
// station's name in code.
import type { AgentStats } from '../../api/types';
import { humanize } from '../../shared/format';

const words = ['No', 'One', 'Two', 'Three', 'Four', 'Five', 'Six', 'Seven', 'Eight', 'Nine', 'Ten'];

function copies(n: number): string {
  const count = n <= 10 ? (words[n] ?? String(n)) : String(n);
  return n === 1 ? `${count} copy runs` : `${count} copies run`;
}

function list(names: string[]): string {
  return names.length <= 1 ? (names[0] ?? '') : `${names.slice(0, -1).join(', ')} and ${names.at(-1)}`;
}

/** "Three copies run at Investigate, the current constraint, on claude-sonnet-5-5." */
export function agentSentence({ stations, model }: Pick<AgentStats, 'stations' | 'model'>): string {
  if (stations.length === 0) return 'No station of the line uses this agent.';
  const where =
    stations.length === 1
      ? `${copies(stations[0]?.slots ?? 0)} at ${humanize(stations[0]?.id ?? '')}`
      : `Works at ${list(stations.map((s) => humanize(s.id)))}`;
  const constraint = stations.some((s) => s.constraint) ? ', the current constraint' : '';
  return `${where}${constraint}${model ? `, on ${model}` : ''}.`;
}

/** "Blocked this week: 2 calls, both aws ssm put-parameter." */
export function blockedSentence({ count, tools }: AgentStats['denied_tool_calls']): string {
  const top = tools[0];
  if (count === 0 || !top) return 'Blocked this week: no calls.';
  if (count === 1) return `Blocked this week: 1 call, ${top.tool}.`;
  if (top.count === count) return `Blocked this week: ${count} calls, ${count === 2 ? 'both' : 'all'} ${top.tool}.`;
  return `Blocked this week: ${count} calls, most often ${top.tool} (${top.count}).`;
}

/** A reason's bar length: its count against the most frequent reason's. */
export function reasonShare(reason: { count: number }, reasons: readonly { count: number }[]): number {
  const max = Math.max(0, ...reasons.map((r) => r.count));
  return max > 0 ? reason.count / max : 0;
}
