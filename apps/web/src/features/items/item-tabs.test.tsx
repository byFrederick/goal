// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
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
  it('shows the views with the file and check counts, and marks the selected one', async () => {
    renderTabs('checks');

    const nav = await screen.findByRole('navigation', { name: 'Item views' });
    expect(within(nav).getByRole('link', { name: 'Timeline' })).toBeTruthy();
    expect(within(nav).getByRole('link', { name: 'Spec' })).toBeTruthy();
    expect(within(nav).getByRole('link', { name: /^Changes\s*6 files$/ })).toBeTruthy();
    expect(
      within(nav)
        .getByRole('link', { name: /^Checks\s*2$/ })
        .getAttribute('aria-current'),
    ).toBe('page');
    expect(within(nav).getByRole('link', { name: 'Timeline' }).getAttribute('aria-current')).toBeNull();
  });

  it('keeps Trace and Teardown inert until they are built', async () => {
    renderTabs('timeline');

    const nav = await screen.findByRole('navigation', { name: 'Item views' });
    for (const name of ['Trace', 'Teardown']) {
      expect(within(nav).getByRole('button', { name }).getAttribute('aria-disabled')).toBe('true');
    }
  });
});

describe('isItemTab', () => {
  it('accepts the four views and nothing else', () => {
    expect(['timeline', 'spec', 'changes', 'checks'].every(isItemTab)).toBe(true);
    expect(isItemTab('trace')).toBe(false);
    expect(isItemTab(undefined)).toBe(false);
  });
});
