// The organization's knowledge sources in words (spec 4 §4.8, §10), for the Knowledge pages and
// the builder's "Knowledge … may search": each kind's name and icon, and what a source reads
// ("github, acme/goal main, docs/**/*.md", "confluence, spaces ENG, SUP").
import { BookOpen, FolderGit2, type LucideIcon, RefreshCw, Upload } from 'lucide-react';
import type { KnowledgeSource, KnowledgeSourceKind } from '../api/types';

type Meta = { label: string; icon: LucideIcon };

const metas: Record<Exclude<KnowledgeSourceKind, 'live'>, Meta> = {
  upload: { label: 'Upload', icon: Upload },
  repository: { label: 'Repository', icon: FolderGit2 },
  confluence: { label: 'Confluence', icon: BookOpen },
  synced: { label: 'Synced', icon: RefreshCw },
};

/** Typed with the contract's kinds; the cast goes when the contract drops `live`. */
export const sourceKinds = metas as Record<KnowledgeSourceKind, Meta>;

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

/** What a source reads, in one line: what of its repository or spaces, or that people upload it. */
export function sourceSummary(s: Pick<KnowledgeSource, 'kind' | 'config'>): string {
  if (s.kind === 'upload') return 'Files people upload';
  const { repository, branch, include, spaces } = s.config;
  if (repository) {
    const at = branch ? `${repositoryName(repository)} ${branch}` : repositoryName(repository);
    return `${at}, ${listOf(include).join(', ') || defaultInclude}`;
  }
  const keys = listOf(spaces);
  if (keys.length > 0) return `${keys.length === 1 ? 'space' : 'spaces'} ${keys.join(', ')}`;
  return '';
}

/**
 * A synced document's address at its source, for a link: only http and https, so a source that
 * reports javascript:, data: or file: addresses never puts one in an href.
 */
export function externalUrl(url: string | null | undefined): string | undefined {
  return url && /^https?:\/\//i.test(url) ? url : undefined;
}
