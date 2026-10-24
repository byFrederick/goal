// The editor's edits as pure, typed helpers (D §10.4): each takes the section value(s) it
// changes and returns an Edit, the section's new value with the files to write (and remove) in
// the same section save, so B stores them in one revision (an added agent's prompt, a renamed
// prompt, a vendored skill). Inputs are never mutated. Edits that touch several sections (rename
// or remove an agent, remove a connection) return one Edit per section touched.
import type { FileMap, ProblemFix, SkillFiles } from '../../../api/types';
import type {
  AgentsSection,
  BasicsSection,
  Budget,
  BuiltinKey,
  BuiltinMode,
  Builtins,
  ConnectionsSection,
  Flow,
  Gate,
  LineSection,
  Model,
  ModelAgent,
  ModelSkill,
  ModelStation,
  ReviewSection,
  Section,
  SectionValues,
  StationCheck,
  StationKind,
} from '../../../shared/definition/model';
import { cleanPath } from '../../../shared/definition/place';
import { stationsUsing } from '../../../shared/definition/where';
import { getPointer, pointerTokens, setPointer } from './pointer';

/** One section's new value, with the files its save writes and removes. */
export interface Edit<S extends Section = Section> {
  section: S;
  value: SectionValues[S];
  files?: FileMap;
  removeFiles?: string[];
}

/** An edit of any section, narrowed by `section`. */
export type AnyEdit = { [S in Section]: Edit<S> }[Section];

const edit = <S extends Section>(
  section: S,
  value: SectionValues[S],
  files?: FileMap,
  removeFiles?: string[],
): Edit<S> => ({
  section,
  value,
  ...(files && Object.keys(files).length > 0 ? { files } : {}),
  ...(removeFiles && removeFiles.length > 0 ? { removeFiles } : {}),
});

/** A slug of a name: lowercase letters, digits and dashes ("Approve spec" → approve-spec). */
export function slug(name: string): string {
  return (
    name
      .normalize('NFKD')
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 63) || 'station'
  );
}

/** base, or base-2, base-3 … when taken. */
export function uniqueName(base: string, taken: Iterable<string>): string {
  const used = new Set(taken);
  if (!used.has(base)) return base;
  for (let n = 2; ; n++) if (!used.has(`${base}-${n}`)) return `${base}-${n}`;
}

// Line.

const stationsOf = (line: LineSection) => line.stations ?? [];

const withStations = (line: LineSection, stations: ModelStation[]): Edit<'line'> => edit('line', { ...line, stations });

function mapStation(line: LineSection, id: string, f: (s: ModelStation) => ModelStation): Edit<'line'> {
  return withStations(
    line,
    stationsOf(line).map((s) => (s.id === id ? f(s) : s)),
  );
}

/** Sets (or, with undefined, removes) one field of a station. */
export function setStationField<K extends Exclude<keyof ModelStation, 'id' | 'kind'>>(
  line: LineSection,
  id: string,
  field: K,
  value: ModelStation[K] | undefined,
): Edit<'line'> {
  return mapStation(line, id, (s) => {
    const { [field]: _old, ...rest } = s;
    return (value === undefined ? rest : { ...rest, [field]: value }) as ModelStation;
  });
}

/**
 * Moves a station to index `to`; undefined when the move is not allowed: Done stays last, nothing
 * passes it, and a move to where the station is changes nothing.
 */
export function moveStation(line: LineSection, id: string, to: number): Edit<'line'> | undefined {
  const stations = stationsOf(line);
  const from = stations.findIndex((s) => s.id === id);
  const station = stations[from];
  if (!station || from === to || to < 0 || to >= stations.length || station.kind === 'done') return undefined;
  const lastIsDone = stations.at(-1)?.kind === 'done';
  if (lastIsDone && to >= stations.length - 1) return undefined;
  const next = [...stations];
  next.splice(from, 1);
  next.splice(to, 0, station);
  return withStations(line, next);
}

const newStation = (kind: Exclude<StationKind, 'done'>, id: string, name: string): ModelStation =>
  kind === 'agent' ? { id, name, kind, wip: 1 } : { id, name, kind };

