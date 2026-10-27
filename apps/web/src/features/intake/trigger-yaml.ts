// A trigger's place in factory.yaml (ADR 0026, factory.schema.json `$defs/trigger`): read it, write it
// back, show it as YAML and say which lines each field takes. Writing goes through the yaml Document
// API, so every other line and comment of the file stays as it was: only spec.board.triggers[name] is
// rewritten, and only the keys the form changed.
import { Document, isMap, isScalar, isSeq, parseDocument, type YAMLMap, type YAMLSeq } from 'yaml';
import type { Trigger } from '../../shared/definition/model';

/** The keys of a trigger, in the order the schema lists them. The name is the route's and never written as a change. */
const keys = ['name', 'enabled', 'on', 'cron', 'timeZone', 'filter', 'only', 'create', 'limits'] as const;
export type TriggerKey = (typeof keys)[number];

/** The page's blocks that a problem can name: the YAML key it sits under decides the block. */
export type TriggerBlock = 'on' | 'filter' | 'only' | 'create' | 'limits';

/** Where a trigger's keys sit in the file: the first and last line of each, 1-based. */
export type KeyLines = Partial<Record<TriggerKey, { from: number; to: number }>>;

/** The board's work types and stations, which the Create section chooses from. */
export interface BoardChoices {
  workTypes: string[];
  stations: string[];
}

interface Shape {
  spec?: { board?: { workTypes?: unknown; stations?: { id?: unknown }[]; triggers?: unknown } };
}

/** The file's triggers as the form reads them, by name. Unparsed or missing: an empty list. */
export function readTriggers(text: string): Trigger[] {
  const js = parseDocument(text).toJS() as Shape | null;
  const list = js?.spec?.board?.triggers;
  return Array.isArray(list) ? (list as Trigger[]) : [];
}

/** The trigger named `name`, or undefined when the file has none under that name. */
export function readTrigger(text: string, name: string): Trigger | undefined {
  return readTriggers(text).find((t) => t.name === name);
}

/** The work types and station ids the file declares, for the Create section's choices. */
export function readBoard(text: string): BoardChoices {
  const board = (parseDocument(text).toJS() as Shape | null)?.spec?.board;
  const strings = (list: unknown) =>
    Array.isArray(list) ? list.filter((s): s is string => typeof s === 'string') : [];
  return {
    workTypes: strings(board?.workTypes),
    stations: strings(board?.stations?.map((s) => s.id)),
  };
}

function triggerSeq(doc: Document): YAMLSeq | undefined {
  const seq = doc.getIn(['spec', 'board', 'triggers'], true);
  return isSeq(seq) ? seq : undefined;
}

function triggerNode(doc: Document, name: string): YAMLMap | undefined {
  const node = triggerSeq(doc)?.items.find((item) => isMap(item) && item.get('name') === name);
  return isMap(node) ? node : undefined;
}

/** The line of each trigger key, so a validation problem's line names the field it is in. */
export function keyLines(text: string, name: string): KeyLines {
  const doc = parseDocument(text);
  const node = triggerNode(doc, name);
  const out: KeyLines = {};
  if (!node) return out;
  const lineAt = (offset: number) => text.slice(0, offset).split('\n').length;
  for (const pair of node.items) {
    if (!isScalar(pair.key) || typeof pair.key.value !== 'string') continue;
    const start = (pair.key.range ?? [0])[0];
    const end = (pair.value as { range?: [number, number, number] } | null)?.range?.[1] ?? start;
    out[pair.key.value as TriggerKey] = { from: lineAt(start), to: lineAt(end) };
  }
  return out;
}

/** The trigger as the form saves it: what the schema accepts, with the defaults left out. */
export function toFile(trigger: Trigger): Trigger {
  const drop = <T extends object>(o: T | undefined) => {
    if (!o) return undefined;
    const kept = Object.fromEntries(
      Object.entries(o).filter(
        ([, v]) => v !== undefined && v !== '' && v !== false && !(Array.isArray(v) && !v.length),
      ),
    );
    return Object.keys(kept).length === 0 ? undefined : kept;
  };
  const out: Trigger = {
    name: trigger.name,
    on: trigger.on,
    create: (drop(trigger.create) ?? {}) as Trigger['create'],
  };
  if (trigger.enabled === false) out.enabled = false;
  if (trigger.on === 'schedule') {
    if (trigger.cron !== undefined) out.cron = trigger.cron;
    if (trigger.timeZone) out.timeZone = trigger.timeZone;
  }
  const filter = drop(trigger.filter);
  if (filter) out.filter = filter;
  const only = drop(trigger.only);
  if (only) out.only = only;
  const limits = drop(trigger.limits);
  if (limits) out.limits = limits;
  return out;
}

/**
 * The file with `trigger` written as spec.board.triggers[name]. Only the keys that changed are spliced
 * into the text, so every other line and comment stays byte for byte; a new trigger is added after the
 * last one. Throws when the text does not parse.
 */
