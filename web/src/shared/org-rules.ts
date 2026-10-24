// goald's rule for an organization's address (domain.CheckSlug), checked as the person types so
// a save is not refused for a reason the form could have said.

const slugPattern = /^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$/;

/** Words of Goal's own routes, which no organization may take as its address. */
const reservedSlugs = new Set([
  'new',
  'settings',
  'api',
  'v1',
  'o',
  'me',
  'orgs',
  'auth',
  'invite',
  'admin',
  'default',
]);

/** Why `slug` cannot be an address, or null when it can. */
export function checkSlug(slug: string): string | null {
  if (!slugPattern.test(slug))
    return '3 to 40 lowercase letters, digits or dashes, not starting or ending with a dash.';
  if (reservedSlugs.has(slug)) return 'That address is reserved.';
  return null;
}
