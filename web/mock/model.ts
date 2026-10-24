// The mock's factory model: factory.yaml read as the Visual editor's five sections (readModel)
// and a section written back into the YAML (applySection) on the `yaml` package's Document, so
// comments, key order, flow and block style, quotes and blank lines survive a Visual save, as
// goald's internal/factorymodel guarantees. It mirrors factorymodel's rules closely enough for
// the editor and its tests: camelCase YAML keys are snake_case model keys (data keys such as
// route lanes, tool names and connector settings are kept as written), identified lists
// (stations by id; agents, skills and connections by name) are matched by identity, never by
// index, null and [] remove a key, and a save that changes nothing leaves the file as it was.
import { createHash } from 'node:crypto';
import { posix } from 'node:path';
import {
  type Document,
  isAlias,
  isMap,
  isScalar,
  isSeq,
  LineCounter,
  Pair,
  parseDocument,
  Scalar,
  type YAMLMap,
  type YAMLSeq,
} from 'yaml';

export type Section = 'basics' | 'line' | 'agents' | 'connections' | 'review';

export const sections: readonly Section[] = ['basics', 'line', 'agents', 'connections', 'review'];

/** The factory model: connections is a list, every other section an object. */
export type Model = Record<Section, unknown>;

/** Model pointers ("/line/stations/3") → the first and last line of factory.yaml holding them. */
export type Ranges = Record<string, [number, number]>;

/** A factory.yaml that does not parse: Visual is read-only until it is fixed in YAML. */
export class ParseError extends Error {
  constructor(
    readonly line: number,
    readonly column: number,
    message: string,
  ) {
    super(message);
    this.name = 'ParseError';
  }
}

/** A section value of the wrong shape (goald's 422 model-invalid). */
export class ModelInvalidError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ModelInvalidError';
  }
}

type Json = Record<string, unknown>;
type Files = ReadonlyMap<string, string | Uint8Array>;

type Kind = 'generic' | 'prompt' | 'runMode' | 'agent' | 'connection';

/** How one position of factory.yaml maps to the model (factorymodel's shape). */
interface Shape {
  kind?: Kind;
  /** Keys kept as written (data keys); otherwise camelCase ↔ snake_case. */
  verbatim?: boolean;
  fields?: Record<string, Shape>;
  /** The shape of every other child, or of a list's items. */
  elem?: Shape;
  /** Canonical YAML key order for inserted keys. */
  order?: string[];
  /** The key naming a list's items: they are matched by it. */
  identity?: string;
}

const renamed: Shape = {};
const verbatim: Shape = { verbatim: true };

const station: Shape = {
  order: [
    'id',
    'name',
    'description',
    'kind',
    'agent',
    'route',
    'agents',
    'wip',
    'buffer',
    'output',
    'checks',
    'gate',
    'action',
  ],
  fields: {
    route: verbatim,
    checks: { elem: { order: ['kind', 'run', 'min'] } },
    gate: { order: ['onReject', 'maxRework'] },
  },
};

const agent: Shape = {
  kind: 'agent',
  order: [
    'name',
    'description',
    'prompt',
    'model',
    'maxTurns',
    'perItemUsd',
    'skills',
    'connections',
    'tools',
    'builtins',
  ],
  fields: {
    prompt: { kind: 'prompt' },
    tools: verbatim,
    builtins: {
      order: ['read', 'edit', 'run', 'web', 'subagents'],
      fields: { run: { kind: 'runMode', order: ['mode', 'commands'] } },
    },
  },
};

const connection: Shape = {
  kind: 'connection',
  verbatim: true,
  order: [
    'connector',
    'name',
    'type',
    'credential',
    'agents',
    'tools',
    'transport',
    'url',
    'image',
    'command',
    'headers',
    'env',
    'network',
    'roleArn',
    'region',
  ],
  fields: {
    tools: { verbatim: true, elem: { verbatim: true, order: ['enabled', 'agents', 'approval', 'scope', 'access'] } },
  },
};

const connectionsShape: Shape = { elem: connection, identity: 'name' };

interface Binding {
  /** The key in the section's value; '' for the whole value (connections). */
  key: string;
  path: string[];
  shape: Shape;
}

