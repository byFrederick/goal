// The one edit the intake switch makes to a factory.yaml: `spec.board.triggers[i].enabled` for the trigger
// named. The document is parsed with yaml's Document API and changed in place, so comments, key order and
// quoting of everything else come back byte for byte (the draft is what people review before publishing).
import { isMap, isSeq, parseDocument } from 'yaml';

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
  const index = isSeq(triggers) ? triggers.items.findIndex((t) => isMap(t) && t.get('name') === name) : -1;
  if (index < 0) throw new TriggerNotFoundError(name);
  doc.setIn(['spec', 'board', 'triggers', index, 'enabled'], enabled);
  return doc.toString();
}
