// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { approveItem } from '../../api/client';
import { inboxQuery } from '../../api/queries';
import type { Factory, InboxEntry, ItemCard, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { NeedsYouCard } from './needs-you-card';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  approveItem: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['task'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'owner',
  can: ['item:create', 'item:operate'],
} as unknown as Factory;

const item = {
  id: 'i1',
  ref: 'sre#2',
  title: 'Rotate keys',
  type: 'task',
  station: 'review',
  state: 'waiting_human',
} as ItemCard;
const entry = {
  id: 'decision:sre#2',
  kind: 'decision',
  factory: { id: factory.id, name: factory.name, title: factory.title },
  since: '2026-10-24T09:00:00Z',
  item,
  station: 'review',
  action: null,
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
} as InboxEntry;

describe('NeedsYouCard actions', () => {
  it('draws Approve with the approve variant: white text on the you colour', async () => {
    renderFactoryPage({
      page: () => <NeedsYouCard />,
      path: '/',
      org,
      factory,
      seed: [[inboxQuery(org.id, factory.id).queryKey, { entries: [entry] }]],
    });
    const approve = await screen.findByRole('button', { name: 'Approve sre#2' });
    expect(approve.className).toContain('bg-you');
    expect(approve.className).toContain('text-white');
    expect(approve.className).not.toContain('text-action-ink');
  });

  it('shows Approve as busy, not disabled, while the approval is pending', async () => {
    vi.mocked(approveItem).mockReturnValue(new Promise(() => {}));
    renderFactoryPage({
      page: () => <NeedsYouCard />,
      path: '/',
      org,
      factory,
      seed: [[inboxQuery(org.id, factory.id).queryKey, { entries: [entry] }]],
    });
    const approve = await screen.findByRole('button', { name: 'Approve sre#2' });
    fireEvent.click(approve);

    await waitFor(() => expect(approve.getAttribute('aria-busy')).toBe('true'));
    expect(approve.getAttribute('aria-disabled')).toBe('true');
    expect((approve as HTMLButtonElement).disabled).toBe(false);
    expect(approve.className).toContain('bg-you');
    await waitFor(() => expect(approve.querySelector('[data-slot="busy"]')?.textContent).toBe('Approving…'));
  });
});
