// Keeping your place between Visual and YAML (D §10.9), over B's `ranges` (model pointer →
// first and last line of factory.yaml). YAML → Visual: the innermost pointer whose lines hold
// the cursor, then the section and entity it is in (/line/stations/3 → line?station=build).
// Visual → YAML: the entity's pointer, then its first line. An agent's prompt file maps to
// agents?agent=x and back.
import type { Ranges } from '../../api/types';
import type { Model, Section } from './model';
import { sectionOfPointer } from './model';

/** The innermost pointer whose lines hold `line`: the smallest span, then the deepest. */
export function pointerAt(ranges: Ranges, line: number): string | undefined {
  let best: string | undefined;
  let span = Number.POSITIVE_INFINITY;
  for (const [pointer, [from = 0, to = 0]] of Object.entries(ranges)) {
    if (from <= 0 || line < from || line > to) continue;
    const s = to - from;
    if (s < span || (s === span && best !== undefined && pointer.length > best.length)) {
      best = pointer;
      span = s;
    }
  }
  return best;
}

/** The first line of a pointer, or of its nearest parent that has lines. */
export function firstLine(ranges: Ranges, pointer: string): number | undefined {
  for (let p = pointer; p; p = p.slice(0, p.lastIndexOf('/'))) {
    const from = ranges[p]?.[0] ?? 0;
    if (from > 0) return from;
  }
  return undefined;
}

/** A section and the entity in it, as the editor's search params name them. */
export interface Place {
  section: Section;
  station?: string;
  agent?: string;
  connection?: string;
}

/** The place a model pointer is in; undefined outside the model. */
export function placeOf(model: Model, pointer: string): Place | undefined {
  const section = sectionOfPointer(pointer);
  if (!section) return undefined;
  const toks = pointer.split('/').slice(2);
  const index = Number(section === 'connections' ? toks[0] : toks[1]);
  if (section === 'line' && toks[0] === 'stations') {
    const id = model.line.stations?.[index]?.id;
    return id ? { section, station: id } : { section };
  }
  if (section === 'agents' && toks[0] === 'agents') {
    const name = model.agents.agents?.[index]?.name;
    return name ? { section, agent: name } : { section };
  }
  if (section === 'connections') {
    const name = model.connections[index]?.name;
    return name ? { section, connection: name } : { section };
  }
  return { section };
}

/** The pointer of a place: its entity's, else its section's. */
export function pointerOf(model: Model, place: Place): string {
  const at = (i: number, prefix: string) => (i >= 0 ? `${prefix}/${i}` : `/${place.section}`);
  if (place.section === 'line' && place.station) {
    return at(model.line.stations?.findIndex((s) => s.id === place.station) ?? -1, '/line/stations');
  }
  if (place.section === 'agents' && place.agent) {
    return at(model.agents.agents?.findIndex((a) => a.name === place.agent) ?? -1, '/agents/agents');
  }
  if (place.section === 'connections' && place.connection) {
    return at(
      model.connections.findIndex((c) => c.name === place.connection),
      '/connections',
    );
  }
  return `/${place.section}`;
}

/** A definition path as the draft's files list names it: "./agents/x.md" → "agents/x.md". */
export function cleanPath(path: string): string {
  return path.replace(/^(\.\/)+/, '');
}

/** The agent whose prompt a file is; undefined for any other file. */
export function placeOfFile(model: Model, path: string): Place | undefined {
  const file = cleanPath(path);
  const agent = model.agents.agents?.find((a) => a.prompt && cleanPath(a.prompt.path) === file);
  return agent ? { section: 'agents', agent: agent.name } : undefined;
}

/**
 * Where Visual opens from a place in the YAML view: the entity at a line of factory.yaml, or the
 * agent whose prompt a file is; undefined when the line is outside the model.
 */
export function placeAtLine(model: Model, ranges: Ranges, file: string, line: number): Place | undefined {
  if (cleanPath(file) !== 'factory.yaml') return placeOfFile(model, file);
  const pointer = pointerAt(ranges, line);
  return pointer ? placeOf(model, pointer) : undefined;
}

/** Where the YAML view opens from a place in Visual: its entity's first line, else its section's. */
export function lineOfPlace(model: Model, ranges: Ranges, place: Place): number | undefined {
  return firstLine(ranges, pointerOf(model, place));
}

/** The prompt file of an agent, as the draft's files list names it. */
export function promptFile(model: Model, agent: string): string | undefined {
  const path = model.agents.agents?.find((a) => a.name === agent)?.prompt?.path;
  return path ? cleanPath(path) : undefined;
}
