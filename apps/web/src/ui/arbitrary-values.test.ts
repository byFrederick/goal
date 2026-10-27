import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  baselineDir,
  categories,
  formatChange,
  lowered,
  measure,
  raised,
  readBaseline,
  srcDir,
  stale,
  writeBaseline,
} from '../../scripts/arbitrary-values.ts';

// Screens use the type scale, the 4px spacing scale and the radius tokens from styles.css. An arbitrary
// value (text-[13px], p-2.5, rounded-[12px]) sits outside them and drifts from the design system.
// The existing uses are counted per file in the baseline under ui/arbitrary-values/, one JSON per source
// file. A file may not gain one, and the baseline may only shrink: when a screen clears its uses, lower
// its counts in the same change. The counting lives in scripts/arbitrary-values.ts, shared with the script.
const command = 'pnpm --filter web baseline:arbitrary';

describe('arbitrary values', () => {
  it.each(categories)('%s uses only go down', (category) => {
    const allowed = readBaseline(baselineDir);
    const measured = measure(srcDir);

    const grew = raised(allowed, measured)
      .filter((change) => change.category === category)
      .map(formatChange);
    const shrank = lowered(allowed, measured)
      .filter((change) => change.category === category)
      .map(formatChange);

    expect(
      grew,
      `new arbitrary ${category} values: use a token from styles.css. In a baseline conflict, take main's JSONs and run ${command}`,
    ).toEqual([]);
    expect(shrank, `lower these counts: run ${command}`).toEqual([]);
  });

  it('every baseline file names a source that still has uses', () => {
    const gone = stale(readBaseline(baselineDir), srcDir);

    expect(gone, `delete these baseline files: run ${command}`).toEqual([]);
  });
});

describe('the counting and the baseline rules', () => {
  let root: string;
  let src: string;
  let baseline: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), 'arbitrary-values-'));
    src = join(root, 'src');
    baseline = join(root, 'baseline');
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  function source(path: string, text: string): void {
    mkdirSync(dirname(join(src, path)), { recursive: true });
    writeFileSync(join(src, path), text);
  }

  function baselineFile(path: string, counts: Record<string, number>): void {
    mkdirSync(dirname(join(baseline, path)), { recursive: true });
    writeFileSync(join(baseline, `${path}.json`), JSON.stringify(counts));
  }

  it('a file that gains a use is raised', () => {
    source('a.tsx', '<p className="text-[13px]" />');

    expect(raised(readBaseline(baseline), measure(src))).toEqual([
      { file: 'a.tsx', category: 'fontSize', from: 0, to: 1 },
    ]);
  });

  it('a count above the measured value is lowered', () => {
    source('a.tsx', '<p className="text-[13px]" />');
    baselineFile('a.tsx', { fontSize: 3 });

    expect(lowered(readBaseline(baseline), measure(src))).toEqual([
      { file: 'a.tsx', category: 'fontSize', from: 3, to: 1 },
    ]);
  });

  it('spacing counts only the values off the 4px scale', () => {
    source('a.tsx', '<p className="p-2.5 p-4 px-[12px] px-[13px]" />');

    expect(measure(src)['a.tsx']?.spacing).toBe(2);
  });

  it('a baseline file whose source is gone or has no uses is stale', () => {
    source('a.tsx', '<p className="p-4" />');
    baselineFile('a.tsx', { fontSize: 0, spacing: 0, radius: 0 });
    baselineFile('gone.tsx', { fontSize: 2, spacing: 0, radius: 0 });

    expect(stale(readBaseline(baseline), src)).toEqual(['a.tsx', 'gone.tsx']);
  });

  it('writeBaseline lowers counts, drops files with none, and a second run changes nothing', () => {
    source('a.tsx', '<p className="text-[13px]" />');
    baselineFile('a.tsx', { fontSize: 3, spacing: 0, radius: 0 });
    baselineFile('b.tsx', { fontSize: 2, spacing: 0, radius: 0 });

    writeBaseline(measure(src), baseline);
    const first = readFileSync(join(baseline, 'a.tsx.json'), 'utf8');

    expect(readBaseline(baseline)).toEqual(measure(src));
    expect(existsSync(join(baseline, 'b.tsx.json'))).toBe(false);

    writeBaseline(measure(src), baseline);
    expect(readFileSync(join(baseline, 'a.tsx.json'), 'utf8')).toBe(first);
  });
});
