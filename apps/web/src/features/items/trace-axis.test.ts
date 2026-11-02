import { describe, expect, it } from 'vitest';
import { axisTicks } from './trace-axis';

const span = 2_280_000;

describe('axisTicks', () => {
  const widths = [240, 480, 1100];
  it.each(widths)('keeps 0 and round, spaced ticks at %i px', (w) => {
    const ticks = axisTicks(span, w);
    expect(ticks[0]).toBe(0);
    const step = ticks[1] ?? 0;
    for (const t of ticks) expect(t % step === 0 || t === 0).toBe(true);
    for (let i = 1; i < ticks.length; i++) {
      const gap = (((ticks[i] ?? 0) - (ticks[i - 1] ?? 0)) * 1000 * w) / span;
      expect(gap).toBeGreaterThanOrEqual(30);
    }
    const last = ticks[ticks.length - 1] ?? 0;
    expect((last * 1000 * w) / span).toBeLessThan(w);
  });
  it('gives wider axes at least as many ticks', () => {
    const counts = widths.map((w) => axisTicks(span, w).length);
    expect(counts[1]).toBeGreaterThanOrEqual(counts[0] ?? 0);
    expect(counts[2]).toBeGreaterThanOrEqual(counts[1] ?? 0);
  });
  it('returns only 0 without a width', () => {
    expect(axisTicks(span, 0)).toEqual([0]);
    expect(axisTicks(span, -5)).toEqual([0]);
  });
});
