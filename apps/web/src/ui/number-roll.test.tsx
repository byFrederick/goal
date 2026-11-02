// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { NumberRoll } from './number-roll';

afterEach(cleanup);

// The animated characters: the visible copy is aria-hidden, so its spans are the characters.
const characters = (container: HTMLElement) => Array.from(container.querySelectorAll('[aria-hidden="true"] > span'));
const popping = (container: HTMLElement) =>
  characters(container).map((el) => el.classList.contains('motion-safe:animate-number-pop-in'));

describe('NumberRoll', () => {
  it('shows the value to a screen reader, and draws the first value without animation', () => {
    const { container } = render(<NumberRoll value="1,299" />);
    expect(screen.getByText('1,299')).toBeTruthy();
    expect(popping(container)).toEqual([false, false, false, false, false]);
  });

  it('renders the new value after a change', () => {
    const { container, rerender } = render(<NumberRoll value="1,299" />);
    rerender(<NumberRoll value="1,305" />);
    expect(screen.getByText('1,305')).toBeTruthy();
    expect(screen.queryByText('1,299')).toBeNull();
    expect(container.textContent).toContain('1,305');
  });

  it('pops in only the digits that changed, and leaves the rest still', () => {
    const { container, rerender } = render(<NumberRoll value="1,299" />);
    rerender(<NumberRoll value="1,305" />);
    // "1," stays; "305" pops in, each one step later than the last.
    expect(popping(container)).toEqual([false, false, true, true, true]);
    const delays = characters(container)
      .filter((el) => el.classList.contains('motion-safe:animate-number-pop-in'))
      .map((el) => (el as HTMLElement).style.animationDelay);
    expect(delays).toEqual([
      'calc(var(--duration-stagger) * 0)',
      'calc(var(--duration-stagger) * 1)',
      'calc(var(--duration-stagger) * 2)',
    ]);
  });

  it('compares digits by place, so a number that shrinks keeps the digits that did not change still', () => {
    const { container, rerender } = render(<NumberRoll value="1234" />);
    rerender(<NumberRoll value="234" />);
    expect(popping(container)).toEqual([false, false, false]);
  });

  it('falls back to a fade under reduced motion', () => {
    const { container, rerender } = render(<NumberRoll value={7} />);
    rerender(<NumberRoll value={8} />);
    const changed = characters(container).find((el) => el.textContent === '8');
    expect(Array.from(changed?.classList ?? [])).toEqual(expect.arrayContaining(['motion-reduce:animate-fade-in']));
  });
});
