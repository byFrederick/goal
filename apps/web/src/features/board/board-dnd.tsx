// Dragging cards between the board's columns (dnd-kit). A drop never moves an item by itself: it
// opens the Move menu with that station chosen, so an upstream move still asks why and every move
// is confirmed. With the keyboard, Space picks the focused card up, the left and right arrows carry
// it from column to column and Space drops it; M opens the Move menu directly (WCAG 2.2 2.5.7).
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
import { createContext, type ReactNode, useContext, useId, useState } from 'react';
import type { Board, BoardStation, ItemCard } from '../../api/types';
import { humanize } from '../../shared/format';
import { cn } from '../../ui/cn';
import type { CardDrag } from './board-card';

/** The arrow keys carry a picked-up card to the column beside it, not 25 px at a time. */
const columnCoordinates: KeyboardCoordinateGetter = (event, { context }) => {
  const { collisionRect, droppableRects, droppableContainers } = context;
  if (!collisionRect || (event.code !== 'ArrowRight' && event.code !== 'ArrowLeft')) return undefined;
  event.preventDefault();

  const center = collisionRect.left + collisionRect.width / 2;
  const columns = droppableContainers
    .getEnabled()
    .map((c) => droppableRects.get(c.id))
    .filter((r) => r !== undefined)
    .sort((a, b) => a.left - b.left);
  const next =
    event.code === 'ArrowRight'
      ? columns.find((r) => r.left > center)
      : columns.findLast((r) => r.left + r.width < center);
  if (!next) return undefined;

  return { x: next.left + next.width / 2 - collisionRect.width / 2, y: next.top + 48 };
};

/** Where the pointer is, else what the card overlaps (the keyboard carries no pointer). */
const collision: CollisionDetection = (args) => {
  const within = pointerWithin(args);
  return within.length > 0 ? within : rectIntersection(args);
};

function findCard(board: Board, id: string | number): ItemCard | undefined {
  return board.stations.flatMap((s) => s.items).find((it) => it.id === id);
}

/** The drag and drop of one board: a drop on another column calls onDrop with that station. */
export function CardDnd({
  board,
  onDrop,
  children,
}: {
  board: Board;
  onDrop: (item: ItemCard, to: string) => void;
  children: ReactNode;
}) {
  const instructions = useId();
  const [active, setActive] = useState<ItemCard | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    // Enter stays the link's: it opens the item.
    useSensor(KeyboardSensor, {
      coordinateGetter: columnCoordinates,
      // A column beside the view scrolls in at once, so the next arrow and the drop see where the
      // columns went (a smooth scroll is still running when they come), as the Line editor does.
      scrollBehavior: 'auto',
      keyboardCodes: { start: ['Space'], cancel: ['Escape'], end: ['Space', 'Enter'] },
    }),
  );

  const refOf = (id: string | number) => findCard(board, id)?.ref ?? 'The card';
  const station = (id: string | number | undefined) => humanize(String(id ?? ''));
  const announcements: Announcements = {
    onDragStart: ({ active: a }) =>
      `Picked up ${refOf(a.id)}. Use the left and right arrow keys to choose a station, Space to drop it, Escape to cancel.`,
    onDragOver: ({ active: a, over }) =>
      over ? `${refOf(a.id)} is over ${station(over.id)}.` : `${refOf(a.id)} is over no station.`,
    onDragEnd: ({ active: a, over }) =>
      over ? `${refOf(a.id)} dropped on ${station(over.id)}. Confirm the move.` : `${refOf(a.id)} dropped.`,
    onDragCancel: ({ active: a }) => `Moving ${refOf(a.id)} was cancelled.`,
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
            'Press M to choose the station this card goes to, or Space to pick it up and the arrow keys to carry it.',
        },
      }}
      onDragStart={({ active: a }) => setActive(findCard(board, a.id) ?? null)}
      onDragCancel={() => setActive(null)}
      onDragEnd={({ active: a, over }) => {
        setActive(null);
        const card = findCard(board, a.id);
        if (card && over && over.id !== card.station) onDrop(card, String(over.id));
      }}
    >
      <InstructionsId.Provider value={instructions}>{children}</InstructionsId.Provider>
      <DragOverlay dropAnimation={null}>
        {active ? (
          <div className="flex w-[200px] flex-col gap-1 rounded-control border border-muted bg-card p-3">
            <span className="text-xs text-muted tabular-nums">{active.ref}</span>
            <span className="text-sm leading-snug font-medium">{active.title}</span>
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

// dnd-kit describes draggables with its own instructions element; the cards point at it.
const InstructionsId = createContext<string | undefined>(undefined);

/** A column cards are dropped on; done stations take none (work finishes by a decision). */
export function DropColumn({
  station,
  className,
  children,
}: {
  station: BoardStation;
  className?: string;
  children: ReactNode;
}) {
  const { setNodeRef, isOver, active } = useDroppable({ id: station.id, disabled: station.kind === 'done' });
  const from = active?.data.current?.station as string | undefined;
  return (
    <section
      ref={setNodeRef}
      aria-label={humanize(station.id)}
      className={cn(className, isOver && from !== station.id && 'border-run shadow-[inset_0_0_0_1px_var(--color-run)]')}
    >
      {children}
    </section>
  );
}

/** One draggable card: the article takes the pointer, the focused link inside it the keys. */
export function DraggableCard({ item, children }: { item: ItemCard; children: (drag: CardDrag) => ReactNode }) {
  const { setNodeRef, listeners, isDragging } = useDraggable({ id: item.id, data: { station: item.station } });
  const describedBy = useContext(InstructionsId);
  return children({
    ref: setNodeRef,
    onPointerDown: listeners?.onPointerDown as CardDrag['onPointerDown'],
    onKeyDown: listeners?.onKeyDown as CardDrag['onKeyDown'],
    dragging: isDragging,
    describedBy: describedBy ? `DndDescribedBy-${describedBy}` : undefined,
  });
}
