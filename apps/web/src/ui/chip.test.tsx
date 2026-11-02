// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Chip, type ChipTone } from './chip';

afterEach(cleanup);

const tones: ChipTone[] = ['plain', 'quiet', 'run', 'you', 'rework', 'constraint', 'expedite', 'ok'];

const classes = (element: Element | null) => Array.from(element?.classList ?? []);
// The chip is the text's parent: the text is wrapped so it can swap in place.
const chipOf = (text: string) => screen.getByText(text).parentElement;
const dotOf = (container: HTMLElement) => container.querySelector('[aria-hidden="true"]');

describe('Chip', () => {
  it.each(tones)('draws the %s tone with a status dot', (tone) => {
    const { container, unmount } = render(
      <Chip tone={tone} dot>
        Label
      </Chip>,
    );
    expect(classes(chipOf('Label'))).toEqual(expect.arrayContaining(['h-5.5', 'rounded-chip']));
    expect(dotOf(container)).not.toBeNull();
    unmount();
  });

  it('draws the dot 6 px across', () => {
    const { container } = render(
      <Chip tone="ok" dot>
        Done
      </Chip>,
    );
    expect(classes(dotOf(container))).toEqual(expect.arrayContaining(['size-1.5']));
    expect(classes(dotOf(container))).not.toContain('size-2');
  });

  it('draws neutral chips panel-coloured with a line border', () => {
    render(<Chip tone="plain">Board</Chip>);
    expect(classes(chipOf('Board'))).toEqual(expect.arrayContaining(['bg-panel', 'border-line', 'text-ink']));
  });

  it('keeps the status tones neutral, and gives the dot the tone colour', () => {
    const { container, unmount } = render(
      <Chip tone="ok" dot>
        Done
      </Chip>,
    );
    expect(classes(chipOf('Done'))).toEqual(expect.arrayContaining(['bg-panel', 'border-line', 'text-ink']));
    expect(classes(chipOf('Done'))).not.toContain('bg-ok-tint');
    expect(classes(dotOf(container))).toContain('bg-ok');
    unmount();

    const { container: amber } = render(
      <Chip tone="constraint" dot>
        Constraint
      </Chip>,
    );
    expect(classes(chipOf('Constraint'))).toEqual(expect.arrayContaining(['bg-panel', 'text-ink']));
    expect(classes(dotOf(amber))).toContain('bg-amber');
  });

  it('keeps a tint fill and the tone ink text for the two tones that ask for action', () => {
    const { unmount } = render(<Chip tone="you">Needs you</Chip>);
    expect(classes(chipOf('Needs you'))).toEqual(expect.arrayContaining(['bg-you-tint', 'text-you-ink']));
    unmount();

    render(<Chip tone="rework">Sent back</Chip>);
    expect(classes(chipOf('Sent back'))).toEqual(expect.arrayContaining(['bg-rework-tint', 'text-rework-ink']));
  });

  it('pulses only the running dot, and reduced motion turns the pulse off', () => {
    const { container, unmount } = render(
      <Chip tone="run" dot>
        Running
      </Chip>,
    );
    expect(classes(dotOf(container))).toEqual(
      expect.arrayContaining(['animate-pulse-dot', 'motion-reduce:animate-none']),
    );
    unmount();

    const { container: waiting } = render(
      <Chip tone="you" dot>
        Waiting
      </Chip>,
    );
    expect(classes(dotOf(waiting))).not.toContain('animate-pulse-dot');
  });

  it('swaps changed text in place, and does not animate the first draw', () => {
    const { rerender } = render(<Chip tone="run">Running</Chip>);
    expect(classes(screen.getByText('Running'))).not.toContain('motion-safe:animate-text-states-swap');

    rerender(<Chip tone="ok">Done</Chip>);
    expect(screen.queryByText('Running')).toBeNull();
    expect(classes(screen.getByText('Done'))).toEqual(
      expect.arrayContaining(['motion-safe:animate-text-states-swap', 'motion-reduce:animate-fade-in']),
    );
  });

  it('is 28 px tall when large', () => {
    render(<Chip size="lg">Large</Chip>);
    expect(classes(chipOf('Large'))).toEqual(expect.arrayContaining(['h-7', 'px-3']));
  });
});