const bindings: Record<Section, Binding[]> = {
  basics: [
    { key: 'id', path: ['metadata', 'id'], shape: renamed },
    { key: 'name', path: ['metadata', 'name'], shape: renamed },
    { key: 'title', path: ['metadata', 'title'], shape: renamed },
    { key: 'purpose', path: ['metadata', 'purpose'], shape: renamed },
    { key: 'work_types', path: ['spec', 'board', 'workTypes'], shape: verbatim },
    {
      key: 'repository',
      path: ['spec', 'runtime', 'repository'],
      shape: { verbatim: true, order: ['url', 'token', 'connection', 'repo'] },
    },
  ],
  line: [
    { key: 'stations', path: ['spec', 'board', 'stations'], shape: { elem: station, identity: 'id' } },
    {
      key: 'intake',
      path: ['spec', 'board', 'intake'],
      shape: { elem: { order: ['source', 'repo', 'labels', 'cron', 'template', 'type'] } },
    },
    {
      key: 'flow',
      path: ['spec', 'board', 'flow'],
      shape: {
        order: ['expedite', 'constraint', 'release', 'andon', 'targets'],
        fields: {
          expedite: { order: ['types', 'wip'] },
          andon: { order: ['reworkRate', 'window'] },
          targets: { order: ['throughputPerDay', 'leadTimeHours', 'expediteLeadTimeMinutes'] },
        },
      },
    },
    { key: 'librarian', path: ['spec', 'learning', 'librarian', 'agent'], shape: renamed },
  ],
  agents: [
    { key: 'default_model', path: ['spec', 'runtime', 'model'], shape: renamed },
    { key: 'agents', path: ['spec', 'agents'], shape: { elem: agent, identity: 'name' } },
    {
      key: 'skills',
      path: ['spec', 'skills'],
      shape: { elem: { order: ['name', 'source', 'origin'] }, identity: 'name' },
    },
  ],
  connections: [{ key: '', path: ['spec', 'connections'], shape: connectionsShape }],
  review: [
    {
      key: 'budget',
      path: ['spec', 'runtime', 'budget'],
      shape: { order: ['dailyUsd', 'weeklyUsd', 'onExhausted', 'perItemUsd', 'maxTurnsPerTask'] },
    },
  ],
};

/** The connection keys the model shows by name; every other key is a setting. */
const connectionKeys = new Set(['name', 'connector', 'type', 'credential', 'agents', 'tools']);

/** Canonical key order of the mappings that hold the sections, for keys an edit creates. */
const containerOrder: Record<string, string[]> = {
  '': ['apiVersion', 'kind', 'metadata', 'spec'],
  metadata: ['id', 'name', 'title', 'purpose', 'description', 'owner'],
  spec: ['runtime', 'knowledgeBase', 'secrets', 'connections', 'skills', 'contracts', 'agents', 'board', 'learning'],
  'spec.runtime': ['runner', 'image', 'auth', 'model', 'repository', 'budget'],
  'spec.board': ['workTypes', 'intake', 'stations', 'flow'],
  'spec.learning': ['librarian', 'kaizen'],
  'spec.learning.librarian': ['agent', 'after', 'writes'],
};

/** How a document pads its flow collections: `{ a: b }` or `{a: b}`, `[a, b]` or `[ a, b ]`. */
interface FlowStyle {
  map: boolean;
  seq: boolean;
}

