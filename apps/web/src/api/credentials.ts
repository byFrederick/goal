// The organization's credentials (ADR 0017) over client.ts's goald client: the list, a create, an
// import, a replaced value or renamed metadata, and a delete. A credential never comes back with its value, and a
// value goes out on replace only. Every change needs a sign-in from the last 10 minutes: a 403
// reauth-required opens the re-auth dialog and the request is sent once more (session.ts).
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { Credential, CredentialImport, CredentialImportResult, CredentialPatch, NewCredential } from './types';

/** The organization's credentials, by name, never a value. */
export async function listCredentials(org: string, signal?: AbortSignal): Promise<Credential[]> {
  const body = await unwrap(goal.GET(orgPath('/credentials'), { params: { path: { org } }, ...cancel(signal) }));
  return body.credentials;
}

/** Creates a credential with its value, sealed (409 credential-name-taken, 400 credential-invalid). The value is never answered. */
export function createCredential(org: string, credential: NewCredential): Promise<Credential> {
  return unwrap(goal.POST(orgPath('/credentials'), { params: { path: { org } }, body: credential }));
}

/** Imports a .env or YAML text. With dry_run it answers what would happen and stores nothing; without it, it writes the names. */
export function importCredentials(org: string, body: CredentialImport): Promise<CredentialImportResult> {
  return unwrap(goal.POST(orgPath('/credentials/import'), { params: { path: { org } }, body }));
}

/** Replaces the value (`{ value }`) or changes the metadata; 404 credential-not-found, 409 credential-name-taken. */
export function updateCredential(org: string, credential: string, patch: CredentialPatch): Promise<Credential> {
  return unwrap(
    goal.PATCH(orgPath('/credentials/{credential}'), { params: { path: { org, credential } }, body: patch }),
  );
}

/** 409 credential-in-use, naming the connections that refer to it as {{name}}. */
export async function deleteCredential(org: string, credential: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/credentials/{credential}'), { params: { path: { org, credential } } }));
}
