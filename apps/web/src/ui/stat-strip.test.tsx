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