export function writeTrigger(text: string, trigger: Trigger): string {
  const doc = parseDocument(text);
  if (doc.errors.length > 0) throw new Error('factory.yaml does not parse');
  const seq = triggerSeq(doc);
  if (!seq) throw new Error('spec.board.triggers is not a list');
  const next = toFile(trigger);
  const node = triggerNode(doc, trigger.name);
  const edits: Splice[] = [];
  if (!node) {
    edits.push(addEntry(text, seq, next));
  } else {
    const current = node.toJSON() as Trigger;
    for (const key of keys) {
      if (key === 'name') continue;
      const value = next[key];
      if (JSON.stringify(value) === JSON.stringify(current[key])) continue;
      const pair = node.items.find((p) => isScalar(p.key) && p.key.value === key);
      if (pair) edits.push(replacePair(text, pair, key, value));
      else if (value !== undefined) edits.push(addPair(text, node, key, value));
    }
  }
  // Applied from the end of the text back, so each splice's offsets still hold.
  return edits
    .sort((a, b) => b.from - a.from)
    .reduce((out, e) => out.slice(0, e.from) + e.text + out.slice(e.to), text);
}

interface Splice {
  from: number;
  to: number;
  text: string;
}

/** The text of a value as one line of YAML: a flow map or list for a collection, as the picture has them. */
function inline(value: unknown): string {
  const d = new Document(value);
  if (isMap(d.contents) || isSeq(d.contents)) d.contents.flow = true;
  return d.toString({ lineWidth: 0 }).trim();
}

/** The offset of the first character of the line that holds `offset`. */
const lineStart = (text: string, offset: number) => text.lastIndexOf('\n', offset - 1) + 1;
/** The offset just past the newline that ends the line holding `offset` (the text's end when it has none). */
const lineEnd = (text: string, offset: number) => {
  const i = text.indexOf('\n', offset);
  return i === -1 ? text.length : i + 1;
};
const columnOf = (text: string, offset: number) => offset - lineStart(text, offset);

/** Replaces one key's whole pair (key, colon and value) with the new value, on the same line. */
function replacePair(text: string, pair: YAMLMap['items'][number], key: string, value: unknown): Splice {
  const from = (pair.key as { range: [number, number, number] }).range[0];
  const to = (pair.value as { range: [number, number, number] }).range[1];
  if (value === undefined) {
    return { from: lineStart(text, from), to: lineEnd(text, to), text: '' };
  }
  return { from, to, text: `${key}: ${inline(value)}` };
}

/** Adds a key after the trigger's last key, at the column the keys sit in. */
function addPair(text: string, node: YAMLMap, key: string, value: unknown): Splice {
  const last = node.items[node.items.length - 1] as {
    value: { range: [number, number, number] };
    key: { range: [number, number, number] };
  };
  const at = last.value.range[1];
  const column = ' '.repeat(columnOf(text, last.key.range[0]));
  const insert = `\n${column}${key}: ${inline(value)}`;
  return { from: at, to: at, text: insert };
}

/** Adds a new trigger after the last list item, as a block entry in the same column as the others. */
function addEntry(text: string, seq: YAMLSeq, trigger: Trigger): Splice {
  const entry = seq.items.at(-1);
  if (!isMap(entry) || !entry.range) throw new Error('spec.board.triggers has no entry to follow');
  const doc = new Document(trigger);
  if (isMap(doc.contents)) {
    for (const pair of doc.contents.items) if (isMap(pair.value) || isSeq(pair.value)) pair.value.flow = true;
  }
  const pad = ' '.repeat(columnOf(text, entry.range[0]) - 2);
  const lines = doc.toString({ lineWidth: 0 }).trimEnd().split('\n');
  const block = [`${pad}- ${lines[0]}`, ...lines.slice(1).map((l) => `${pad}  ${l}`)].join('\n');
  const at = lastLineEnd(text, seq);
  const lead = at === text.length && !text.endsWith('\n') ? '\n' : '';
  return { from: at, to: at, text: `${lead}${block}\n` };
}

/** The offset just past the last line of the list's last entry. */
function lastLineEnd(text: string, seq: YAMLSeq): number {
  const entry = seq.items.at(-1) as YAMLMap;
  let end = 0;
  for (const pair of entry.items) {
    const v = (pair.value as { range?: [number, number, number] } | null)?.range;
    if (v) end = Math.max(end, v[1]);
  }
  return lineEnd(text, end);
}

/** The trigger as YAML the way the page shows it: a list entry whose maps are flow maps, as the picture has them. */
export function triggerYaml(trigger: Trigger): string {
  const doc = new Document([toFile(trigger)]);
  const seq = doc.contents;
  if (!isSeq(seq)) return '';
  const item = seq.items[0];
  if (isMap(item)) {
    for (const pair of item.items) {
      if (isMap(pair.value) || isSeq(pair.value)) pair.value.flow = true;
    }
  }
  return doc.toString({ lineWidth: 0 });
}
