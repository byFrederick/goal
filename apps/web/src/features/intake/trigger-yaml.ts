// The one edit the intake switch makes to a factory.yaml: `spec.board.triggers[i].enabled` for the trigger
// named. The edit is made on the text, at the offsets the parser reports, so every other byte comes back as
// it was: comments, spacing, flow lists and quoting. The draft is what people review before it publishes.
import { isMap, isScalar, isSeq, parseDocument, type YAMLMap } from 'yaml';

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
