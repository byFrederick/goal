import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import {
  applyTheme,
  readChoice,
  resolveTheme,
  type ThemeStorage,
  themeKey,
  watchSystemTheme,
  writeChoice,
} from './theme';

function memoryStorage(initial: Record<string, string> = {}): ThemeStorage & { values: Record<string, string> } {
  const values = { ...initial };
  return {
    values,
    getItem: (key) => values[key] ?? null,
    setItem: (key, value) => {
      values[key] = value;
    },
  };
}

describe('the saved choice', () => {
  it('is System when nothing is saved', () => {
    expect(readChoice(memoryStorage())).toBe('system');
  });

  it('falls back to System for a value that is not one of the three', () => {
    expect(readChoice(memoryStorage({ [themeKey]: 'blue' }))).toBe('system');
    expect(readChoice(memoryStorage({ [themeKey]: '' }))).toBe('system');
  });

  it('is System when storage throws or is missing', () => {
    const refusing: ThemeStorage = {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: () => {
        throw new Error('denied');
      },
    };
    expect(readChoice(refusing)).toBe('system');
    expect(readChoice(undefined)).toBe('system');
  });

  it('persists under one key and reads back what was written', () => {
    const storage = memoryStorage();
    writeChoice(storage, 'dark');
    expect(storage.values).toEqual({ [themeKey]: 'dark' });
    expect(readChoice(storage)).toBe('dark');
    writeChoice(storage, 'light');
    expect(readChoice(storage)).toBe('light');
  });

  it('does not throw when storage refuses the write', () => {
    const refusing: ThemeStorage = {
      getItem: () => null,
      setItem: () => {
        throw new Error('quota');
      },
    };
    expect(() => writeChoice(refusing, 'dark')).not.toThrow();
  });
});

describe('resolving the theme', () => {
  it('follows the system while the choice is System', () => {
    expect(resolveTheme('system', true)).toBe('dark');
    expect(resolveTheme('system', false)).toBe('light');
  });

  it('ignores the system once the choice is Light or Dark', () => {
    expect(resolveTheme('light', true)).toBe('light');
    expect(resolveTheme('dark', false)).toBe('dark');
  });
});

describe('following the system live', () => {
  it('reports each change of the preference until stopped', () => {
    const listeners: Array<(event: { matches: boolean }) => void> = [];
    const media = {
      matches: false,
      addEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => listeners.push(listener),
      removeEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => {
        listeners.splice(listeners.indexOf(listener), 1);
      },
    };
    const seen: boolean[] = [];
    const stop = watchSystemTheme(media as unknown as MediaQueryList, (prefersDark) => seen.push(prefersDark));

    for (const listener of listeners) listener({ matches: true });
    for (const listener of listeners) listener({ matches: false });
    stop();
    expect(listeners).toHaveLength(0);
    expect(seen).toEqual([true, false]);
  });

  it('does nothing without a media query (no window)', () => {
    expect(() => watchSystemTheme(undefined, () => {})()).not.toThrow();
  });
});

describe('applying the theme to the page', () => {
  it('sets data-theme and color-scheme on the root', () => {
    const root = { dataset: {} as Record<string, string>, style: { colorScheme: '' } };
    applyTheme(root as unknown as HTMLElement, 'dark');
    expect(root.dataset.theme).toBe('dark');
    expect(root.style.colorScheme).toBe('dark');
  });
});

describe('the token overrides in styles.css', () => {
  const css = readFileSync(new URL('../styles.css', import.meta.url), 'utf8');
  const block = (start: string) => css.slice(css.indexOf(start), css.indexOf('}', css.indexOf(start)));
  const tokens = (text: string) => [...text.matchAll(/(--color-[\w-]+):/g)].map((m) => m[1] ?? '').sort();

  it('defines the dark values of every themed color token, in both places', () => {
    // transparent and currentColor do not change with the theme.
    const fixed = ['--color-current', '--color-transparent'];
    const light = tokens(css.slice(css.indexOf('@theme {'), css.indexOf('--radius-andon'))).filter(
      (t) => !fixed.includes(t),
    );
    const dark = tokens(block(':root[data-theme="dark"]'));
    const system = tokens(block(':root:not([data-theme="light"])'));
    expect(dark).toEqual(light);
    expect(system).toEqual(light);
  });
});
