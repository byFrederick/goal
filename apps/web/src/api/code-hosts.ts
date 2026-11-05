// The organization's code hosts (ADR 0041, decision 1): github.com and gitlab.com built in, then the hosts
// its owners added. Tokens go only to these hosts.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { CodeHostInput, CodeHosts } from './types';

export function getCodeHosts(org: string, signal?: AbortSignal): Promise<CodeHosts> {
  return unwrap(goal.GET(orgPath('/code-hosts'), { params: { path: { org } }, ...cancel(signal) }));
}

/**
 * Replaces the added hosts (org:code_hosts, a sign-in from the last 10 minutes). 400 code-host-invalid or
 * code-host-built-in; 403 reauth-required is handled by the API client's session handler.
 */
export function setCodeHosts(org: string, hosts: CodeHostInput[]): Promise<CodeHosts> {
  return unwrap(goal.PUT(orgPath('/code-hosts'), { params: { path: { org } }, body: { hosts } }));
}
