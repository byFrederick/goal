// The Secrets list's URL state (frontend.md: every filter lives in the URL): the filter (?filter=expiring
// or unused) and the search text. Unknown or malformed values are dropped, never thrown on.
export const secretFilters = ['expiring', 'unused'] as const;
export type SecretFilter = (typeof secretFilters)[number];

export interface SecretsSearch {
  /** One filter; absent for All. */
  filter?: SecretFilter | undefined;
  /** Text the name or description contains. */
  q?: string | undefined;
  /** The New secret sheet is open. */
  new?: true | undefined;
}

const text = (v: unknown, max = 100): string | undefined =>
  typeof v === 'string' && v.trim() !== '' && v.length <= max ? v : undefined;

export function secretsSearch(search: Record<string, unknown>): SecretsSearch {
  const filter = secretFilters.find((f) => f === search.filter);
  const q = text(search.q);
  return {
    ...(filter ? { filter } : {}),
    ...(q ? { q } : {}),
    ...(search.new === true || search.new === 'true' ? { new: true as const } : {}),
  };
}