/** Adds a station after `after`, else before Done (else at the end), its id the slug of its name. */
export function addStation(
  line: LineSection,
  kind: Exclude<StationKind, 'done'>,
  { name, after }: { name: string; after?: string },
): { edit: Edit<'line'>; station: ModelStation } {
  const stations = stationsOf(line);
  const station = newStation(
    kind,
    uniqueName(
      slug(name),
      stations.map((s) => s.id),
    ),
    name,
  );
  const doneAt = stations.findIndex((s) => s.kind === 'done');
  const afterAt = after === undefined ? -1 : stations.findIndex((s) => s.id === after);
  let at = afterAt >= 0 ? afterAt + 1 : doneAt >= 0 ? doneAt : stations.length;
  if (doneAt >= 0 && at > doneAt) at = doneAt;
  const next = [...stations];
  next.splice(at, 0, station);
  return { edit: withStations(line, next), station };
}

/**
 * Copies a station right after it, as "<name> copy" with the slug of that as its id; Done is not
 * copied (a line has one end).
 */
export function duplicateStation(
  line: LineSection,
  id: string,
): { edit: Edit<'line'>; station: ModelStation } | undefined {
  const stations = stationsOf(line);
  const at = stations.findIndex((s) => s.id === id);
  const source = stations[at];
  if (!source || source.kind === 'done') return undefined;
  const name = `${source.name || source.id} copy`;
  const station: ModelStation = {
    ...structuredClone(source),
    id: uniqueName(
      slug(name),
      stations.map((s) => s.id),
    ),
    name,
  };
  const next = [...stations];
  next.splice(at + 1, 0, station);
  return { edit: withStations(line, next), station };
}

/** Rewrites what names a station: other stations' gates and the flow's constraint. */
function renameStationRefs(line: LineSection, from: string, to: string | undefined): LineSection {
  const stations = stationsOf(line).map((s) => {
    if (s.gate?.on_reject !== from) return s;
    if (to === undefined) {
      const { gate: _gate, ...rest } = s;
      return rest;
    }
    return { ...s, gate: { ...s.gate, on_reject: to } };
  });
  let flow = line.flow;
  if (flow?.constraint === from) {
    const { constraint: _c, ...rest } = flow;
    flow = to === undefined ? rest : { ...flow, constraint: to };
  }
  return { ...line, stations, ...(flow ? { flow } : {}) };
}

/** Removes a station and what pointed at it (a gate sending work there, the constraint). */
export function removeStation(line: LineSection, id: string): Edit<'line'> {
  const without = { ...line, stations: stationsOf(line).filter((s) => s.id !== id) };
  return edit('line', renameStationRefs(without, id, undefined));
}

/**
 * Renames a station. While the station is new (absent from the base version, `followId`), its id
 * follows the slug of its name and every reference moves with it; after that the id is fixed.
 */
export function setStationName(
  line: LineSection,
  id: string,
  name: string,
  { followId }: { followId: boolean },
): Edit<'line'> {
  const named = mapStation(line, id, (s) => ({ ...s, name })).value;
  if (!followId) return edit('line', named);
  const others = stationsOf(line)
    .map((s) => s.id)
    .filter((x) => x !== id);
  const next = uniqueName(slug(name), others);
  if (next === id) return edit('line', named);
  const moved = { ...named, stations: stationsOf(named).map((s) => (s.id === id ? { ...s, id: next } : s)) };
  return edit('line', renameStationRefs(moved, id, next));
}

/** The fields each kind of station keeps. */
const kindFields: Record<StationKind, readonly (keyof ModelStation)[]> = {
  queue: ['id', 'name', 'description', 'kind'],
  agent: ['id', 'name', 'description', 'kind', 'agent', 'route', 'agents', 'wip', 'buffer', 'output', 'checks', 'gate'],
  human: ['id', 'name', 'description', 'kind', 'wip', 'gate', 'action'],
  done: ['id', 'name', 'description', 'kind'],
};

/** Changes a station's kind, keeping only the fields valid for it; `dropped` names the others (the toast). */
export function setStationKind(
  line: LineSection,
  id: string,
  kind: StationKind,
): { edit: Edit<'line'>; dropped: string[] } {
  const dropped: string[] = [];
  const result = mapStation(line, id, (s) => {
    const keep = kindFields[kind];
    const next: Record<string, unknown> = {};
    for (const [k, v] of Object.entries({ ...s, kind })) {
      if (keep.includes(k as keyof ModelStation)) next[k] = v;
      else dropped.push(k);
    }
    return next as unknown as ModelStation;
  });
  return { edit: result, dropped };
}

