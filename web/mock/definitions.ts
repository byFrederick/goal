// The mock's definitions and builder routes (B §8, D §8): factories created from a template, a
// clone or imported files; the one shared draft per factory with Visual section saves (applied to
// factory.yaml by model.ts, so comments survive), single files, the whole tree, validation with
// an overlay, publish, versions, compare and restore; the template catalog; and the builder's
// meta and skills catalog. Writes are conditional like goald's (412 draft-conflict with the
// current value, 428 without a precondition), every draft change is a `definition` event on the
// organization's stream, and a factory's runtime fields (stations, work types, current, draft)
// follow its versions. The checks and the semantic diff are smaller than goald's: enough for
// the editor's screens and tests, with goald's codes, pointers and shapes.
import { createHash, randomUUID } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import type {
  BuilderMeta,
  Change,
  CheckResult,
  CompareSide,
  Comparison,
  DefinitionCounts,
  DefinitionEvent,
  DefinitionProblem,
  Draft,
  DraftModel,
  Factory,
  FactoryModel,
  FileChange,
  Org,
  ProblemFix,
  SaveResult,
  SectionSummary,
  SkillListing,
  Station,
  Template,
  TemplateSummary,
  Validation,
  Version,
  VersionDetail,
  VersionRef,
} from '@goal/api-client';
import { parse as parseYaml } from 'yaml';
import { localUser, type MockDefinition, type MockSkill, type MockVersion, type Tree } from './definition-seed.ts';
import type { MockFault } from './faults.ts';
import type { MockState } from './fixtures.ts';
import {
  applySection,
  cleanPath,
  edited,
  equal,
  formatYaml,
  identityOf,
  type Model,
  ModelInvalidError,
  normalizeAt,
  ParseError,
  type Ranges,
  readModel,
  revert,
  type Section,
  sectionEtag,
  sectionLines,
  sections,
} from './model.ts';

type Json = Record<string, unknown>;

export interface DefinitionsDeps {
  state: () => MockState;
  /** Sends one event to every stream subscriber of the organization. */
  broadcast: (org: string, event: string, data: unknown) => void;
}

const actorRef = 'user:local';
const yamlFile = 'factory.yaml';

const sha = (b: Uint8Array) => createHash('sha256').update(b).digest('hex');
const decode = (b: Uint8Array | undefined) => (b ? new TextDecoder().decode(b) : '');
const encode = (s: string) => new TextEncoder().encode(s);
const isObject = (v: unknown): v is Json => typeof v === 'object' && v !== null && !Array.isArray(v);
const list = (v: unknown): Json[] => (Array.isArray(v) ? v.filter(isObject) : []);
const now = () => new Date().toISOString();

const send = (res: ServerResponse, status: number, body: unknown, headers: Record<string, string> = {}) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  for (const [k, v] of Object.entries(headers)) res.setHeader(k, v);
  res.end(status === 204 ? undefined : JSON.stringify(body));
};

const problem = (status: number, title: string, code?: string, extra: Json = {}, detail?: string) => ({
  title,
  status,
  ...(detail ? { detail } : {}),
  ...(code ? { code } : {}),
  ...extra,
});

/** A refusal: thrown by a handler, answered as its Problem. */
class Refusal extends Error {
  constructor(
    readonly status: number,
    readonly body: Json,
  ) {
    super(String(body.title));
  }
}

const refuse = (status: number, title: string, code?: string, extra: Json = {}, detail?: string): never => {
  throw new Refusal(status, problem(status, title, code, extra, detail));
};

async function readRaw(req: IncomingMessage): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

async function readJson(req: IncomingMessage): Promise<Json> {
  const raw = await readRaw(req);
  if (raw.length === 0) return {};
  try {
    const v = JSON.parse(raw.toString('utf8')) as unknown;
    return isObject(v) ? v : {};
  } catch {
    return refuse(400, 'Bad Request', undefined, {}, 'the body is not JSON');
  }
}

