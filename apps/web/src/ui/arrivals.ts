// The keys of list rows that arrived after the list first had rows (MOTION.md, A new item appears in a list).
// The rows a list opens with do not animate, so a page that loads does not move; a row added later fades in.
// The first non-empty set of keys is the baseline and never changes, so a row keeps its class once it has played.
import { useRef } from 'react';

/** Returns a test for each key: true when the row is new since the list first had rows. */
export function useArrivals(keys: readonly string[]): (key: string) => boolean {
  const baseline = useRef<Set<string> | null>(null);
  if (baseline.current === null && keys.length > 0) baseline.current = new Set(keys);
  const known = baseline.current;
  return (key) => known !== null && !known.has(key);
}
