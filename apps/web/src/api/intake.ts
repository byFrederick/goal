// The intake read API (contracts/api/openapi.yaml, getIntakeTrigger): one trigger of the current
// definition with what its items came to this week and its 20 newest events. Reads only; a trigger
// is written through the definition's draft files (features/intake/trigger-save.ts).
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { IntakeTriggerDetail } from './types';

/** The file a trigger lives in: the definition's root file. */
export const definitionFile = 'factory.yaml';

export function getIntakeTrigger(
  org: string,
  factory: string,
  trigger: string,
  signal?: AbortSignal,
): Promise<IntakeTriggerDetail> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/intake/triggers/{trigger}'), {
      params: { path: { org, factory, trigger } },
      ...cancel(signal),
    }),
  );
}
