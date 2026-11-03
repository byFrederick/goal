// The Knowledge pages' URL state (frontend.md: every filter and selection lives in the URL): the
// list's factory and Find a file's query; Add a source's kind; a source's note after a create and
// its files filter. Unknown or
// malformed values are dropped, never thrown on.

const text = (v: unknown, max = 100): string | undefined =>
  typeof v === 'string' && v.trim() !== '' && v.length <= max ? v : undefined;

export interface KnowledgeSearch {
  /** A factory's name: only the sources its current version lists. */
  factory?: string | undefined;
  /** What Find a file looks for. */
  q?: string | undefined;
}

export function knowledgeSearch(search: Record<string, unknown>): KnowledgeSearch {
  const factory = text(search.factory);
  const q = text(search.q, 1000);
  return { ...(factory ? { factory } : {}), ...(q ? { q } : {}) };
}

export interface NewSourceSearch {
  kind?: 'upload' | 'repository' | 'confluence';
}

const kinds = ['upload', 'repository', 'confluence'] as const;

export function newSourceSearch(search: Record<string, unknown>): NewSourceSearch {
  const kind = kinds.find((k) => k === search.kind);
  return kind ? { kind } : {};
}

export interface SourceSearch {
  /** Just created here: the page says what happens next. */
  created?: true;
  /** The path filter of the files table. */
  find?: string | undefined;
}

export function sourceSearch(search: Record<string, unknown>): SourceSearch {
  const created = search.created === true || search.created === 'true' || search.created === 1;
  const find = text(search.find, 1024);
  return { ...(created ? { created: true as const } : {}), ...(find ? { find } : {}) };
}
