// The organization's model and tool policy (getOrgPolicy and putOrgPolicy, ADR 0024) over client.ts's
// goald client. The answer's ETag is the policy's revision, quoted: a save sends it back as If-Match,
// and goald refuses a save made on a revision that has since moved (409 policy-conflict).
import { orgPath, type Result, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { OrgPolicyInput, OrgPolicyView } from './types';

/** The policy as goald answered it, with the ETag of that answer: the If-Match of the next save. */
export interface PolicyLoad {
  view: OrgPolicyView;
  etag: string;
}

/** Unwraps a call and keeps the ETag its response carried. */
async function withEtag<T>(call: Promise<Result<T>>): Promise<PolicyLoad> {
  let etag = '';
  const view = await unwrap(
    call.then((result) => {
      etag = result.response.headers.get('ETag') ?? '';
      return result;
    }),
  );
  return { view: view as OrgPolicyView, etag };
}

/** The organization's policy, the install's models and box sizes, and the egress addresses. */
export function getOrgPolicy(org: string, signal?: AbortSignal): Promise<PolicyLoad> {
  return withEtag(goal.GET(orgPath('/policy'), { params: { path: { org } }, ...cancel(signal) }));
}

/**
 * Replaces the policy. `etag` is the ETag the form was loaded with; goald answers the saved policy
 * with its new ETag, and refuses a stale one with 409 policy-conflict, which changes nothing.
 */
export function putOrgPolicy(org: string, body: OrgPolicyInput, etag: string): Promise<PolicyLoad> {
  return withEtag(
    goal.PUT(orgPath('/policy'), { params: { path: { org }, header: { 'If-Match': etag } }, body }),
  );
}
