// The Knowledge pages' URL state (frontend.md: every filter and selection lives in the URL): the
// list's factory, the source whose documents show beside it and the search tried as a factory;
// New source's kind; a source's note after a create and the document open over it. Unknown or
// malformed values are dropped, never thrown on.
import type { KnowledgeSourceKind } from '../../api/types';

const text = (v: unknown, max = 100): string | undefined =>
  typeof v === 'string' && v.trim() !== '' && v.length <= max ? v : undefined;

export interface KnowledgeSearch {
  /** A factory's name: only the sources its current version may search. */
  factory?: string | undefined;
  /** The source whose documents show beside the list. */
  source?: string | undefined;
  /** The query tried. */
  q?: string | undefined;
  /** The factory the search is tried as: only the sources it lists. */
  as?: string | undefined;
}

export function knowledgeSearch(search: Record<string, unknown>): KnowledgeSearch {
  const factory = text(search.factory);
  const source = text(search.source, 40);
  const q = text(search.q, 1000);
  const as = text(search.as);
  return {
    ...(factory ? { factory } : {}),
    ...(source ? { source } : {}),
    ...(q ? { q } : {}),
    ...(as ? { as } : {}),
  };
}

export interface NewSourceSearch {
  kind?: KnowledgeSourceKind;
}

const kinds: readonly KnowledgeSourceKind[] = ['upload', 'synced', 'live'];

export function newSourceSearch(search: Record<string, unknown>): NewSourceSearch {
  const kind = kinds.find((k) => k === search.kind);
  return kind ? { kind } : {};
}

export interface SourceSearch {
  /** Just created here: the page says what happens next. */
  created?: true;
  /** A document's id: its indexed text opens over the page. */
  document?: string | undefined;
}

export function sourceSearch(search: Record<string, unknown>): SourceSearch {
  const created = search.created === true || search.created === 'true' || search.created === 1;
  const document = text(search.document, 64);
  return { ...(created ? { created: true as const } : {}), ...(document ? { document } : {}) };
}
