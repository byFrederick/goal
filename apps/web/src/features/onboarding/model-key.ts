// The model key of a factory made in onboarding (ADR 0037 decision 7): factory.yaml's `spec.runtime.modelKey`,
// one whole `{{secret}}` reference. The draft model has no such field, so the card edits the file's text with
// the yaml Document API: comments and the order of every other key stay as they are.
import { parseDocument } from 'yaml';

/** The suggested name of the secret that holds the model key. */
export const modelKeyName = 'model-key';

/** factory.yaml's text with `spec.runtime.modelKey` set to the whole reference to the secret. */
export function withModelKey(text: string, secret: string): string {
  const doc = parseDocument(text);
  doc.setIn(['spec', 'runtime', 'modelKey'], `{{${secret}}}`);
  return String(doc);
}
