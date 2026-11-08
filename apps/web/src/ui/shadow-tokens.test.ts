import { readFileSync } from 'node:fs';
import { compile } from 'tailwindcss';
import { describe, expect, it } from 'vitest';

// Tailwind compiles a @theme value into the utility at build time, so a dark block that overrides the
// shadow itself never reaches it. The utilities must read only variables the dark blocks override.
const css = readFileSync(new URL('../styles.css', import.meta.url), 'utf8').replace(/@import[^;]*;/g, '');

function block(start: string): string {
  const from = css.indexOf(start);
  expect(from, start).toBeGreaterThanOrEqual(0);
  const open = css.indexOf('{', from);
  let depth = 0;
  for (let i = open; i < css.length; i++) {
    if (css[i] === '{') depth++;
    if (css[i] === '}' && --depth === 0) return css.slice(open + 1, i);
  }
  throw new Error(`unclosed block ${start}`);
}

const ring = (body: string) => body.match(/--color-shadow-ring:\s*([^;]+);/)?.[1];

describe('shadow tokens follow the theme', () => {
  it('compiles shadow-pop and shadow-raised without the ring colour fixed in', async () => {
    const { build } = await compile(`@theme {${block('@theme')}} @tailwind utilities;`);
    const out = build(['shadow-pop', 'shadow-raised']);
    expect(out).toContain('var(--color-shadow-ring)');
    expect(out).not.toMatch(/#ffffff|white/);
  });

  it('keeps the ring ink at 6% on light and white at 8% on dark', () => {
    const theme = block('@theme');
    expect(theme).toMatch(/--shadow-pop:\s*0 0 0 1px var\(--color-shadow-ring\)/);
    expect(ring(theme)).toBe('color-mix(in srgb, var(--color-shadow) 6%, transparent)');
    for (const start of [':root[data-theme="dark"]', ':root:not([data-theme="light"])']) {
      const dark = block(start);
      expect(ring(dark), start).toBe('color-mix(in srgb, #ffffff 8%, transparent)');
      expect(dark, start).not.toMatch(/--shadow-pop:/);
    }
  });
});
