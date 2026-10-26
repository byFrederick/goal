// The organization's API keys (spec 3 §8.9) over client.ts's goald client. Creating and revoking
// need a sign-in from the last 10 minutes: a 403 reauth-required opens the re-auth dialog and the
// request is sent once more (session.ts). A created key's secret is in the answer once; nothing
// here keeps it.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { ApiKey, CreatedApiKey, CreatedPersonalToken, NewApiKey, NewPersonalToken, PersonalToken } from './types';

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

// Personal tokens (3-12b): the caller's own in the organization (listing and revoking need no
// recent sign-in; creating does, and only in the browser), and every member's for owners and
// admins (revoking one needs a recent sign-in).

/** The caller's live tokens and those revoked in the last 30 days, live ones first, newest first. */
export async function listPersonalTokens(org: string, signal?: AbortSignal): Promise<PersonalToken[]> {
  const body = await unwrap(goal.GET(orgPath('/personal-tokens'), { params: { path: { org } }, ...cancel(signal) }));
  return body.tokens;
}

/** 400 scope-not-allowed, 409 personal-token-name-taken, 403 browser-session-required. */
export function createPersonalToken(org: string, token: NewPersonalToken): Promise<CreatedPersonalToken> {
  return unwrap(goal.POST(orgPath('/personal-tokens'), { params: { path: { org } }, body: token }));
}

/** One of the caller's own; anything using it stops working at once. */
export async function revokePersonalToken(org: string, token: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/personal-tokens/{token}'), { params: { path: { org, token } } }));
}

/** Every member's tokens, with their owner (api_key:read). */
export async function listMemberTokens(org: string, signal?: AbortSignal): Promise<PersonalToken[]> {
  const body = await unwrap(goal.GET(orgPath('/member-tokens'), { params: { path: { org } }, ...cancel(signal) }));
  return body.tokens;
}

/** A member's token (api_key:manage); anything using it stops working at once. */
export async function revokeMemberToken(org: string, token: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/member-tokens/{token}'), { params: { path: { org, token } } }));
}
