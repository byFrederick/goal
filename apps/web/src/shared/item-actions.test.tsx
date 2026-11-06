// @vitest-environment jsdom
import { useQuery } from '@tanstack/react-query';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, approveItem, mergeItem } from '../api/client';
import { keys } from '../api/queries';
import type { Factory, Item, OrgSummary } from '../api/types';
import { renderFactoryPage } from '../test/route';
import { type DecidableItem, ItemDecisions } from './item-actions';

vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  approveItem: vi.fn(),
  mergeItem: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const waiting: DecidableItem = {
  key: 'sre#4',
  ref: 'sre#4',
  title: 'Add the audit log',
  state: 'waiting_human',
  pull_requests: ['https://github.com/acme/app/pull/9'],
};

/** The item as the page holds it: a successful decision writes the updated item into the cache, as the item page reads it. */
function Harness({ action }: { action: 'approve' | 'merge' }) {
  const { data } = useQuery({
    queryKey: keys.item(org.id, waiting.key),
    queryFn: () => waiting,
    initialData: waiting,
    enabled: false,
  });
  return (
    <ItemDecisions
      item={data as DecidableItem}
      factory={factory.id}
      station={{ kind: 'human', action: action === 'merge' ? 'merge' : null } as never}
    />
  );
}

const done = { state: 'queued', station: 'merge', pull_requests: [] } as unknown as Item;

function renderActions(action: 'approve' | 'merge') {
  renderFactoryPage({ page: () => <Harness action={action} />, path: '/', org, factory });
}

describe('Approve on an item at a human station', () => {
  it('swaps the label for a check once approveItem resolved, and keeps the button for the hold', async () => {
    vi.mocked(approveItem).mockResolvedValue(done);
    renderActions('approve');

    const button = await screen.findByRole('button', { name: /^Approve/ });
    expect(button.querySelector('svg')?.getAttribute('class') ?? '').not.toContain('success-check');
    fireEvent.click(button);

    await waitFor(() => expect(button.querySelector('svg')?.getAttribute('class')).toContain('animate-success-check'));
    expect(within(button).queryByText('Approve')).toBeNull();
    expect(button.isConnected).toBe(true);
    await waitFor(() => expect(button.isConnected).toBe(false), { timeout: 3000 });
  });

  it('brings the label back and plays no check when approveItem fails', async () => {
    vi.mocked(approveItem).mockRejectedValue(new Error('goald is down'));
    renderActions('approve');

    const button = await screen.findByRole('button', { name: /^Approve/ });
    fireEvent.click(button);

    await waitFor(() => expect(approveItem).toHaveBeenCalled());
    await waitFor(() => expect(within(button).getByText('Approve')).toBeTruthy());
    expect(button.querySelector('svg')?.getAttribute('class') ?? '').not.toContain('success-check');
  });
});

describe('A pending decision', () => {
  it('shows Approve as busy, not disabled, while approveItem is pending', async () => {
    vi.mocked(approveItem).mockReturnValue(new Promise(() => {}));
    renderActions('approve');

    const button = await screen.findByRole('button', { name: /^Approve/ });
    fireEvent.click(button);

    await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('true'));
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect((button as HTMLButtonElement).disabled).toBe(false);
    await waitFor(() => expect(button.querySelector('[data-slot="busy"]')?.textContent).toBe('Approving…'));
  });

  it('shows the merge confirm as busy, not disabled, while mergeItem is pending', async () => {
    vi.mocked(mergeItem).mockReturnValue(new Promise(() => {}));
    renderActions('merge');

    fireEvent.click(await screen.findByRole('button', { name: 'Merge' }));
    const confirm = await screen.findByRole('button', { name: 'Squash and merge' });
    fireEvent.click(confirm);

    await waitFor(() => expect(confirm.getAttribute('aria-busy')).toBe('true'));
    expect(confirm.getAttribute('aria-disabled')).toBe('true');
    expect((confirm as HTMLButtonElement).disabled).toBe(false);
    await waitFor(() => expect(confirm.querySelector('[data-slot="busy"]')?.textContent).toBe('Merging…'));
  });
});

describe('Merge', () => {
  const open = async () => {
    fireEvent.click(await screen.findByRole('button', { name: 'Merge' }));
    return screen.findByRole('button', { name: 'Squash and merge' });
  };

  it('swaps Squash and merge for a check, and closes the dialog after the hold', async () => {
    vi.mocked(mergeItem).mockResolvedValue(done);
    renderActions('merge');

    const confirm = await open();
    fireEvent.click(confirm);

    await waitFor(() => expect(confirm.querySelector('svg')?.getAttribute('class')).toContain('animate-success-check'));
    expect(within(confirm).queryByText('Squash and merge')).toBeNull();
    expect(screen.getByRole('dialog')).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull(), { timeout: 3000 });
  });

  it('keeps the dialog and its refusal, with the label, when goald refuses the merge', async () => {
    vi.mocked(mergeItem).mockRejectedValue(
      new ApiError({
        type: 'cannot-merge',
        title: 'Cannot merge',
        status: 409,
        code: 'cannot-merge',
        detail: 'It has conflicts',
      } as never),
    );
    renderActions('merge');

    const confirm = await open();
    fireEvent.click(confirm);

    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy());
    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(confirm.querySelector('svg')?.getAttribute('class') ?? '').not.toContain('success-check');
  });
});
