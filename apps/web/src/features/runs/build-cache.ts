import type { Run } from '../../api/types';

type BuildCache = NonNullable<Run['build_cache']>;

const labels: Record<BuildCache, string> = {
  hit: 'Cache hit',
  miss: 'Cache miss',
  off: 'Cache off',
  error: 'Cache error',
};

/** The label a run's build cache outcome shows, or null when the run has none. */
export function buildCacheLabel(outcome: BuildCache | null | undefined): string | null {
  return outcome ? labels[outcome] : null;
}

/** The text colour of the label: the error token for a failed restore, muted for everything else. */
export function buildCacheClass(outcome: BuildCache): string {
  return outcome === 'error' ? 'text-rework-ink' : 'text-muted';
}
