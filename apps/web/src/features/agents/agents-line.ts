// The line the Agents page draws, read from the running version's model. Nothing here names a station,
// a lane or an agent: the definition says who staffs what, and these functions only arrange it.
import type { FactoryModel } from '../../api/types';
import { type Model, type ModelAgent, type ModelStation, modelOf } from '../../shared/definition/model';

/** The agents the running version defines, in the order its file declares them. */
export function agentsOf(model: Model): ModelAgent[] {
  return model.agents.agents ?? [];
}

/** Each defined agent's name once: the names the statistics are read for. */
export function agentNamesOf(model: FactoryModel): string[] {
  return [...new Set(agentsOf(modelOf(model)).map((agent) => agent.name))];
}

/** One agent's place at a station: the station's own agent, or the agent a lane routes to. */
export interface Staffing {
  /** Unique within the line: the station's id, with the lane when the station routes by lane. */
  key: string;
  /** The lane the agent takes work from, when the station routes by lane. */
  lane?: string;
  agent: ModelAgent;
}

/** The places at a station, in the order the definition gives them; a station with no agent defined has none. */
export function staffingOf(station: ModelStation, agents: ModelAgent[]): Staffing[] {
  const find = (name: string | undefined) => agents.find((agent) => agent.name === name);
  if (station.route) {
    return Object.entries(station.route).flatMap(([lane, name]) => {
      const agent = find(name);
      return agent ? [{ key: `${station.id}/${lane}`, lane, agent }] : [];
    });
  }
  const agent = find(station.agent);
  return agent ? [{ key: station.id, agent }] : [];
}

/** The line's stations, in line order, as the definition declares them. */
export function stationsOf(model: Model): ModelStation[] {
  return model.line.stations ?? [];
}

/** The first agent the station after `index` names: what the items waiting before it are waiting for. */
export function nextAgentName(stations: ModelStation[], index: number): string | undefined {
  for (const station of stations.slice(index + 1)) {
    if (station.kind === 'agent' && station.agent) return station.agent;
  }
  return undefined;
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** The page's summary sentence: how many roles and agents staff the factory's line. */
export function lineSentence(roles: number, positions: number, title: string): string {
  const tail = 'Each station decides who works on an item.';
  if (roles === 0) return `No agents staff ${title}'s line yet. ${tail}`;
  return `${count(roles, 'role', 'roles')} and ${count(positions, 'agent', 'agents')} staff ${title}'s line. ${tail}`;
}

/** A value of the definition's runtime size as the page shows it: "Large". Medium when the file leaves it out. */
export function sizeLabel(size: string | undefined): string {
  const value = size ?? 'medium';
  return value.charAt(0).toUpperCase() + value.slice(1);
}

/** The two letters a card's mark shows: the first letter of the agent's name's first two words. */
export function initialsOf(name: string): string {
  return name
    .split(/[-_\s]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word.charAt(0).toUpperCase())
    .join('');
}
