// The sidebar's collapsed or expanded choice, kept per browser under one key (MASTER.md "Collapsed
// rail"). It is read in the first render, so the sidebar never shows expanded and then folds to the
// rail. Storage that refuses (blocked site data) falls back to expanded, and the choice then lasts for
// this page only.
import { useCallback, useState } from 'react';

/** The localStorage key of the choice. */
export const sidebarKey = 'goal.sidebar';

export interface SidebarStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/** Whether the saved choice is the rail; expanded when none is saved or storage throws. */
export function readCollapsed(storage: SidebarStorage | undefined): boolean {
  try {
    return storage?.getItem(sidebarKey) === 'collapsed';
  } catch {
    return false;
  }
}

/** Keeps the choice for the next visit; a refusing storage keeps it for this page only. */
export function writeCollapsed(storage: SidebarStorage | undefined, collapsed: boolean): void {
  try {
    storage?.setItem(sidebarKey, collapsed ? 'collapsed' : 'expanded');
  } catch {
    // Storage refused (private mode, blocked cookies): the choice applies to this page.
  }
}

function browserStorage(): SidebarStorage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

/** The collapsed choice, read synchronously on the first render and written on every change. */
export function useSidebarCollapsed(): [boolean, (collapsed: boolean) => void] {
  const [collapsed, setCollapsed] = useState(() => readCollapsed(browserStorage()));
  const change = useCallback((next: boolean) => {
    writeCollapsed(browserStorage(), next);
    setCollapsed(next);
  }, []);
  return [collapsed, change];
}
