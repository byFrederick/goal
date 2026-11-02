// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { BakeOffArm } from '../../api/types';
import { PassCostPlot } from './bake-off-plot';

function arm(n: number, model: string, pass: number, cost: number): BakeOffArm {
  return {
    arm: n,
    model,
    is_current: false,
    summary: { pass_rate: pass, cost_per_item_usd: cost, time_per_item_seconds: 60, turns: 10 },
  } as BakeOffArm;
}

const arms = [
  arm(1, 'claude-haiku-5-5', 0.93, 0.24),
  arm(2, 'claude-sonnet-5-5', 0.87, 0.9),
  arm(3, 'claude-opus-5-5', 0.93, 2.4),
];

describe('PassCostPlot', () => {
  it('draws the recommended model in ink and every other in rule2, and names every point', () => {
    render(<PassCostPlot arms={arms} recommendedArm={1} />);
    const dots = screen.getAllByTestId('plot-dot');
    expect(dots.map((d) => d.className.match(/bg-(ink|rule)\b/)?.[0])).toEqual(['bg-ink', 'bg-rule', 'bg-rule']);
    const label = screen.getByRole('img').getAttribute('aria-label') ?? '';
    expect(label).toContain('haiku 5.5 93% at $0.24');
    expect(label).toContain('sonnet 5.5');
    expect(label).toContain('opus 5.5');
  });
});
