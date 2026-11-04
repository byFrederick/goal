// A factory's knowledge.sources entries (ADR 0038 decision 5): a source's name, or an object that also says
// which runs read it (appliesTo) and when (readWhen). The draft model carries them as written.

export interface AppliesTo {
  agents?: string[];
  stations?: string[];
  lanes?: string[];
  workTypes?: string[];
}

export interface SourceEntry {
  name: string;
  appliesTo?: AppliesTo;
  readWhen?: string;
}

export type RawSourceEntry = string | SourceEntry;

export function entryOf(raw: RawSourceEntry): SourceEntry {
  return typeof raw === 'string' ? { name: raw } : raw;
}

const scopeWords: readonly [keyof AppliesTo, string][] = [
  ['agents', 'agents'],
  ['stations', 'stations'],
  ['lanes', 'lanes'],
  ['workTypes', 'work types'],
];

/** Who reads the source: "Every agent", or "agents: spec, build · stations: review". */
export function readersOf(entry: SourceEntry): string {
  const parts = scopeWords.flatMap(([key, label]) => {
    const names = entry.appliesTo?.[key];
    return names && names.length > 0 ? [`${label}: ${names.join(', ')}`] : [];
  });
  return parts.length > 0 ? parts.join(' · ') : 'Every agent';
}

export function addSource(list: readonly RawSourceEntry[], name: string): RawSourceEntry[] {
  return list.some((r) => entryOf(r).name === name) ? [...list] : [...list, name];
}

export function removeSource(list: readonly RawSourceEntry[], name: string): RawSourceEntry[] {
  return list.filter((r) => entryOf(r).name !== name);
}
