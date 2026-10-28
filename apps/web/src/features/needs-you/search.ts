// The Needs you page's URL: ?type= (the tab; absent is All), ?factory= (a factory's name; absent is
// all factories) and ?entry= (the selected entry's id). A linked URL shows the same tab, factory and
// entry for whoever opens it, so the phone's approve view is a URL too.
import type { InboxEntry } from '../../api/types';
import { inTab, type NeedsTab, tabs } from './types';

/** The URL's params; an absent type is All. */
export interface NeedsSearch {
  type?: NeedsTab | undefined;
  factory?: string | undefined;
  entry?: string | undefined;
}

const tabValues: ReadonlySet<string> = new Set(tabs.map((t) => t.value));

/** Reads the search params: an unknown or All tab is absent, and empty values are absent. */
export function needsSearch(raw: Record<string, unknown>): NeedsSearch {
  const search: NeedsSearch = {};
  if (typeof raw.type === 'string' && tabValues.has(raw.type) && raw.type !== 'all') search.type = raw.type as NeedsTab;
  if (typeof raw.factory === 'string' && raw.factory !== '') search.factory = raw.factory;
  if (typeof raw.entry === 'string' && raw.entry !== '') search.entry = raw.entry;
  return search;
}

/** The entries of one factory (by its name) under one tab, in the order the API gave them. */
export function visibleEntries(entries: readonly InboxEntry[], { type = 'all', factory }: NeedsSearch): InboxEntry[] {
  return entries.filter((e) => (!factory || e.factory.name === factory) && inTab(e, type));
}

/** How many entries each tab holds, for the factory chosen. Approval has none yet. */
export function tabCounts(entries: readonly InboxEntry[], factory?: string): Record<NeedsTab, number> {
  const counts: Record<NeedsTab, number> = { all: 0, approval: 0, decision: 0, question: 0, alert: 0 };
  for (const e of entries) {
    if (factory && e.factory.name !== factory) continue;
    counts.all += 1;
    for (const tab of ['approval', 'decision', 'question', 'alert'] as const) if (inTab(e, tab)) counts[tab] += 1;
  }
  return counts;
}

/** The factories the entries belong to, by name, with the title they show. */
export function factoriesOf(entries: readonly InboxEntry[]): { name: string; title: string }[] {
  const seen = new Map<string, string>();
  for (const e of entries) if (!seen.has(e.factory.name)) seen.set(e.factory.name, e.factory.title ?? e.factory.name);
  return [...seen].map(([name, title]) => ({ name, title })).sort((a, b) => a.title.localeCompare(b.title));
}
