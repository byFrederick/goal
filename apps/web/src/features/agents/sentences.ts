// What the agent page says in words (E §10.2), from AgentStats alone: never an agent's or a
// station's name in code.
import type { AgentStats } from '../../api/types';

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
