// Where an agent works on the line: the agents list's "Investigate, × 2", "Build, frontend lane",
// "Review, gate" and "Done" (the librarian), and the stations that keep it from being removed.
import type { LineSection, ModelStation } from './model';

export interface Workplace {
  /** The station's id. */
  station: string;
  /** The station's name, else its id. */
  label: string;
  /** How the agent is placed there. */
  how: 'agent' | 'lane' | 'librarian';
  /** The lanes routed to the agent (how = lane). */
  lanes: string[];
  /** Parallel agents at the station (how = agent). */
  count: number;
  /** The station sends work back when it rejects. */
  gate: boolean;
}

const label = (s: ModelStation) => s.name || s.id;

/** Every place the agent works, in line order. */
export function whereAgentWorks(line: LineSection, agent: string): Workplace[] {
  const out: Workplace[] = [];
  for (const s of line.stations ?? []) {
    const gate = s.gate?.on_reject !== undefined;
    if (s.agent === agent) {
      out.push({ station: s.id, label: label(s), how: 'agent', lanes: [], count: s.agents ?? 1, gate });
      continue;
    }
    const lanes = Object.entries(s.route ?? {})
      .filter(([, a]) => a === agent)
      .map(([lane]) => lane);
    if (lanes.length > 0) out.push({ station: s.id, label: label(s), how: 'lane', lanes, count: 1, gate });
    else if (s.kind === 'done' && line.librarian === agent) {
      out.push({ station: s.id, label: label(s), how: 'librarian', lanes: [], count: 1, gate: false });
    }
  }
  return out;
}

const and = (words: string[]) =>
  words.length < 2 ? (words[0] ?? '') : `${words.slice(0, -1).join(', ')} and ${words.at(-1)}`;

/** One place as the agents list says it. */
export function workplaceLabel(w: Workplace): string {
  if (w.how === 'lane') return `${w.label}, ${and(w.lanes)} ${w.lanes.length === 1 ? 'lane' : 'lanes'}`;
  if (w.how === 'agent' && w.count > 1) return `${w.label}, × ${w.count}`;
  if (w.gate) return `${w.label}, gate`;
  return w.label;
}

/** Where the agent works, every place joined, or "Not on the line". */
export function whereLabel(line: LineSection, agent: string): string {
  const places = whereAgentWorks(line, agent);
  return places.length > 0 ? places.map(workplaceLabel).join(' · ') : 'Not on the line';
}

/** The stations that use the agent (by agent, lane or as the librarian): Remove is blocked by them. */
export function stationsUsing(line: LineSection, agent: string): ModelStation[] {
  const ids = new Set(whereAgentWorks(line, agent).map((w) => w.station));
  return (line.stations ?? []).filter((s) => ids.has(s.id));
}
