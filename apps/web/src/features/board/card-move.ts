// How a card travels when it lands in another column or lane (MOTION.md, "Board card moves"): a live
// update or a drop changes the board, and the card moves from its old box to its new one while the other
// cards reflow. View Transitions do it where the browser has them (each card has the name card-<ref>);
// elsewhere FLIP does: measure, change, and ease each card from its old box. Under reduced motion the
// board just changes. The browser pieces are arguments so the tests can stand in for them.
import { useEffect, useState } from 'react';
import { flushSync } from 'react-dom';
import type { Board } from '../../api/types';

/** The browser's parts of a card move; `browserDeps` is the real set. */
export interface CardMoveDeps {
  startViewTransition: ((update: () => void) => { finished: Promise<unknown> }) | undefined;
  reducedMotion: () => boolean;
  /** Another view transition (a route change) is running: the card move waits its turn and just changes. */
  transitionRunning: () => boolean;
  /** The cards on screen, those a person can see. */
  cards: () => HTMLElement[];
}

export function browserDeps(): CardMoveDeps {
  const doc = document as Document & {
    startViewTransition?: (update: () => void) => { finished: Promise<unknown> };
    activeViewTransition?: unknown;
  };
  return {
    startViewTransition: doc.startViewTransition?.bind(doc),
    reducedMotion: () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false,
    transitionRunning: () => Boolean(doc.activeViewTransition),
    cards: () =>
      [...document.querySelectorAll<HTMLElement>('[data-card]')].filter((el) => el.getClientRects().length > 0),
  };
}

/** The place of every card: its station, lane and buffer, so a change of any of them is a move. */
function placeOf(board: Board): Map<string, string> {
  const places = new Map<string, string>();
  for (const s of board.stations) {
    for (const it of s.items) places.set(it.id, `${s.id}|${it.lane ?? ''}|${it.in_buffer ? 'buffer' : ''}`);
  }
  return places;
}

/** The ids of the cards on both boards that sit in another column, lane or buffer on the second. */
export function movedCards(before: Board, after: Board): string[] {
  const was = placeOf(before);
  return [...placeOf(after)].filter(([id, place]) => was.has(id) && was.get(id) !== place).map(([id]) => id);
}

/** A view-transition-name for a card: an identifier made of the ref's letters, digits and dashes. */
export const cardTransitionName = (ref: string) => `card-${ref.replace(/[^a-zA-Z0-9_-]/g, '-')}`;

/**
 * Runs `apply` (which must change the board synchronously) and animates the cards from where they were to
 * where they are. It always calls `apply` exactly once.
 */
export function animateCardMove(apply: () => void, deps: CardMoveDeps = browserDeps()): void {
  const cards = deps.cards();
  if (deps.reducedMotion() || cards.length === 0) {
    apply();
    return;
  }

  if (deps.startViewTransition && !deps.transitionRunning()) {
    const named = nameCards(cards);
    const transition = deps.startViewTransition(apply);
    const clear = () => {
      for (const el of named) {
        el.style.removeProperty('view-transition-name');
        el.style.removeProperty('view-transition-class');
      }
    };
    transition.finished.then(clear, clear);
    return;
  }
  flip(cards, apply, deps);
}

/** Gives each visible card its name (once: a name two cards share makes the browser skip the transition). */
function nameCards(cards: HTMLElement[]): HTMLElement[] {
  const taken = new Set<string>();
  const named: HTMLElement[] = [];
  for (const el of cards) {
    const ref = el.dataset.cardRef;
    if (!ref) continue;
    const name = cardTransitionName(ref);
    if (taken.has(name)) continue;
    taken.add(name);
    el.style.setProperty('view-transition-name', name);
    el.style.setProperty('view-transition-class', 'board-card');
    named.push(el);
  }
  return named;
}

/** FLIP: each card that changed place starts at its old box and eases to the new one. */
function flip(cards: HTMLElement[], apply: () => void, deps: CardMoveDeps): void {
  const first = new Map(cards.map((el) => [el.dataset.card, el.getBoundingClientRect()]));
  apply();
  const moving: HTMLElement[] = [];
  for (const el of deps.cards()) {
    const from = first.get(el.dataset.card);
    if (!from) continue;
    const to = el.getBoundingClientRect();
    const dx = from.left - to.left;
    const dy = from.top - to.top;
    if (Math.abs(dx) < 1 && Math.abs(dy) < 1) continue;
    el.style.transition = 'none';
    el.style.transform = `translate(${dx}px, ${dy}px)`;
    moving.push(el);
  }
  if (moving.length === 0) return;
  // Reading the layout makes the browser take the start position before the transition is set.
  void moving[0]?.offsetWidth;
  for (const el of moving) {
    el.style.transition = 'transform var(--duration-fast) var(--ease-in-out)';
    el.style.transform = '';
    el.addEventListener('transitionend', () => el.style.removeProperty('transition'), { once: true });
  }
}

/** A CSS time token ("250ms") in milliseconds, for the drag's settle; 0 under reduced motion. */
export function tokenMs(name: string, deps: Pick<CardMoveDeps, 'reducedMotion'> = browserDeps()): number {
  if (deps.reducedMotion()) return 0;
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const ms = Number.parseFloat(value);
  if (Number.isNaN(ms)) return 0;
  return value.endsWith('ms') ? ms : ms * 1000;
}

/**
 * The board to draw. A new board in which a card sits in another column or lane is drawn inside
 * `animateCardMove`, so the card travels; any other change is drawn at once.
 */
export function useCardMoves(board: Board): Board {
  const [shown, setShown] = useState(board);
  const moved = shown !== board && movedCards(shown, board).length > 0;
  useEffect(() => {
    if (shown === board) return;
    if (!moved) {
      setShown(board);
      return;
    }
    // Out of the effect, where flushSync may run.
    queueMicrotask(() => animateCardMove(() => flushSync(() => setShown(board))));
  }, [board, shown, moved]);
  return moved ? shown : board;
}