/** Turns a human station's merge step on or off (`action: merge`). */
export function setMergeStep(line: LineSection, id: string, on: boolean): Edit<'line'> {
  return setStationField(line, id, 'action', on ? 'merge' : undefined);
}

export function addCheck(line: LineSection, id: string, check: string | StationCheck): Edit<'line'> {
  return mapStation(line, id, (s) => ({ ...s, checks: [...(s.checks ?? []), check] }));
}

export function removeCheck(line: LineSection, id: string, index: number): Edit<'line'> {
  return mapStation(line, id, (s) => {
    const checks = (s.checks ?? []).filter((_, i) => i !== index);
    const { checks: _old, ...rest } = s;
    return checks.length > 0 ? { ...rest, checks } : rest;
  });
}

/** Sets the rework rule ("send back to … at most n times"), or removes it with null. */
export function setGate(line: LineSection, id: string, gate: Gate | null): Edit<'line'> {
  return setStationField(line, id, 'gate', gate ?? undefined);
}

/** Routes a station by lane (lane → agent), or removes the routing with null. */
export function setRoute(line: LineSection, id: string, route: Record<string, string> | null): Edit<'line'> {
  return setStationField(line, id, 'route', route && Object.keys(route).length > 0 ? route : undefined);
}

export function setFlow(line: LineSection, flow: Flow): Edit<'line'> {
  return edit('line', { ...line, flow });
}

/** The agent at Done (`learning.librarian.agent`), or nobody. */
export function setLibrarian(line: LineSection, agent: string | null): Edit<'line'> {
  const { librarian: _old, ...rest } = line;
  return edit('line', agent ? { ...rest, librarian: agent } : rest);
}

// Agents.

const agentsOf = (a: AgentsSection) => a.agents ?? [];

function mapAgent(agents: AgentsSection, name: string, f: (a: ModelAgent) => ModelAgent): AgentsSection {
  return { ...agents, agents: agentsOf(agents).map((a) => (a.name === name ? f(a) : a)) };
}

const promptPathFor = (name: string) => `./agents/${name}.md`;

/** The model agents use when they name none (Basics › Advanced); undefined leaves goald's default. */
export function setDefaultModel(agents: AgentsSection, model: string | undefined): Edit<'agents'> {
  const { default_model: _old, ...rest } = agents;
  return edit('agents', model === undefined ? rest : { ...rest, default_model: model });
}

/** Adds an agent with `prompt: ./agents/<name>.md`, and that file, in the same save. */
export function addAgent(agents: AgentsSection, name: string): Edit<'agents'> {
  const path = promptPathFor(name);
  return edit(
    'agents',
    { ...agents, agents: [...agentsOf(agents), { name, prompt: { path } }] },
    {
      [cleanPath(path)]: { text: `# ${name}\n` },
    },
  );
}

/** Copies an agent as `<name>-copy` with a copy of its prompt (`promptText`, the prompt file's text). */
export function duplicateAgent(agents: AgentsSection, name: string, promptText: string): Edit<'agents'> {
  const source = agentsOf(agents).find((a) => a.name === name);
  if (!source) return edit('agents', agents);
  const copy = uniqueName(
    `${name}-copy`,
    agentsOf(agents).map((a) => a.name),
  );
  const path = promptPathFor(copy);
  const agent: ModelAgent = { ...structuredClone(source), name: copy, prompt: { path } };
  return edit(
    'agents',
    { ...agents, agents: [...agentsOf(agents), agent] },
    { [cleanPath(path)]: { text: promptText } },
  );
}

/** A skill copied with an agent from another factory: its entry there and its folder's files. */
export interface CopiedSkill {
  skill: ModelSkill;
  /** Paths relative to the skill's folder; none for a skill that is not a local folder. */
  files: FileMap;
}

/**
 * Copies an agent from another factory ("Copy from another factory"), under its name or a free
 * one, with its prompt (`promptText`) and the skills it uses: one this factory already has by
 * that name is used as it is; another local folder is copied to skills/<name>/ with its origin.
 * Connections are not copied, nor a legacy tools block (the model cannot carry its rules).
 */
