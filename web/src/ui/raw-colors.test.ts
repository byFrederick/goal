import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// Components name colors by their tokens (styles.css), so a theme switches them without edits.
// A raw color anywhere else breaks the dark theme silently.
const srcDir = fileURLToPath(new URL('../', import.meta.url));

// Files that still hold raw colors until their own item moves them to tokens. Keep this short.
const allowed = new Set(['features/factory-editor/yaml/monaco-editor.tsx']);

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
});
