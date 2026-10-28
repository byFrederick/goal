// The intake switch's write: open the draft, change the trigger's `enabled` in factory.yaml, save the file
// and publish it. It only runs with no draft open (the switch refuses before that), so a publish ships this
// change and nothing else. Publishing names the version the switch showed, so a publish that landed in the
// meantime fails with a conflict instead of being overwritten.
import { getDraftFile, openDraft, publishDraft, putDraftFile } from '../../api/definitions';
import { withTriggerEnabled } from './trigger-yaml';

export interface SetTriggerEnabled {
  org: string;
  factory: string;
  /** The version the switch was read against; null for a factory never published. */
  currentVersionId: string | null;
  trigger: string;
  enabled: boolean;
}

export interface SetTriggersEnabled extends Omit<SetTriggerEnabled, 'trigger'> {
  /** The triggers to change, all in one draft and one publish. */
  triggers: string[];
}

/** Thrown when the draft the switch opened already had someone's edits: nothing is written or published. */
export class OpenDraftError extends Error {}

export function setTriggerEnabled({ trigger, ...rest }: SetTriggerEnabled) {
  return setTriggersEnabled({ ...rest, triggers: [trigger] });
}

/**
 * Sets `enabled` on each trigger in one draft: one save of factory.yaml and one publish. The publish names
 * `currentVersionId`, so a version published in the meantime is a conflict, not an overwrite.
 */
export async function setTriggersEnabled({ org, factory, currentVersionId, triggers, enabled }: SetTriggersEnabled) {
  // openDraft returns a draft that is already open (200) as it is. Only a draft with no saves and no changes
  // is one this write can own; anything else holds someone's unpublished edits, which a publish must not ship.
  const draft = await openDraft(org, factory);
  if (draft.revision !== 1 || draft.change_count > 0) throw new OpenDraftError();
  const file = await getDraftFile(org, factory, 'factory.yaml');
  const next = triggers.reduce((text, trigger) => withTriggerEnabled(text, trigger, enabled), file.text);
  const saved = await putDraftFile(org, factory, 'factory.yaml', next, { sha256: file.sha256 });
  const names = triggers.join(', ');
  await publishDraft(org, factory, {
    revision: saved.revision,
    expected_current_version_id: currentVersionId,
    message: `${enabled ? 'Turn on' : 'Turn off'} the trigger${triggers.length > 1 ? 's' : ''} ${names}`,
  });
}
