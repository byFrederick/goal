// The organization's spend over a window and its factories' budgets (getOrgSpend, spec 4 §11) over
// client.ts's goald client. The window is week, month or 90d; goald answers the figures it does not
// split by factory as null or empty when a factory the caller cannot see spent (complete is false).
// The CSV of the same window is exportOrgSpend (ADR 0020, decision 3).
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import { type CsvFile, csvFileOf } from './download';
import type { OrgSpend, SpendWindow } from './types';

/** The organization's spend over the window, with its budgets. */
export function getOrgSpend(org: string, window: SpendWindow, signal?: AbortSignal): Promise<OrgSpend> {
  return unwrap(goal.GET(orgPath('/spend'), { params: { path: { org }, query: { window } }, ...cancel(signal) }));
}

/** The spend over the window as CSV (GET /v1/orgs/{org}/spend/export): one row per factory, agent and model. */
export async function exportOrgSpend(org: string, window: SpendWindow, signal?: AbortSignal): Promise<CsvFile> {
  const result = await goal.GET(orgPath('/spend/export'), {
    params: { path: { org }, query: { window } },
    parseAs: 'blob',
    headers: { Accept: 'text/csv' },
    ...cancel(signal),
  });
  return csvFileOf(result, 'spend.csv');
}
