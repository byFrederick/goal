// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getJonahOverview, removeKeptTest } from '../../api/client';
import { keptTestsQuery, membersQuery, proposalsQuery } from '../../api/queries';
import type { Factory, KeptTest, OrgSummary } from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { groupByStation, TestsPage } from './tests-page';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() }, Toaster: () => null }));
vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  getJonahOverview: vi.fn(),
  listKeptTests: vi.fn(),
  removeKeptTest: vi.fn(),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(getJonahOverview).mockResolvedValue({} as never);
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };
const editor: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['feature'],
  stations: [
    { id: 'spec', kind: 'agent' },
    { id: 'build', kind: 'agent' },
    { id: 'review', kind: 'human' },
  ],
  current: { id: 'version-21', number: 21 } as unknown as Factory['current'],
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit', 'factory:read'],
};
const viewer: Factory = { ...editor, role: 'viewer', can: ['factory:read'] };

const kept = (id: string, number: number, title: string, station: string, by = 'user:u1'): KeptTest => ({
  id,
  item: { id: `item-${number}`, number, title },
  station,
  kept_by: by,
  kept_at: '2026-11-01T10:00:00Z',
});
const tests = [
  kept('t1', 497, 'Board column header meta', 'review'),
  kept('t2', 418, 'Needs you header in the top bar', 'build'),
  kept('t3', 451, 'A sidebar request gets the frontend lane', 'spec'),
  kept('t4', 493, 'Phone fixes on three screens', 'build', 'agent:jonah'),
];

function seeds(factory: Factory, list: KeptTest[]): Seed[] {
  return [
    [keptTestsQuery(org.id, factory.id).queryKey, { tests: list }],
    [proposalsQuery(org.id, factory.id).queryKey, { proposals: [] }],
    [
      membersQuery(org.id).queryKey,
      [{ member_id: 'm1', user: { id: 'u1', name: 'Freddy Cruz', email: 'f@example.com' } }],
    ],
  ];
}

function renderTests(factory: Factory = editor, list: KeptTest[] = tests) {
  return renderFactoryPage({
    page: () => <TestsPage />,
    path: 'jonah/tests',
    org,
    factory,
    seed: seeds(factory, list),
    topBar: true,
  });
}

describe('groupByStation', () => {
  it('orders the groups as the line does and leaves out a station with no test', () => {
    expect(
      groupByStation(tests, ['spec', 'build', 'review', 'merge']).map(([station, list]) => [station, list.length]),
    ).toEqual([
      ['spec', 1],
      ['build', 2],
      ['review', 1],
    ]);
  });
  it('puts a station the line no longer has after the others', () => {
    expect(groupByStation([kept('t9', 1, 'Old', 'retired'), ...tests], ['spec']).map(([station]) => station)).toEqual([
      'spec',
      'retired',
      'review',
      'build',
    ]);
  });
});

describe('the Tests tab', () => {
  it('lists kept tests by station with their number, title, who kept each and when', async () => {
    renderTests();

    const kept = await screen.findByRole('region', { name: 'Kept tests' });
    const groups = within(kept).getAllByRole('region');
    expect(groups.map((g) => g.getAttribute('aria-label'))).toEqual(['spec tests', 'build tests', 'review tests']);
    const build = within(groups[1] as HTMLElement);
    expect(build.getByText('#418')).toBeTruthy();
    expect(build.getByText('Needs you header in the top bar')).toBeTruthy();
    expect(build.getByText('Freddy Cruz, Nov 1')).toBeTruthy();
    expect(build.getByText('jonah, Nov 1')).toBeTruthy();
  });

  it('is empty when nothing is kept', async () => {
    renderTests(editor, []);
    expect(await screen.findByText('No kept tests yet')).toBeTruthy();
  });

  it('shows the skeleton while the list loads', async () => {
    renderFactoryPage({ page: () => <TestsPage />, path: 'jonah/tests', org, factory: editor, seed: [], topBar: true });
    expect((await screen.findAllByText('Loading'))[0]?.closest('[aria-busy]')?.getAttribute('aria-busy')).toBe('true');
  });

  it('lets a person with definition:edit remove a test from its row menu', async () => {
    vi.mocked(removeKeptTest).mockResolvedValue(undefined);
    renderTests();

    fireEvent.pointerDown(await screen.findByRole('button', { name: 'Actions for test #418' }), {
      button: 0,
      ctrlKey: false,
    });
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove' }));

    await waitFor(() => expect(removeKeptTest).toHaveBeenCalledWith('org-1', 'factory-1', 't2'));
  });

  it('offers no menu to a person without definition:edit', async () => {
    renderTests(viewer);
    await screen.findByRole('region', { name: 'Kept tests' });
    expect(screen.queryByRole('button', { name: /Actions for test/ })).toBeNull();
  });
});
