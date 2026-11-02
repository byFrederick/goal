import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// Reads the token values from styles.css, so a token edit that drops a pair below AA fails here.
const css = readFileSync(new URL('../styles.css', import.meta.url), 'utf8');

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

function tokens(body: string): Record<string, string> {
  return Object.fromEntries([...body.matchAll(/--color-([\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)].map((m) => [m[1], m[2]]));
}

const light = tokens(block('@theme'));
const dark = tokens(block(':root[data-theme="dark"]'));
const media = tokens(block(':root:not([data-theme="light"])'));
// White is not a token in the dark block; it is the fixed text on status fills.
const withWhite = (t: Record<string, string>): Record<string, string> => ({ white: '#ffffff', ...t });

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = Number.parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * (r ?? 0) + 0.7152 * (g ?? 0) + 0.0722 * (b ?? 0);
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return ((hi ?? 0) + 0.05) / ((lo ?? 0) + 0.05);
}

const pairs: [string, string, string][] = [
  ['action-ink', 'action', 'the primary button'],
  ['ink', 'hover', 'the selected station tab'],
  ['action-ink', 'muted', 'the avatar'],
  ['white', 'run', 'text on a run fill'],
  ['white', 'you', 'text on a you fill'],
  ['white', 'rework', 'text on a rework fill'],
];

// Text tokens: body text on each surface it sits on.
const textPairs: [string, string][] = ['link', 'rework-ink', 'ok-ink', 'you-ink', 'ink-soft'].flatMap((fg) =>
  ['card', 'panel', 'shell'].map((bg): [string, string] => [fg, bg]),
);

describe.each([
  ['light', light],
  ['dark', dark],
] as const)('theme contrast in %s', (_name, theme) => {
  it.each(pairs)('%s on %s reaches 4.5:1 (%s)', (fg, bg) => {
    const t = withWhite(theme);
    expect(t[fg], fg).toBeDefined();
    expect(t[bg], bg).toBeDefined();
    expect(contrast(t[fg] as string, t[bg] as string)).toBeGreaterThanOrEqual(4.5);
  });

  it.each(textPairs)('text %s on %s reaches 4.5:1', (fg, bg) => {
    expect(theme[fg], fg).toBeDefined();
    expect(theme[bg], bg).toBeDefined();
    expect(contrast(theme[fg] as string, theme[bg] as string)).toBeGreaterThanOrEqual(4.5);
  });
});

describe('the prefers-color-scheme dark block', () => {
  it('has the same values as the data-theme dark block for the checked tokens', () => {
    for (const name of [
      'action',
      'action-ink',
      'ink',
      'hover',
      'muted',
      'run',
      'you',
      'rework',
      'link',
      'rework-ink',
      'ok-ink',
      'you-ink',
      'ink-soft',
    ]) {
      expect(media[name], name).toBe(dark[name]);
    }
  });
});
