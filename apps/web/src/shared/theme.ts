// The colour theme: System follows the device, Light and Dark stay as chosen. The choice is kept
// per browser under one key and applied by public/theme.js before first paint; here it is read,
// written and followed while the app runs. The tokens for each theme are in styles.css.
import { useEffect, useSyncExternalStore } from 'react';

export type ThemeChoice = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

/** The localStorage key of the choice. public/theme.js reads the same key. */
export const themeKey = 'goal-theme';

const darkQuery = '(prefers-color-scheme: dark)';
const choices: readonly string[] = ['system', 'light', 'dark'];

export interface ThemeStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/** The saved choice; System when none is saved, the saved value is not one of the three, or storage throws. */
export function readChoice(storage: ThemeStorage | undefined): ThemeChoice {
  try {
    const saved = storage?.getItem(themeKey);
    return saved !== undefined && saved !== null && choices.includes(saved) ? (saved as ThemeChoice) : 'system';
  } catch {
    return 'system';
  }
}

/** Keeps the choice for the next visit; a refusing storage keeps it for this page only. */
export function writeChoice(storage: ThemeStorage | undefined, choice: ThemeChoice): void {
  try {
    storage?.setItem(themeKey, choice);
  } catch {
    // Storage refused (private mode, blocked cookies): the choice applies to this page.
  }
}

export function resolveTheme(choice: ThemeChoice, prefersDark: boolean): ResolvedTheme {
  if (choice === 'system') return prefersDark ? 'dark' : 'light';
  return choice;
}

/** Writes the resolved theme to <html>: the CSS tokens switch on data-theme, native controls on color-scheme. */
export function applyTheme(root: Pick<HTMLElement, 'dataset' | 'style'>, theme: ResolvedTheme): void {
  root.dataset.theme = theme;
  root.style.colorScheme = theme;
}

/** Calls onChange when the device's preference changes; returns the function that stops it. */
export function watchSystemTheme(
  media: Pick<MediaQueryList, 'matches' | 'addEventListener' | 'removeEventListener'> | undefined,
  onChange: (prefersDark: boolean) => void,
): () => void {
  if (!media) return () => {};
  const listener = (event: MediaQueryListEvent) => onChange(event.matches);
  media.addEventListener('change', listener);
  return () => media.removeEventListener('change', listener);
}

// The choice is shared by every component that shows or follows it (the account menu, the toaster).
const listeners = new Set<() => void>();
let choice: ThemeChoice | undefined;

function browserStorage(): ThemeStorage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

function systemMedia(): MediaQueryList | undefined {
  return typeof window === 'undefined' ? undefined : window.matchMedia(darkQuery);
}

function getChoice(): ThemeChoice {
  choice ??= readChoice(browserStorage());
  return choice;
}

function subscribeChoice(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function subscribeSystem(listener: () => void): () => void {
  return watchSystemTheme(systemMedia(), listener);
}

function systemPrefersDark(): boolean {
  return systemMedia()?.matches ?? false;
}

function setThemeChoice(next: ThemeChoice): void {
  choice = next;
  writeChoice(browserStorage(), next);
  for (const listener of listeners) listener();
}

/** The choice, the theme it resolves to now, and the setter. Applies the resolved theme to <html>. */
export function useTheme(): { choice: ThemeChoice; resolved: ResolvedTheme; setChoice: (next: ThemeChoice) => void } {
  const current = useSyncExternalStore(subscribeChoice, getChoice, (): ThemeChoice => 'system');
  const prefersDark = useSyncExternalStore(subscribeSystem, systemPrefersDark, () => false);
  const resolved = resolveTheme(current, prefersDark);
  useEffect(() => applyTheme(document.documentElement, resolved), [resolved]);
  return { choice: current, resolved, setChoice: setThemeChoice };
}
