// Where each section's block of factory.yaml is, in words ("board, lines 31–45"): what the
// section tiles say in YAML mode. No dependencies, so the frame can use it without loading the
// YAML view.
import type { LineRange, Section } from '../../../api/types';

/** The YAML block each section edits, as the tiles name it in YAML mode. */
export const sectionBlocks: Record<Section, string> = {
  basics: 'metadata',
  line: 'board',
  agents: 'agents',
  connections: 'connections',
  review: 'budget',
};

/** "lines 31–45", "line 30", or that the block is not in the file. */
export function linesText([from, to]: LineRange): string {
  if (!from) return 'not in the file yet';
  return from === to || !to ? `line ${from}` : `lines ${from}–${to}`;
}

/** "Lines 23–26" or "Line 42", at the start of a sentence. */
export function linesLabel(range: LineRange): string {
  const t = linesText(range);
  return t.charAt(0).toUpperCase() + t.slice(1);
}
