// The handle on the sidebar's right edge (MASTER.md "The shell"): a 6 px hit area centred on the border,
// showing a 2 px cobalt line on hover and while dragging. Dragging follows the pointer by writing the
// shell's --sidebar-width inside requestAnimationFrame, so React does not render on each move; the width
// is saved and the sidebar re-rendered once, on release (settle in shared/sidebar-width.ts decides the snap).
// It is the same handle on the rail, where dragging out past the snap point expands the sidebar again.
import { type KeyboardEvent, type PointerEvent, type RefObject, useEffect, useRef } from 'react';
import {
  previewWidth,
  resizeFromKey,
  type SidebarSize,
  settle,
  shownWidth,
  sidebarDefault,
  sidebarMax,
  sidebarRail,
} from '../shared/sidebar-width';

/** The custom property the sidebar's column width reads (sidebar.tsx). */
export const sidebarWidthVar = '--sidebar-width';

interface Drag {
  pointer: number;
  /** The pointer's x, which is the sidebar's width: the sidebar is pinned to the left edge. */
  x: number;
  /** Whether the pointer moved; a click or a double-click alone changes nothing. */
  moved: boolean;
  frame: number | null;
}

interface SidebarResizeHandleProps {
  size: SidebarSize;
  /** The column whose --sidebar-width the drag writes. */
  column: RefObject<HTMLDivElement | null>;
  onChange: (size: SidebarSize) => void;
}

export function SidebarResizeHandle({ size, column, onChange }: SidebarResizeHandleProps) {
  const handle = useRef<HTMLDivElement>(null);
  const drag = useRef<Drag | null>(null);

  // Unmounting mid-drag must not leave the page without text selection or a pending frame.
  useEffect(
    () => () => {
      if (drag.current?.frame != null) cancelAnimationFrame(drag.current.frame);
      drag.current = null;
      document.body.style.userSelect = '';
    },
    [],
  );

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    // No text selection and no native drag: the handle takes focus itself, and it captures the pointer.
    e.preventDefault();
    e.currentTarget.focus();
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { pointer: e.pointerId, x: e.clientX, moved: false, frame: null };
    document.body.style.userSelect = 'none';
    e.currentTarget.setAttribute('data-dragging', '');
    if (column.current) column.current.style.transition = 'none';
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const state = drag.current;
    if (!state || e.pointerId !== state.pointer) return;
    state.x = e.clientX;
    state.moved = true;
    if (state.frame !== null) return;
    state.frame = requestAnimationFrame(() => {
      state.frame = null;
      column.current?.style.setProperty(sidebarWidthVar, `${previewWidth(state.x)}px`);
    });
  };

  // Ends the drag: the pointer is let go, and a move that happened is settled as the new size.
  const finish = (pointer: number | null) => {
    const state = drag.current;
    if (!state) return;
    drag.current = null;
    if (state.frame !== null) cancelAnimationFrame(state.frame);
    document.body.style.userSelect = '';
    handle.current?.removeAttribute('data-dragging');
    if (column.current) column.current.style.transition = '';
    if (pointer !== null && handle.current?.hasPointerCapture(pointer)) handle.current.releasePointerCapture(pointer);
    if (!state.moved) return;
    const next = settle(state.x, size.width);
    column.current?.style.setProperty(sidebarWidthVar, `${shownWidth(next)}px`);
    onChange(next);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const next = resizeFromKey(size, e.key);
    if (!next) return;
    e.preventDefault();
    onChange(next);
  };

  const reset = () => {
    onChange({ collapsed: false, width: sidebarDefault });
  };

  return (
    // biome-ignore lint/a11y/useSemanticElements: a focusable window splitter, which <hr> does not give with its role and focus
    <div
      ref={handle}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize sidebar"
      aria-valuemin={sidebarRail}
      aria-valuemax={sidebarMax}
      aria-valuenow={shownWidth(size)}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={(e) => finish(e.pointerId)}
      onPointerCancel={(e) => finish(e.pointerId)}
      onLostPointerCapture={() => finish(null)}
      onDoubleClick={reset}
      onKeyDown={onKeyDown}
      className="group absolute inset-y-0 right-0 z-10 w-1.5 translate-x-1/2 cursor-col-resize touch-none"
    >
      <span
        aria-hidden="true"
        className="absolute inset-y-0 left-1/2 w-0.5 -translate-x-1/2 bg-transparent group-hover:bg-run group-data-[dragging]:bg-run"
      />
    </div>
  );
}
