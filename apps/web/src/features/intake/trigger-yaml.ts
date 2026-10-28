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
  const raw = doc.getIn(['spec', 'board', 'triggers'], true);
  // No triggers key, or an empty one (`triggers:` with nothing, or `[]`), is a list with no entries yet.
  const empty = raw === undefined || (isScalar(raw) && raw.value === null);
  if (!empty && !isSeq(raw)) throw new Error('spec.board.triggers is not a list');
  const seq = isSeq(raw) ? raw : undefined;
  const next = toFile(trigger);
  const edits: Splice[] = [];
  if (!seq || seq.items.length === 0) {
    edits.push(addFirstEntry(text, doc, next));
  } else {
    const node = triggerNode(doc, trigger.name);
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
/** The offset just past the last non-blank character before `offset`: a node's range can run into the blank lines after it. */
const contentEnd = (text: string, offset: number) => {
  let e = offset;
  while (e > 0 && /\s/.test(text[e - 1] ?? '')) e--;
  return e;
};

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

/** A trigger as one block-style list entry whose dash sits at `dashPad`, its maps as flow maps. */
function entryText(trigger: Trigger, dashPad: string): string {
  const doc = new Document(trigger);
  if (isMap(doc.contents)) {
    for (const pair of doc.contents.items) if (isMap(pair.value) || isSeq(pair.value)) pair.value.flow = true;
  }
  const lines = doc.toString({ lineWidth: 0 }).trimEnd().split('\n');
  return [`${dashPad}- ${lines[0]}`, ...lines.slice(1).map((l) => `${dashPad}  ${l}`)].join('\n');
}

/** Adds a new trigger after the last list item, as a block entry in the same column as the others. */
function addEntry(text: string, seq: YAMLSeq, trigger: Trigger): Splice {
  const entry = seq.items.at(-1);
  if (!isMap(entry) || !entry.range) throw new Error('spec.board.triggers has no entry to follow');
  const block = entryText(trigger, ' '.repeat(columnOf(text, entry.range[0]) - 2));
  const at = lastLineEnd(text, seq);
  const lead = at === text.length && !text.endsWith('\n') ? '\n' : '';
  return { from: at, to: at, text: `${lead}${block}\n` };
}

/**
 * Writes the first trigger: `triggers:` under spec.board, where it is missing or empty, and `board:`
 * under spec when the board is missing too. The key takes the column of its siblings, so the file's
 * indentation holds.
 */
function addFirstEntry(text: string, doc: Document, trigger: Trigger): Splice {
  const spec = doc.get('spec', true);
  if (!isMap(spec)) throw new Error('factory.yaml has no spec');
  const board = spec.get('board', true);
  if (board !== undefined && !isMap(board)) throw new Error('spec.board is not a map');
  if (isMap(board)) {
    const pair = board.items.find((p) => isScalar(p.key) && p.key.value === 'triggers');
    if (pair) {
      // Replace the empty `triggers` pair's lines with the list.
      const keyStart = (pair.key as { range: [number, number, number] }).range[0];
      const valueEnd = (pair.value as { range?: [number, number, number] } | null)?.range?.[1];
      const to = lineEnd(text, contentEnd(text, valueEnd ?? keyStart));
      const column = ' '.repeat(columnOf(text, keyStart));
      const block = `${column}triggers:\n${entryText(trigger, `${column}  `)}\n`;
      return { from: lineStart(text, keyStart), to, text: block };
    }
    const first = board.items[0];
    if (!first) throw new Error('spec.board is empty');
    const column = ' '.repeat(columnOf(text, (first.key as { range: [number, number, number] }).range[0]));
    return insertAfter(text, board, `${column}triggers:\n${entryText(trigger, `${column}  `)}`);
  }
  const first = spec.items[0];
  if (!first) throw new Error('factory.yaml has an empty spec');
  const column = ' '.repeat(columnOf(text, (first.key as { range: [number, number, number] }).range[0]));
  const lines = `${column}board:\n${column}  triggers:\n${entryText(trigger, `${column}    `)}`;
  return insertAfter(text, spec, lines);
}

/** Inserts `lines` after the last line of `map`, as its own lines, and ends the text with a newline. */
function insertAfter(text: string, map: YAMLMap, lines: string): Splice {
  let end = 0;
  for (const pair of map.items) {
    const v = (pair.value as { range?: [number, number, number] } | null)?.range;
    if (v) end = Math.max(end, v[1]);
  }
  const at = lineEnd(text, contentEnd(text, end));
  const lead = at === text.length && !text.endsWith('\n') ? '\n' : '';
  return { from: at, to: at, text: `${lead}${lines}\n` };
}

/** The offset just past the last line of the list's last entry. */
function lastLineEnd(text: string, seq: YAMLSeq): number {
  const entry = seq.items.at(-1) as YAMLMap;
  let end = 0;
  for (const pair of entry.items) {
    const v = (pair.value as { range?: [number, number, number] } | null)?.range;
    if (v) end = Math.max(end, v[1]);
  }
  return lineEnd(text, contentEnd(text, end));
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

/** The trigger the file does not have: its name is not in spec.board.triggers. */
export class TriggerNotFoundError extends Error {
  constructor(name: string) {
    super(`The definition has no trigger named ${name}.`);
    this.name = 'TriggerNotFoundError';
  }
}

/** Returns the file's text with the trigger's `enabled` set; throws when the file does not parse or has no such trigger. */
export function withTriggerEnabled(text: string, name: string, enabled: boolean): string {
  const doc = parseDocument(text);
  if (doc.errors.length > 0)
    throw new Error(`factory.yaml does not parse: ${doc.errors[0]?.message ?? 'unknown error'}`);
  const triggers = doc.getIn(['spec', 'board', 'triggers'], true);
  const trigger = isSeq(triggers) ? triggers.items.find((t) => isMap(t) && t.get('name') === name) : undefined;
  if (!isMap(trigger)) throw new TriggerNotFoundError(name);

  const next = replaceEnabled(text, trigger, enabled);
  if (parseDocument(next).errors.length > 0) throw new Error('The edit would leave factory.yaml unparsable.');
  return next;
}

/** A value already written is replaced where it is; a missing one is added after the trigger's last key. */
function replaceEnabled(text: string, trigger: YAMLMap, enabled: boolean): string {
  const pair = trigger.items.find((p) => isScalar(p.key) && p.key.value === 'enabled');
  const value = pair?.value;
  if (isScalar(value) && value.range) {
    const [start, end] = value.range;
    return `${text.slice(0, start)}${enabled}${text.slice(end)}`;
  }

  const keys = trigger.items.map((p) => p.key);
  const first = keys[0];
  const last = trigger.items[trigger.items.length - 1];
  const end = rangeEnd(last?.value);
  if (!isScalar(first) || !first.range || end === undefined)
    throw new Error('The trigger has no place to set enabled.');
  // Keys share one column: the name's, which after the "- " of a sequence item is where the others start.
  const lineStart = text.lastIndexOf('\n', first.range[0] - 1) + 1;
  const indent = ' '.repeat(first.range[0] - lineStart);
  return `${text.slice(0, end)}\n${indent}enabled: ${enabled}${text.slice(end)}`;
}

/** Where a node's text ends in the file, when the parser kept its range. */
function rangeEnd(node: unknown): number | undefined {
  return (node as { range?: readonly number[] } | null | undefined)?.range?.[1];
}
