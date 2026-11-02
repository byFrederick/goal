import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// Text uses the link and ink tokens, which reach AA in both themes. The base tokens run, rework and ok are
// fills and marks: as a text colour they fall below 4.5:1 in dark (or in light, for ok).
const srcDir = fileURLToPath(new URL('../', import.meta.url));

const fillAsText = /text-(?:run|rework|ok)(?![-\w])/;

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !/text-tokens\.test\.ts$/.test(name) ? [path] : [];
  });
}

function offenders(file: string, text: string): string[] {
  return text.split('\n').flatMap((line, i) => (fillAsText.test(line) ? [`${file}:${i + 1}: ${line.trim()}`] : []));
}

describe('text colour tokens', () => {
  it('flags a fill token used as a text colour, with or without a variant', () => {
    expect(offenders('f.tsx', 'a\n<p className="text-rework">x</p>')).toHaveLength(1);
    expect(offenders('f.tsx', '<a className="hover:text-run group-hover:text-ok">x</a>')).toHaveLength(1);
  });

  it('passes ink and tint classes and fill uses', () => {
    const ok = '<p className="text-rework-ink bg-rework text-run-ink text-ok-ink border-ok bg-run-tint">x</p>';
    expect(offenders('f.tsx', ok)).toEqual([]);
  });

  it('finds none in the app: use text-link, text-rework-ink or text-ok-ink', () => {
    const found = sources(srcDir).flatMap((path) =>
      offenders(relative(srcDir, path).split('\\').join('/'), readFileSync(path, 'utf8')),
    );
    expect(found).toEqual([]);
  });
});
