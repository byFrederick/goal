// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { dropFollowUp, fileFollowUp } from '../../api/client';
import { itemQuery } from '../../api/queries';
import type { Factory, FollowUp, Item } from '../../api/types';
import { expectBusyOnClick } from '../../test/busy';
import { renderFactoryPage } from '../../test/route';
import { FollowUpsCard, NotesCard } from './follow-ups-card';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  dropFollowUp: vi.fn(),
  fileFollowUp: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.mocked(dropFollowUp).mockReset();
  vi.mocked(fileFollowUp).mockReset();
});

const org = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  work_types: [],
  stations: [{ id: 'intake', kind: 'queue' }],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:create'],
} as unknown as Factory;

const followUp = (n: number, over: Partial<FollowUp>): FollowUp => ({
  id: `fu-${n}`,
  item_id: 'item-519',
  station: 'build',
  agent: 'backend-engineer',
  text: `Follow-up ${n}`,
  state: 'waiting',
  created_at: '2026-11-01T10:00:00Z',
  ...over,
});

const item = (over: Partial<Item> = {}): Item =>
  ({
    id: 'item-519',
    ref: 'software#519',
    factory_id: 'factory-1',
    number: 519,
    type: 'feature',
    title: 'Convert every organization',
    body: '',
    station: 'done',
    state: 'done',
    class_of_service: 'standard',
    priority: 0,
    rework_count: 0,
    cost_usd: 0,
    outputs: {},
    created_at: '2026-10-25T08:00:00Z',
    updated_at: '2026-10-25T08:05:00Z',
    ...over,
  }) as Item;

const follow = [
  followUp(1, { target: 'note', state: 'delivered', for: 'software#523' }),
  followUp(2, { target: 'start', state: 'waiting', start: { title: 'Drop tables', in: '14d', factory: 'sre' } }),
  followUp(3, { target: 'triage', state: 'waiting' }),
  followUp(4, { state: 'dropped', reason: 'dropped by user:u1' }),
  followUp(5, { target: 'triage', state: 'filed', filed_item_id: 'triage-1', filed_item: 'software#570' }),
];

function render(source: Item, seed: Parameters<typeof renderFactoryPage>[0]['seed'] = []) {
  return renderFactoryPage({
    page: () => <FollowUpsCard item={source} />,
    path: 'items/',
    org,
    factory,
    seed,
  });
}

describe('FollowUpsCard', () => {
  it('lists each follow-up with where it went', async () => {
    render(item({ follow_ups: follow }));
    const card = await screen.findByRole('region', { name: /Follow-ups/ });

    expect(within(card).getByText('Follow-up 1')).toBeTruthy();
    expect(within(card).getByText(/Note on/).textContent).toBe('Note on software#523, delivered');
    expect(within(card).getByText(/Filed in sre when/).textContent).toBe(
      'Filed in sre when software#519 lands, starts 14d later',
    );
    expect(within(card).getByText(/For the architect when/).textContent).toBe(
      'For the architect when software#519 lands',
    );
    expect(within(card).getByText('Dropped: dropped by user:u1')).toBeTruthy();
    expect(within(card).getByRole('link', { name: 'software#523' }).getAttribute('href')).toBe(
      '/o/acme/f/software/items/523',
    );
  });

  it('has Drop on a waiting follow-up and calls dropFollowUp', async () => {
    vi.mocked(dropFollowUp).mockResolvedValue(follow[2] as FollowUp);
    render(item({ follow_ups: follow }));
    const card = await screen.findByRole('region', { name: /Follow-ups/ });

    expect(within(card).getAllByRole('button', { name: /^Drop:/ })).toHaveLength(2);
    fireEvent.click(within(card).getByRole('button', { name: 'Drop: Follow-up 3' }));

    await waitFor(() => expect(dropFollowUp).toHaveBeenCalledWith('org-1', 'item-519', 'fu-3'));
  });

  it('has File on a dropped follow-up, opens New item with its text and files through fileFollowUp', async () => {
    vi.mocked(fileFollowUp).mockResolvedValue({ ref: 'software#600', station: 'intake' } as Item);
    render(item({ follow_ups: follow }));
    const card = await screen.findByRole('region', { name: /Follow-ups/ });

    fireEvent.click(within(card).getByRole('button', { name: 'File: Follow-up 4' }));
    const title = (await screen.findByRole('textbox', { name: 'Title' })) as HTMLInputElement;
    expect(title.value).toBe('Follow-up 4');
    fireEvent.click(screen.getByRole('button', { name: 'Create item' }));

    await waitFor(() =>
      expect(fileFollowUp).toHaveBeenCalledWith('org-1', 'item-519', 'fu-4', { title: 'Follow-up 4' }),
    );
  });

  it('has File on a triage follow-up only once its triage item is closed', async () => {
    const triage = (state: Item['state']) =>
      [[itemQuery('org-1', 'triage-1').queryKey, item({ id: 'triage-1', state })]] as never;

    render(item({ follow_ups: [follow[4] as FollowUp] }), triage('cancelled'));
    const closed = await screen.findByRole('region', { name: /Follow-ups/ });
    expect(await within(closed).findByRole('button', { name: 'File: Follow-up 5' })).toBeTruthy();
    cleanup();

    render(item({ follow_ups: [follow[4] as FollowUp] }), triage('queued'));
    const open = await screen.findByRole('region', { name: /Follow-ups/ });
    expect(within(open).getByText(/In the triage item/)).toBeTruthy();
    expect(within(open).queryByRole('button', { name: /^File/ })).toBeNull();
  });

  it('shows nothing for an item without follow-ups', async () => {
    render(item());
    await waitFor(() => expect(screen.queryByRole('region')).toBeNull());
  });
});

describe('NotesCard', () => {
  it('lists notes as plain text with their source and agent', async () => {
    renderFactoryPage({
      page: () => (
        <NotesCard
          notes={[
            {
              id: 1,
              text: 'Confirm the brief deletes internal/connkind.',
              from_item: 'software#517',
              agent: 'backend-engineer',
              station: 'build',
              at: '2026-11-01T10:00:00Z',
            },
            {
              id: 2,
              text: '<b>Check</b> found a stale ticket.',
              trigger: 'jira',
              agent: 'jira-checker',
              at: '2026-11-02T10:00:00Z',
            },
          ]}
        />
      ),
      path: 'items/',
      org,
      factory,
    });
    const card = await screen.findByRole('region', { name: /Notes from other items/ });

    expect(within(card).getByText('Confirm the brief deletes internal/connkind.')).toBeTruthy();
    expect(within(card).getByRole('link', { name: 'software#517' })).toBeTruthy();
    expect(within(card).getByText('<b>Check</b> found a stale ticket.')).toBeTruthy();
    expect(within(card).getByText(/From the check of jira, jira-checker/)).toBeTruthy();
  });

  it('shows Drop as busy while the request is pending and sends it once', async () => {
    vi.mocked(dropFollowUp).mockImplementation(() => new Promise(() => {}));
    render(item({ follow_ups: follow }));
    const card = await screen.findByRole('region', { name: /Follow-ups/ });

    await expectBusyOnClick(within(card).getByRole('button', { name: 'Drop: Follow-up 3' }), 'Dropping…');

    expect(dropFollowUp).toHaveBeenCalledTimes(1);
  });
});
