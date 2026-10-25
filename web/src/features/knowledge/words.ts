// What the Knowledge pages say, as pure functions (words.test.ts): a source's status (never
// colour alone) and its last and next sync, a document's status, the schedules people pick, a
// refusal's sentence, which files an upload takes, and a search hit's snippet cut to the part
// around the query's words with those words marked.
import { ApiError } from '../../api/client';
import type { KnowledgeDocument, KnowledgeSource } from '../../api/types';
import { since, type Tone } from '../../shared/connections';
import { formatAge, formatClock, formatDay, formatDuration } from '../../shared/format';

export interface StatusLine {
  tone: Tone;
  text: string;
  /** Why, under the status: the last sync's error. */
  detail?: string;
}

/** "Indexed", "Synced", "Syncing", "Sync failed" (with why), "Live, not indexed", or not yet. */
export function sourceStatus(s: KnowledgeSource): StatusLine {
  if (s.kind === 'live') return { tone: 'muted', text: 'Live, not indexed' };
  if (s.status === 'syncing') return { tone: 'muted', text: 'Syncing' };
  if (s.status === 'failed') {
    const kept = s.last_synced_at ? ` The documents from ${formatDay(s.last_synced_at)} stay searchable.` : '';
    return {
      tone: 'rework',
      text: 'Sync failed',
      detail: `${s.last_error ? `${sentence(s.last_error)}.` : 'It stopped before the end.'}${kept}`,
    };
  }
  if (!s.last_synced_at) {
    return s.kind === 'upload' && s.documents === 0
      ? { tone: 'muted', text: 'No documents yet' }
      : { tone: 'muted', text: 'Waiting for its first sync' };
  }
  return { tone: 'ok', text: s.kind === 'upload' ? 'Indexed' : 'Synced' };
}

const sentence = (text: string) => text.charAt(0).toUpperCase() + text.slice(1).replace(/\.$/, '');

const dayMs = 86_400_000;

/** "1 h ago" within a day, else "Oct 22". */
function ago(iso: string, now: number): string {
  return now - Date.parse(iso) < dayMs ? since(iso, now) : formatDay(iso);
}

/**
 * When it last synced and when it syncs next, as the Last sync column says them: "1 h ago" and
 * "Next in 15 min"; an upload's "When files are added"; "Running" while it syncs.
 */
export function syncTimes(s: KnowledgeSource, now: number): { last: string; next?: string } {
  if (s.kind === 'live') return { last: 'Searched at question time' };
  if (s.status === 'syncing') return { last: 'Running' };
  const last = s.last_synced_at ? ago(s.last_synced_at, now) : 'Not yet';
  if (s.kind === 'upload') return { last, next: 'When files are added' };
  if (!s.next_sync_at) return { last };
  const wait = Date.parse(s.next_sync_at) - now;
  if (wait < 60_000) return { last, next: 'Due now' };
  if (wait < 3_600_000) return { last, next: `Next in ${formatAge(wait / 1000)}` };
  if (wait < dayMs) return { last, next: `Next ${formatClock(s.next_sync_at, new Date(now))}` };
  return { last, next: `Next ${formatDay(s.next_sync_at)}` };
}

/** "Indexed 14:00", "Waiting to be indexed", "Failed: …", "Skipped: …". */
export function documentStatus(d: KnowledgeDocument, now: number): StatusLine {
  switch (d.status) {
    case 'indexed':
      return { tone: 'ok', text: d.indexed_at ? `Indexed ${formatClock(d.indexed_at, new Date(now))}` : 'Indexed' };
    case 'failed':
      return { tone: 'rework', text: `Failed: ${d.error || 'the sync gave no reason'}` };
    case 'skipped':
      return { tone: 'muted', text: `Skipped: ${d.error || 'it has no text'}` };
    default:
      return { tone: 'muted', text: 'Waiting to be indexed' };
  }
}

/** The schedules a synced source takes (at least every 15 minutes). */
export const syncChoices = [
  { seconds: 900, label: '15 minutes' },
  { seconds: 3600, label: '1 hour' },
  { seconds: 6 * 3600, label: '6 hours' },
  { seconds: 86_400, label: '1 day' },
] as const;

