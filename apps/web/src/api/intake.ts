// The intake read API (contracts/api/openapi.yaml): the factory's intake (getIntake, its newest items'
// sources) and one trigger of the current definition with what its items came to this week and its 20
// newest events. Reads only; a trigger is written through the definition's draft files
// (features/intake/trigger-save.ts).
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { Intake, IntakeTriggerDetail } from './types';

/** The file a trigger lives in: the definition's root file. */
export const definitionFile = 'factory.yaml';

/** The intake of the factory; `items` is how many of its newest items the sources count (the server's default is 48). */
export function getIntake(org: string, factory: string, items?: number, signal?: AbortSignal): Promise<Intake> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/intake'), {
      params: { path: { org, factory }, query: items !== undefined ? { items } : {} },
      ...cancel(signal),
    }),
  );
}

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