function flowStyle(text: string): FlowStyle {
  const count = (re: RegExp) => (text.match(re) ?? []).length;
  return { map: count(/\{ \S/g) >= count(/\{[^\s}]/g), seq: count(/\[ \S/g) > count(/\[[^\s\]]/g) };
}

/** One encoded line with its flow collections padded the document's way; quotes and comments kept. */
function padLine(line: string, style: FlowStyle): string {
  let out = '';
  let quote = '';
  for (let i = 0; i < line.length; i++) {
    const c = line[i] as string;
    if (quote) {
      out += c;
      if (c === quote && line[i - 1] !== '\\') quote = '';
      continue;
    }
    if (c === '#' && (i === 0 || line[i - 1] === ' ')) return out + line.slice(i);
    if (c === '"' || (c === "'" && /[\s,[{:]/.test(line[i - 1] ?? ' '))) quote = c;
    const open = c === '{' || c === '[';
    const close = c === '}' || c === ']';
    const pad = c === '{' || c === '}' ? style.map : style.seq;
    if (open) {
      out += c;
      while (line[i + 1] === ' ') i++;
      const empty = line[i + 1] === (c === '{' ? '}' : ']');
      if (pad && !empty) out += ' ';
      continue;
    }
    if (close) {
      const prev = out.at(-1);
      out = out.replace(/ +$/, '');
      if (pad && prev !== '{' && prev !== '[' && !/[{[]$/.test(out)) out += ' ';
      out += c;
      continue;
    }
    out += c;
  }
  return out;
}

/** The document as text, in the original's flow style. */
function encode(doc: Document, style: FlowStyle): string {
  const text = doc.toString({ lineWidth: 0, flowCollectionPadding: false });
  return text
    .split('\n')
    .map((l) => (/[[{]/.test(l) ? padLine(l, style) : l))
    .join('\n');
}

/**
 * The edited encoding with the original text kept on every line the edit did not change: the
 * unedited tree is encoded the same way and aligned with both (goald's line-keeping), so only
 * changed lines are normalized. When the encodings cannot be aligned with the original, the
 * edited encoding is the answer.
 */
function keepLines(original: string, unedited: string, edited: string): string {
  const orig = original.split('\n');
  const base = unedited.split('\n');
  if (orig.length !== base.length) return edited;
  const next = edited.split('\n');
  // Longest common subsequence of base and next lines.
  const n = base.length;
  const m = next.length;
  const width = m + 1;
  const lcs = new Int32Array((n + 1) * width);
  const at = (i: number, j: number) => lcs[i * width + j] ?? 0;
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i * width + j] = base[i] === next[j] ? at(i + 1, j + 1) + 1 : Math.max(at(i + 1, j), at(i, j + 1));
    }
  }
  const out: string[] = [];
  let i = 0;
  let j = 0;
  while (j < m) {
    if (i < n && base[i] === next[j]) {
      out.push(orig[i] as string);
      i++;
      j++;
    } else if (i < n && at(i + 1, j) >= at(i, j + 1)) i++;
    else out.push(next[j++] as string);
  }
  return out.join('\n');
}

const snake = (s: string) => s.replace(/[A-Z]/g, (c, i: number) => (i > 0 ? '_' : '') + c.toLowerCase());
const camel = (s: string) => s.replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase());

function child(s: Shape, yamlKey?: string): Shape {
  if (yamlKey !== undefined && s.fields?.[yamlKey]) return s.fields[yamlKey];
  if (s.elem) return s.elem;
  return s.verbatim ? verbatim : renamed;
}

const modelKey = (s: Shape, k: string) => (s.verbatim ? k : snake(k));
const yamlKey = (s: Shape, k: string) => (s.verbatim ? k : camel(k));

const isObject = (v: unknown): v is Json => typeof v === 'object' && v !== null && !Array.isArray(v);

/** A value that writes nothing: absent, null or an empty list. */
const isEmpty = (v: unknown) => v === undefined || v === null || (Array.isArray(v) && v.length === 0);

const escapeToken = (k: string) => k.replaceAll('~', '~0').replaceAll('/', '~1');
const unescapeToken = (k: string) => k.replaceAll('~1', '/').replaceAll('~0', '~');

// Parsing.

interface Parsed {
  doc: Document.Parsed;
  lines: LineCounter;
  text: string;
}

function parse(text: string): Parsed {
  const lines = new LineCounter();
  const doc = parseDocument(text, { lineCounter: lines, keepSourceTokens: false });
  const err = doc.errors[0];
  if (err) {
    const at = err.linePos?.[0];
    throw new ParseError(at?.line ?? 0, at?.col ?? 0, err.message.split('\n')[0] ?? err.message);
  }
  return { doc, lines, text };
}

function deref(p: Parsed | Document, n: unknown): unknown {
  let node = n;
  const doc = 'doc' in p ? p.doc : p;
  for (let i = 0; i < 10 && isAlias(node); i++) node = node.resolve(doc);
  return node;
}

function pairOf(map: unknown, key: string): Pair | undefined {
  if (!isMap(map)) return undefined;
  return (map as YAMLMap).items.find((it) => keyOf(it) === key) as Pair | undefined;
}

function keyOf(pair: Pair): string {
  return isScalar(pair.key) ? String(pair.key.value) : String(pair.key);
}

function lookup(p: Parsed, path: string[]): { pair: Pair | undefined; node: unknown } {
  let node: unknown = p.doc.contents;
  let pair: Pair | undefined;
  for (const k of path) {
    pair = pairOf(deref(p, node), k);
    if (!pair) return { pair: undefined, node: undefined };
    node = pair.value;
  }
  return { pair, node };
}

/** Where the purpose is written: metadata.purpose, or the legacy description when only it is there. */
function purposePath(p: Parsed): string[] {
  if (!lookup(p, ['metadata', 'purpose']).pair && lookup(p, ['metadata', 'description']).pair) {
    return ['metadata', 'description'];
  }
  return ['metadata', 'purpose'];
}

const pathOf = (p: Parsed, b: Binding) => (b.key === 'purpose' ? purposePath(p) : b.path);

// Reading.

interface Reader {
  p: Parsed;
  files: Files | undefined;
  ranges: Ranges;
}

/** Records the lines of a value (from its key's line when it has one) at pointer. */
function record(r: Reader, pointer: string, key: unknown, value: unknown) {
  const node = value as { range?: [number, number, number] } | null;
  const keyNode = key as { range?: [number, number, number] } | null;
  const start = keyNode?.range?.[0] ?? node?.range?.[0];
  if (start === undefined) return;
  let end = node?.range?.[1] ?? keyNode?.range?.[1] ?? start + 1;
  while (end > start + 1 && /\s/.test(r.p.text[end - 1] ?? '')) end--;
  const first = r.p.lines.linePos(start).line;
  const last = r.p.lines.linePos(Math.max(start, end - 1)).line;
  const cur = r.ranges[pointer];
  r.ranges[pointer] = cur ? [Math.min(cur[0], first), Math.max(cur[1], last)] : [first, last];
}

function scalarValue(n: Scalar): unknown {
  const v = n.value;
  return typeof v === 'bigint' ? Number(v) : v;
}

function readNode(r: Reader, n: unknown, s: Shape, pointer: string): unknown {
  const node = deref(r.p, n);
  if (s.kind === 'prompt' && isScalar(node)) return promptOf(r.files, String(node.value));
  if (s.kind === 'runMode' && isScalar(node)) return { mode: scalarValue(node) };
  if (isMap(node)) return readMap(r, node as YAMLMap, s, pointer);
  if (isSeq(node)) {
    return (node as YAMLSeq).items.map((c, i) => {
      const p = `${pointer}/${i}`;
      record(r, p, null, c);
      return readNode(r, c, child(s), p);
    });
  }
  if (isScalar(node)) return scalarValue(node);
  return null;
}

function readMap(r: Reader, map: YAMLMap, s: Shape, pointer: string): Json {
  const out: Json = {};
  for (const item of map.items as Pair[]) {
    const k = keyOf(item);
    if (s.kind === 'agent' && k === 'tools') {
      out.custom_tools = true;
      record(r, `${pointer}/custom_tools`, item.key, item.value);
      continue;
    }
    if (s.kind === 'connection' && !connectionKeys.has(k)) {
      const settings = (out.settings as Json | undefined) ?? {};
      out.settings = settings;
      const p = `${pointer}/settings/${escapeToken(k)}`;
      record(r, p, item.key, item.value);
      settings[k] = readNode(r, item.value, verbatim, p);
      continue;
    }
    const mk = modelKey(s, k);
    const p = `${pointer}/${escapeToken(mk)}`;
    record(r, p, item.key, item.value);
    out[mk] = readNode(r, item.value, child(s, k), p);
  }
  if (s.kind === 'connection') {
    if (out.name === undefined && typeof out.connector === 'string') out.name = connectorName(out.connector);
    out.legacy = 'type' in out;
  }
  return out;
}

const connectorName = (c: string) => c.split('@')[0] ?? c;

function bytesOf(v: string | Uint8Array): Uint8Array {
  return typeof v === 'string' ? new TextEncoder().encode(v) : v;
}

/** A definition path as stored: relative, slash-separated, without "./". */
export function cleanPath(p: string): string {
  return posix.normalize(p).replace(/^\.\//, '');
}

function promptOf(files: Files | undefined, path: string): Json {
  const out: Json = { path };
  const body = files?.get(cleanPath(path));
  if (body === undefined) return out;
  const bytes = bytesOf(body);
  out.sha256 = createHash('sha256').update(bytes).digest('hex');
  out.words = new TextDecoder().decode(bytes).split(/\s+/).filter(Boolean).length;
  return out;
}

/** The model of a factory.yaml and where its pointers are, or where it stops parsing. */
export function readModel(text: string, files?: Files): { model: Model; ranges: Ranges } | ParseError {
  let p: Parsed;
  try {
    p = parse(text);
  } catch (err) {
    if (err instanceof ParseError) return err;
    throw err;
  }
  const r: Reader = { p, files, ranges: {} };
  const model = {} as Model;
  for (const section of sections) {
    if (section === 'connections') {
      const b = bindings.connections[0] as Binding;
      const { pair, node } = lookup(p, b.path);
      if (pair) record(r, '/connections', pair.key, pair.value);
      const v = node === undefined ? [] : readNode(r, node, b.shape, '/connections');
      model.connections = Array.isArray(v) ? v : [];
      continue;
    }
    const out: Json = {};
    for (const b of bindings[section]) {
      const { pair, node } = lookup(p, pathOf(p, b));
      if (!pair) continue;
      const pointer = `/${section}/${b.key}`;
      record(r, pointer, pair.key, pair.value);
      out[b.key] = readNode(r, node, b.shape, pointer);
    }
    model[section] = out;
  }
  for (const section of sections) {
    const lines = sectionLines(r.ranges, section);
    if (lines[0] > 0) r.ranges[`/${section}`] = r.ranges[`/${section}`] ?? lines;
  }
  return { model, ranges: r.ranges };
}

/** The lines a section spans: the first and last line of any of its values; [0, 0] when none. */
export function sectionLines(ranges: Ranges, section: Section): [number, number] {
  let from = 0;
  let to = 0;
  const prefix = `/${section}`;
  for (const [p, [a, b]] of Object.entries(ranges)) {
    if (p !== prefix && !p.startsWith(`${prefix}/`)) continue;
    if (from === 0 || a < from) from = a;
    to = Math.max(to, b);
  }
  return [from, to];
}

// Writing.

function checkShape(section: Section, value: unknown): void {
  if (section === 'connections') {
    if (!Array.isArray(value)) throw new ModelInvalidError('connections is a list');
    return;
  }
  if (!isObject(value)) throw new ModelInvalidError(`${section} is an object`);
  const known = new Set(bindings[section].map((b) => b.key));
  for (const k of Object.keys(value)) {
    if (!known.has(k)) throw new ModelInvalidError(`${section} has no key ${k}`);
  }
}

/**
 * factory.yaml with one section's new value applied on its node tree: what the section does not
 * own is untouched, and so are the comments and style of what it does. Throws ParseError and
 * ModelInvalidError.
 */
export function applySection(text: string, section: Section, value: unknown): string {
  const p = parse(text);
  checkShape(section, value);

  const before = readModel(text);
  if (
    !(before instanceof ParseError) &&
    equal(normalizeSection(section, before.model[section]), normalizeSection(section, value))
  ) {
    return text;
  }

  for (const b of bindings[section]) {
    const v = section === 'connections' ? value : (value as Json)[b.key];
    if (b.key === 'id' && v === undefined) continue; // metadata.id is read-only
    setAt(p, pathOf(p, b), v, b.shape);
  }
  const style = flowStyle(text);
  const out = keepLines(text, encode(parse(text).doc, style), encode(p.doc, style));
  if (readModel(out) instanceof ParseError) throw new Error('the edited factory.yaml does not read back');
  return out;
}

/** A file formatted the way Visual saves write it ("Format"); throws ParseError. */
export function formatYaml(text: string): string {
  return encode(parse(text).doc, flowStyle(text));
}

function setAt(p: Parsed, path: string[], value: unknown, shape: Shape): void {
  const parentPath = path.slice(0, -1);
  const key = path.at(-1) as string;
  if (isEmpty(value)) {
    const parent = deref(p, lookup(p, parentPath).node ?? (parentPath.length === 0 ? p.doc.contents : undefined));
    if (isMap(parent)) {
      const map = parent as YAMLMap;
      map.items = map.items.filter((it) => keyOf(it as Pair) !== key);
    }
    return;
  }
  const parent = ensureMap(p, parentPath);
  const pair = pairOf(parent, key);
  if (pair) {
    pair.value = setValue(p.doc, pair.value, value, shape);
    return;
  }
  insertPair(parent, key, toNode(p.doc, value, shape), containerOrder[parentPath.join('.')] ?? []);
}

function ensureMap(p: Parsed, path: string[]): YAMLMap {
  let node = deref(p, p.doc.contents);
  if (!isMap(node)) throw new ModelInvalidError('factory.yaml is not a mapping');
  for (let i = 0; i < path.length; i++) {
    const k = path[i] as string;
    const map: YAMLMap = node as YAMLMap;
    const pair = pairOf(map, k);
    let next = pair ? deref(p, pair.value) : undefined;
    if (!isMap(next)) {
      next = p.doc.createNode({});
      if (pair) pair.value = next;
      else insertPair(map, k, next, containerOrder[path.slice(0, i).join('.')] ?? []);
    }
    node = next;
  }
  return node as YAMLMap;
}

function insertPair(map: YAMLMap, key: string, value: unknown, order: string[]): void {
  const pair = new Pair(new Scalar(key), value);
  const at = order.indexOf(key);
  const items = map.items as Pair[];
  if (at >= 0) {
    let after = -1;
    let before = -1;
    items.forEach((it, i) => {
      const j = order.indexOf(keyOf(it));
      if (j < 0) return;
      if (j < at) after = i;
      else if (j > at && before < 0) before = i;
    });
    if (after >= 0) return void items.splice(after + 1, 0, pair);
    if (before >= 0) return void items.splice(before, 0, pair);
  }
  items.push(pair);
}

type Styled = { comment?: string | null; commentBefore?: string | null; spaceBefore?: boolean; flow?: boolean };

function setValue(doc: Document, node: unknown, value: unknown, s: Shape): unknown {
  if (isAlias(node)) return toNode(doc, value, s);
  if (s.kind === 'prompt' && isScalar(node)) {
    const path = isObject(value) ? value.path : value;
    if (typeof path === 'string') {
      if (node.value !== path) node.value = path;
      return node;
    }
  }
  if (s.kind === 'runMode' && isScalar(node) && isObject(value) && isEmpty(value.commands)) {
    if (node.value !== value.mode) node.value = value.mode;
    return node;
  }
  if (isObject(value) && isMap(node)) {
    mergeMap(doc, node as YAMLMap, value, s);
    return node;
  }
  if (Array.isArray(value) && isSeq(node)) {
    mergeSeq(doc, node as YAMLSeq, value, s);
    return node;
  }
  if (!isObject(value) && !Array.isArray(value) && isScalar(node)) {
    if (node.value !== value) {
      node.value = value;
      if (typeof value !== 'string') node.type = Scalar.PLAIN;
    }
    return node;
  }
  const old = node as Styled;
  const fresh = toNode(doc, value, s) as Styled;
  if (old?.comment) fresh.comment = old.comment;
  if (old?.commentBefore) fresh.commentBefore = old.commentBefore;
  if (old?.spaceBefore) fresh.spaceBefore = old.spaceBefore;
  if (old?.flow && (isMap(fresh) || isSeq(fresh))) fresh.flow = true;
  return fresh;
}

/** A model object as written to YAML at shape s: settings flattened, read-only keys left out. */
function yamlEntries(value: Json, s: Shape): [string, unknown][] {
  let v = value;
  if (s.kind === 'connection') {
    const { settings, legacy: _legacy, ...rest } = v;
    v = { ...rest, ...(isObject(settings) ? settings : {}) };
  }
  if (s.kind === 'agent') {
    const { custom_tools: _custom, ...rest } = v;
    v = rest;
  }
  return Object.entries(v).map(([k, val]) => [yamlKey(s, k), val]);
}

function mergeMap(doc: Document, map: YAMLMap, value: Json, s: Shape): void {
  const entries = new Map(yamlEntries(value, s));
  if (s.kind === 'connection' && !pairOf(map, 'name')) {
    const connector = entries.get('connector');
    if (typeof connector === 'string' && entries.get('name') === connectorName(connector)) entries.delete('name');
  }
  const keepTools = s.kind === 'agent' && isEmpty(value.builtins);

  const items: Pair[] = [];
  for (const pair of map.items as Pair[]) {
    const k = keyOf(pair);
    if (s.kind === 'agent' && k === 'tools') {
      if (keepTools) items.push(pair);
      continue;
    }
    const v = entries.get(k);
    entries.delete(k);
    if (isEmpty(v)) continue;
    pair.value = setValue(doc, pair.value, v, child(s, k));
    items.push(pair);
  }
  map.items = items;
  for (const [k, v] of entries) {
    if (isEmpty(v)) continue;
    insertPair(map, k, toNode(doc, v, child(s, k)), s.order ?? []);
  }
}

function identityOfNode(doc: Document, n: unknown, s: Shape): string {
  const node = isAlias(n) ? n.resolve(doc) : n;
  const v = pairOf(node, s.identity ?? '')?.value;
  const id = isScalar(v) ? String(v.value) : '';
  if (id || s.elem?.kind !== 'connection') return id;
  const c = pairOf(node, 'connector')?.value;
  return isScalar(c) ? connectorName(String(c.value)) : '';
}

function identityOfValue(v: unknown, s: Shape): string {
  if (!isObject(v)) return '';
  const id = v[snake(s.identity ?? '')];
  if (typeof id === 'string' && id) return id;
  if (s.elem?.kind === 'connection' && typeof v.connector === 'string') return connectorName(v.connector);
  return '';
}

function mergeSeq(doc: Document, seq: YAMLSeq, values: unknown[], s: Shape): void {
  const elem = child(s);
  const flow = seq.items.some((it) => isMap(it) && (it as YAMLMap).flow);
  const fresh = (v: unknown) => {
    const n = toNode(doc, v, elem);
    if (flow && isMap(n) && JSON.stringify(toYaml(v, elem)).length <= 90) (n as YAMLMap).flow = true;
    return n;
  };

  if (s.identity) {
    const byId = new Map<string, unknown>();
    for (const it of seq.items) {
      const id = identityOfNode(doc, it, s);
      if (id && !byId.has(id)) byId.set(id, it);
    }
    seq.items = values.map((v) => {
      const id = identityOfValue(v, s);
      const node = id ? byId.get(id) : undefined;
      if (node === undefined) return fresh(v);
      byId.delete(id);
      return setValue(doc, node, v, elem);
    });
    return;
  }
  seq.items = values.map((v, i) => (i < seq.items.length ? setValue(doc, seq.items[i], v, elem) : fresh(v)));
}

/** A model value as the plain YAML value written for it, keys in canonical order. */
function toYaml(value: unknown, s: Shape): unknown {
  if (s.kind === 'prompt') return isObject(value) ? value.path : value;
  if (s.kind === 'runMode' && isObject(value) && isEmpty(value.commands)) return value.mode;
  if (Array.isArray(value)) return value.map((v) => toYaml(v, child(s)));
  if (!isObject(value)) return value;
  const order = s.order ?? [];
  const rank = (k: string) => {
    const i = order.indexOf(k);
    return i < 0 ? order.length : i;
  };
  const entries = yamlEntries(value, s)
    .filter(([, v]) => !isEmpty(v))
    .sort(([a], [b]) => rank(a) - rank(b));
  return Object.fromEntries(entries.map(([k, v]) => [k, toYaml(v, child(s, k))]));
}

function toNode(doc: Document, value: unknown, s: Shape): unknown {
  return doc.createNode(toYaml(value, s));
}

// Comparing.

/** A JSON text of v with object keys sorted: equal values, equal text. */
export function canonical(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(canonical).join(',')}]`;
  if (isObject(v)) {
    const keys = Object.keys(v)
      .filter((k) => v[k] !== undefined)
      .sort();
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonical(v[k])}`).join(',')}}`;
  }
  return JSON.stringify(v ?? null);
}

export const equal = (a: unknown, b: unknown) => canonical(a) === canonical(b);

/** A section's concurrency token: the first 8 bytes of the sha256 of its value. */
export function sectionEtag(value: unknown): string {
  return createHash('sha256').update(canonical(value)).digest('hex').slice(0, 16);
}

function readOnlyKey(s: Shape, k: string): boolean {
  switch (s.kind) {
    case 'prompt':
      return k === 'sha256' || k === 'words';
    case 'agent':
      return k === 'custom_tools';
    case 'connection':
      return k === 'legacy';
    default:
      return false;
  }
}

/** A value as compared: nulls and empty lists absent, read-only parts left out, short forms expanded. */
function normalize(value: unknown, s: Shape): unknown {
  let v = value;
  if (typeof v === 'string' && s.kind === 'prompt') v = { path: v };
  if (typeof v === 'string' && s.kind === 'runMode') v = { mode: v };
  if (Array.isArray(v)) return v.map((e) => normalize(e, child(s)));
  if (!isObject(v)) return v;
  const out: Json = {};
  for (const [k, val] of Object.entries(v)) {
    if (readOnlyKey(s, k)) continue;
    const n = normalize(val, child(s, yamlKey(s, k)));
    if (isEmpty(n) || (s.kind === 'connection' && k === 'settings' && isObject(n) && Object.keys(n).length === 0)) {
      continue;
    }
    out[k] = n;
  }
  if (s.kind === 'connection' && out.name === undefined && typeof out.connector === 'string') {
    out.name = connectorName(out.connector);
  }
  return out;
}

/** A section's value as compared. */
export function normalizeSection(section: Section, value: unknown): unknown {
  if (section === 'connections') return normalize(value, connectionsShape);
  if (!isObject(value)) return value;
  const out: Json = {};
  for (const b of bindings[section]) {
    const n = normalize(value[b.key], b.shape);
    if (!isEmpty(n)) out[b.key] = n;
  }
  return out;
}

/** A value at one key of a section (an item of its list when item is set) as compared. */
export function normalizeAt(section: Section, key: string, value: unknown, item = false): unknown {
  const b = bindings[section].find((x) => x.key === key);
  if (!b) return value;
  return normalize(value, item ? (b.shape.elem ?? renamed) : b.shape);
}

/** The identity of an item of a section's identified list at key (a station's id, an agent's name). */
export function identityOf(section: Section, key: string, item: unknown): string {
  const b = bindings[section].find((x) => x.key === key);
  return b?.shape.identity ? identityOfValue(normalize(item, b.shape.elem ?? renamed), b.shape) : '';
}

/**
 * What a draft changed in one section against its base, for "Edited · Undo": section-relative
 * pointers into the draft's value mapped to the base's value there (null where it had none).
 * Identified lists are matched by identity, so a moved station is not edited.
 */
export function edited(base: Model, draft: Model, section: Section): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (section === 'connections') {
    editedList(asList(base.connections), asList(draft.connections), connectionsShape, '', out);
    return out;
  }
  const bm = isObject(base[section]) ? (base[section] as Json) : {};
  const dm = isObject(draft[section]) ? (draft[section] as Json) : {};
  for (const b of bindings[section]) {
    const p = `/${b.key}`;
    const bv = bm[b.key];
    const dv = dm[b.key];
    if (b.shape.identity && Array.isArray(bv) && Array.isArray(dv)) editedList(bv, dv, b.shape, p, out);
    else editedValue(bv, dv, b.shape, p, out);
  }
  return out;
}

const asList = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

function editedList(base: unknown[], draft: unknown[], s: Shape, p: string, out: Record<string, unknown>) {
  const id = (v: unknown) => identityOfValue(normalize(v, s.elem ?? renamed), s);
  draft.forEach((d, j) => {
    const want = id(d);
    const i = want ? base.findIndex((b) => id(b) === want) : -1;
    if (i < 0) out[`${p}/${j}`] = null;
    else editedValue(base[i], d, s.elem ?? renamed, `${p}/${j}`, out);
  });
}

function editedValue(base: unknown, draft: unknown, s: Shape, p: string, out: Record<string, unknown>) {
  if (equal(normalize(base, s), normalize(draft, s))) return;
  if (!isObject(base) || !isObject(draft) || s.kind === 'prompt' || s.kind === 'runMode') {
    out[p] = base ?? null;
    return;
  }
  for (const k of new Set([...Object.keys(base), ...Object.keys(draft)])) {
    if (readOnlyKey(s, k)) continue;
    editedValue(base[k], draft[k], child(s, yamlKey(s, k)), `${p}/${escapeToken(k)}`, out);
  }
}

/** The tokens of a JSON pointer. */
export function pointerTokens(p: string): string[] {
  return p === '' ? [] : p.slice(1).split('/').map(unescapeToken);
}

function comparePointers(a: string, b: string): number {
  const x = pointerTokens(a);
  const y = pointerTokens(b);
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    const [s, t] = [x[i] as string, y[i] as string];
    if (s === t) continue;
    const [m, n] = [Number(s), Number(t)];
    if (Number.isInteger(m) && Number.isInteger(n)) return m - n;
    return s < t ? -1 : 1;
  }
  return x.length - y.length;
}

/**
 * value with the base's value put back at each pointer of edited that pointers names (removed
 * where the base had none); later list items go first, so earlier ones keep their pointers.
 */
export function revert(value: unknown, marks: Record<string, unknown>, pointers: string[]): unknown {
  let v = structuredClone(value);
  const ps = [...new Set(pointers)].sort(comparePointers).reverse();
  for (const p of ps) {
    if (!(p in marks)) continue;
    const base = marks[p];
    if (p === '') {
      v = structuredClone(base);
      continue;
    }
    v = setPointer(v, pointerTokens(p), structuredClone(base), base === null);
  }
  return v;
}

/** v with the value at toks set (or removed); a path whose parent is missing leaves v as it is. */
export function setPointer(v: unknown, toks: string[], val: unknown, remove: boolean): unknown {
  if (toks.length === 0) return val;
  const [tok, ...rest] = toks as [string, ...string[]];
  if (Array.isArray(v)) {
    const i = tok === '-' ? v.length : Number(tok);
    if (!Number.isInteger(i) || i < 0 || i > v.length) return v;
    if (rest.length > 0) {
      if (i < v.length) v[i] = setPointer(v[i], rest, val, remove);
    } else if (remove) v.splice(i, 1);
    else v[i] = val;
    return v;
  }
  if (isObject(v)) {
    if (rest.length > 0) {
      if (tok in v) v[tok] = setPointer(v[tok], rest, val, remove);
    } else if (remove) delete v[tok];
    else v[tok] = val;
    return v;
  }
  return v;
}
