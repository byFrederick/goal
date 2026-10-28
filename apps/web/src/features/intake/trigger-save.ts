// Saves one trigger as a new version of the definition, through the draft files API (the page's only
// write): open the draft, read factory.yaml on the sha256 it is based on, write the trigger into it,
// put the file back on that sha256 (If-Match), validate the draft and publish it. The draft-model API
// cannot carry spec.board.triggers yet (internal/factorymodel/model.go), so this is the one path.

import { getDraftFile, openDraft, publishDraft, putDraftFile, validateDraft } from '../../api/definitions';
import { definitionFile } from '../../api/intake';
import type { DefinitionProblem, Version } from '../../api/types';
import type { Trigger } from '../../shared/definition/model';
import { writeTrigger } from './trigger-yaml';

export type SaveOutcome =
  | { kind: 'published'; version: Version }
  /** The draft holds the trigger's write, but validation found errors: nothing is published. */
  | { kind: 'invalid'; problems: DefinitionProblem[]; text: string };

/**
 * Saves `trigger` (its name is the route's) and publishes the draft. `current` is the version that
 * runs now, which publishing must still find (expected_current_version_id). A 412 draft-conflict
 * from the file write means the definition moved since it was read; it is thrown for the page to show.
 */
export async function saveTrigger(
  org: string,
  factory: string,
  trigger: Trigger,
  current: string | null,
): Promise<SaveOutcome> {
  await openDraft(org, factory);
  const file = await getDraftFile(org, factory, definitionFile);
  const text = writeTrigger(file.text, trigger);
  const saved = await putDraftFile(org, factory, definitionFile, text, { sha256: file.sha256 });
  const validation = await validateDraft(org, factory, {});
  if (validation.problems.some((p) => p.severity === 'error')) {
    return { kind: 'invalid', problems: validation.problems, text };
  }
  const version = await publishDraft(org, factory, {
    revision: saved.revision,
    expected_current_version_id: current,
    message: `Change the trigger ${trigger.name}`,
  });
  return { kind: 'published', version };
}
