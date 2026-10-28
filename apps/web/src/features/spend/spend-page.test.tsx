// @vitest-environment jsdom
// The spend screen as a person sees it: the window in the URL, the figures, and each of its states
// (loading, error with retry, empty, and an incomplete answer that shows dashes, never $0). The API
// call is replaced, so the tests read the answer the screen was given.
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { OrgSpend, SpendWindow } from '../../api/types';
import { answerFor, spendFixture } from './fixture';
import { renderSpend } from './harness';

const { getOrgSpend } = vi.hoisted(() => ({ getOrgSpend: vi.fn() }));
vi.mock('../../api/spend', () => ({ getOrgSpend }));

/** Answers each window from `answer`. */
const answerWith = (answer: (window: SpendWindow) => OrgSpend) =>
  getOrgSpend.mockImplementation(async (_org: string, window: SpendWindow) => answer(window));

beforeEach(() => {
  answerWith(answerFor);
});

afterEach(() => {
  cleanup();
  getOrgSpend.mockReset();
});

describe('the spend screen, ready', () => {
  it('shows the title, the window control and the figures', async () => {
    renderSpend();

    expect(await screen.findByRole('heading', { name: 'Spend and billing' })).toBeTruthy();
    expect((screen.getByRole('radio', { name: 'This week' }) as HTMLInputElement).checked).toBe(true);
    expect(await screen.findByText('Spend this week')).toBeTruthy();
    expect(screen.getByText('$1,940')).toBeTruthy();
    expect(screen.getByText('September $2,310')).toBeTruthy();
    expect(screen.getByText('$3.40')).toBeTruthy();
    expect(screen.getByText('Spent on rework')).toBeTruthy();
  });

  it('lists the factories with their budgets, the stop and the rules', async () => {
    renderSpend();

    const section = (await screen.findByRole('heading', { name: 'Budgets' })).closest('section') as HTMLElement;
    expect(within(section).getByText('Software')).toBeTruthy();
    expect(within(section).getByText('Stopped')).toBeTruthy();
    expect(within(section).getByText('Andon pulled, no runs today')).toBeTruthy();
    expect(within(section).getByText('64% today')).toBeTruthy();
    expect(within(section).getByText('At 80%')).toBeTruthy();
    expect(within(section).getByText('At 100%')).toBeTruthy();
    expect(within(section).getByText('Per item')).toBeTruthy();
  });

  it('lists who pays, with the connection used by every factory and the router row not built yet', async () => {
    renderSpend();

    const section = (await screen.findByRole('heading', { name: 'Who pays for models' })).closest(
      'section',
    ) as HTMLElement;
    expect(within(section).getByText('Connected')).toBeTruthy();
    expect(within(section).getByText('Connection claude, used by all 3 factories')).toBeTruthy();
    expect(within(section).getByRole('link', { name: 'Add a key' }).getAttribute('href')).toBe(
      '/o/acme/connections/new',
    );
    expect(within(section).getByRole('button', { name: 'Set up' }).getAttribute('aria-disabled')).toBe('true');
  });

  it('draws Export CSV as not available yet, with aria-disabled', async () => {
    renderSpend();

    const exportButton = await screen.findByRole('button', { name: 'Export CSV' });
    expect(exportButton.getAttribute('aria-disabled')).toBe('true');
  });

  it('never shows an agent copy number', async () => {
    renderSpend();

    await screen.findByRole('heading', { name: 'By agent' });
    expect(screen.queryByText(/copy/i)).toBeNull();
  });
});

describe('the window in the URL', () => {
  it('asks for the window the URL names, and moves the URL when another is chosen', async () => {
    const { router } = renderSpend('/o/acme/spend?window=month');

    await screen.findByRole('radio', { name: 'This month' });
    expect(getOrgSpend).toHaveBeenCalledWith('org-1', 'month', expect.anything());

    fireEvent.click(screen.getByRole('radio', { name: 'Last 90 days' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ window: '90d' }));
    await waitFor(() => expect(getOrgSpend).toHaveBeenCalledWith('org-1', '90d', expect.anything()));
  });

  it('falls back to this week for any other value', async () => {
    renderSpend('/o/acme/spend?window=yesterday');

    await screen.findByRole('heading', { name: 'Budgets' });
    expect(getOrgSpend).toHaveBeenCalledWith('org-1', 'week', expect.anything());
    expect((screen.getByRole('radio', { name: 'This week' }) as HTMLInputElement).checked).toBe(true);
  });
});

describe('the other states', () => {
  it('shows a loading state while the answer is pending', async () => {
    getOrgSpend.mockReturnValue(new Promise(() => {}));
    renderSpend();

    const status = await screen.findByRole('status');
    expect(status.textContent).toBe('Loading spend and billing');
    expect(status.closest('[aria-busy="true"]')).not.toBeNull();
  });

  it('shows an error with Try again, which asks again', async () => {
    getOrgSpend.mockRejectedValueOnce(new Error('Failed to fetch'));
    renderSpend();

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText(/Couldn’t load spend and billing/)).toBeTruthy();
    fireEvent.click(within(alert).getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('heading', { name: 'Budgets' })).toBeTruthy();
    expect(getOrgSpend).toHaveBeenCalledTimes(2);
  });

  it('shows the empty state when nothing was spent and no factory has a budget', async () => {
    answerWith(() =>
      spendFixture({
        total_usd: 0,
        this_month_usd: 0,
        last_month_usd: 0,
        budgets: [],
        by_agent: [],
        by_model: [],
        days: spendFixture().days.map((d) => ({ ...d, usd: 0, by_factory: [] })),
      }),
    );
    renderSpend();

    expect(await screen.findByText('No spend in this window')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Budgets' })).toBeNull();
  });

  it('shows dashes, not $0, for the figures an incomplete answer does not give', async () => {
    answerWith(() =>
      spendFixture({
        complete: false,
        this_month_usd: null,
        last_month_usd: null,
        merged: null,
        cost_per_merged_usd: null,
        weekly: [],
        rework_usd: null,
        rework_share: null,
        by_agent: [],
        by_model: [],
        days: spendFixture().days.map((d) => ({ ...d, by_factory: [] })),
      }),
    );
    renderSpend();

    await screen.findByRole('heading', { name: 'By agent' });
    expect(screen.getAllByText('Not split by factory, so not shown.')).toHaveLength(2);
    expect(screen.getByText('Some spend is not split by factory: a factory you cannot see spent it.')).toBeTruthy();
    const figures = screen.getByRole('region', { name: 'Spend this week' });
    expect(within(figures).queryByText('$0')).toBeNull();
    expect(within(figures).getAllByText('—').length).toBeGreaterThanOrEqual(3);
  });
});
