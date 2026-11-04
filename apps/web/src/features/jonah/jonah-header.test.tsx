// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Factory, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { type JonahTab, JonahTabs } from './jonah-header';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };
const factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: [],
} as Factory;

/** The tab bar at the address a Jonah route has, with the tab its page names. */
function renderTabs(tab: JonahTab, path: string) {
  return renderFactoryPage({ page: () => <JonahTabs tab={tab} />, path, org, factory });
}

describe('the Jonah tabs', () => {
  it('links each tab to its own page', async () => {
    renderTabs('overview', 'jonah/');
    const nav = await screen.findByRole('navigation', { name: 'Jonah' });
    const hrefs = within(nav)
      .getAllByRole('link')
      .map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual([
      '/o/acme/f/software/jonah',
      '/o/acme/f/software/jonah/proposals',
      '/o/acme/f/software/jonah/tests',
    ]);
  });

  it.each([
    ['Overview', 'overview', 'jonah/'],
    ['Proposals', 'proposals', 'jonah/proposals'],
    ['Tests', 'tests', 'jonah/tests'],
  ] as const)('marks only %s as current on its route', async (name, tab, path) => {
    renderTabs(tab, path);
    const nav = await screen.findByRole('navigation', { name: 'Jonah' });
    const current = nav.querySelectorAll('[aria-current="page"]');
    expect(current).toHaveLength(1);
    expect(current[0]?.textContent).toBe(name);
    expect(within(nav).getByRole('link', { name })).toBe(current[0]);
  });

  it('draws the underline variant: a rule under the row that scrolls in its own box', async () => {
    renderTabs('tests', 'jonah/tests');
    const nav = await screen.findByRole('navigation', { name: 'Jonah' });
    expect(nav.className).toContain('border-b');
    expect(nav.className).toContain('overflow-x-auto');
    expect(nav.className).not.toContain('rounded-control');
  });
});
