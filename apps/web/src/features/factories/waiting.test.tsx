// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { FactoryOverviewList, InboxEntry, OrgSummary } from '../../api/types';
import { renderOrgPage } from '../../test/route';
import { WaitingBanner, WaitingList } from './waiting';

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };

/** A decision about an item, or about nothing when `item` is null. */
const decision = (item: { ref: string; title: string; station: string } | null): InboxEntry =>
  ({
    id: `decision:${item?.ref ?? 'none'}`,
    kind: 'decision',
    factory: { id: 'factory-1', name: 'sre', title: null },
    since: '2026-10-25T09:00:00Z',
    station: 'merge',
    action: 'merge',
    item: item ? { ...item, station: item.station } : null,
    approval: null,
    blocked: null,
    andon: null,
    rework: null,
  }) as unknown as InboxEntry;

const overview = (waiting: number): FactoryOverviewList =>
  ({
    window: '7d',
    since: '2026-10-20',
    until: '2026-10-26',
    waiting_on_you: waiting,
    spend_usd: 0,
    budget_usd: null,
    factories: [],
  }) as unknown as FactoryOverviewList;

afterEach(cleanup);

describe('WaitingList', () => {
  it('is titled Needs you first and its aside opens the Needs you page', async () => {
    renderOrgPage({
      page: () => <WaitingList org={org} inbox={{ entries: [decision(null)], truncated: false }} />,
      org,
      path: '/',
    });

    expect(await screen.findByRole('heading', { name: 'Needs you first' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open all' }).getAttribute('href')).toBe('/o/acme/needs-you');
  });
});

describe('WaitingBanner', () => {
  it('leads to the Needs you page when more than one thing waits', async () => {
    renderOrgPage({
      page: () => (
        <WaitingBanner
          org={org}
          overview={overview(2)}
          inbox={{
            entries: [decision({ ref: 'sre#4', title: 'Fix', station: 'merge' }), decision(null)],
            truncated: false,
          }}
        />
      ),
      org,
      path: '/',
    });

    const banner = await screen.findByRole('link', { name: /^2 waiting on you/ });
    expect(banner.getAttribute('href')).toBe('/o/acme/needs-you');
  });

  it('leads to the Needs you page when one thing waits but it is not about an item', async () => {
    renderOrgPage({
      page: () => (
        <WaitingBanner org={org} overview={overview(1)} inbox={{ entries: [decision(null)], truncated: false }} />
      ),
      org,
      path: '/',
    });

    const banner = await screen.findByRole('link', { name: /^1 waiting on you/ });
    expect(banner.getAttribute('href')).toBe('/o/acme/needs-you');
  });

  it('leads to the item when exactly one thing waits and it is an item', async () => {
    renderOrgPage({
      page: () => (
        <WaitingBanner
          org={org}
          overview={overview(1)}
          inbox={{ entries: [decision({ ref: 'sre#4', title: 'Fix', station: 'merge' })], truncated: false }}
        />
      ),
      org,
      path: '/',
    });

    const banner = await screen.findByRole('link', { name: /^1 waiting on you/ });
    expect(banner.getAttribute('href')).toContain('/o/acme/f/sre/items/');
  });

  it('shows nothing when nothing waits', async () => {
    renderOrgPage({
      page: () => <WaitingBanner org={org} overview={overview(0)} inbox={{ entries: [], truncated: false }} />,
      org,
      path: '/',
    });

    expect(screen.queryByRole('link', { name: /waiting on you/ })).toBeNull();
  });
});