export function copyAgent(
  agents: AgentsSection,
  source: ModelAgent,
  promptText: string,
  skills: CopiedSkill[],
): { edit: Edit<'agents'>; name: string } {
  const name = uniqueName(
    source.name,
    agentsOf(agents).map((a) => a.name),
  );
  const path = promptPathFor(name);
  const { connections: _c, custom_tools: _t, prompt: _p, ...rest } = structuredClone(source);
  const files: FileMap = { [cleanPath(path)]: { text: promptText } };
  const have = new Set((agents.skills ?? []).map((s) => s.name));
  const added: ModelSkill[] = [];
  for (const { skill, files: folder } of skills) {
    if (have.has(skill.name) || !(source.skills ?? []).includes(skill.name)) continue;
    const local = skill.source.startsWith('./');
    added.push({
      name: skill.name,
      source: local ? `./skills/${skill.name}` : skill.source,
      ...(skill.origin ? { origin: skill.origin } : {}),
    });
    if (local) for (const [p, c] of Object.entries(folder)) files[`skills/${skill.name}/${p}`] = c;
  }
  const agent: ModelAgent = { ...rest, name, prompt: { path } };
  return {
    name,
    edit: edit(
      'agents',
      {
        ...agents,
        agents: [...agentsOf(agents), agent],
        ...(added.length > 0 ? { skills: [...(agents.skills ?? []), ...added] } : {}),
      },
      files,
    ),
  };
}

/** Sets (or, with undefined, removes) one field of an agent. */
export function setAgentField<K extends Exclude<keyof ModelAgent, 'name' | 'custom_tools'>>(
  agents: AgentsSection,
  name: string,
  field: K,
  value: ModelAgent[K] | undefined,
): Edit<'agents'> {
  return edit(
    'agents',
    mapAgent(agents, name, (a) => {
      const { [field]: _old, ...rest } = a;
      return (value === undefined ? rest : { ...rest, [field]: value }) as ModelAgent;
    }),
  );
}

const rename = (list: string[], from: string, to: string) => list.map((x) => (x === from ? to : x));

/**
 * Renames an agent everywhere B matches it by name: station agents, lane routes, the librarian,
 * connector-form connections and their tool rules; its prompt moves with it when it is the
 * conventional ./agents/<name>.md (`promptText` is that file's text).
 */
export function renameAgent(model: Model, from: string, to: string, promptText: string): AnyEdit[] {
  const out: AnyEdit[] = [];
  const old = agentsOf(model.agents).find((a) => a.name === from);
  if (!old) return out;

  const moves = old.prompt?.path !== undefined && cleanPath(old.prompt.path) === cleanPath(promptPathFor(from));
  const agents = mapAgent(model.agents, from, (a) => ({
    ...a,
    name: to,
    ...(moves ? { prompt: { path: promptPathFor(to) } } : {}),
  }));
  out.push(
    moves
      ? edit('agents', agents, { [cleanPath(promptPathFor(to))]: { text: promptText } }, [
          cleanPath(promptPathFor(from)),
        ])
      : edit('agents', agents),
  );

  const line = model.line;
  let lineChanged = line.librarian === from;
  const stations = stationsOf(line).map((s) => {
    const route =
      s.route && Object.values(s.route).includes(from)
        ? Object.fromEntries(Object.entries(s.route).map(([l, a]) => [l, a === from ? to : a]))
        : s.route;
    if (s.agent !== from && route === s.route) return s;
    lineChanged = true;
    return { ...s, ...(s.agent === from ? { agent: to } : {}), ...(route ? { route } : {}) };
  });
  if (lineChanged) out.push(edit('line', { ...line, stations, ...(line.librarian === from ? { librarian: to } : {}) }));

  let connectionsChanged = false;
  const connections = model.connections.map((c) => {
    const tools = c.tools
      ? Object.fromEntries(
          Object.entries(c.tools).map(([tool, rule]) => {
            const list = Array.isArray(rule.agents) ? (rule.agents as string[]) : undefined;
            return [tool, list?.includes(from) ? { ...rule, agents: rename(list, from, to) } : rule];
          }),
        )
      : undefined;
    const changed = c.agents?.includes(from) || JSON.stringify(tools) !== JSON.stringify(c.tools);
    if (!changed) return c;
    connectionsChanged = true;
    return { ...c, ...(c.agents ? { agents: rename(c.agents, from, to) } : {}), ...(tools ? { tools } : {}) };
  });
  if (connectionsChanged) out.push(edit('connections', connections));
  return out;
}

/** An agent a station still uses: Remove is blocked, listing them. */
export class AgentInUseError extends Error {
  constructor(
    readonly agent: string,
    readonly stations: string[],
  ) {
    super(`${agent} works at ${stations.join(', ')}`);
    this.name = 'AgentInUseError';
  }
}

