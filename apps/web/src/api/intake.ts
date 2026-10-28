// The intake read (ADR 0026): where a factory's newest items came from, and each trigger of its current
// definition with the items it created in the last 7 days. Triggers are edited through the definition's
// draft (definitions.ts), so this module only reads.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { Intake } from './types';

/** The intake of the factory; `items` is how many of its newest items the sources count (the server's default is 48). */
export function getIntake(org: string, factory: string, items?: number, signal?: AbortSignal): Promise<Intake> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/intake'), {
      params: { path: { org, factory }, query: items !== undefined ? { items } : {} },
      ...cancel(signal),
    }),
  );
}
