// What the organization's agents reach (ADR 0037 decision 14) over client.ts's goald client: the Access
// read model, computed from each factory's published version, the environment tests and the secrets.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { Access, AccessView } from './types';

/** The rows grouped by what agents reach, by factory or by secret. */
export function getAccess(org: string, by: AccessView, signal?: AbortSignal): Promise<Access> {
  return unwrap(goal.GET(orgPath('/access'), { params: { path: { org }, query: { by } }, ...cancel(signal) }));
}
