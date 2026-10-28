// The desktop sidebar's width rules (MASTER.md "The shell"): a 216–400 px sidebar, 256 px by default, and
// the 60 px rail below a snap point of 160 px. Pure, so the drag, the keyboard and the saved value share one
// set of rules (sidebar.tsx, sidebar-resize-handle.tsx, sidebar-choice.ts).

export const sidebarMin = 216;
export const sidebarMax = 400;
export const sidebarDefault = 256;
/** The 60 px icon rail. */
export const sidebarRail = 60;
/** A release below this width folds the sidebar into the rail. */
export const sidebarSnap = 160;
/** How far ArrowLeft and ArrowRight move the width. */
export const sidebarStep = 16;

/** Whether the sidebar is the rail, and the last width it was expanded to (kept while it is the rail). */
export interface SidebarSize {
  collapsed: boolean;
  width: number;
}

/** Clamps a width to 216–400 and rounds it to whole pixels; a value that is not a number is the default. */
export function clampWidth(px: number): number {
  if (!Number.isFinite(px)) return sidebarDefault;
  return Math.min(sidebarMax, Math.max(sidebarMin, Math.round(px)));
}

/**
 * Where releasing the handle at `px` leaves the sidebar: below the snap point it is the rail (the width
 * it had stays), anything else is expanded at the width clamped to 216–400.
 */
export function settle(px: number, width: number): SidebarSize {
  if (Number.isNaN(px)) return { collapsed: false, width: sidebarDefault };
  if (px < sidebarSnap) return { collapsed: true, width };
  return { collapsed: false, width: clampWidth(px) };
}

/** The width the sidebar column is drawn at: the rail's 60 px, or the expanded width. */
export function shownWidth(size: SidebarSize): number {
  return size.collapsed ? sidebarRail : size.width;
}

/**
 * The width the sidebar shows while the handle is dragged: the pointer's, held between the rail and the
 * widest sidebar. The snap is decided on release (settle), so the preview never shows a width the
 * sidebar cannot lay out below 216 px except while the pointer is still.
 */
export function previewWidth(px: number): number {
  if (!Number.isFinite(px)) return sidebarDefault;
  return Math.min(sidebarMax, Math.max(sidebarRail, Math.round(px)));
}

/**
 * The state after a key on the handle, or null when the key does nothing. ArrowRight on the rail expands
 * it at the width it had; ArrowLeft steps by 16 px under the same snap rule as a release; Home collapses;
 * End sets 400 px; Enter resets to 256 px.
 */
export function resizeFromKey(size: SidebarSize, key: string): SidebarSize | null {
  switch (key) {
    case 'ArrowRight':
      return size.collapsed
        ? { collapsed: false, width: size.width }
        : { collapsed: false, width: clampWidth(size.width + sidebarStep) };
    case 'ArrowLeft':
      return size.collapsed ? null : settle(size.width - sidebarStep, size.width);
    case 'Home':
      return { collapsed: true, width: size.width };
    case 'End':
      return { collapsed: false, width: sidebarMax };
    case 'Enter':
      return { collapsed: false, width: sidebarDefault };
    default:
      return null;
  }
}
