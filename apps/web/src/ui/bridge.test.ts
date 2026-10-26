import { describe, expect, it } from 'vitest';
import { bridgeGeometry } from './bridge';

const canvas = {
  target: 6,
  steps: [
    { key: 'waiting:investigate', value: 2.1 },
    { key: 'rework:review', value: 0.6 },
    { key: 'waiting_on_you:', value: 0.4 },
    { key: 'waiting:triage', value: 0.1 },
  ],
  actual: 9.2,
};

describe('bridgeGeometry', () => {
  it('chains each step from the end of the one before, from the target to the actual', () => {
    const g = bridgeGeometry(canvas);
    const steps = g.rows.filter((r) => r.kind === 'step');
    expect(steps[0]?.from).toBe(6);
    for (let i = 1; i < steps.length; i++) expect(steps[i]?.from).toBeCloseTo(steps[i - 1]?.to ?? Number.NaN, 9);
    expect(steps.at(-1)?.to).toBeCloseTo(canvas.actual, 9);
    const sum = steps.reduce((n, s) => n + (s.to - s.from), 0);
    expect(sum).toBeCloseTo(canvas.actual - canvas.target, 9);
  });

  it('sets the axis half a tick around target and actual, as the approved design', () => {
    const g = bridgeGeometry(canvas);
    expect(g.ticks).toEqual([5.5, 6.5, 7.5, 8.5, 9.5]);
    expect(g.domain).toEqual([5.5, 9.5]);
  });

  it('places bars in percent of the axis: the target from its start, the actual a marker at its end', () => {
    const g = bridgeGeometry(canvas);
    const target = g.rows[0];
    const actual = g.rows.at(-1);
    expect(target).toMatchObject({ kind: 'target', left: 0, width: 12.5 });
    expect(actual?.kind).toBe('actual');
    expect(actual?.end).toBeCloseTo(92.5, 9);
    const first = g.rows[1];
    expect(first?.left).toBeCloseTo(12.5, 9);
    expect(first?.width).toBeCloseTo(52.5, 9);
  });

  it('never starts the axis below zero', () => {
    const g = bridgeGeometry({ target: 0.2, steps: [{ key: 'a', value: 3 }], actual: 3.2 });
    expect(g.domain[0]).toBeGreaterThanOrEqual(0);
    expect(g.ticks.at(-1)).toBeGreaterThanOrEqual(3.2);
  });
});
