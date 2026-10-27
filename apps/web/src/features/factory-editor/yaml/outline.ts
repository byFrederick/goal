// The Outline of factory.yaml (screens build/definition): the keys of the file with their depth,
// the number of items a list holds, and the problems under each key. A pure function over the
// text, so it needs no parser and no network; it reads the two-space layout that Format writes.
// A key whose value is a list shows the list's length; a key with problems shows them instead.

export interface OutlineItem {
  /** The key's name, as written. */
  key: string;
  /** 1-based line of the key. */
  line: number;
  /** 0 for a top-level key, 1 under it, 2 under that. */
  depth: number;
  /** "7" for a list of seven items, when the key has no problems. */
  count?: number | undefined;
  /** Problems on the key's lines (its block). */
  problems: number;
}

const keyLine = /^( *)([A-Za-z][\w-]*):(?: +(.*))?$/;
const maxDepth = 2;

/** The outline of `text`; `problemLines` are the 1-based lines that have an error. */
export function outlineOf(text: string, problemLines: readonly number[] = []): OutlineItem[] {
  const lines = text.split('\n');
  const keys = lines.flatMap((raw, i) => {
    const m = keyLine.exec(raw);
    if (!m) return [];
    const indent = (m[1] ?? '').length;
    return indent % 2 === 0 && indent / 2 <= maxDepth ? [{ i, indent, key: m[2] ?? '', value: m[3] ?? '' }] : [];
  });

  return keys.map((k, n) => {
    // The block ends before the next key at the same or a shallower indent.
    const next = keys.slice(n + 1).find((other) => other.indent <= k.indent);
    const end = next ? next.i : lines.length;
    const line = k.i + 1;
    const problems = problemLines.filter((p) => p >= line && p <= end).length;
    return {
      key: k.key,
      line,
      depth: k.indent / 2,
      count: problems > 0 ? undefined : listLength(k, lines, end),
      problems,
    };
  });
}

/** How many items a key's list holds: a flow list counts its commas, a block list its "- " lines. */
function listLength(k: { i: number; indent: number; value: string }, lines: string[], end: number): number | undefined {
  if (k.value.startsWith('[')) {
    const inner = k.value.replace(/^\[|\]$/g, '').trim();
    return inner === '' ? 0 : inner.split(',').length;
  }
  if (k.value !== '') return undefined;
  const items = lines.slice(k.i + 1, end).filter((l) => new RegExp(`^ {${k.indent + 2}}- `).test(l)).length;
  return items > 0 ? items : undefined;
}
