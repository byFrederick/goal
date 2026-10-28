// The views of the Agent page, kept in the URL as ?view= (MASTER.md, tabs). An unknown or missing
// value reads as Overview, so a link never shows an empty page.
export const agentViews = ['overview', 'prompt', 'tools', 'runs'] as const;

export type AgentView = (typeof agentViews)[number];

export interface AgentSearch {
  /** Optional so that links to the page need no search; the route always reads a view. */
  view?: AgentView;
}

export function agentViewOf(value: unknown): AgentView {
  return (agentViews as readonly unknown[]).includes(value) ? (value as AgentView) : 'overview';
}
