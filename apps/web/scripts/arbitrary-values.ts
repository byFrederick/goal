// The arbitrary-value counting shared by src/ui/arbitrary-values.test.ts and scripts/baseline-arbitrary.ts,
// so the test and the script cannot count differently. It lives outside src/, so the test never counts its
// own patterns. The baseline is one JSON per source file under src/ui/arbitrary-values/, at the source's
// path with `.json` added (features/board/board-card.tsx.json).
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

export const patterns = {
  fontSize: /(?<![\w-])text-\[\d*\.?\d+(?:px|rem|em)\]/g,
  // Off the 4px scale: a fractional step (gap-1.5, py-2.5) or an arbitrary pixel value that is not a multiple of 4.
  spacing:
    /(?<![\w-])(?:p|px|py|pt|pr|pb|pl|m|mx|my|mt|mr|mb|ml|gap|gap-x|gap-y|space-x|space-y)-(?:\d+\.\d+|\[\d+px\])(?![\w.])/g,
  radius: /(?<![\w-])rounded(?:-[a-z]{1,2})?-\[[^\]\s]+\]/g,
} as const;

export type Category = keyof typeof patterns;
export const categories = Object.keys(patterns) as Category[];

export type Counts = Record<Category, number>;
// Keyed by a source path relative to src/, such as features/board/board-card.tsx.
export type Files = Record<string, Counts>;

// One count that differs between the baseline and the measured sources.
export type Change = { file: string; category: Category; from: number; to: number };

export const srcDir = fileURLToPath(new URL('../src/', import.meta.url));
export const baselineDir = join(srcDir, 'ui/arbitrary-values');

function zero(): Counts {
  return { fontSize: 0, spacing: 0, radius: 0 };
}

function total(counts: Counts): number {
  return categories.reduce((sum, category) => sum + counts[category], 0);
}

function offScale(token: string): boolean {
  const px = /\[(\d+)px\]$/.exec(token);
  return px ? Number(px[1]) % 4 !== 0 : true;
}

function walk(dir: string, keep: (name: string) => boolean): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return walk(path, keep);
    return keep(name) ? [path] : [];
  });
}

function rel(root: string, path: string): string {
  return relative(root, path).split('\\').join('/');
}

// The uses of every category in each source file under root that has any.
export function measure(root: string = srcDir): Files {
  const files: Files = {};
  const sources = walk(root, (name) => /\.tsx?$/.test(name) && !/\.(test|gen)\.tsx?$/.test(name));
  for (const path of sources) {
    const text = readFileSync(path, 'utf8');
    const counts = zero();
    for (const category of categories) {
      const hits = text.match(patterns[category]) ?? [];
      counts[category] = category === 'spacing' ? hits.filter(offScale).length : hits.length;
    }
    if (total(counts) > 0) files[rel(root, path)] = counts;
  }
  return files;
}

// The baseline JSONs under dir. A missing category in a file counts as zero.
export function readBaseline(dir: string = baselineDir): Files {
  if (!existsSync(dir)) return {};
  const files: Files = {};
  for (const path of walk(dir, (name) => name.endsWith('.json'))) {
    const json = JSON.parse(readFileSync(path, 'utf8')) as Partial<Counts>;
    const counts = zero();
    for (const category of categories) counts[category] = json[category] ?? 0;
    files[rel(dir, path).replace(/\.json$/, '')] = counts;
  }
  return files;
}

// Writes one JSON per file with any use and deletes the JSONs of files that have none. Writes the same
// bytes for the same counts, so running it twice changes nothing.
export function writeBaseline(files: Files, dir: string = baselineDir): void {
  const kept = new Set<string>();
  for (const [file, counts] of Object.entries(files)) {
    if (total(counts) === 0) continue;
    const path = join(dir, `${file}.json`);
    const ordered = Object.fromEntries(categories.map((category) => [category, counts[category]]));
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, `${JSON.stringify(ordered, null, 2)}\n`);
    kept.add(path);
  }
  if (existsSync(dir)) {
    for (const path of walk(dir, (name) => name.endsWith('.json'))) if (!kept.has(path)) rmSync(path);
  }
}

// Every count that differs from allowed to measured, sorted by file.
function differences(allowed: Files, measured: Files): Change[] {
  const files = [...new Set([...Object.keys(allowed), ...Object.keys(measured)])].sort();
  return files.flatMap((file) =>
    categories.flatMap((category) => {
      const from = allowed[file]?.[category] ?? 0;
      const to = measured[file]?.[category] ?? 0;
      return from === to ? [] : [{ file, category, from, to }];
    }),
  );
}

// Counts that would go up: a new arbitrary value.
export function raised(allowed: Files, measured: Files): Change[] {
  return differences(allowed, measured).filter((change) => change.to > change.from);
}

// Counts that would go down: uses that were cleared, or a file that no longer has any.
export function lowered(allowed: Files, measured: Files): Change[] {
  return differences(allowed, measured).filter((change) => change.to < change.from);
}

export function formatChange(change: Change): string {
  return `${change.file} ${change.category} ${change.from} -> ${change.to}`;
}

// Baseline files that name a source which is gone, or that have no uses left.
export function stale(baseline: Files, root: string = srcDir): string[] {
  return Object.entries(baseline)
    .filter(([file, counts]) => !existsSync(join(root, file)) || total(counts) === 0)
    .map(([file]) => file)
    .sort();
}
