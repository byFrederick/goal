// What step 4's cards add to the wizard's line logic: the "Add a station here" control between the cards, the
// move and Remove buttons of a card, and the move's FLIP (MOTION.md, "A station card moves earlier or later").
// The edits are the wizard's (`addStation`, `moveStation`, `removeStation` through `useLineEditor`).
import { ArrowLeft, ArrowRight, Plus, X } from 'lucide-react';
import { useId, useLayoutEffect, useRef } from 'react';
import type { ModelStation } from '../../shared/definition/model';
import { cn } from '../../ui/cn';
import { type Direction, stepBlocked } from '../factory-editor/line/reorder';

const iconButton =
  'inline-flex size-11 shrink-0 items-center justify-center rounded-control border border-line bg-card text-ink transition-colors duration-(--duration-quick) hover:bg-shell aria-disabled:cursor-not-allowed aria-disabled:opacity-45 aria-disabled:hover:bg-card';

/** The control between two cards: a 24 px mark inside a 44 px hit target. */
export function AddHere({ onAdd }: { onAdd: () => void }) {
  return (
    <button
      type="button"
      aria-label="Add a station here"
      onClick={onAdd}
      className="group -mx-2.5 grid size-11 shrink-0 place-items-center rounded-control focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-run"
    >
      <span className="grid size-6 place-items-center rounded-control border border-dashed border-line bg-card text-muted transition-colors duration-(--duration-quick) group-hover:bg-shell group-hover:text-ink">
        <Plus size={12} strokeWidth={2} aria-hidden="true" />
      </span>
    </button>
  );
}

function MoveButton({
  dir,
  label,
  blocked,
  onStep,
}: {
  dir: Direction;
  label: string;
  blocked: string | undefined;
  onStep: (dir: Direction, pointer: boolean) => void;
}) {
  const id = useId();
  const Icon = dir === -1 ? ArrowLeft : ArrowRight;
  return (
    <>
      <button
        type="button"
        aria-label={`Move ${label} ${dir === -1 ? 'earlier' : 'later'}`}
        aria-disabled={blocked ? true : undefined}
        aria-describedby={blocked ? id : undefined}
        title={blocked}
        onClick={(e) => {
          if (!blocked) onStep(dir, e.detail > 0);
        }}
        className={iconButton}
      >
        <Icon size={16} strokeWidth={1.5} aria-hidden="true" />
      </button>
      {blocked ? (
        <span id={id} className="sr-only">
          {blocked}
        </span>
      ) : null}
    </>
  );
}

/** Move earlier, move later and Remove for one card. Done has none: it stays last. */
export function CardControls({
  station,
  stations,
  label,
  onStep,
  onRemove,
}: {
  station: ModelStation;
  stations: readonly ModelStation[];
  label: string;
  onStep: (dir: Direction, pointer: boolean) => void;
  onRemove: () => void;
}) {
  if (station.kind === 'done') return null;
  return (
    <div className="mt-auto flex items-center gap-2 border-t border-line pt-3">
      <MoveButton dir={-1} label={label} blocked={stepBlocked(stations, station.id, -1)} onStep={onStep} />
      <MoveButton dir={1} label={label} blocked={stepBlocked(stations, station.id, 1)} onStep={onStep} />
      <button
        type="button"
        aria-label={`Remove ${label}`}
        onClick={onRemove}
        className={cn(iconButton, 'ml-auto hover:text-rework-ink')}
      >
        <X size={16} strokeWidth={1.5} aria-hidden="true" />
      </button>
    </div>
  );
}

/**
 * A card that moves travels from its old place to its new one (FLIP) after a pointer press. A keyboard press
 * (`detail` 0) and `prefers-reduced-motion` change the order at once. `mark` is called as the move starts.
 */
export function useCardFlip() {
  const nodes = useRef(new Map<string, HTMLElement>());
  const before = useRef<Map<string, number> | null>(null);

  const mark = () => {
    const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;
    if (reduced) return;
    before.current = new Map([...nodes.current].map(([key, el]) => [key, el.getBoundingClientRect().left]));
  };

  // After every commit: a card whose place changed starts at its old place and eases to the new one.
  useLayoutEffect(() => {
    const was = before.current;
    if (!was) return;
    before.current = null;
    for (const [key, el] of nodes.current) {
      const from = was.get(key);
      if (from === undefined) continue;
      const dx = from - el.getBoundingClientRect().left;
      if (dx === 0) continue;
      el.style.transition = 'none';
      el.style.transform = `translateX(${dx}px)`;
      void el.offsetWidth;
      el.style.transition = 'transform var(--duration-fast) var(--ease-in-out)';
      el.style.transform = '';
      el.addEventListener('transitionend', () => (el.style.transition = ''), { once: true });
    }
  });

  const ref = (key: string) => (el: HTMLElement | null) => {
    if (el) nodes.current.set(key, el);
    else nodes.current.delete(key);
  };
  return { mark, ref };
}
