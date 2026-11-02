// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Bars } from './bars';
import { Meter } from './meter';
import { TargetBar } from './target-bar';

const bars = [
  { key: 'a', value: 2, label: 'Week 1', detail: '2 a day' },
  { key: 'b', value: 4, label: 'Week 2', detail: '4 a day' },
];

afterEach(cleanup);

describe('chart primitives', () => {
  it('draws Bars with the latest in ink, earlier ones in rule, on a hover track', () => {
    const { container } = render(
      <Bars bars={bars} target={5} targetLabel="Target 5" summary="s" caption="c" columns={['Week', 'Rate']} />,
    );
    const fills = container.querySelectorAll('[style*="scaleY"]');
    expect(fills).toHaveLength(2);
    expect(fills[0]?.className).toContain('bg-rule');
    expect(fills[1]?.className).toContain('bg-ink');
    expect(fills[1]?.className).toContain('origin-bottom');
    expect(fills[1]?.parentElement?.className).toContain('bg-hover');
    expect(container.querySelector('.border-dashed.border-muted')).not.toBeNull();
    expect(container.querySelector('pattern')).toBeNull();
    expect(container.innerHTML).not.toContain('bg-run');
  });

  it('draws TargetBar in ink on a hover track', () => {
    const { container, getByRole } = render(<TargetBar value={4} target={8} targetLabel="Target 8" label="m" />);
    expect(getByRole('meter').className).toContain('bg-hover');
    expect(container.querySelector('.bg-ink.origin-left')).not.toBeNull();
    expect(container.querySelector('pattern')).toBeNull();
    expect(container.innerHTML).not.toContain('bg-run');
  });

  it('draws Meter in ink, and in rework when asked', () => {
    const { container, rerender, getByRole } = render(<Meter value={3} max={10} label="m" />);
    expect(getByRole('meter').className).toContain('bg-hover');
    expect(container.querySelector('.bg-ink.origin-left')).not.toBeNull();
    rerender(<Meter value={12} max={10} tone="rework" label="m" />);
    expect(container.querySelector('.bg-rework.origin-left')).not.toBeNull();
    expect(container.querySelector('pattern')).toBeNull();
  });
});
