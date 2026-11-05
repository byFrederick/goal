// Lists a source in a factory's draft, as `goal knowledge add --factory` does (internal/cli
// knowledge.go, listInDraft): it appends the source to spec.knowledge.sources in the draft's
// factory.yaml, keeps the file's comments and order, and leaves a source already listed alone.
// It only writes the draft; publishing is the factory's own step.
import { isMap, isScalar, isSeq, parseDocument } from 'yaml';
import { getDraftFile, openDraft, putDraftFile } from '../../api/definitions';

const file = 'factory.yaml';
const path = ['spec', 'knowledge', 'sources'];

/** Whether a draft's knowledge.sources entry (a name, or an object with a name) is `source`. */
function names(item: unknown, source: string): boolean {
  if (isScalar(item)) return item.value === source;
  return isMap(item) && item.get('name') === source;
}

/**
 * Appends `source` to the factory's draft. True when it was added, false when the draft listed it
 * already. A draft that does not parse is refused, not overwritten.
 */
export async function listInDraft(org: string, factory: string, source: string): Promise<boolean> {
  // As the CLI does: a factory with no open draft gets one (the call returns the open one otherwise).
  await openDraft(org, factory);
  const draft = await getDraftFile(org, factory, file);
  const doc = parseDocument(draft.text);
  if (doc.errors.length > 0) throw new Error(`The draft’s ${file} does not parse: ${doc.errors[0]?.message ?? ''}`);
  if (!isMap(doc.contents)) throw new Error(`The draft’s ${file} is not a mapping.`);

  const list = doc.getIn(path, true);
  if (isSeq(list) && list.items.some((item) => names(item, source))) return false;
  if (isSeq(list)) list.add(doc.createNode(source));
  else doc.setIn(path, doc.createNode([source]));

  await putDraftFile(org, factory, file, doc.toString(), { sha256: draft.sha256 });
  return true;
}

/** What writing one factory's draft came to: it listed the source, had it already, or failed. */
export interface DraftResult {
  factory: string;
  outcome: 'added' | 'listed' | 'failed';
  /** Why it failed. */
  error?: string;
}

/** Lists `source` in each factory's draft, one by one, and says what each came to. Never publishes. */
export async function listInDrafts(
  org: string,
  factories: readonly { id: string; name: string }[],
  source: string,
): Promise<DraftResult[]> {
  const out: DraftResult[] = [];
  for (const f of factories) {
    try {
      out.push({ factory: f.name, outcome: (await listInDraft(org, f.id, source)) ? 'added' : 'listed' });
    } catch (err) {
      out.push({ factory: f.name, outcome: 'failed', error: err instanceof Error ? err.message : 'It failed.' });
    }
  }
  return out;
}
