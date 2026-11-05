import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { editorTheme, syntaxTokens, type TokenReader } from './editor-theme';

const css = readFileSync(fileURLToPath(new URL('../../../styles.css', import.meta.url)), 'utf8');

/** The declarations of the first block that opens with `selector {`. */
function block(selector: string): string {
  const start = css.indexOf(`${selector} {`);
  expect(start).toBeGreaterThan(-1);
  return css.slice(start, css.indexOf('\n}', start));
}

function reader(source: string, fallback: string): TokenReader {
  const own = (name: string) => new RegExp(`--color-${name}:\\s*(#[0-9a-fA-F]{6})`).exec(source)?.[1];
  return (name) => {
    const value = own(name) ?? new RegExp(`--color-${name}:\\s*(#[0-9a-fA-F]{6})`).exec(fallback)?.[1];
    if (!value) throw new Error(`no value for --color-${name}`);
    return value;
  };
}

const light = reader(block('@theme'), '');
const dark = reader(block(':root[data-theme="dark"]'), block('@theme'));

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

describe.each([
  ['light', light, false, 'vs'],
  ['dark', dark, true, 'vs-dark'],
] as const)('the editor theme in %s', (_name, read, isDark, base) => {
  const theme = editorTheme(read, isDark);

  it('builds on the base that matches the page theme', () => {
    expect(theme.base).toBe(base);
  });

  it('maps every syntax token, operators, delimiters and tags included', () => {
    expect(theme.rules.map((r) => r.token).sort()).toEqual(Object.keys(syntaxTokens).sort());
  });

  it('reads at WCAG AA (4.5:1) against the editor background', () => {
    const background = theme.colors['editor.background'] as string;
    for (const rule of theme.rules) {
      expect(contrast(`#${rule.foreground}`, background), rule.token).toBeGreaterThanOrEqual(4.5);
    }
    expect(contrast(theme.colors['editorLineNumber.foreground'] as string, background)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(theme.colors['editorLineNumber.dimmedForeground'] as string, background)).toBeGreaterThanOrEqual(
      4.5,
    );
  });

  it('themes the suggest and hover widgets', () => {
    for (const key of [
      'editorSuggestWidget.background',
      'editorSuggestWidget.border',
      'editorHoverWidget.background',
      'editorHoverWidget.border',
    ]) {
      expect(theme.colors[key]).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });
});

describe('the editor theme with shortened hex values', () => {
  it.each([
    ['#fff', '#ffffff', 'ffffff'],
    ['#ffff', '#ffffffff', 'ffffffff'],
  ])('expands %s so Monaco accepts every value', (short, colour, rule) => {
    const theme = editorTheme(() => short, false);
    for (const value of Object.values(theme.colors)) expect(value).toBe(colour);
    for (const r of theme.rules) expect(r.foreground).toBe(rule);
  });
});
