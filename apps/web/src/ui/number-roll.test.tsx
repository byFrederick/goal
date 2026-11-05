// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NumberRoll, QUIET_MS } from './number-roll';

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

// The animated characters: the visible copy is aria-hidden, so its spans are the characters.
const characters = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[aria-hidden="true"] > span > span'));
const popping = (container: HTMLElement) =>
  characters(container).map((el) => el.classList.contains('motion-safe:animate-number-pop-in'));

describe('NumberRoll', () => {
  it('keeps a space between characters visible', () => {
    const { container } = render(<NumberRoll value="2 loops" />);
    const space = characters(container)[1];
    expect(space?.textContent).toBe(' ');
    expect(space?.classList.contains('whitespace-pre')).toBe(true);
  });

  it('groups a multi-word value into one nowrap group per word and keeps the full text for screen readers', () => {
    const { container } = render(<NumberRoll value="About 16 h 43 min" />);
    const groups = container.querySelectorAll('[aria-hidden="true"] > span');
    expect(groups).toHaveLength(5);
    for (const group of groups) expect(group.classList.contains('whitespace-nowrap')).toBe(true);
    expect(container.querySelector('[aria-hidden="true"]')?.classList.contains('flex-wrap')).toBe(true);
    expect(screen.getByText('About 16 h 43 min', { selector: '.sr-only' })).toBeTruthy();
  });

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

  it('swaps in place when the value changes again within the quiet window, so no digit shows twice', () => {
    vi.useFakeTimers();
    const { container, rerender } = render(<NumberRoll value="$3.20" />);
    rerender(<NumberRoll value="$3.21" />);
    expect(popping(container)).toContain(true);
    vi.advanceTimersByTime(3000);
    rerender(<NumberRoll value="$3.22" />);
    expect(popping(container).some(Boolean)).toBe(false);
    expect(characters(container).some((el) => el.classList.contains('motion-reduce:animate-fade-in'))).toBe(false);
    expect(screen.getByText('$3.22')).toBeTruthy();
  });

  it('rolls again once the value has held for the quiet window', () => {
    vi.useFakeTimers();
    const { container, rerender } = render(<NumberRoll value="14" />);
    rerender(<NumberRoll value="15" />);
    vi.advanceTimersByTime(QUIET_MS + 1);
    rerender(<NumberRoll value="16" />);
    expect(popping(container)).toEqual([false, true]);
  });
});
