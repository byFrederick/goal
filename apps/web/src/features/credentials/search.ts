// The Credentials pages' URL state (frontend.md: every filter lives in the URL): the type tab
// (?type=api_key and so on) and the search text. Unknown or malformed values are dropped, never
// thrown on.
import type { CredentialType } from '../../api/types';
import { credentialTypes } from './words';

export interface CredentialsSearch {
  /** One type's tab; absent for All. */
  type?: CredentialType | undefined;
  /** Text the name or description contains. */
  q?: string | undefined;
}

const text = (v: unknown, max = 100): string | undefined =>
  typeof v === 'string' && v.trim() !== '' && v.length <= max ? v : undefined;

export function credentialsSearch(search: Record<string, unknown>): CredentialsSearch {
  const type = credentialTypes.find((t) => t === search.type);
  const q = text(search.q);
  return { ...(type ? { type } : {}), ...(q ? { q } : {}) };
}

export interface NewCredentialSearch {
  /** The import panel, opened from the New credential menu's second entry. */
  panel?: 'import' | undefined;
}

export function newCredentialSearch(search: Record<string, unknown>): NewCredentialSearch {
  return search.panel === 'import' ? { panel: 'import' } : {};
}
