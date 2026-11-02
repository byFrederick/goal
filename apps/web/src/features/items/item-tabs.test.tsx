// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Factory } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ItemTabs, isItemTab } from './item-tabs';

afterEach(cleanup);

const org = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

function renderTabs(tab: Parameters<typeof ItemTabs>[0]['tab']) {
  // The URL names the same tab as the prop, as the item page keeps them.
  const search = tab === 'timeline' ? '' : `?tab=${tab}`;
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <ItemTabs tab={tab} files={6} checks={2} />
      </TooltipProvider>
    ),
    path: 'items/$ref',
    location: `items/147${search}`,
    org,
    factory,
    seed: [],
  });
}

describe('ItemTabs', () => {
  it('shows the views as tabs with the counts, and marks the selected one', async () => {
    renderTabs('checks');

    const list = await screen.findByRole('tablist', { name: 'Item views' });
    const tabs = within(list).getAllByRole('tab');
    expect(tabs.map((t) => t.textContent)).toEqual([
      'Timeline',
      'Spec',
      'Changes6 files',
      'Checks2',
      'Trace',
      'Teardown',
    ]);
    const selected = tabs.filter((t) => t.getAttribute('aria-selected') === 'true');
    expect(selected.map((t) => t.textContent)).toEqual(['Checks2']);
    expect(selected[0]?.tabIndex).toBe(0);
    expect(within(list).getByRole('tab', { name: 'Timeline' }).tabIndex).toBe(-1);
  });

  it('draws one underline that slides between tabs, on the motion tokens', async () => {
    renderTabs('spec');

    const list = await screen.findByRole('tablist', { name: 'Item views' });
    const underline = list.querySelector('span[aria-hidden="true"]');
    expect(underline).not.toBeNull();
    expect(underline?.className).toContain('duration-(--duration-fast)');
    expect(underline?.className).toContain('ease-out');
    expect(underline?.className).toContain('motion-reduce:transition-none');
  });

  it('moves to the next tab with ArrowRight and puts it in the URL', async () => {
    const { router } = renderTabs('timeline');

    const list = await screen.findByRole('tablist', { name: 'Item views' });
    const timeline = within(list).getByRole('tab', { name: 'Timeline' });
    timeline.focus();
    fireEvent.keyDown(timeline, { key: 'ArrowRight' });
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: 'spec' }));
    expect(document.activeElement).toBe(within(list).getByRole('tab', { name: 'Spec' }));
  });

  it('keeps the URL clean when Home returns to Timeline, and ends at Trace, skipping Teardown', async () => {
    const { router } = renderTabs('spec');

    const list = await screen.findByRole('tablist', { name: 'Item views' });
    const spec = within(list).getByRole('tab', { name: 'Spec' });
    fireEvent.keyDown(spec, { key: 'End' });
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: 'trace' }));
    fireEvent.keyDown(within(list).getByRole('tab', { name: 'Trace' }), { key: 'Home' });
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });

  it('never disables Trace, and keeps Teardown inert until it is built', async () => {
    renderTabs('timeline');

    const list = await screen.findByRole('tablist', { name: 'Item views' });
    const trace = within(list).getByRole('tab', { name: 'Trace' });
    expect(trace.getAttribute('aria-disabled')).toBeNull();
    expect(trace.getAttribute('href')).toContain('tab=trace');
    const teardown = within(list).getByRole('tab', { name: 'Teardown' });
    expect(teardown.getAttribute('aria-disabled')).toBe('true');
    expect(teardown.getAttribute('aria-selected')).toBe('false');
  });

  it('reaches Trace with ArrowRight from Checks', async () => {
    const { router } = renderTabs('checks');

    const list = await screen.findByRole('tablist', { name: 'Item views' });
    fireEvent.keyDown(within(list).getByRole('tab', { name: /^Checks/ }), { key: 'ArrowRight' });
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: 'trace' }));
  });
});

describe('isItemTab', () => {
  it('accepts the five views and nothing else', () => {
    expect(['timeline', 'spec', 'changes', 'checks', 'trace'].every(isItemTab)).toBe(true);
    expect(isItemTab('teardown')).toBe(false);
    expect(isItemTab(undefined)).toBe(false);
  });
});
