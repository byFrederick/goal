// How Goal names a person on screen: by their name, or by their email (and its initials) while they
// have none. Better Auth gives a person who signs up by code an empty name until they choose one.

/** A person as goald answers them: a name, empty until they choose one, and an email. */
export interface Named {
  name: string;
  email: string;
}

/** How Goal shows a person: their name, or their email while they have none. */
export function displayName(p: Named): string {
  return p.name.trim() || p.email;
}

/** Whether the person has chosen a name (else displayName is their email). */
export function hasName(p: Named): boolean {
  return p.name.trim() !== '';
}

/**
 * "FC" for F. Cruz, "AO" for a.ortiz: the first letters of the first and last words of the name,
 * or of the email's local part while there is no name ("AO" for a.ortiz@example.com).
 */
export function initialsOf(p: Named): string {
  const source = hasName(p) ? p.name : (p.email.split('@')[0] ?? '');
  const words = source.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  const first = words[0]?.[0] ?? '?';
  const last = words.length > 1 ? (words.at(-1)?.[0] ?? '') : '';
  return `${first}${last}`.toUpperCase();
}
