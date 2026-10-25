// The Connections pages' URL state (frontend.md: every filter lives in the URL): the list's
// factory, kind and text filters, New connection's chosen kind, and the detail's note after a
// create. Unknown or malformed values are dropped, never thrown on.

export interface ConnectionsSearch {
  /** A factory's name: only the connections its current version names. */
  factory?: string | undefined;
  /** A kind's name. */
  kind?: string | undefined;
  /** Text the name, description or kind contains. */
  q?: string | undefined;
}

const text = (v: unknown, max = 100): string | undefined =>
  typeof v === 'string' && v.trim() !== '' && v.length <= max ? v : undefined;

export function connectionsSearch(search: Record<string, unknown>): ConnectionsSearch {
  const factory = text(search.factory);
  const kind = text(search.kind, 40);
  const q = text(search.q);
  return { ...(factory ? { factory } : {}), ...(kind ? { kind } : {}), ...(q ? { q } : {}) };
}

export interface NewConnectionSearch {
  /** The kind chosen. */
  kind?: string;
}

export function newConnectionSearch(search: Record<string, unknown>): NewConnectionSearch {
  const kind = text(search.kind, 40);
  return kind ? { kind } : {};
}

export interface ConnectionSearch {
  /** Just created here: the page says so and offers its test. */
  created?: true;
}

export function connectionSearch(search: Record<string, unknown>): ConnectionSearch {
  return search.created === true || search.created === 'true' || search.created === 1 ? { created: true } : {};
}
