// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { type Stat, StatStrip } from './stat-strip';

const stats: Stat[] = [
  { label: 'Throughput', value: '7.6', unit: 'a day', target: 'Target 10', delta: { text: '−0.6', good: false } },
  { label: 'Lead time, p85', value: 31, unit: 'h', target: 'Target 48 h', delta: { text: '−4 h', good: true } },
  { label: 'Sent back', value: 14, unit: '%', note: 'No target set' },
  { label: 'Cost per item', value: '$3.10', note: 'Per finished item' },
  { label: 'Hands-off', value: 80, unit: '%' },
];

afterEach(cleanup);

describe('StatStrip', () => {
  it('lets a long figure wrap between words so it never leaves its cell', () => {
    render(<StatStrip label="Time" stats={[{ label: 'Time to finish', value: 'About 1 d 16 h 43 min' }]} />);

    const cell = screen.getByText('Time to finish').parentElement as HTMLElement;
    const figure = cell.children[1] as HTMLElement;
    for (const c of ['flex-wrap', 'min-w-0', 'break-words']) expect(figure.className).toContain(c);
    expect(within(cell).getByText('About 1 d 16 h 43 min').className).toContain('sr-only');
  });

  it('shows at most four stats, so a fifth is not drawn', () => {
    render(<StatStrip label="Flow" stats={stats} />);

    const strip = screen.getByRole('region', { name: 'Flow' });
    expect(within(strip).getByText('Throughput')).toBeTruthy();
    expect(within(strip).getByText('Cost per item')).toBeTruthy();
    expect(within(strip).queryByText('Hands-off')).toBeNull();
    expect(within(strip).getAllByText(/^(Throughput|Lead time, p85|Sent back|Cost per item)$/)).toHaveLength(4);
  });

  it('shows the target and the signed delta in the meta line', () => {
    render(<StatStrip label="Flow" stats={stats} />);

    expect(screen.getByText('Target 10')).toBeTruthy();
    expect(screen.getByText('−0.6').className).toContain('text-rework-ink');
    expect(screen.getByText('−4 h').className).toContain('text-ok-ink');
  });

  it('shows an em dash with an accessible "No data yet" for a missing value, never a blank', () => {
    render(<StatStrip label="Flow" stats={[{ label: 'Throughput', value: null, target: 'Target 10' }]} />);

    expect(screen.getByText('No data yet')).toBeTruthy();
    expect(screen.getByText('—', { selector: '[aria-hidden="true"]' })).toBeTruthy();
  });

  it('draws the progress bar only when there is a target', () => {
    const { container, rerender } = render(
      <StatStrip label="Flow" stats={[{ label: 'Throughput', value: 5, target: 'Target 10', progress: 0.5 }]} />,
    );
    expect(container.querySelector('[style*="scaleX(0.5)"]')).not.toBeNull();

    rerender(<StatStrip label="Flow" stats={[{ label: 'Throughput', value: 5, progress: 0.5 }]} />);
    expect(container.querySelector('[style*="scaleX"]')).toBeNull();
  });
});

describe('StatStrip layout', () => {
  it('is a grid of two columns, one per figure from md, with no rule on a row start', () => {
    render(<StatStrip label="Flow" stats={stats} />);

    const strip = screen.getByRole('region', { name: 'Flow' });
    expect(strip.className).toContain('grid-cols-2');
    expect(strip.className).toContain('md:grid-cols-4');
    const cells = Array.from(strip.children) as HTMLElement[];
    // Below md the rows start at cells 0 and 2: neither has a left rule.
    expect(cells[0]?.className).not.toMatch(/(^|\s)border-l/);
    expect(cells[2]?.className).not.toMatch(/(^|\s)border-l/);
    expect(cells[1]?.className).toMatch(/(^|\s)border-l/);
    expect(cells[3]?.className).toMatch(/(^|\s)border-l/);
  });
});

describe('StatStrip narrow', () => {
  it('stays two by two until 2xl', () => {
    render(<StatStrip label="Narrow" stats={stats} narrow />);

    const strip = screen.getByRole('region', { name: 'Narrow' });
    expect(strip.className).toContain('2xl:grid-cols-4');
    expect(strip.className).not.toContain('md:grid-cols-4');
    const cells = Array.from(strip.children) as HTMLElement[];
    expect(cells[2]?.className).not.toMatch(/(^|\s)(md:)?border-l/);
  });
});
