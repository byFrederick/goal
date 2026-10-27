// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Chip, type ChipTone } from './chip';

afterEach(cleanup);

const tones: ChipTone[] = ['plain', 'quiet', 'run', 'you', 'rework', 'constraint', 'expedite', 'ok'];

const classes = (element: Element | null) => Array.from(element?.classList ?? []);
const dotOf = (container: HTMLElement) => container.querySelector('[aria-hidden="true"]');

describe('Chip', () => {
  it.each(tones)('draws the %s tone with a status dot', (tone) => {
    const { container, unmount } = render(
      <Chip tone={tone} dot>
        Label
      </Chip>,
    );
    expect(classes(screen.getByText('Label'))).toEqual(expect.arrayContaining(['h-5.5', 'rounded-chip']));
    expect(dotOf(container)).not.toBeNull();
    unmount();
  });

  it('draws neutral chips panel-coloured with a line border', () => {
    render(<Chip tone="plain">Board</Chip>);
    expect(classes(screen.getByText('Board'))).toEqual(expect.arrayContaining(['bg-panel', 'border-line', 'text-ink']));
  });

  it('gives a tint fill and the tone ink text to each status tone', () => {
    const { unmount } = render(<Chip tone="you">Needs you</Chip>);
    expect(classes(screen.getByText('Needs you'))).toEqual(expect.arrayContaining(['bg-you-tint', 'text-you-ink']));
    unmount();

    render(<Chip tone="rework">Sent back</Chip>);
    expect(classes(screen.getByText('Sent back'))).toEqual(
      expect.arrayContaining(['bg-rework-tint', 'text-rework-ink']),
    );
  });

  it('draws the constraint and expedite tones in yellow', () => {
    const { unmount } = render(<Chip tone="constraint">Constraint</Chip>);
    expect(classes(screen.getByText('Constraint'))).toEqual(
      expect.arrayContaining(['bg-amber-tint', 'text-amber-ink']),
    );
    unmount();

    render(<Chip tone="expedite">Expedite</Chip>);
    expect(classes(screen.getByText('Expedite'))).toEqual(expect.arrayContaining(['bg-amber-tint', 'text-amber-ink']));
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

  it('is 28 px tall when large', () => {
    render(<Chip size="lg">Large</Chip>);
    expect(classes(screen.getByText('Large'))).toEqual(expect.arrayContaining(['h-7', 'px-3']));
  });
});
