import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// Components name colors by their tokens (styles.css), so a theme switches them without edits.
// A raw color anywhere else breaks the dark theme silently.
const srcDir = fileURLToPath(new URL('../', import.meta.url));

// Files that still hold raw colors until their own item moves them to tokens. Keep this short: none now.
const allowed = new Set<string>();

const rawColor = /#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{4}|[0-9a-fA-F]{3})\b|\b(?:rgba?|hsla?)\(/;

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

describe('raw colors', () => {
  it('appear only in the allow-listed files', () => {
    const found = sources(srcDir).flatMap((path) =>
      readFileSync(path, 'utf8')
        .split('\n')
        .flatMap((line, i) =>
          rawColor.test(line) && !allowed.has(rel(path)) ? [`${rel(path)}:${i + 1}: ${line.trim()}`] : [],
        ),
    );
    expect(found).toEqual([]);
  });

  it('keeps the allow-list honest: each allowed file exists and still has a raw color', () => {
    for (const file of allowed) {
      const text = readFileSync(join(srcDir, file), 'utf8');
      expect(rawColor.test(text), `${file} has no raw color any more: take it off the allow-list`).toBe(true);
    }
  });

  // An inverted fill is bg-action with text-action-ink: bg-ink flips to near-white in dark, so white text on it vanishes.
  // text-white stays on fills that are dark in both themes (run, you, rework, mark).
  const themeClash: [RegExp, string][] = [
    [
      /!?\bbg-(?:ink|action)\b(?!-).*\btext-white\b|\btext-white\b.*!?\bbg-(?:ink|action)\b(?!-)/,
      'white text on an inverted fill: use text-action-ink',
    ],
    [/\bhover:bg-ink\//, 'hover:bg-ink/ on a filled control: use hover:bg-action/'],
    [/\bbg-white\b/, 'bg-white: use a surface token'],
    [/\bbg-black\b/, 'bg-black: use a token'],
    [/\btext-black\b/, 'text-black: use text-ink or text-action-ink'],
  ];

  it('never pairs white text with an inverted fill or names white or black', () => {
    const found = sources(srcDir).flatMap((path) =>
      readFileSync(path, 'utf8')
        .split('\n')
        .flatMap((line, i) =>
          themeClash.flatMap(([re, why]) => (re.test(line) ? [`${rel(path)}:${i + 1}: ${why}`] : [])),
        ),
    );
    expect(found).toEqual([]);
  });
});
