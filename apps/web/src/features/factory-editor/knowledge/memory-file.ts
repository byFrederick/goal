// memory.yaml, the factory's memory (ADR 0038 decision 6): a list of short facts, each with the items
// it came from and the day it was added. These helpers read the file's text and write it back for the
// draft session's file edits; the limits are goald's (memory-too-long, memory-too-big).
import { parse, stringify } from 'yaml';

export const memoryPath = 'memory.yaml';
export const maxMemoryText = 300;
export const maxMemoryBytes = 8 * 1024;
export const maxMemoryEntries = 50;

export interface MemoryEntry {
  text: string;
  /** The items the fact came from, as <factory>#<number>. */
  from: string[];
  /** YYYY-MM-DD. */
  added: string;
}

/** The entries of memory.yaml's text; none for an empty file or one that does not hold a list. */
export function readMemory(text: string | undefined): MemoryEntry[] {
  if (!text?.trim()) return [];
  let doc: unknown;
  try {
    doc = parse(text);
  } catch {
    return [];
  }
  if (!Array.isArray(doc)) return [];
  const out: MemoryEntry[] = [];
  for (const raw of doc) {
    const e = (raw ?? {}) as { text?: unknown; from?: unknown; added?: unknown };
    if (typeof e.text !== 'string') continue;
    out.push({
      text: e.text,
      from: Array.isArray(e.from) ? e.from.filter((x): x is string => typeof x === 'string') : [],
      added: typeof e.added === 'string' ? e.added : e.added instanceof Date ? e.added.toISOString().slice(0, 10) : '',
    });
  }
  return out;
}

/** The file's text for these entries; empty for none, so the draft drops nothing but a blank file. */
export function writeMemory(entries: readonly MemoryEntry[]): string {
  if (entries.length === 0) return '';
  return stringify(
    entries.map((e) => ({ text: e.text, ...(e.from.length > 0 ? { from: e.from } : {}), added: e.added })),
    { lineWidth: 0 },
  );
}

export const today = (now: Date = new Date()): string => now.toISOString().slice(0, 10);

/** What stops a fact from being saved, in plain words; undefined when it is fine. */
export function memoryProblem(entries: readonly MemoryEntry[], index: number, text: string): string | undefined {
  const t = text.trim();
  if (t === '') return 'Write the fact.';
  if (t.length > maxMemoryText) return `At most ${maxMemoryText} characters (now ${t.length}).`;
  if (index < 0 && entries.length >= maxMemoryEntries) return `Factory memory holds at most ${maxMemoryEntries} facts.`;
  const next = entries.map((e, i) => (i === index ? { ...e, text: t } : e));
  if (index < 0) next.push({ text: t, from: [], added: today() });
  if (new TextEncoder().encode(writeMemory(next)).length > maxMemoryBytes) return 'Factory memory would pass 8 KB.';
  return undefined;
}

export function addEntry(entries: readonly MemoryEntry[], text: string, now?: Date): MemoryEntry[] {
  return [...entries, { text: text.trim(), from: [], added: today(now) }];
}

export function editEntry(entries: readonly MemoryEntry[], index: number, text: string): MemoryEntry[] {
  return entries.map((e, i) => (i === index ? { ...e, text: text.trim() } : e));
}

export function removeEntry(entries: readonly MemoryEntry[], index: number): MemoryEntry[] {
  return entries.filter((_, i) => i !== index);
}
