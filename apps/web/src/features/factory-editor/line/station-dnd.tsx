// Dragging stations into a new order (D §10.6, dnd-kit as the board uses it). The row number is
// the handle: by pointer (6 px before it starts) or by keyboard (Space picks the row up, the up
// and down arrows carry it a row at a time, Space drops, Escape cancels; screen readers hear
// where it is). A drop calls the same move as the ↑ ↓ buttons and Alt+Arrow, which stay the
// alternatives to dragging (WCAG 2.2 2.5.7). Done is never dragged and nothing drops on it.
import {
  type Announcements,
  type CollisionDetection,
  DndContext,
  DragOverlay,
  type KeyboardCoordinateGetter,
  KeyboardSensor,
  PointerSensor,
  pointerWithin,
  rectIntersection,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
} from '@dnd-kit/core';
import { type ReactNode, useCallback, useId, useState } from 'react';
import type { ModelStation } from '../../../shared/definition/model';
import { stationLabel } from './labels';
import { useLineEditor } from './line-editor';
import { dropTarget } from './reorder';

/** The arrow keys carry a picked-up row to the row above or below, not 25 px at a time. */
const rowCoordinates: KeyboardCoordinateGetter = (event, { context }) => {
  const { collisionRect, droppableRects, droppableContainers } = context;
  if (!collisionRect || (event.code !== 'ArrowDown' && event.code !== 'ArrowUp')) return undefined;
  event.preventDefault();
  const center = collisionRect.top + collisionRect.height / 2;
  const rows = droppableContainers
    .getEnabled()
    .map((c) => droppableRects.get(c.id))
    .filter((r) => r !== undefined)
    .sort((a, b) => a.top - b.top);
  const next =
    event.code === 'ArrowDown' ? rows.find((r) => r.top > center) : rows.findLast((r) => r.top + r.height < center);
  if (!next) return undefined;
  return { x: collisionRect.left, y: next.top + next.height / 2 - collisionRect.height / 2 };
};

/** Where the pointer is, else the row the carried station overlaps most (the keyboard has no pointer). */
const collision: CollisionDetection = (args) => {
  const within = pointerWithin(args);
  return within.length > 0 ? within : rectIntersection(args);
};

export function StationDnd({ children }: { children: ReactNode }) {
  const editor = useLineEditor();
  const { stations, move } = editor;
  const instructions = useId();
  const [active, setActive] = useState<ModelStation | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: rowCoordinates,
      // A row above or below the viewport scrolls into view at once, so the next key and the drop
      // see where the row went (a smooth scroll is still running when they come).
      scrollBehavior: 'auto',
      keyboardCodes: { start: ['Space', 'Enter'], cancel: ['Escape'], end: ['Space', 'Enter'] },
    }),
  );

  const find = useCallback((id: string | number) => stations.find((s) => s.id === id), [stations]);
  const name = (id: string | number | undefined) => {
    const s = id === undefined ? undefined : find(id);
    return s ? stationLabel(s) : 'the station';
  };
  const position = (id: string | number) => stations.findIndex((s) => s.id === id) + 1;
  const announcements: Announcements = {
    onDragStart: ({ active: a }) =>
      `Picked up ${name(a.id)}. Use the up and down arrow keys to choose its place, Space to drop it, Escape to cancel.`,
    onDragOver: ({ active: a, over }) =>
      over
        ? `${name(a.id)} is over ${name(over.id)}, position ${position(over.id)} of ${stations.length}.`
        : `${name(a.id)} is over no station.`,
    // The section's own live region says where the station went.
    onDragEnd: ({ active: a, over }) =>
      over && dropTarget(stations, String(a.id), String(over.id)) !== undefined
        ? undefined
        : `${name(a.id)} stays at position ${position(a.id)}.`,
    onDragCancel: ({ active: a }) => `Moving ${name(a.id)} was cancelled.`,
  };

  return (
    <DndContext
      id={instructions}
      sensors={sensors}
      collisionDetection={collision}
      accessibility={{
        announcements,
        screenReaderInstructions: {
          draggable:
            'Press Space to pick the station up, the up and down arrow keys to carry it, and Space to drop it. Alt with the up or down arrow moves it one place from anywhere in its row.',
        },
      }}
      onDragStart={({ active: a }) => setActive(find(a.id) ?? null)}
      onDragCancel={() => setActive(null)}
      onDragEnd={({ active: a, over }) => {
        setActive(null);
        if (!over) return;
        const to = dropTarget(stations, String(a.id), String(over.id));
        if (to !== undefined) move(String(a.id), to);
      }}
    >
      {children}
      <DragOverlay dropAnimation={null}>
        {active ? (
          <div className="flex w-[240px] items-center gap-3 rounded-control border border-muted bg-card px-3 py-2.5 shadow-pop">
            <span className="flex size-7 items-center justify-center rounded-chip bg-shell text-[13px] font-semibold tabular-nums">
              {position(active.id)}
            </span>
            <span className="truncate text-sm font-medium">{stationLabel(active)}</span>
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

/** A row's drag: its node (measured and dropped on), the handle's props, and whether it is over or moving. */
export function useStationDrag(station: ModelStation) {
  const done = station.kind === 'done';
  const drag = useDraggable({ id: station.id, disabled: done });
  const drop = useDroppable({ id: station.id, disabled: done });
  const setNodeRef = useCallback(
    (el: HTMLElement | null) => {
      drag.setNodeRef(el);
      drop.setNodeRef(el);
    },
    [drag.setNodeRef, drop.setNodeRef],
  );
  return {
    setNodeRef,
    handle: done
      ? null
      : {
          ref: drag.setActivatorNodeRef,
          ...drag.attributes,
          ...drag.listeners,
        },
    dragging: drag.isDragging,
    over: drop.isOver && drop.active?.id !== station.id,
  };
}
