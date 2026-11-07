// @vitest-environment jsdom
// The budget rows: a day's share is the weekly budget ÷ 7 (what Home shows), the 7-day figure is rolling,
// and the 100% rule says the factory resumes when the last 7 days' spend is under the budget.
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { SpendBudget } from '../../api/types';
import { Budgets } from './budgets';

const budget: SpendBudget = {
  factory: { id: 'f1', name: 'software', title: 'Software' },
  daily_usd: 400,
  weekly_usd: 2500,
  today_usd: 102.27,
  week_usd: 1790.14,
  state: 'running',
  reason: null,
  model_connections: [],
};

afterEach(cleanup);

describe('Budgets', () => {
  it('shows a day as the weekly budget ÷ 7 and never the definition daily cap', () => {
    const { container } = render(<Budgets budgets={[budget]} />);
    expect(screen.getByText(/about \$357 a day, \$2,500 a week/)).toBeTruthy();
    expect(screen.getByText(/Today \$102\.27 of \$357/)).toBeTruthy();
    expect(screen.getByText(/Last 7 days \$1,790\.14 of \$2,500/)).toBeTruthy();
    expect(container.textContent).not.toContain('$400');
  });

  it('says what happens at 100% in terms of the last 7 days, not a daily reset', () => {
    const { container } = render(<Budgets budgets={[budget]} />);
    expect(
      screen.getByText(
        "The factory drains: running items finish; nothing new starts until the last 7 days' spend is under the budget",
      ),
    ).toBeTruthy();
    expect(container.textContent?.toLowerCase()).not.toContain('midnight');
  });
});
