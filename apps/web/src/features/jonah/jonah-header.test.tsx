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
function renderTabs(tab: JonahTab, path: string, search?: Record<string, string>) {
  return renderFactoryPage({ page: () => <JonahTabs tab={tab} />, path, org, factory, search });
}

describe('the Jonah tabs', () => {
  it.each([
    ['Overview', 'overview', 'jonah/', undefined],
    ['Proposals', 'proposals', 'jonah/', { tab: 'proposals' }],
    ['Judges', 'judges', 'jonah/judges', undefined],
    ['Evals', 'evals', 'jonah/evals', undefined],
    ['Bake-offs', 'bake-offs', 'jonah/bake-offs', undefined],
  ] as const)('marks only %s as current on its route', async (name, tab, path, search) => {
    renderTabs(tab, path, search);
    const nav = await screen.findByRole('navigation', { name: 'Jonah' });
    const current = nav.querySelectorAll('[aria-current="page"]');
    expect(current).toHaveLength(1);
    expect(current[0]?.textContent).toBe(name);
    expect(within(nav).getByRole('link', { name })).toBe(current[0]);
  });

  it('draws the underline variant: a rule under the row that scrolls in its own box', async () => {
    renderTabs('judges', 'jonah/judges');
    const nav = await screen.findByRole('navigation', { name: 'Jonah' });
    expect(nav.className).toContain('border-b');
    expect(nav.className).toContain('overflow-x-auto');
    expect(nav.className).not.toContain('rounded-control');
  });
});
