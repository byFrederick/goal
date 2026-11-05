// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { meterLayout, naturalWidth, SegmentMeter } from './segment-meter';

afterEach(cleanup);

const tones = { filled: 'bg-ink', waiting: 'bg-you', empty: 'bg-rule' };

describe('meterLayout', () => {
  it.each([
    [1, 'segments'],
    [3, 'segments'],
    [10, 'segments'],
    [20, 'segments'],
    [60, 'bar'],
    [500, 'bar'],
  ] as const)('chooses %i units in 160 px: %s', (total, mode) => {
    expect(meterLayout(total, 160, 4)).toBe(mode);
  });

  it('adds the gaps to the natural width', () => {
    expect(naturalWidth(20, 8, 4)).toBe(236);
    expect(naturalWidth(0, 8, 4)).toBe(0);
  });
});

describe('SegmentMeter', () => {
  const draw = (total: number) =>
    render(
      <>
        <SegmentMeter total={total} filled={2} segmentWidth={8} height={4} gap={4} tones={tones} testId="m" />
        <p>2 of {total}</p>
      </>,
    );

  it.each([1, 3, 10, 20])('draws %i segments, never more than one per unit', (total) => {
    draw(total);
    const meter = screen.getByTestId('m');
    expect(meter.dataset.mode).toBe('segments');
    expect(meter.querySelectorAll('i')).toHaveLength(total);
    expect(screen.getByText(`2 of ${total}`)).toBeTruthy();
  });

  it.each([60, 500])('draws one bar for %i units', (total) => {
    draw(total);
    const meter = screen.getByTestId('m');
    expect(meter.dataset.mode).toBe('bar');
    expect(meter.querySelectorAll('i')).toHaveLength(0);
    expect(screen.getByText(`2 of ${total}`)).toBeTruthy();
  });

  it('fills the bar to filled over total and the waiting share after it', () => {
    const { container } = render(
      <SegmentMeter total={100} filled={25} waiting={10} height={4} gap={4} tones={tones} label="25 of 100" />,
    );
    expect(screen.getByRole('img', { name: '25 of 100' })).toBeTruthy();
    const fills = container.querySelectorAll<HTMLElement>('[style*="scaleX"]');
    expect(fills[0]?.style.transform).toBe('scaleX(0.25)');
    expect(fills[1]?.style.transform).toBe('translateX(25%) scaleX(0.1)');
  });

  it('is hidden from assistive technology without a label', () => {
    draw(3);
    expect(screen.getByTestId('m').getAttribute('aria-hidden')).toBe('true');
  });
});