/** Removes an agent, its prompt (B keeps one another agent uses) and its connection access. */
export function removeAgent(model: Model, name: string): AnyEdit[] {
  const using = stationsUsing(model.line, name);
  if (using.length > 0)
    throw new AgentInUseError(
      name,
      using.map((s) => s.id),
    );
  const agent = agentsOf(model.agents).find((a) => a.name === name);
  const out: AnyEdit[] = [
    edit(
      'agents',
      { ...model.agents, agents: agentsOf(model.agents).filter((a) => a.name !== name) },
      undefined,
      agent?.prompt?.path ? [cleanPath(agent.prompt.path)] : [],
    ),
  ];
  if (model.connections.some((c) => c.agents?.includes(name))) {
    out.push(
      edit(
        'connections',
        model.connections.map((c) =>
          c.agents?.includes(name) ? { ...c, agents: c.agents.filter((a) => a !== name) } : c,
        ),
      ),
    );
  }
  return out;
}

export function addSkillToAgent(agents: AgentsSection, agent: string, skill: string): Edit<'agents'> {
  return edit(
    'agents',
    mapAgent(agents, agent, (a) => (a.skills?.includes(skill) ? a : { ...a, skills: [...(a.skills ?? []), skill] })),
  );
}

export function removeSkillFromAgent(agents: AgentsSection, agent: string, skill: string): Edit<'agents'> {
  return edit(
    'agents',
    mapAgent(agents, agent, (a) => ({ ...a, skills: (a.skills ?? []).filter((s) => s !== skill) })),
  );
}

/** Removes a skill from the factory and every agent; its folder stays in the draft (YAML deletes it). */
export function removeSkill(agents: AgentsSection, skill: string): Edit<'agents'> {
  return edit('agents', {
    ...agents,
    skills: (agents.skills ?? []).filter((s) => s.name !== skill),
    agents: agentsOf(agents).map((a) =>
      a.skills?.includes(skill) ? { ...a, skills: a.skills.filter((s) => s !== skill) } : a,
    ),
  });
}

/** A skill of that name already in the factory, from somewhere else: the add is refused. */
export class SkillExistsError extends Error {
  constructor(readonly skill: string) {
    super(`A skill named ${skill} already exists in this factory`);
    this.name = 'SkillExistsError';
  }
}

/** The origin a vendored skills.sh skill records: skills.sh:<owner>/<repo>@<skill>#<commit>. */
export const skillOrigin = (s: SkillFiles['skill']) => `skills.sh:${s.owner}/${s.repo}@${s.skill}#${s.commit}`;

/**
 * Vendors a catalog skill into the draft for one agent, in one agents save: its files under
 * skills/<skill>/, the factory skill {name, source, origin} and the name on the agent. The same
 * skill already vendored is only added to the agent; another skill of that name is refused.
 */
export function addSkillFromCatalog(agents: AgentsSection, agent: string, skill: SkillFiles): Edit<'agents'> {
  const name = skill.skill.skill;
  const origin = skillOrigin(skill.skill);
  const existing = agents.skills?.find((s) => s.name === name);
  if (existing && existing.origin !== origin) throw new SkillExistsError(name);
  const withAgent = addSkillToAgent(agents, agent, name).value;
  if (existing) return edit('agents', withAgent);
  const files: FileMap = Object.fromEntries(Object.entries(skill.files).map(([p, c]) => [`skills/${name}/${p}`, c]));
  return edit(
    'agents',
    { ...withAgent, skills: [...(agents.skills ?? []), { name, source: `./skills/${name}`, origin }] },
    files,
  );
}

/** Sets one built-in tool's mode (Off / Ask me / Allowed), keeping run's command list. */
export function setBuiltin(agents: AgentsSection, agent: string, key: BuiltinKey, mode: BuiltinMode): Edit<'agents'> {
  return edit(
    'agents',
    mapAgent(agents, agent, (a) => {
      const builtins: Builtins = { ...(a.builtins ?? {}) };
      if (key === 'run') builtins.run = { ...builtins.run, mode };
      else builtins[key] = mode;
      return { ...a, builtins };
    }),
  );
}

