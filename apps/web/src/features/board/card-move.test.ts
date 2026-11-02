// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Board, ItemCard } from '../../api/types';
import { animateCardMove, type CardMoveDeps, cardTransitionName, movedCards } from './card-move';

afterEach(() => {
  document.body.replaceChildren();
});

const card = (id: string, station: string, over: Partial<ItemCard> = {}) => ({ id, station, ...over }) as ItemCard;
const board = (...stations: [string, ItemCard[]][]): Board =>
  ({ stations: stations.map(([id, items]) => ({ id, items })) }) as unknown as Board;

describe('movedCards', () => {
  it('lists the cards that sit in another column, lane or buffer', () => {
    const before = board(
      ['spec', [card('a', 'spec'), card('b', 'spec'), card('c', 'spec', { lane: 'x' })]],
      ['build', []],
    );
    const after = board(
      ['spec', [card('c', 'spec', { lane: 'y' })]],
      ['build', [card('a', 'build'), card('b', 'build', { in_buffer: true }), card('new', 'build')]],
    );
    expect(movedCards(before, after).sort()).toEqual(['a', 'b', 'c']);
  });

  it('is empty when nothing moved, a card appeared or one left', () => {
    const before = board(['spec', [card('a', 'spec'), card('gone', 'spec')]]);
    expect(movedCards(before, board(['spec', [card('a', 'spec'), card('new', 'spec')]]))).toEqual([]);
  });
});

it('names a card by its ref, as a valid identifier', () => {
  expect(cardTransitionName('sre#12')).toBe('card-sre-12');
});

function element(id: string, ref: string, rect: { left: number; top: number }) {
  const el = document.createElement('article');
  el.dataset.card = id;
  el.dataset.cardRef = ref;
  el.getBoundingClientRect = () => ({ ...rect, width: 100, height: 40 }) as DOMRect;
  document.body.append(el);
  return el;
}

const deps = (over: Partial<CardMoveDeps> = {}): CardMoveDeps => ({
  startViewTransition: undefined,
  reducedMotion: () => false,
  transitionRunning: () => false,
  cards: () => [...document.querySelectorAll<HTMLElement>('[data-card]')],
  ...over,
});

describe('animateCardMove', () => {
  it('runs the change inside a view transition, naming each card, and clears the names after', async () => {
    const el = element('a', 'sre#1', { left: 0, top: 0 });
    let finish: () => void = () => {};
    const finished = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const apply = vi.fn();
    const start = vi.fn((update: () => void) => {
      update();
      return { finished };
    });

    animateCardMove(apply, deps({ startViewTransition: start }));

    expect(start).toHaveBeenCalledOnce();
    expect(apply).toHaveBeenCalledOnce();
    expect(el.style.getPropertyValue('view-transition-name')).toBe('card-sre-1');
    expect(el.style.getPropertyValue('view-transition-class')).toBe('board-card');
    finish();
    await finished;
    await Promise.resolve();
    expect(el.style.getPropertyValue('view-transition-name')).toBe('');
  });

  it('names a ref once, so two cards never share a name', () => {
    const one = element('a', 'sre#1', { left: 0, top: 0 });
    const two = element('b', 'sre#1', { left: 0, top: 0 });
    animateCardMove(() => {}, deps({ startViewTransition: () => ({ finished: new Promise(() => {}) }) }));
    expect([one, two].filter((el) => el.style.getPropertyValue('view-transition-name') !== '')).toHaveLength(1);
  });

  it('leaves the view transition to the route while one runs, and uses FLIP', () => {
    element('a', 'sre#1', { left: 0, top: 0 });
    const start = vi.fn();
    const apply = vi.fn();
    animateCardMove(apply, deps({ startViewTransition: start, transitionRunning: () => true }));
    expect(start).not.toHaveBeenCalled();
    expect(apply).toHaveBeenCalledOnce();
  });

  it('falls back to FLIP: the card starts at its old box and eases to the new one', () => {
    const el = element('a', 'sre#1', { left: 0, top: 0 });
    const apply = vi.fn(() => {
      el.getBoundingClientRect = () => ({ left: 300, top: 50, width: 100, height: 40 }) as DOMRect;
    });

    animateCardMove(apply, deps());

    expect(apply).toHaveBeenCalledOnce();
    expect(el.style.transform).toBe('');
    expect(el.style.transition).toBe('transform var(--duration-fast) var(--ease-in-out)');
  });

  it('leaves a card that did not change place alone in FLIP', () => {
    const el = element('a', 'sre#1', { left: 10, top: 10 });
    animateCardMove(() => {}, deps());
    expect(el.style.transition).toBe('');
  });

  it('animates nothing under reduced motion, in either path', () => {
    const el = element('a', 'sre#1', { left: 0, top: 0 });
    const start = vi.fn();
    const apply = vi.fn(() => {
      el.getBoundingClientRect = () => ({ left: 300, top: 50, width: 100, height: 40 }) as DOMRect;
    });
    animateCardMove(apply, deps({ startViewTransition: start, reducedMotion: () => true }));
    animateCardMove(apply, deps({ reducedMotion: () => true }));
    expect(start).not.toHaveBeenCalled();
    expect(apply).toHaveBeenCalledTimes(2);
    expect(el.style.transition).toBe('');
    expect(el.style.getPropertyValue('view-transition-name')).toBe('');
  });
});
