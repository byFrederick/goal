// The organization's API keys (spec 3 §8.9) over client.ts's goald client. Creating and revoking
// need a sign-in from the last 10 minutes: a 403 reauth-required opens the re-auth dialog and the
// request is sent once more (session.ts). A created key's secret is in the answer once; nothing
// here keeps it.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { ApiKey, CreatedApiKey, NewApiKey } from './types';

/** Live keys and those revoked in the last 30 days, live ones first, newest first. */
export async function listApiKeys(org: string, signal?: AbortSignal): Promise<ApiKey[]> {
  const body = await unwrap(goal.GET(orgPath('/api-keys'), { params: { path: { org } }, ...cancel(signal) }));
  return body.keys;
}

/** 400 scope-not-allowed, 409 api-key-name-taken, 404 factory-not-found. */
export function createApiKey(org: string, key: NewApiKey): Promise<CreatedApiKey> {
  return unwrap(goal.POST(orgPath('/api-keys'), { params: { path: { org } }, body: key }));
}

/** Anything using the key stops working at once. */
export async function revokeApiKey(org: string, key: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/api-keys/{key}'), { params: { path: { org, key } } }));
}
