import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// Screens use the type scale, the 4px spacing scale and the radius tokens from styles.css. An arbitrary
// value (text-[13px], p-2.5, rounded-[12px]) sits outside them and drifts from the design system.
// The existing uses are counted per file in arbitrary-values.baseline.json. A file may not gain one, and
// the baseline may only shrink: when a screen clears its uses, lower its counts in the same change.
const srcDir = fileURLToPath(new URL('../', import.meta.url));
const baselinePath = join(srcDir, 'ui/arbitrary-values.baseline.json');

const patterns = {
  fontSize: /(?<![\w-])text-\[\d*\.?\d+(?:px|rem|em)\]/g,
  // Off the 4px scale: a fractional step (gap-1.5, py-2.5) or an arbitrary pixel value that is not a multiple of 4.
  spacing:
    /(?<![\w-])(?:p|px|py|pt|pr|pb|pl|m|mx|my|mt|mr|mb|ml|gap|gap-x|gap-y|space-x|space-y)-(?:\d+\.\d+|\[\d+px\])(?![\w.])/g,
  radius: /(?<![\w-])rounded(?:-[a-z]{1,2})?-\[[^\]\s]+\]/g,
} as const;

type Category = keyof typeof patterns;

function offScale(token: string): boolean {
  const px = /\[(\d+)px\]$/.exec(token);
  return px ? Number(px[1]) % 4 !== 0 : true;
}

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    const app = /\.tsx?$/.test(name) && !/\.(test|gen)\.tsx?$/.test(name);
    return app ? [path] : [];
  });
}

function rel(path: string): string {
  return relative(srcDir, path).split('\\').join('/');
}

// The uses of one category in each file that has any.
function measure(category: Category): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const path of sources(srcDir)) {
    const hits = readFileSync(path, 'utf8').match(patterns[category]) ?? [];
    const uses = category === 'spacing' ? hits.filter(offScale) : hits;
    if (uses.length > 0) counts[rel(path)] = uses.length;
  }
  return counts;
}

const baseline = JSON.parse(readFileSync(baselinePath, 'utf8')) as Record<Category, Record<string, number>>;

describe('arbitrary values', () => {
  it.each(Object.keys(patterns) as Category[])('%s uses only go down', (category) => {
    const counts = measure(category);
    const allowed = baseline[category];

    const grew = Object.entries(counts)
      .filter(([file, n]) => n > (allowed[file] ?? 0))
      .map(([file, n]) => `${file}: ${allowed[file] ?? 0} -> ${n}`);
    const shrank = Object.entries(allowed)
      .filter(([file, n]) => (counts[file] ?? 0) < n)
      .map(([file, n]) => `${file}: ${n} -> ${counts[file] ?? 0}`);

    expect(grew, `new arbitrary ${category} values: use a token from styles.css`).toEqual([]);
    expect(shrank, `lower these counts in arbitrary-values.baseline.json`).toEqual([]);
  });
});
