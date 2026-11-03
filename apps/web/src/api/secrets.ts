// The organization's secrets (ADR 0037) over client.ts's goald client: the list, one secret, a create,
// an import, a replaced value, changed settings and a delete. A secret never comes back with its value,
// and a value goes out in a create, a replace and an import only. Every change needs a sign-in from the
// last 10 minutes: a 403 reauth-required opens the re-auth dialog and the request is sent once more
// (session.ts).
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { AuditPage, NewSecret, Secret, SecretImport, SecretImportResult, SecretPatch } from './types';

/** The organization's secrets, by name, never a value. */
export async function listSecrets(org: string, signal?: AbortSignal): Promise<Secret[]> {
  const body = await unwrap(goal.GET(orgPath('/secrets'), { params: { path: { org } }, ...cancel(signal) }));
  return body.secrets;
}

/** One secret; 404 secret-not-found. */
export function getSecret(org: string, secret: string, signal?: AbortSignal): Promise<Secret> {
  return unwrap(
    goal.GET(orgPath('/secrets/{secret}'), { params: { path: { org, secret } }, ...cancel(signal) }),
  );
}

/** Creates a secret with its pasted value, sealed (400 secret-invalid, 409 secret-name-taken). The value is never answered. */
export function createSecret(org: string, secret: NewSecret): Promise<Secret> {
  return unwrap(goal.POST(orgPath('/secrets'), { params: { path: { org } }, body: secret }));
}

/** Imports a .env or YAML text. With dry_run it answers what would happen and stores nothing. */
export function importSecrets(org: string, body: SecretImport): Promise<SecretImportResult> {
  return unwrap(goal.POST(orgPath('/secrets/import'), { params: { path: { org } }, body }));
}

/** Changes the description, the factory scope, or the expiry with its reminder. */
export function patchSecret(org: string, secret: string, patch: SecretPatch): Promise<Secret> {
  return unwrap(goal.PATCH(orgPath('/secrets/{secret}'), { params: { path: { org, secret } }, body: patch }));
}

/** Replaces the value; the answer holds none. */
export function replaceSecretValue(org: string, secret: string, value: string): Promise<Secret> {
  return unwrap(
    goal.PUT(orgPath('/secrets/{secret}/value'), { params: { path: { org, secret } }, body: { value } }),
  );
}

/** 409 secret-in-use while something uses it. */
export async function deleteSecret(org: string, secret: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/secrets/{secret}'), { params: { path: { org, secret } } }));
}

/** The newest audit entries about one secret, by its id. */
export function listSecretActivity(org: string, id: string, signal?: AbortSignal): Promise<AuditPage> {
  return unwrap(
    goal.GET(orgPath('/audit'), {
      params: { path: { org }, query: { target_kind: 'secret', target_id: id, limit: 10 } },
      ...cancel(signal),
    }),
  );
}