/** The entity tag of a conditional request, unquoted; undefined when absent. */
function tagOf(req: IncomingMessage, header: 'if-match' | 'if-none-match'): string | undefined {
  const v = req.headers[header];
  return typeof v === 'string' ? v.replace(/^W\//, '').replaceAll('"', '') : undefined;
}

const requireMatch = (req: IncomingMessage): string =>
  tagOf(req, 'if-match') ?? refuse(428, 'Precondition Required', 'precondition-required');

/** A draft path: relative, no `..`, no hidden files. */
function checkPath(p: string): string {
  const parts = p.split('/');
  if (
    p === '' ||
    p.startsWith('/') ||
    p.includes('\\') ||
    parts.some((s) => s === '' || s === '..' || s.startsWith('.'))
  ) {
    refuse(422, 'Unprocessable Content', 'bad-path', { path: p }, `${p} is not a path a definition may hold`);
  }
  return p;
}

function treeOf(files: unknown): Tree {
  const t: Tree = new Map();
  if (!isObject(files)) return t;
  for (const [p, c] of Object.entries(files)) {
    const path = checkPath(cleanPath(p));
    if (isObject(c) && typeof c.text === 'string') t.set(path, encode(c.text));
    else if (isObject(c) && typeof c.base64 === 'string') t.set(path, new Uint8Array(Buffer.from(c.base64, 'base64')));
    else refuse(400, 'Bad Request', undefined, {}, `${p}: a file is {text} or {base64}`);
  }
  return t;
}

type Read = { model: Model; ranges: Ranges };

function modelOf(t: Tree): Read | ParseError | undefined {
  const doc = t.get(yamlFile);
  if (!doc) return undefined;
  return readModel(decode(doc), t);
}

const asModel = (m: Model) => m as unknown as FactoryModel;

// Validation (B §4.7, a subset): goald's codes, checks, pointers and fixes.

interface Checked {
  validation: Validation;
  read: Read | undefined;
}

const checkSections: Record<string, string> = { yaml: 'files', schema: 'files', line: 'line', name: 'basics' };

const passed: Record<string, string> = {
  yaml: 'Every file parses',
  schema: 'factory.yaml matches the schema',
  references: 'Every agent, skill, contract, connection and prompt it names exists',
  availability: 'Everything it declares is available on this install',
};

const plural = (n: number, one: string, many: string) => (n === 1 ? `1 ${one}` : `${n} ${many}`);

const stationKinds = new Set(['queue', 'agent', 'human', 'done']);
const builtinKeys = ['read', 'edit', 'run', 'web', 'subagents'];

function validate(t: Tree, opts: { org: string; factory?: string; state: MockState; changes: number }): Checked {
  const problems: DefinitionProblem[] = [];
  const counts: DefinitionCounts = {
    stations: 0,
    agents: 0,
    skills: 0,
    connections: 0,
    contracts: 0,
    changes: opts.changes,
  };
  const read = modelOf(t);

  const finish = (ran: string[], r: Read | undefined, name = ''): Checked => {
    const checks: CheckResult[] = ran.map((id) => {
      const mine = problems.filter((p) => p.check === id);
      const errors = mine.filter((p) => p.severity === 'error').length;
      const warnings = mine.filter((p) => p.severity === 'warning').length;
      const section = checkSections[id] ?? mine[0]?.section ?? 'files';
      if (errors > 0) {
        const summary =
          plural(errors, 'error', 'errors') + (warnings > 0 ? `, ${plural(warnings, 'warning', 'warnings')}` : '');
        return { id, section, status: 'failed', summary };
      }
      if (warnings > 0) return { id, section, status: 'warning', summary: plural(warnings, 'warning', 'warnings') };
      let summary = passed[id] ?? 'Passed';
      if (id === 'line') {
        const ids = list((r?.model.line as Json | undefined)?.stations).map((s) => String(s.id));
        summary = ids.length
          ? `${plural(ids.length, 'station', 'stations')}, ends with ${ids.at(-1)}, every agent station has an agent`
          : 'The line is well formed';
      }
      if (id === 'name') summary = `The name ${name} is free`;
      return { id, section, status: 'passed', summary };
    });
    return {
      validation: { ok: !problems.some((p) => p.severity === 'error'), revision: 0, counts, checks, problems },
      read: r,
    };
  };

  if (read === undefined) {
    problems.push(
      located('factory-yaml-missing', 'error', 'yaml', 'files', undefined, 'factory.yaml is missing', {}, []),
    );
    return finish(['yaml'], undefined);
  }
  if (read instanceof ParseError) {
    problems.push({
      code: 'yaml-invalid',
      severity: 'error',
      check: 'yaml',
      section: 'files',
      file: yamlFile,
      line: read.line,
      column: read.column,
      end_line: read.line,
      end_column: read.column,
      message: `factory.yaml does not parse at line ${read.line}: ${read.message}`,
      fixes: [],
    });
    return finish(['yaml'], undefined);
  }

  const { model, ranges } = read;
  const add = (
    code: string,
    severity: DefinitionProblem['severity'],
    check: string,
    pointer: string,
    message: string,
    fixes: ProblemFix[] = [],
  ) => problems.push(located(code, severity, check, pointer.split('/')[1] ?? 'files', pointer, message, ranges, fixes));

  const basics = (model.basics ?? {}) as Json;
  const line = (model.line ?? {}) as Json;
  const agentsSection = (model.agents ?? {}) as Json;
  const stations = list(line.stations);
  const agents = list(agentsSection.agents);
  const skills = list(agentsSection.skills);
  const connections = list(model.connections);
  const agentNames = agents.map((a) => String(a.name));
  const stationIds = stations.map((s) => String(s.id));
  let contracts = 0;
  try {
    const raw = parseYaml(decode(t.get(yamlFile))) as { spec?: { contracts?: Json } } | null;
    contracts = Object.keys(raw?.spec?.contracts ?? {}).length;
  } catch {}
  Object.assign(counts, {
    stations: stations.length,
    agents: agents.length,
    skills: skills.length,
    connections: connections.length,
    contracts,
  });

  // schema
  const name = typeof basics.name === 'string' ? basics.name : '';
  if (!name) add('schema-required', 'error', 'schema', '/basics/name', 'metadata.name is required');
  else if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(name))
    add('schema-invalid', 'error', 'schema', '/basics/name', `name "${name}" is not a slug`);
  stations.forEach((s, i) => {
    if (!stationKinds.has(String(s.kind))) {
      add(
        'schema-enum',
        'error',
        'schema',
        `/line/stations/${i}/kind`,
        `station ${s.id}: kind must be queue, agent, human or done`,
      );
    }
  });

  // line
  const last = stations.at(-1);
  if (last && last.kind !== 'done') {
    add(
      'last-station-not-done',
      'error',
      'line',
      `/line/stations/${stations.length - 1}`,
      'the last station must be a done station',
    );
  }
  stations.forEach((s, i) => {
    const at = `/line/stations/${i}`;
    const wip = Number(s.wip ?? 0);
    const n = Number(s.agents ?? 1);
    if (s.kind === 'agent' && wip > 0 && n > wip) {
      add(
        'agents-exceed-wip',
        'error',
        'line',
        `${at}/agents`,
        `station ${s.id}: ${n} agents exceed wip ${wip}; extra agents would idle`,
        [
          replaceFix(`Set WIP to ${n}`, 'line', `/stations/${i}/wip`, n),
          replaceFix(`Set agents to ${wip}`, 'line', `/stations/${i}/agents`, wip),
        ],
      );
    }
    const gate = isObject(s.gate) ? s.gate : undefined;
    if (gate?.on_reject !== undefined) {
      const j = stationIds.indexOf(String(gate.on_reject));
      if (j < 0)
        add(
          'unknown-station',
          'error',
          'references',
          `${at}/gate/on_reject`,
          `station ${s.id}: gate.onReject "${gate.on_reject}" is not a station`,
        );
      else if (j >= i) {
        add(
          'gate-not-upstream',
          'error',
          'line',
          `${at}/gate/on_reject`,
          `station ${s.id}: gate.onReject "${gate.on_reject}" must be a station before it, where the work is redone`,
        );
      }
    }
    if (s.action === 'merge' && !isObject(basics.repository)) {
      add(
        'merge-needs-repository',
        'error',
        'line',
        `${at}/action`,
        `station ${s.id}: action merge needs runtime.repository, whose pull requests it merges`,
        [{ label: 'Remove the merge step', section: 'line', patch: [{ op: 'remove', path: `/stations/${i}/action` }] }],
      );
    }
  });

  // references
  const agentFixes = (pointer: string) =>
    agentNames.slice(0, 3).map((a) => replaceFix(`Set agent to ${a}`, 'line', pointer.replace(/^\/line/, ''), a));
  stations.forEach((s, i) => {
    if (s.agent !== undefined && !agentNames.includes(String(s.agent))) {
      const p = `/line/stations/${i}/agent`;
      add('unknown-agent', 'error', 'references', p, `station ${s.id}: unknown agent "${s.agent}"`, agentFixes(p));
    }
    for (const [lane, a] of Object.entries(isObject(s.route) ? s.route : {})) {
      if (!agentNames.includes(String(a))) {
        const p = `/line/stations/${i}/route/${lane}`;
        add(
          'unknown-agent',
          'error',
          'references',
          p,
          `station ${s.id}: lane ${lane} routes to unknown agent "${a}"`,
          agentFixes(p),
        );
      }
    }
  });
  if (typeof line.librarian === 'string' && !agentNames.includes(line.librarian)) {
    add(
      'unknown-agent',
      'error',
      'references',
      '/line/librarian',
      `learning.librarian: unknown agent "${line.librarian}"`,
    );
  }
  const skillNames = skills.map((s) => String(s.name));
  const connectionNames = connections.map((c) => String(c.name));
  agents.forEach((a, i) => {
    const prompt = isObject(a.prompt) ? String(a.prompt.path ?? '') : '';
    if (prompt && !t.has(cleanPath(prompt))) {
      add(
        'prompt-not-found',
        'error',
        'references',
        `/agents/agents/${i}/prompt`,
        `agent ${a.name} prompt: ${prompt} not found`,
      );
    }
    (Array.isArray(a.skills) ? a.skills : []).forEach((s, j) => {
      if (!skillNames.includes(String(s)))
        add(
          'unknown-skill',
          'error',
          'references',
          `/agents/agents/${i}/skills/${j}`,
          `agent ${a.name}: unknown skill "${s}"`,
        );
    });
    (Array.isArray(a.connections) ? a.connections : []).forEach((c, j) => {
      if (!connectionNames.includes(String(c))) {
        add(
          'unknown-connection',
          'error',
          'references',
          `/agents/agents/${i}/connections/${j}`,
          `agent ${a.name}: unknown connection "${c}"`,
        );
      }
    });
    const builtins = isObject(a.builtins) ? a.builtins : {};
    for (const k of builtinKeys) {
      const v = builtins[k];
      const mode = isObject(v) ? v.mode : v;
      if (mode === 'ask') {
        add(
          'builtin-ask-unavailable',
          'warning',
          'availability',
          `/agents/agents/${i}/builtins/${k}`,
          `agent ${a.name}: "Ask me" is asked as Off until tool approvals arrive`,
        );
      }
    }
  });
  skills.forEach((s, i) => {
    const source = String(s.source ?? '');
    if (source.startsWith('./') && ![...t.keys()].some((p) => p.startsWith(`${cleanPath(source)}/`))) {
      add(
        'skill-not-found',
        'error',
        'references',
        `/agents/skills/${i}/source`,
        `skill ${s.name}: ${source} not found`,
      );
    }
  });
  connections.forEach((c, i) => {
    (Array.isArray(c.agents) ? c.agents : []).forEach((a, j) => {
      if (!agentNames.includes(String(a)))
        add(
          'unknown-agent',
          'error',
          'references',
          `/connections/${i}/agents/${j}`,
          `connection ${c.name}: unknown agent "${a}"`,
        );
    });
  });

  // name
  const taken = opts.state.factories.some((f) => f.org_id === opts.org && f.name === name && f.id !== opts.factory);
  if (name && taken)
    add('name-taken', 'error', 'name', '/basics/name', `the organization already has a factory named ${name}`);

  return finish(['yaml', 'schema', 'line', 'references', 'availability', 'name'], read, name);
}

