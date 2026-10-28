// The organization's spend over a window and its factories' budgets (getOrgSpend, spec 4 §11) over
// client.ts's goald client. The window is week, month or 90d; goald answers the figures it does not
// split by factory as null or empty when a factory the caller cannot see spent (complete is false).
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { OrgSpend, SpendWindow } from './types';

/** The organization's spend over the window, with its budgets. */
export function getOrgSpend(org: string, window: SpendWindow, signal?: AbortSignal): Promise<OrgSpend> {
  return unwrap(goal.GET(orgPath('/spend'), { params: { path: { org }, query: { window } }, ...cancel(signal) }));
}