/** "Limit to commands…": the commands Run commands may start (empty: any). */
export function setRunCommands(agents: AgentsSection, agent: string, commands: string[]): Edit<'agents'> {
  return edit(
    'agents',
    mapAgent(agents, agent, (a) => {
      const run = { mode: a.builtins?.run?.mode ?? 'allowed', ...(commands.length > 0 ? { commands } : {}) };
      return { ...a, builtins: { ...(a.builtins ?? {}), run } };
    }),
  );
}

/** "Switch to simple settings…": replaces a legacy tools block with built-in settings (all off). */
export function useBuiltins(agents: AgentsSection, agent: string): Edit<'agents'> {
  return edit(
    'agents',
    mapAgent(agents, agent, (a) => {
      const { custom_tools: _legacy, ...rest } = a;
      return { ...rest, builtins: { read: 'off', edit: 'off', run: { mode: 'off' }, web: 'off', subagents: 'off' } };
    }),
  );
}

// Connections.

/**
 * Sets which agents may use a connection: a connector-form entry's own `agents`; for a legacy
 * entry, each agent's `connections` (an agents save).
 */
export function setConnectionAgents(model: Model, name: string, allowed: string[]): AnyEdit {
  const connection = model.connections.find((c) => c.name === name);
  if (!connection?.legacy) {
    return edit(
      'connections',
      model.connections.map((c) => (c.name === name ? { ...c, agents: allowed } : c)),
    );
  }
  return edit('agents', {
    ...model.agents,
    agents: agentsOf(model.agents).map((a) => {
      const has = a.connections?.includes(name) ?? false;
      const wants = allowed.includes(a.name);
      if (has === wants) return a;
      return {
        ...a,
        connections: wants ? [...(a.connections ?? []), name] : (a.connections ?? []).filter((c) => c !== name),
      };
    }),
  });
}

/** Removes a connection and every agent's reference to it, one save per section touched. */
export function removeConnection(model: Model, name: string): AnyEdit[] {
  const out: AnyEdit[] = [edit('connections', model.connections.filter((c) => c.name !== name) as ConnectionsSection)];
  if (agentsOf(model.agents).some((a) => a.connections?.includes(name))) {
    out.push(
      edit('agents', {
        ...model.agents,
        agents: agentsOf(model.agents).map((a) =>
          a.connections?.includes(name) ? { ...a, connections: a.connections.filter((c) => c !== name) } : a,
        ),
      }),
    );
  }
  return out;
}

// Basics and review.

export function setBasics<K extends Exclude<keyof BasicsSection, 'id'>>(
  basics: BasicsSection,
  field: K,
  value: BasicsSection[K] | undefined,
): Edit<'basics'> {
  const { [field]: _old, ...rest } = basics;
  return edit('basics', value === undefined ? rest : { ...rest, [field]: value });
}

export function addWorkType(basics: BasicsSection, type: string): Edit<'basics'> {
  const types = basics.work_types ?? [];
  return setBasics(basics, 'work_types', types.includes(type) ? types : [...types, type]);
}

export function removeWorkType(basics: BasicsSection, type: string): Edit<'basics'> {
  return setBasics(
    basics,
    'work_types',
    (basics.work_types ?? []).filter((t) => t !== type),
  );
}

export function setBudget<K extends keyof Budget>(
  review: ReviewSection,
  field: K,
  value: Budget[K] | undefined,
): Edit<'review'> {
  const { [field]: _old, ...rest } = review.budget ?? {};
  return edit('review', { ...review, budget: value === undefined ? rest : { ...rest, [field]: value } });
}

// Fixes.

/** Applies a problem's fix (an RFC 6902 patch on one section's value) as an edit of that section. */
export function applyFix(model: Model, fix: ProblemFix): AnyEdit {
  const section = fix.section as Section;
  let value: unknown = structuredClone(model[section]);
  for (const op of fix.patch) {
    const parent = op.path.slice(0, op.path.lastIndexOf('/'));
    const last = pointerTokens(op.path).at(-1) ?? '';
    const container = op.path === '' ? undefined : getPointer(value, parent);
    if (op.op === 'remove') value = setPointer(value, op.path, undefined);
    else if (op.op === 'add' && Array.isArray(container)) {
      const next = [...container];
      next.splice(last === '-' ? next.length : Number(last), 0, op.value);
      value = setPointer(value, parent, next);
    } else value = setPointer(value, op.path, op.value);
  }
  return edit(section, value as SectionValues[typeof section]) as AnyEdit;
}