function replaceFix(label: string, section: string, path: string, value: unknown): ProblemFix {
  return { label, section, patch: [{ op: 'replace', path, value }] };
}

/** A problem placed by its pointer's lines, or its nearest parent's. */
function located(
  code: string,
  severity: DefinitionProblem['severity'],
  check: string,
  section: string,
  pointer: string | undefined,
  message: string,
  ranges: Ranges,
  fixes: ProblemFix[],
): DefinitionProblem {
  let lines: [number, number] = [0, 0];
  for (let p = pointer ?? ''; p; p = p.slice(0, p.lastIndexOf('/'))) {
    const r = ranges[p];
    if (r) {
      lines = r;
      break;
    }
  }
  return {
    code,
    severity,
    check,
    section,
    ...(pointer ? { pointer } : {}),
    file: yamlFile,
    line: lines[0],
    column: lines[0] ? 1 : 0,
    end_line: lines[1],
    end_column: 0,
    message,
    fixes,
  };
}

// The semantic diff (B §4.5, a subset): one change per changed item or field, by identity.

const zero: [number, number] = [0, 0];

interface Side {
  model?: Model;
  ranges: Ranges;
  files: Tree;
}

function side(t: Tree | undefined): Side {
  if (!t) return { ranges: {}, files: new Map() };
  const read = modelOf(t);
  return read && !(read instanceof ParseError) ? { ...read, files: t } : { ranges: {}, files: t };
}

const show = (v: unknown) => (typeof v === 'string' ? v : JSON.stringify(v));

function fieldChange(
  section: string,
  key: string,
  from: unknown,
  to: unknown,
  a: Side,
  b: Side,
  pointer: string,
): Change {
  const label = key.replaceAll('_', ' ');
  return {
    section,
    op: 'changed',
    target: { kind: 'field', id: key },
    field: key,
    ...(from !== undefined ? { from } : {}),
    ...(to !== undefined ? { to } : {}),
    summary:
      from === undefined
        ? `Sets ${label} to ${show(to)}`
        : to === undefined
          ? `Removes ${label}`
          : `${label[0]?.toUpperCase()}${label.slice(1)} changes from ${show(from)} to ${show(to)}`,
    from_lines: a.ranges[pointer] ?? zero,
    to_lines: b.ranges[pointer] ?? zero,
  };
}

const kindNames: Record<string, string> = {
  station: 'station',
  agent: 'agent',
  skill: 'skill',
  connection: 'connection',
};

function listChanges(section: Section, key: string, kind: Change['target']['kind'], a: Side, b: Side): Change[] {
  const container = (m: Model | undefined) =>
    section === 'connections' ? m?.connections : (m?.[section] as Json | undefined)?.[key];
  const from = list(container(a.model));
  const to = list(container(b.model));
  const prefix = section === 'connections' ? '/connections' : `/${section}/${key}`;
  const id = (v: Json) => identityOf(section, key, v);
  const fromIds = from.map(id);
  const toIds = to.map(id);
  const out: Change[] = [];
  const noun = kindNames[kind] ?? kind;

  to.forEach((item, j) => {
    const name = id(item);
    const i = fromIds.indexOf(name);
    const label = String(item.name ?? name);
    if (i < 0) {
      out.push({
        section,
        op: 'added',
        target: { kind, id: name },
        to: item,
        summary: `Adds the ${noun} ${label}`,
        from_lines: zero,
        to_lines: b.ranges[`${prefix}/${j}`] ?? zero,
      });
      return;
    }
    const before = normalizeAt(section, key, from[i], true) as Json;
    const after = normalizeAt(section, key, item, true) as Json;
    if (equal(before, after)) return;
    const fields = [...new Set([...Object.keys(before), ...Object.keys(after)])].filter(
      (k) => !equal(before[k], after[k]),
    );
    const one = fields.length === 1 ? (fields[0] as string) : undefined;
    out.push({
      section,
      op: 'changed',
      target: { kind, id: name },
      ...(one ? { field: one, from: before[one], to: after[one] } : { from: from[i], to: item }),
      summary: one
        ? `${label}: ${one.replaceAll('_', ' ')} changes from ${show(before[one] ?? 'none')} to ${show(after[one] ?? 'none')}`
        : `${label} changes`,
      from_lines: a.ranges[`${prefix}/${i}`] ?? zero,
      to_lines: b.ranges[`${prefix}/${j}`] ?? zero,
    });
  });
  from.forEach((item, i) => {
    const name = id(item);
    if (!toIds.includes(name)) {
      out.push({
        section,
        op: 'removed',
        target: { kind, id: name },
        from: item,
        summary: `Removes the ${noun} ${String(item.name ?? name)}`,
        from_lines: a.ranges[`${prefix}/${i}`] ?? zero,
        to_lines: zero,
      });
    }
  });
  const common = (ids: string[], other: string[]) => ids.filter((x) => other.includes(x));
  const was = common(fromIds, toIds);
  const now = common(toIds, fromIds);
  now.forEach((name, k) => {
    if (was.indexOf(name) > k) {
      const j = toIds.indexOf(name);
      out.push({
        section,
        op: 'moved',
        target: { kind, id: name },
        summary: `Moves the ${noun} ${name} up to position ${j + 1}`,
        from_lines: a.ranges[`${prefix}/${fromIds.indexOf(name)}`] ?? zero,
        to_lines: b.ranges[`${prefix}/${j}`] ?? zero,
      });
    }
  });
  return out;
}

function fieldChanges(section: Section, keys: string[], a: Side, b: Side): Change[] {
  const out: Change[] = [];
  for (const key of keys) {
    const from = (a.model?.[section] as Json | undefined)?.[key];
    const to = (b.model?.[section] as Json | undefined)?.[key];
    if (!equal(normalizeAt(section, key, from), normalizeAt(section, key, to))) {
      out.push(fieldChange(section, key, from, to, a, b, `/${section}/${key}`));
    }
  }
  return out;
}

