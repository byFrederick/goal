// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { inboxQuery } from '../api/queries';
import type { Factory, InboxEntry, OrgSummary } from '../api/types';
import { renderFactoryPage } from '../test/route';
import { NeedsYouLink } from './needs-you-link';

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  name: 'sre',
  title: 'SRE',
} as Factory;

const decision = (id: string): InboxEntry =>
  ({
    id,
    kind: 'decision',
    factory: { id: factory.id, name: factory.name, title: factory.title ?? null },
    since: '2026-10-25T09:00:00Z',
    station: 'merge',
    action: 'merge',
    item: null,
    approval: null,
    blocked: null,
    andon: null,
    rework: null,
  }) as InboxEntry;

function renderLink(entries: InboxEntry[]) {
  return renderFactoryPage({
    page: () => <NeedsYouLink factory={factory} />,
    path: '/',
    org,
    factory,
    seed: [[inboxQuery(org.id, factory.id).queryKey, { entries, truncated: false }]],
  });
}

afterEach(cleanup);

describe('NeedsYouLink', () => {
  it('leads to the Needs you page filtered to this factory', async () => {
    renderLink([]);

    const link = await screen.findByRole('link', { name: 'Needs you' });
    expect(link.getAttribute('href')).toBe('/o/acme/needs-you?factory=sre');
  });

  it('shows the count of what waits on people in this factory', async () => {
    renderLink([decision('a'), decision('b')]);

    const link = await screen.findByRole('link', { name: /^Needs you/ });
    expect(link.textContent).toContain('2');
    expect(link.textContent).toContain('waiting on you');
  });

  it('shows no count when nothing waits', async () => {
    renderLink([]);

    const link = await screen.findByRole('link', { name: /^Needs you/ });
    expect(link.textContent).toBe('Needs you');
  });
});
