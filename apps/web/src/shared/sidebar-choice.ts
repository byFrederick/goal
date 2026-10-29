// The sidebar's collapsed or expanded choice and its width, kept per browser under two keys (MASTER.md
// "The shell"). They are read in the first render, so the sidebar never shows expanded and then folds
// to the rail or changes width. Storage that refuses (blocked site data) falls back to expanded at the
// default width, and the choice then lasts for this page only.
import { useCallback, useState } from 'react';
import { clampWidth, type SidebarSize, sidebarDefault, sidebarMax, sidebarMin } from './sidebar-width';

/** The localStorage key of the choice. */
export const sidebarKey = 'goal.sidebar';
/** The localStorage key of the expanded width, in whole pixels. */
export const sidebarWidthKey = 'goal.sidebar.width';

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

/**
 * The expanded width the person chose. A missing, non-numeric, non-integer or out-of-range value reads as
 * the default (256 px), so a corrupt value never sizes the sidebar.
 */
export function readWidth(storage: SidebarStorage | undefined): number {
  try {
    const raw = storage?.getItem(sidebarWidthKey);
    if (raw === null || raw === undefined) return sidebarDefault;
    const width = Number(raw);
    return Number.isInteger(width) && width >= sidebarMin && width <= sidebarMax ? width : sidebarDefault;
  } catch {
    return sidebarDefault;
  }
}

/** Keeps the width for the next visit; a refusing storage keeps it for this page only. */
export function writeWidth(storage: SidebarStorage | undefined, width: number): void {
  try {
    storage?.setItem(sidebarWidthKey, String(clampWidth(width)));
  } catch {
    // Storage refused (private mode, blocked cookies): the width applies to this page.
  }
}

/** The saved sidebar: the collapsed choice and the width, read together. */
export function readSize(storage: SidebarStorage | undefined): SidebarSize {
  return { collapsed: readCollapsed(storage), width: readWidth(storage) };
}

function browserStorage(): SidebarStorage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

/** The sidebar's size, read synchronously on the first render and written on every change. */
export function useSidebarSize(): [SidebarSize, (size: SidebarSize) => void] {
  const [size, setSize] = useState(() => readSize(browserStorage()));
  const change = useCallback((next: SidebarSize) => {
    const storage = browserStorage();
    writeCollapsed(storage, next.collapsed);
    writeWidth(storage, next.width);
    setSize(next);
  }, []);
  return [size, change];
}