function diff(a: Side, b: Side): { files: FileChange[]; changes: Change[] } {
  const paths = [...new Set([...a.files.keys(), ...b.files.keys()])].sort();
  const files: FileChange[] = paths.map((path) => {
    const x = a.files.get(path);
    const y = b.files.get(path);
    const op = !x ? 'added' : !y ? 'removed' : sha(x) === sha(y) ? 'unchanged' : 'modified';
    return { path, op, ...(x ? { from_sha256: sha(x) } : {}), ...(y ? { to_sha256: sha(y) } : {}) };
  });
  if (!b.model) return { files, changes: [] };

  const budget = (m: Model | undefined) => ((m?.review as Json | undefined)?.budget ?? {}) as Json;
  const changes: Change[] = [
    ...fieldChanges('basics', ['name', 'title', 'purpose', 'work_types', 'repository'], a, b),
    ...listChanges('line', 'stations', 'station', a, b),
    ...fieldChanges('line', ['intake', 'flow', 'librarian'], a, b),
    ...fieldChanges('agents', ['default_model'], a, b),
    ...listChanges('agents', 'agents', 'agent', a, b),
    ...listChanges('agents', 'skills', 'skill', a, b),
    ...listChanges('connections', '', 'connection', a, b),
    ...[...new Set([...Object.keys(budget(a.model)), ...Object.keys(budget(b.model))])]
      .filter((k) => !equal(budget(a.model)[k], budget(b.model)[k]))
      .map((k) => fieldChange('review', k, budget(a.model)[k], budget(b.model)[k], a, b, `/review/budget/${k}`)),
  ];

  // A changed prompt is a change of its agent; other files are files changes.
  const prompts = new Map(
    list((b.model.agents as Json | undefined)?.agents).map((ag) => [
      cleanPath(String((ag.prompt as Json | undefined)?.path ?? '')),
      String(ag.name),
    ]),
  );
  for (const f of files) {
    if (f.op === 'unchanged' || f.path === yamlFile) continue;
    const agent = prompts.get(f.path);
    if (agent) {
      if (!changes.some((c) => c.target.kind === 'agent' && c.target.id === agent)) {
        changes.push({
          section: 'agents',
          op: 'changed',
          target: { kind: 'agent', id: agent },
          field: 'prompt',
          summary: `Prompt of ${agent} changes`,
          from_lines: zero,
          to_lines: zero,
        });
      }
      continue;
    }
    const kind = f.path.startsWith('contracts/') ? 'contract' : 'file';
    const op = f.op === 'modified' ? 'changed' : f.op;
    changes.push({
      section: 'files',
      op,
      target: { kind, id: f.path },
      summary: `${f.path} is ${f.op}`,
      from_lines: zero,
      to_lines: zero,
    });
  }
  return { files, changes };
}

// Store.

export interface Definitions {
  /** Serves a definitions or builder route; false when rest is none of them. */
  handle: (
    org: Org,
    rest: string,
    method: string,
    url: URL,
    req: IncomingMessage,
    res: ServerResponse,
  ) => Promise<boolean>;
  /** The body of an injected fault, as goald would answer that status at that path. */
  faultBody: (path: string, fault: MockFault) => Json;
}

