// What the Knowledge pages derive from the API's answers, as pure functions: a source's size, what it
// is read from, and the 7-day reads (KnowledgeReadCount rows, one per source, path, factory and day).
import type { KnowledgeReadCount, KnowledgeSource } from '../../api/types';
import { plural } from '../../shared/format';
import { repositoryName } from '../../shared/knowledge';
import { formatBytes } from './words';

/** "212 files" and "3.1 MiB" for a source with a snapshot; nothing before its first sync. */
export function sizeOf(s: KnowledgeSource): { files: string; bytes: string } | undefined {
  const n = s.latest_snapshot;
  return n ? { files: plural(n.files, 'file'), bytes: formatBytes(n.bytes) } : undefined;
}

/** What a source reads from, in one line: a repository and its folders, a Confluence space, or an upload. */
export function fromDetail(s: KnowledgeSource): string {
  if (s.kind === 'upload') return 'Files people upload';
  if (s.kind === 'repository' || (s.kind === 'synced' && s.config.repository)) {
    const folders = s.config.include ? ` ${s.config.include.split(/[,\n]/)[0]?.trim() ?? ''}` : '';
    return `${repositoryName(s.config.repository ?? '')}${folders}`.trim();
  }
  if (s.kind === 'confluence' || s.config.spaces) return `Space ${s.config.spaces ?? ''}`.trim();
  return s.connection ?? '';
}

/** The runs' reads and searches of each source, by name. */
export function readsBySource(reads: readonly KnowledgeReadCount[]): Map<string, number> {
  const out = new Map<string, number>();
  for (const r of reads) out.set(r.source, (out.get(r.source) ?? 0) + r.reads + r.searches);
  return out;
}

/** The runs' reads of each file of a source, by path; searches of a whole source ("." ) are not a file's. */
export function readsByPath(reads: readonly KnowledgeReadCount[]): Map<string, number> {
  const out = new Map<string, number>();
  for (const r of reads) if (r.path !== '.') out.set(r.path, (out.get(r.path) ?? 0) + r.reads);
  return out;
}

/** The most read files across sources, most first: `source/path` and its reads. */
export function mostRead(
  reads: readonly KnowledgeReadCount[],
  limit = 5,
): { source: string; path: string; reads: number }[] {
  const by = new Map<string, { source: string; path: string; reads: number }>();
  for (const r of reads) {
    if (r.path === '.' || r.reads === 0) continue;
    const key = `${r.source}/${r.path}`;
    const row = by.get(key) ?? { source: r.source, path: r.path, reads: 0 };
    row.reads += r.reads;
    by.set(key, row);
  }
  return [...by.values()]
    .sort((a, b) => b.reads - a.reads || `${a.source}/${a.path}`.localeCompare(`${b.source}/${b.path}`))
    .slice(0, limit);
}

/**
 * The line agents see for a source in their prompt (internal/knowledge's Knowledge section): its
 * folder, when to read it, how many files and the folders at its top.
 */
export function agentLine(s: KnowledgeSource, files: readonly { path: string }[]): string {
  const folders = [...new Set(files.filter((f) => f.path.includes('/')).map((f) => `${f.path.split('/')[0]}/`))].slice(
    0,
    4,
  );
  const count = s.latest_snapshot?.files ?? files.length;
  const parts = [`/knowledge/${s.name}`, s.read_when ? ` — ${s.read_when}` : ''];
  const tail = count > 0 ? ` ${plural(count, 'file')}${folders.length > 0 ? `: ${folders.join(', ')}` : ''}.` : '';
  return `${parts.join('')}${tail}`;
}
