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

export async function setTriggerEnabled({ org, factory, currentVersionId, trigger, enabled }: SetTriggerEnabled) {
  await openDraft(org, factory);
  const file = await getDraftFile(org, factory, 'factory.yaml');
  const next = withTriggerEnabled(file.text, trigger, enabled);
  const saved = await putDraftFile(org, factory, 'factory.yaml', next, { sha256: file.sha256 });
  await publishDraft(org, factory, {
    revision: saved.revision,
    expected_current_version_id: currentVersionId,
    message: `${enabled ? 'Turn on' : 'Turn off'} the trigger ${trigger}`,
  });
}
