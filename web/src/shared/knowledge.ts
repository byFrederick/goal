// The organization's knowledge sources in words (spec 4 §4.8, §10), for the Knowledge pages and
// the builder's "Knowledge … may search": each kind's name and icon, and what a source reads
// ("github, acme/goal main, docs/**/*.md", "confluence, spaces ENG, SUP").
import { type LucideIcon, RefreshCw, Upload, Zap } from 'lucide-react';
import type { KnowledgeSource, KnowledgeSourceKind } from '../api/types';

export const sourceKinds: Record<KnowledgeSourceKind, { label: string; icon: LucideIcon }> = {
  upload: { label: 'Upload', icon: Upload },
  synced: { label: 'Synced', icon: RefreshCw },
  live: { label: 'Live', icon: Zap },
};

/** gitdocs's include when a source names none. */
export const defaultInclude = '**/*.md';

/** A list of globs or space keys as goald reads it: separated by commas or new lines. */
export function listOf(value: string | undefined): string[] {
  return (value ?? '')
    .split(/[,\n]/)
    .map((v) => v.trim())
    .filter(Boolean);
}

/** "acme/goal" from https://github.com/acme/goal; anything else as it is. */
export function repositoryName(url: string): string {
  try {
    const path = new URL(url).pathname.replace(/^\/+|\/+$/g, '').replace(/\.git$/, '');
    return path || url;
  } catch {
    return url;
  }
}

/** What a source reads, in one line: its connection and what of it, or that people upload it. */
export function sourceSummary(s: Pick<KnowledgeSource, 'kind' | 'connection' | 'config'>): string {
  if (s.kind === 'upload') return 'Files people upload';
  const connection = s.connection ?? '';
  if (s.kind === 'live') return `${connection}, searched by its own tools`;
  const { repository, branch, include, spaces } = s.config;
  if (repository) {
    const at = branch ? `${repositoryName(repository)} ${branch}` : repositoryName(repository);
    return `${connection}, ${at}, ${listOf(include).join(', ') || defaultInclude}`;
  }
  const keys = listOf(spaces);
  if (keys.length > 0) return `${connection}, ${keys.length === 1 ? 'space' : 'spaces'} ${keys.join(', ')}`;
  return connection;
}