/** "1 hour", or "2 h" for a schedule set elsewhere (the CLI's --every). */
export function everyLabel(seconds: number): string {
  return syncChoices.find((c) => c.seconds === seconds)?.label ?? formatDuration(seconds * 1000);
}

/** The file types an upload takes, by extension (internal/knowledge.TypeOf). */
export const uploadTypes = ['.txt', '.text', '.md', '.markdown', '.html', '.htm', '.json', '.yaml', '.yml'] as const;

/** Why a file can't be uploaded before it is sent, or undefined when goald may take it. */
export function refusedFile(name: string): string | undefined {
  const dot = name.lastIndexOf('.');
  const ext = dot < 0 ? '' : name.slice(dot).toLowerCase();
  return (uploadTypes as readonly string[]).includes(ext)
    ? undefined
    : 'Not text, Markdown, HTML, JSON or YAML. PDF and Office files come later.';
}

/** "820 B", "14.2 KiB", "3.1 MiB". */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const kib = n / 1024;
  if (kib < 1024) return `${kib.toFixed(kib < 10 ? 1 : 0)} KiB`;
  const mib = kib / 1024;
  return `${mib.toFixed(mib < 10 ? 1 : 0)} MiB`;
}

/** The sentence for a refusal of a knowledge change (goald's Problem detail, or why it could not ask). */
export function refusalText(err: unknown): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Try again once it is running.';
  return err.detail ?? err.title;
}

export interface Segment {
  text: string;
  /** One of the query's words. */
  mark: boolean;
}

/** The query's words worth marking: letters and digits, two or more. */
function terms(query: string): string[] {
  return [...new Set(query.toLowerCase().match(/[\p{L}\p{N}]{2,}/gu) ?? [])];
}

const escapeRegExp = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/**
 * A hit's snippet as a search result shows it: one line of plain text (no Markdown heading marks),
 * at most `max` characters starting a little before the first of the query's words, cut at words,
 * with an ellipsis where it was cut and the query's words marked where they start a word.
 */
export function excerpt(snippet: string, query: string, max = 240): Segment[] {
  const text = snippet
    .replace(/^#{1,6}\s+/gm, '')
    .replace(/\s+/g, ' ')
    .trim();
  const words = terms(query);
  const pattern =
    words.length > 0 ? new RegExp(`(?<![\\p{L}\\p{N}])(${words.map(escapeRegExp).join('|')})`, 'giu') : null;
  const first = pattern ? text.search(pattern) : -1;
  let start = first > 60 ? text.lastIndexOf(' ', first - 40) + 1 : 0;
  if (start < 0) start = 0;
  let end = Math.min(text.length, start + max);
  if (end < text.length) {
    const space = text.lastIndexOf(' ', end);
    if (space > start) end = space;
  }
  const cut = `${start > 0 ? '…' : ''}${text.slice(start, end)}${end < text.length ? '…' : ''}`;
  if (!pattern) return [{ text: cut, mark: false }];
  const out: Segment[] = [];
  let at = 0;
  for (const m of cut.matchAll(pattern)) {
    const i = m.index ?? 0;
    if (i > at) out.push({ text: cut.slice(at, i), mark: false });
    out.push({ text: m[0], mark: true });
    at = i + m[0].length;
  }
  if (at < cut.length) out.push({ text: cut.slice(at), mark: false });
  return out;
}

/** goald's pattern of a source's name: 3 to 40 lowercase letters, digits and inner dashes. */
export const sourceName = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/** A name from a title ("Infra docs" infra-docs), numbered when it is taken; "" when none fits. */
export function nameFor(title: string, taken: readonly string[]): string {
  const base = title
    .normalize('NFKD')
    .replace(/\p{M}/gu, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^[^a-z]+/, '')
    .slice(0, 36)
    .replace(/-+$/, '');
  if (!sourceName.test(base)) return '';
  if (!taken.includes(base)) return base;
  for (let i = 2; ; i++) {
    const name = `${base}-${i}`;
    if (!taken.includes(name)) return name;
  }
}
