import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { themeKey } from './theme';

const script = readFileSync(new URL('../../public/theme.js', import.meta.url), 'utf8');
const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');

interface Boot {
  saved?: string;
  prefersDark?: boolean;
  storageThrows?: boolean;
}

// Runs public/theme.js against fake window and document objects, as the browser runs it.
function boot({ saved, prefersDark = false, storageThrows = false }: Boot) {
  const documentElement = { dataset: {} as Record<string, string>, style: { colorScheme: '' } };
  const win = {
    get localStorage() {
      if (storageThrows) throw new Error('SecurityError');
      return { getItem: (key: string) => (key === themeKey && saved !== undefined ? saved : null) };
    },
    matchMedia: (query: string) => ({ matches: prefersDark && query === '(prefers-color-scheme: dark)' }),
  };
  new Function('window', 'document', script)(win, { documentElement });
  return documentElement;
}

describe('public/theme.js, before first paint', () => {
  it('follows the system when nothing is saved', () => {
    expect(boot({}).dataset.theme).toBe('light');
    expect(boot({ prefersDark: true }).dataset.theme).toBe('dark');
  });

  it('applies a saved Light or Dark whatever the system says', () => {
    expect(boot({ saved: 'dark', prefersDark: false }).dataset.theme).toBe('dark');
    expect(boot({ saved: 'light', prefersDark: true }).dataset.theme).toBe('light');
  });

  it('treats a saved System, or an unknown value, as the system', () => {
    expect(boot({ saved: 'system', prefersDark: true }).dataset.theme).toBe('dark');
    expect(boot({ saved: 'purple', prefersDark: true }).dataset.theme).toBe('dark');
  });

  it('sets color-scheme with the theme, so native controls match', () => {
    const root = boot({ saved: 'dark' });
    expect(root.style.colorScheme).toBe('dark');
  });

  it('survives storage that throws, and follows the system', () => {
    const root = boot({ storageThrows: true, prefersDark: true });
    expect(root.dataset.theme).toBe('dark');
    expect(root.style.colorScheme).toBe('dark');
  });
});

describe('its place in index.html', () => {
  it('is a plain script in <head>, before the app module', () => {
    const head = html.slice(html.indexOf('<head>'), html.indexOf('</head>'));
    const tag = '<script src="/theme.js"></script>';
    expect(head).toContain(tag);
    expect(head.indexOf(tag)).toBeGreaterThan(head.indexOf('<title>'));
    expect(html.indexOf(tag)).toBeLessThan(html.indexOf('type="module"'));
  });

  it('declares both colour schemes and a theme colour for each', () => {
    expect(html).toContain('<meta name="color-scheme" content="light dark" />');
    expect(html).toContain('media="(prefers-color-scheme: light)"');
    expect(html).toContain('media="(prefers-color-scheme: dark)"');
  });
});