export function createDefinitions(deps: DefinitionsDeps): Definitions {
  const st = deps.state;

  const factoryOf = (org: Org, key: string) =>
    st().factories.find((f) => f.org_id === org.id && (f.id === key || f.name === key)) ??
    refuse(404, 'Factory not found', 'factory-not-found', {}, key);

  const defOf = (f: Factory): MockDefinition => {
    const defs = st().definitions;
    defs[f.id] ??= { versions: [], draft: null, current: null };
    return defs[f.id] as MockDefinition;
  };

  const draftOf = (def: MockDefinition) =>
    def.draft ?? refuse(404, 'No draft', 'no-draft', {}, 'the factory has no open draft');

  const versionRef = (v: MockVersion): VersionRef => ({
    id: v.id,
    number: v.number ?? 0,
    published_at: v.published_at ?? v.updated_at,
    published_by: v.published_by ?? localUser,
  });

  const byId = (def: MockDefinition, id: string | null) => def.versions.find((v) => v.id === id);

  /** The tree a draft's changes are counted against: its base, else the tree it started from. */
  const referenceOf = (def: MockDefinition, d: MockVersion): Tree | undefined =>
    byId(def, d.base)?.files ?? d.start ?? undefined;

  const emit = (f: Factory, data: Omit<DefinitionEvent, 'org_id' | 'factory_id'>) =>
    deps.broadcast(f.org_id, 'definition', { org_id: f.org_id, factory_id: f.id, ...data } satisfies DefinitionEvent);

  const checked = (f: Factory, t: Tree, reference: Tree | undefined) => {
    const changes = reference ? diff(side(reference), side(t)).changes : [];
    return { ...validate(t, { org: f.org_id, factory: f.id, state: st(), changes: changes.length }), changes };
  };

  const summaries = (f: Factory, def: MockDefinition, d: MockVersion) => {
    const { validation, read, changes } = checked(f, d.files, referenceOf(def, d));
    const counted = validation.problems.filter((p) => p.severity !== 'info');
    const out: SectionSummary[] = sections.map((section) => ({
      section,
      changes: changes.filter((c) => c.section === section).length,
      problems: counted.filter((p) => p.section === section).length,
      lines: read ? sectionLines(read.ranges, section) : zero,
    }));
    return { change_count: changes.length, problem_count: counted.length, sections: out };
  };

  const draftOut = (f: Factory, def: MockDefinition, d: MockVersion): Draft => {
    const current = byId(def, def.current);
    const base = byId(def, d.base);
    return {
      id: d.id,
      revision: d.revision,
      next_number: Math.max(0, ...def.versions.map((v) => v.number ?? 0)) + 1,
      created_at: d.created_at,
      created_by: d.created_by,
      updated_at: d.updated_at,
      updated_by: d.updated_by,
      base: base ? versionRef(base) : null,
      current: current ? versionRef(current) : null,
      source: d.source,
      ...summaries(f, def, d),
    };
  };

  const saveResult = (f: Factory, def: MockDefinition, d: MockVersion, extra: Partial<SaveResult> = {}): SaveResult => {
    const s = summaries(f, def, d);
    return { revision: d.revision, updated_at: d.updated_at, ...s, ...extra };
  };

  /** The factory's runtime fields from its versions: what goald's factory list says. */
  const sync = (f: Factory, def: MockDefinition) => {
    const current = byId(def, def.current);
    f.current = current ? versionRef(current) : null;
    f.draft = def.draft
      ? {
          id: def.draft.id,
          revision: def.draft.revision,
          updated_at: def.draft.updated_at,
          updated_by: def.draft.updated_by,
        }
      : null;
    const read = current ? modelOf(current.files) : undefined;
    if (!read || read instanceof ParseError) return;
    const basics = read.model.basics as Json;
    const name = typeof basics.name === 'string' ? basics.name : f.name;
    if (name !== f.name) rename(f.name, name);
    f.name = name;
    if (typeof basics.title === 'string') f.title = basics.title;
    if (typeof basics.purpose === 'string') f.description = basics.purpose;
    f.work_types = Array.isArray(basics.work_types) ? basics.work_types.map(String) : [];
    f.stations = list((read.model.line as Json).stations).map(
      (s): Station => ({
        id: String(s.id),
        kind: (stationKinds.has(String(s.kind)) ? s.kind : 'queue') as Station['kind'],
        ...(s.action === 'merge' ? { action: 'merge' as const } : {}),
      }),
    );
  };

  /** A published rename: the mock keys items and board state by the factory's name. */
  const rename = (from: string, to: string) => {
    const s = st();
    for (const it of s.items) if (it.factory === from) it.factory = to;
    for (const rec of [s.stations, s.constraint, s.history, s.andon, s.gates, s.weeklyBudget, s.flow] as Record<
      string,
      unknown
    >[]) {
      if (from in rec) {
        rec[to] = rec[from];
        delete rec[from];
      }
    }
  };

  /** Saves files (and removals) on the draft as one revision; nothing changes, nothing is saved. */
  const commit = (f: Factory, d: MockVersion, files: Tree, remove: string[] = []): boolean => {
    let changed = false;
    for (const [p, b] of files) {
      const cur = d.files.get(p);
      if (!cur || sha(cur) !== sha(b)) {
        d.files.set(p, b);
        changed = true;
      }
    }
    for (const p of remove) changed = d.files.delete(p) || changed;
    if (changed) {
      d.revision += 1;
      d.updated_at = now();
      d.updated_by = localUser;
      emit(f, { kind: 'draft', version_id: d.id, revision: d.revision, by: actorRef });
    }
    return changed;
  };

  /** A version by selector: a number, current, draft, base or source. */
  const select = (def: MockDefinition, selector: string): { version?: MockVersion; files: Tree } | undefined => {
    if (/^[1-9][0-9]*$/.test(selector)) {
      const v = def.versions.find((x) => x.number === Number(selector));
      return v ? { version: v, files: v.files } : undefined;
    }
    switch (selector) {
      case 'current': {
        const v = byId(def, def.current);
        return v ? { version: v, files: v.files } : undefined;
      }
      case 'draft':
        return def.draft ? { version: def.draft, files: def.draft.files } : undefined;
      case 'base': {
        const v = def.draft ? byId(def, def.draft.base) : undefined;
        if (v) return { version: v, files: v.files };
        return def.draft?.start ? { files: def.draft.start } : undefined;
      }
      case 'source':
        if (def.draft?.start) return { files: def.draft.start };
        return def.draft ? select(def, 'base') : undefined;
    }
    return undefined;
  };

  const versionOut = (def: MockDefinition, f: Factory, v: MockVersion): Version => {
    const previous = v.number ? def.versions.find((x) => x.number === (v.number as number) - 1) : byId(def, v.base);
    const changes = previous ? diff(side(previous.files), side(v.files)).changes.length : 0;
    const { validation } = validate(v.files, { org: f.org_id, factory: f.id, state: st(), changes });
    return {
      id: v.id,
      number: v.number,
      revision: v.revision,
      state: v.state,
      message: v.message,
      source: v.source,
      published_at: v.published_at,
      published_by: v.published_by,
      current: v.id === def.current,
      counts: validation.counts,
    };
  };

  /** The open items a version without some stations would strand, unless station_moves maps them. */
  const stranded = (f: Factory, t: Tree, moves: Json) => {
    const read = modelOf(t);
    const ids =
      read && !(read instanceof ParseError) ? list((read.model.line as Json).stations).map((s) => String(s.id)) : [];
    const open = st().items.filter((it) => it.factory === f.name && it.state !== 'done' && !ids.includes(it.station));
    const at = (it: (typeof open)[number]) => ({ id: it.id, number: it.number, station: it.station, state: it.state });
    const in_progress = open.filter((it) => it.state === 'in_progress').map(at);
    const unmapped = open
      .filter((it) => it.state !== 'in_progress' && !ids.includes(String(moves[it.station] ?? '')))
      .map(at);
    if (in_progress.length || unmapped.length) {
      refuse(
        409,
        'Conflict',
        'items-at-removed-station',
        { items: { in_progress, unmapped } },
        'open items are at stations this version removes',
      );
    }
    for (const it of open) it.station = String(moves[it.station]);
  };

  const fileOut = (res: ServerResponse, b: Uint8Array, immutable: boolean) => {
    res.statusCode = 200;
    res.setHeader(
      'Content-Type',
      decode(b).includes('\u0000') ? 'application/octet-stream' : 'text/plain; charset=utf-8',
    );
    res.setHeader('ETag', `"${sha(b)}"`);
    if (immutable) res.setHeader('Cache-Control', 'private, max-age=31536000, immutable');
    res.end(Buffer.from(b));
  };

  // Factories (§8.1).

  const createFactory = async (org: Org, req: IncomingMessage, res: ServerResponse) => {
    const body = await readJson(req);
    const start = isObject(body.start) ? body.start : refuse(400, 'Bad Request', undefined, {}, 'start is required');
    let files: Tree;
    let source: MockVersion['source'];
    switch (start.kind) {
      case 'blank':
      case 'template': {
        const name = start.kind === 'blank' ? 'blank' : String(start.template ?? '');
        const versions = st().templates.filter((t) => t.name === name);
        const t =
          start.version !== undefined
            ? versions.find((v) => v.version === start.version)
            : versions.sort((a, b) => b.version - a.version)[0];
        if (!t) refuse(404, 'Template not found', 'template-not-found', {}, name);
        files = new Map((t as NonNullable<typeof t>).files);
        source =
          start.kind === 'blank' ? { kind: 'blank' } : { kind: 'template', template: `${t?.name}@${t?.version}` };
        break;
      }
      case 'clone': {
        const from = factoryOf(org, String(start.factory ?? ''));
        const def = defOf(from);
        const picked = select(def, String(start.version ?? (def.current ? 'current' : 'draft')));
        if (!picked) refuse(404, 'Version not found', 'version-not-found');
        files = new Map(picked?.files);
        source = {
          kind: 'clone',
          factory: from.name,
          version: String(picked?.version?.number ?? start.version ?? 'draft'),
        };
        break;
      }
      case 'import': {
        files = treeOf(start.files);
        const read = modelOf(files);
        if (!read || read instanceof ParseError)
          refuse(422, 'Unprocessable Content', 'import-invalid', {}, 'factory.yaml is missing or does not parse');
        source = { kind: 'import' };
        break;
      }
      default:
        return refuse(400, 'Bad Request', undefined, {}, 'start.kind is blank, template, clone or import');
    }
    const read = modelOf(files) as Read;
    const basics = read.model.basics as Json;
    const name = typeof body.name === 'string' && body.name ? body.name : String(basics.name ?? '');
    if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(name))
      refuse(400, 'Bad Request', undefined, {}, `name "${name}" is not a slug`);
    if (st().factories.some((f) => f.org_id === org.id && f.name === name)) {
      refuse(409, 'Conflict', 'name-taken', {}, `the organization already has a factory named ${name}`);
    }
    const keep = start.kind === 'import' && start.keep_id === true && typeof basics.id === 'string';
    const id = keep ? String(basics.id) : randomUUID();
    const title = typeof body.title === 'string' && body.title ? body.title : basics.title;
    const start_ = new Map(files);
    const yaml = applySection(decode(files.get(yamlFile)), 'basics', {
      ...basics,
      id,
      name,
      ...(title !== undefined ? { title } : {}),
    });
    files.set(yamlFile, encode(yaml));

    const at = now();
    const factory: Factory = {
      id,
      org_id: org.id,
      name,
      ...(typeof title === 'string' ? { title } : {}),
      description: typeof basics.purpose === 'string' ? basics.purpose : '',
      work_types: [],
      stations: [],
      current: null,
      draft: null,
      paused: false,
      created_at: at,
    };
    const draft: MockVersion = {
      id: randomUUID(),
      number: null,
      revision: 1,
      state: 'draft',
      message: '',
      source,
      files,
      base: null,
      start: start_,
      created_at: at,
      created_by: localUser,
      updated_at: at,
      updated_by: localUser,
      published_at: null,
      published_by: null,
    };
    const s = st();
    s.factories.push(factory);
    s.definitions[id] = { versions: [], draft, current: null };
    s.stations[name] = {};
    s.constraint[name] = null;
    sync(factory, s.definitions[id] as MockDefinition);
    emit(factory, { kind: 'factory' });
    emit(factory, { kind: 'draft', version_id: draft.id, revision: 1, by: actorRef });
    send(res, 201, factory, { Location: `/v1/orgs/${org.slug}/factories/${id}` });
  };

  const templateSummary = (t: MockState['templates'][number]): TemplateSummary => {
    const read = modelOf(t.files) as Read;
    return {
      name: t.name,
      version: t.version,
      title: t.title,
      summary: t.summary,
      stations: list((read.model.line as Json).stations).length,
      agents: list((read.model.agents as Json).agents).length,
      connectors: t.connectors,
      changed: t.changed,
    };
  };

  // Builder (D §8).

  const listing = (s: MockSkill): SkillListing => ({
    key: `${s.owner}/${s.repo}/${s.skill}`,
    owner: s.owner,
    repo: s.repo,
    skill: s.skill,
    commit: s.commit,
    description: s.description,
    installs: s.installs,
    files: Object.keys(s.files).length,
    bytes: Object.values(s.files).reduce((n, f) => n + encode(f).length, 0),
  });

  const meta: BuilderMeta = {
    models: [
      { id: 'claude-opus-5-5', label: 'Claude Opus 5.5', default: false },
      { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', default: true },
      { id: 'claude-haiku-5-5', label: 'Claude Haiku 5.5', default: false },
    ],
    capabilities: { credentials: false, connectors: false, tool_approvals: false },
    limits: { skill_bytes: 1 << 20, skill_files: 200 },
  };

  // Routes.

  async function route(
    org: Org,
    rest: string,
    method: string,
    url: URL,
    req: IncomingMessage,
    res: ServerResponse,
  ): Promise<boolean> {
    const q = url.searchParams;

    if (rest === '/builder/meta' && method === 'GET') {
      send(res, 200, meta);
      return true;
    }
    if (rest === '/builder/skills' && method === 'GET') {
      const limit = Number(q.get('limit') ?? 20);
      if (!Number.isInteger(limit) || limit < 1 || limit > 50)
        refuse(400, 'Bad Request', undefined, {}, 'limit must be in [1, 50]');
      const term = (q.get('q') ?? '').toLowerCase();
      const after = q.get('after') ?? '';
      const all = st()
        .skills.map(listing)
        .filter((s) => !term || `${s.key}\n${s.skill}\n${s.description}`.toLowerCase().includes(term))
        .sort((a, b) => b.installs - a.installs || a.key.localeCompare(b.key));
      const from = after ? all.findIndex((s) => s.key === after) + 1 : 0;
      const skills = all.slice(from, from + limit);
      send(res, 200, { skills, next_after: from + limit < all.length ? (skills.at(-1)?.key ?? null) : null });
      return true;
    }
    let m = rest.match(/^\/builder\/skills\/([^/]+)\/([^/]+)\/([^/]+)\/files$/);
    if (m && method === 'GET') {
      const [owner, repo, name] = m.slice(1).map(decodeURIComponent);
      const s = st().skills.find((x) => x.owner === owner && x.repo === repo && x.skill === name);
      if (!s) refuse(404, 'Skill not found', 'skill-not-found', {}, `${owner}/${repo}/${name}`);
      const skill = s as MockSkill;
      const files = Object.fromEntries(Object.entries(skill.files).map(([p, text]) => [p, { text }]));
      send(res, 200, { skill: listing(skill), files });
      return true;
    }

    if (rest === '/factories' && method === 'POST') {
      await createFactory(org, req, res);
      return true;
    }
    if (rest === '/templates' && method === 'GET') {
      const latest = new Map<string, MockState['templates'][number]>();
      for (const t of st().templates) if ((latest.get(t.name)?.version ?? 0) < t.version) latest.set(t.name, t);
      const templates = [...latest.values()].sort((a, b) => a.name.localeCompare(b.name)).map(templateSummary);
      send(res, 200, { templates });
      return true;
    }
    m = rest.match(/^\/templates\/([^/]+)$/);
    if (m && method === 'GET') {
      const name = decodeURIComponent(m[1] ?? '');
      const version = q.get('version');
      const t = st()
        .templates.filter((x) => x.name === name && (version === null || x.version === Number(version)))
        .sort((a, b) => b.version - a.version)[0];
      if (!t) refuse(404, 'Template not found', 'template-not-found', {}, name);
      const tpl = t as MockState['templates'][number];
      const body: Template = {
        ...templateSummary(tpl),
        description: tpl.description,
        model: asModel((modelOf(tpl.files) as Read).model),
      };
      send(res, 200, body);
      return true;
    }
    if (rest === '/definitions/validate' && method === 'POST') {
      const body = await readJson(req);
      send(res, 200, validate(treeOf(body.files), { org: org.id, state: st(), changes: 0 }).validation);
      return true;
    }
    if (rest === '/definitions/format' && method === 'POST') {
      const text = (await readRaw(req)).toString('utf8');
      const read = readModel(text);
      if (read instanceof ParseError) {
        refuse(422, 'Unprocessable Content', 'yaml-invalid', {
          parse_error: { line: read.line, column: read.column, message: read.message },
        });
      }
      res.statusCode = 200;
      res.setHeader('Content-Type', 'text/yaml');
      res.end(formatYaml(text));
      return true;
    }

    m = rest.match(/^\/factories\/([^/]+)(\/.*)?$/);
    if (!m) return false;
    const sub = m[2] ?? '';
    const known =
      /^(|\/pause|\/resume|\/draft(\/model(\/[a-z]+(\/revert)?)?|\/files(\/[^/]+)?|\/tree|\/validate|\/publish)?|\/versions(\/[^/]+(\/model|\/files\/[^/]+|\/restore)?)?|\/compare)$/;
    if (!known.test(sub) || (sub === '' && method !== 'GET' && method !== 'DELETE')) return false;
    const f = factoryOf(org, decodeURIComponent(m[1] ?? ''));
    const def = defOf(f);

    if (sub === '' && method === 'GET') {
      send(res, 200, f);
      return true;
    }
    if (sub === '' && method === 'DELETE') {
      if (def.current) refuse(409, 'Conflict', 'factory-published', {}, 'a published factory cannot be deleted');
      const s = st();
      s.factories = s.factories.filter((x) => x.id !== f.id);
      delete s.definitions[f.id];
      emit(f, { kind: 'factory' });
      send(res, 204, null);
      return true;
    }
    if ((sub === '/pause' || sub === '/resume') && method === 'POST') {
      f.paused = sub === '/pause';
      emit(f, { kind: 'current', ...(def.current ? { version_id: def.current } : {}) });
      send(res, 200, f);
      return true;
    }

    // The draft (§8.2).
    if (sub === '/draft' && method === 'GET') {
      const d = draftOf(def);
      send(res, 200, draftOut(f, def, d), { ETag: `"${d.revision}"` });
      return true;
    }
    if (sub === '/draft' && method === 'POST') {
      const body = await readJson(req);
      const wanted = select(def, String(body.base ?? 'current'));
      if (def.draft && !body.replace) {
        if (body.base !== undefined && wanted?.version?.id !== def.draft.base) {
          refuse(409, 'Conflict', 'draft-exists', {}, 'a draft on another version is open');
        }
        send(res, 200, draftOut(f, def, def.draft));
        return true;
      }
      if (wanted?.version?.state !== 'published') refuse(404, 'Version not found', 'version-not-found');
      const base = wanted?.version as MockVersion;
      if (def.draft) def.draft.state = 'archived';
      const at = now();
      def.draft = {
        ...base,
        id: randomUUID(),
        number: null,
        revision: 1,
        state: 'draft',
        message: '',
        source: { kind: 'edit', version: String(base.number) },
        files: new Map(base.files),
        base: base.id,
        start: null,
        created_at: at,
        created_by: localUser,
        updated_at: at,
        updated_by: localUser,
        published_at: null,
        published_by: null,
      };
      sync(f, def);
      emit(f, { kind: 'draft', version_id: def.draft.id, revision: 1, by: actorRef });
      send(res, 201, draftOut(f, def, def.draft));
      return true;
    }
    if (sub === '/draft' && method === 'DELETE') {
      const tag = requireMatch(req);
      const d = draftOf(def);
      if (tag !== String(d.revision))
        refuse(412, 'Precondition Failed', 'draft-conflict', {}, `the draft is at revision ${d.revision}`);
      d.state = 'archived';
      def.draft = null;
      sync(f, def);
      emit(f, { kind: 'draft', version_id: d.id, revision: d.revision, by: actorRef });
      send(res, 204, null);
      return true;
    }
    if (sub === '/draft/model' && method === 'GET') {
      const d = draftOf(def);
      const read = modelOf(d.files);
      if (!read)
        refuse(409, 'Conflict', 'yaml-invalid', {
          parse_error: { line: 0, column: 0, message: 'factory.yaml is missing' },
        });
      if (read instanceof ParseError) {
        refuse(
          409,
          'Conflict',
          'yaml-invalid',
          { parse_error: { line: read.line, column: read.column, message: read.message } },
          read.message,
        );
      }
      const { model, ranges } = read as Read;
      const ref = referenceOf(def, d);
      const refRead = ref ? modelOf(ref) : undefined;
      const refModel =
        refRead && !(refRead instanceof ParseError) ? refRead.model : ({ connections: [] } as unknown as Model);
      const marks: Record<string, unknown> = {};
      const etags = {} as DraftModel['etags'];
      for (const s of sections) {
        etags[s] = sectionEtag(model[s]);
        for (const [p, v] of Object.entries(edited(refModel, model, s))) marks[`/${s}${p}`] = v;
      }
      const body: DraftModel = { revision: d.revision, model: asModel(model), etags, edited: marks, ranges };
      send(res, 200, body);
      return true;
    }
    m = sub.match(/^\/draft\/model\/([a-z]+)(\/revert)?$/);
    if (m && ((method === 'PUT' && !m[2]) || (method === 'POST' && m[2]))) {
      const section = m[1] as Section;
      if (!sections.includes(section)) refuse(400, 'Bad Request', undefined, {}, `section ${section}`);
      const tag = requireMatch(req);
      const body = await readJson(req);
      const d = draftOf(def);
      const text = decode(d.files.get(yamlFile));
      const read = readModel(text, d.files);
      if (read instanceof ParseError) {
        refuse(
          409,
          'Conflict',
          'yaml-invalid',
          { parse_error: { line: read.line, column: read.column, message: read.message } },
          read.message,
        );
      }
      const { model } = read as Read;
      const etag = sectionEtag(model[section]);
      if (tag !== etag) {
        refuse(
          412,
          'Precondition Failed',
          'draft-conflict',
          { section, value: model[section], etag },
          `${section} changed since it was read`,
        );
      }
      let value: unknown;
      const files: Tree = new Map();
      let remove: string[] = [];
      if (m[2]) {
        const pointers = Array.isArray(body.pointers) ? body.pointers.map(String) : [];
        const rel = pointers.map((p) =>
          p === `/${section}` || p.startsWith(`/${section}/`)
            ? p.slice(section.length + 1)
            : refuse(400, 'Bad Request', undefined, {}, `pointer ${p} is not in ${section}`),
        );
        const ref = referenceOf(def, d);
        const refRead = ref ? modelOf(ref) : undefined;
        const refModel =
          refRead && !(refRead instanceof ParseError) ? refRead.model : ({ connections: [] } as unknown as Model);
        value = revert(model[section], edited(refModel, model, section), rel);
      } else {
        if (!('value' in body)) refuse(400, 'Bad Request', undefined, {}, "a section save needs the section's value");
        value = body.value;
        for (const [p, b] of treeOf(body.files)) files.set(p, b);
        remove = Array.isArray(body.remove_files) ? body.remove_files.map((p) => checkPath(cleanPath(String(p)))) : [];
      }
      let out: string;
      try {
        out = applySection(text, section, value);
      } catch (err) {
        if (err instanceof ModelInvalidError)
          return refuse(422, 'Unprocessable Content', 'model-invalid', {}, err.message);
        throw err;
      }
      if (out !== text) files.set(yamlFile, encode(out));
      // A removed prompt another agent still uses is kept.
      const after = readModel(out, new Map([...d.files, ...files]));
      const used =
        after instanceof ParseError
          ? []
          : list((after.model.agents as Json).agents).map((a) =>
              cleanPath(String((a.prompt as Json | undefined)?.path ?? '')),
            );
      commit(
        f,
        d,
        files,
        remove.filter((p) => !used.includes(p)),
      );
      sync(f, def);
      const saved = readModel(decode(d.files.get(yamlFile)), d.files) as Read;
      send(res, 200, saveResult(f, def, d, { etag: sectionEtag(saved.model[section]) }));
      return true;
    }
    if (sub === '/draft/files' && method === 'GET') {
      const d = draftOf(def);
      const files = [...d.files]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([path, b]) => ({ path, sha256: sha(b), size: b.length }));
      send(res, 200, { files });
      return true;
    }
    m = sub.match(/^\/draft\/files\/([^/]+)$/);
    if (m) {
      const path = checkPath(cleanPath(decodeURIComponent(m[1] ?? '')));
      const d = draftOf(def);
      const cur = d.files.get(path);
      if (method === 'GET') {
        if (!cur) refuse(404, 'File not found', 'file-not-found', {}, path);
        fileOut(res, cur as Uint8Array, false);
        return true;
      }
      const conflict = () =>
        refuse(
          412,
          'Precondition Failed',
          'draft-conflict',
          { path, ...(cur ? { sha256: sha(cur) } : {}) },
          `${path} changed since it was read`,
        );
      const match = tagOf(req, 'if-match');
      const none = tagOf(req, 'if-none-match');
      if (method === 'PUT') {
        if (match === undefined && none === undefined) refuse(428, 'Precondition Required', 'precondition-required');
        if (match !== undefined && none !== undefined)
          refuse(400, 'Bad Request', undefined, {}, 'If-Match or If-None-Match, not both');
        if (none === '*' ? cur !== undefined : !cur || sha(cur) !== match) conflict();
        const body = new Uint8Array(await readRaw(req));
        commit(f, d, new Map([[path, body]]));
        sync(f, def);
        send(res, 200, saveResult(f, def, d, { sha256: sha(body) }));
        return true;
      }
      if (method === 'DELETE') {
        const tag = requireMatch(req);
        if (!cur || sha(cur) !== tag) conflict();
        commit(f, d, new Map(), [path]);
        sync(f, def);
        send(res, 200, saveResult(f, def, d));
        return true;
      }
    }
    if (sub === '/draft/tree' && method === 'PUT') {
      const body = await readJson(req);
      const d = draftOf(def);
      if (body.expected_revision !== undefined && body.expected_revision !== d.revision) {
        refuse(412, 'Precondition Failed', 'draft-conflict', {}, `the draft is at revision ${d.revision}`);
      }
      const files = treeOf(body.files);
      commit(
        f,
        d,
        files,
        [...d.files.keys()].filter((p) => !files.has(p)),
      );
      sync(f, def);
      send(res, 200, saveResult(f, def, d));
      return true;
    }
    if (sub === '/draft/validate' && method === 'POST') {
      const body = await readJson(req);
      const d = draftOf(def);
      const files = new Map([...d.files, ...treeOf(body.overlay)]);
      const { validation } = checked(f, files, referenceOf(def, d));
      send(res, 200, { ...validation, revision: d.revision });
      return true;
    }
    if (sub === '/draft/publish' && method === 'POST') {
      const body = await readJson(req);
      const d = draftOf(def);
      if (body.revision !== d.revision)
        refuse(412, 'Precondition Failed', 'draft-conflict', {}, `the draft is at revision ${d.revision}`);
      if ((body.expected_current_version_id ?? null) !== def.current) {
        refuse(409, 'Conflict', 'draft-base-moved', {}, 'another version was published or restored since');
      }
      const { validation, changes } = checked(f, d.files, referenceOf(def, d));
      if (!validation.ok) {
        const name = validation.problems.find((p) => p.code === 'name-taken');
        refuse(
          409,
          'Conflict',
          name ? 'name-taken' : 'definition-invalid',
          { problems: validation.problems },
          'fix the problems first',
        );
      }
      stranded(f, d.files, isObject(body.station_moves) ? body.station_moves : {});
      const at = now();
      const first = def.current === null;
      const message =
        typeof body.message === 'string' && body.message
          ? body.message.slice(0, 200)
          : changes
              .slice(0, 2)
              .map((c) => c.summary)
              .join('; ');
      const version: MockVersion = {
        ...d,
        number: Math.max(0, ...def.versions.map((v) => v.number ?? 0)) + 1,
        state: 'published',
        message,
        files: new Map(d.files),
        published_at: at,
        published_by: localUser,
      };
      def.versions.push(version);
      def.current = version.id;
      def.draft = null;
      if (first && body.start === false) f.paused = true;
      sync(f, def);
      emit(f, {
        kind: 'draft',
        version_id: version.id,
        number: version.number as number,
        revision: version.revision,
        by: actorRef,
      });
      emit(f, { kind: 'current', version_id: version.id, number: version.number as number });
      send(res, 201, versionOut(def, f, version));
      return true;
    }

    // Versions and compare (§8.3).
    if (sub === '/versions' && method === 'GET') {
      const limit = Number(q.get('limit') ?? 20);
      if (!Number.isInteger(limit) || limit < 1 || limit > 50)
        refuse(400, 'Bad Request', undefined, {}, 'limit must be in [1, 50]');
      const before = Number(q.get('before') ?? 0);
      const all = [...def.versions].reverse().filter((v) => !before || (v.number ?? 0) < before);
      const page = all.slice(0, limit);
      send(res, 200, {
        versions: page.map((v) => versionOut(def, f, v)),
        next_before: all.length > limit ? (page.at(-1)?.number ?? null) : null,
      });
      return true;
    }
    m = sub.match(/^\/versions\/([^/]+)(\/model|\/files\/([^/]+)|\/restore)?$/);
    if (m) {
      const selector = decodeURIComponent(m[1] ?? '');
      const picked = select(def, selector);
      if (!picked) refuse(404, 'Version not found', 'version-not-found', {}, selector);
      const { version, files } = picked as NonNullable<typeof picked>;
      if (!m[2] && method === 'GET') {
        if (!version) refuse(404, 'Version not found', 'version-not-found', {}, selector);
        const v = version as MockVersion;
        const detail: VersionDetail = {
          ...versionOut(def, f, v),
          files: [...v.files]
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([path, b]) => ({ path, sha256: sha(b), size: b.length })),
        };
        send(res, 200, detail);
        return true;
      }
      if (m[2] === '/model' && method === 'GET') {
        const read = modelOf(files);
        if (!read || read instanceof ParseError) refuse(409, 'Conflict', 'yaml-invalid');
        const { model, ranges } = read as Read;
        send(res, 200, { model: asModel(model), ranges });
        return true;
      }
      if (m[3] !== undefined && method === 'GET') {
        const path = cleanPath(decodeURIComponent(m[3]));
        const b = files.get(path);
        if (!b) refuse(404, 'File not found', 'file-not-found', {}, path);
        fileOut(res, b as Uint8Array, version?.state === 'published');
        return true;
      }
      if (m[2] === '/restore' && method === 'POST') {
        const body = await readJson(req);
        if (version?.state !== 'published') refuse(404, 'Version not found', 'version-not-found', {}, selector);
        if ((body.expected_current_version_id ?? null) !== def.current) {
          refuse(409, 'Conflict', 'draft-base-moved', {}, 'another version was published or restored since');
        }
        const v = version as MockVersion;
        stranded(f, v.files, isObject(body.station_moves) ? body.station_moves : {});
        def.current = v.id;
        sync(f, def);
        emit(f, { kind: 'current', version_id: v.id, number: v.number as number });
        send(res, 200, f);
        return true;
      }
    }
    if (sub === '/compare' && method === 'GET') {
      const from = q.get('from') ?? 'base';
      const to = q.get('to') ?? 'draft';
      const a = select(def, from);
      const b = select(def, to);
      if (!a || !b) refuse(404, 'Version not found', 'version-not-found', {}, !a ? from : to);
      const sideOf = (selector: string, v: MockVersion | undefined): CompareSide => ({
        selector,
        id: v?.id ?? null,
        number: v?.number ?? null,
        state: v?.state ?? null,
      });
      const { files, changes } = diff(side(a?.files), side(b?.files));
      const body: Comparison = {
        from: sideOf(from, a?.version),
        to: sideOf(to, b?.version),
        files,
        changes,
        counts: { changes: changes.length, sections: new Set(changes.map((c) => c.section)).size },
      };
      send(res, 200, body);
      return true;
    }
    return false;
  }

  return {
    handle: async (org, rest, method, url, req, res) => {
      try {
        return await route(org, rest, method, url, req, res);
      } catch (err) {
        if (err instanceof Refusal) {
          send(res, err.status, err.body);
          return true;
        }
        throw err;
      }
    },

    faultBody: (path, fault) => {
      const status = fault.status ?? 500;
      const title =
        { 409: 'Conflict', 412: 'Precondition Failed', 428: 'Precondition Required' }[status] ?? 'Injected failure';
      const m = path.match(/^\/v1\/orgs\/([^/]+)\/factories\/([^/]+)\/draft(?:\/model\/([a-z]+)|\/files\/([^/]+))?/);
      const org = m ? st().orgs.find((o) => o.slug === m[1] || o.id === m[1]) : undefined;
      const f = org
        ? st().factories.find((x) => x.org_id === org.id && (x.id === m?.[2] || x.name === m?.[2]))
        : undefined;
      const d = f ? st().definitions[f.id]?.draft : undefined;
      if (status === 412) {
        const extra: Json = {};
        if (d && m?.[3]) {
          const read = modelOf(d.files);
          if (read && !(read instanceof ParseError)) {
            const section = m[3] as Section;
            Object.assign(extra, { section, value: read.model[section], etag: sectionEtag(read.model[section]) });
          }
        }
        if (d && m?.[4]) {
          const p = cleanPath(decodeURIComponent(m[4]));
          const cur = d.files.get(p);
          Object.assign(extra, { path: p, ...(cur ? { sha256: sha(cur) } : {}) });
        }
        return problem(412, title, fault.code ?? 'draft-conflict', extra);
      }
      if (status === 428) return problem(428, title, fault.code ?? 'precondition-required');
      if (status === 409 && (fault.code ?? 'definition-invalid') === 'definition-invalid') {
        const own = f && d ? checked(f, d.files, referenceOf(defOf(f), d)).validation.problems : [];
        const problems =
          fault.problems ??
          (own.length
            ? own
            : [
                located(
                  'unknown-agent',
                  'error',
                  'references',
                  'line',
                  '/line/stations/1/agent',
                  'station 1: unknown agent "ghost"',
                  {},
                  [],
                ),
              ]);
        return problem(409, title, 'definition-invalid', { problems }, 'fix the problems first');
      }
      if (status === 409 && fault.code === 'yaml-invalid') {
        return problem(409, title, 'yaml-invalid', { parse_error: { line: 1, column: 1, message: 'injected' } });
      }
      return problem(status, title, fault.code);
    },
  };
}
