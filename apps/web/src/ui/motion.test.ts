import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// Components name durations, easings and transitions by their motion tokens (styles.css, MOTION.md, Tokens),
// so one change to the motion scale reaches every component. A raw value anywhere else is a second scale.
// styles.css is exempt: it is where the tokens are defined. It is not a .ts or .tsx file, so it is never read.
const srcDir = fileURLToPath(new URL('../', import.meta.url));

// Files that still hold raw motion values until their own item moves them to tokens. Keep this short: none now.
const allowed = new Set<string>();

const rawMotion = /\b\d+ms\b|cubic-bezier\(|transition:\s*all\b/;

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

describe('raw motion values', () => {
  it('appear only in the allow-listed files', () => {
    const found = sources(srcDir).flatMap((path) =>
      readFileSync(path, 'utf8')
        .split('\n')
        .flatMap((line, i) =>
          rawMotion.test(line) && !allowed.has(rel(path)) ? [`${rel(path)}:${i + 1}: ${line.trim()}`] : [],
        ),
    );
    expect(found).toEqual([]);
  });

  it('keeps the allow-list honest: each allowed file exists and still has a raw motion value', () => {
    for (const file of allowed) {
      const text = readFileSync(join(srcDir, file), 'utf8');
      expect(rawMotion.test(text), `${file} has no raw motion value any more: take it off the allow-list`).toBe(true);
    }
  });
});
