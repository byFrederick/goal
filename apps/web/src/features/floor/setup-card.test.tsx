// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listMembers } from '../../api/client';
import {
  connectionsQuery,
  intakeQuery,
  invitationsQuery,
  itemsQuery,
  knowledgeSourcesQuery,
  membersQuery,
  versionModelQuery,
} from '../../api/queries';
import type {
  Connection,
  Factory,
  Intake,
  IntakeTrigger,
  ItemCard,
  Member,
  OrgSummary,
  VersionModel,
} from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { SetupCard, setupHiddenKey } from './setup-card';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  listMembers: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  window.localStorage.clear();
});

beforeEach(() => {
  window.localStorage.clear();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
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

const member = { member_id: 'm-1' } as Member;
const github = { id: 'c-gh', name: 'github', kind: 'github' } as Connection;
const trigger = (name: string, on: string, enabled = true) => ({ name, on, enabled }) as IntakeTrigger;

/** The published model at the picture's state: the repository, the line and a budget are set. */
const published = {
  model: {
    basics: { repository: { url: 'https://github.com/acme/app', connection: 'github' } },
    line: {
      stations: [
        { id: 'intake', kind: 'queue' },
        { id: 'spec', kind: 'agent' },
        { id: 'build', kind: 'agent' },
        { id: 'review', kind: 'agent' },
        { id: 'done', kind: 'done' },
      ],
    },
    review: { budget: { weekly_usd: 800 } },
  },
  ranges: {},
} as unknown as VersionModel;

const intake = {
  items: 0,
  from: null,
  to: null,
  sources: { people: 0, cli: 0, api: 0, schedule: 0, webhook: 0, github: 0 },
  triggers: [trigger('issues', 'github.issues.labeled'), trigger('nightly', 'schedule'), trigger('audit', 'schedule')],
} as Intake;

const doneItem = { id: 'item-1', ref: 'software#1', number: 1, state: 'done' } as ItemCard;

/** The seeds every card reads, at the picture's state: 6 of 9 done (no Slack, no knowledge, no invitations). */
function seeds({
  members = [member],
  invitations = [],
  connections = [github],
  knowledge = [],
  model = published,
  triggers = intake,
  finished = [doneItem],
}: {
  members?: Member[];
  invitations?: unknown[];
  connections?: Connection[];
  knowledge?: unknown[];
  model?: VersionModel;
  triggers?: Intake;
  finished?: ItemCard[];
} = {}): Seed[] {
  return [
    [membersQuery(org.id).queryKey, members],
    [invitationsQuery(org.id).queryKey, invitations],
    [connectionsQuery(org.id).queryKey, connections],
    [knowledgeSourcesQuery(org.id).queryKey, knowledge],
    [versionModelQuery(org.id, factory.id, 'current').queryKey, model],
    [intakeQuery(org.id, factory.id).queryKey, triggers],
    [itemsQuery(org.id, factory.id, { state: 'done', limit: 1 }).queryKey, { items: finished, next_after: null }],
  ];
}

/** Every seed but the members: the one query a test leaves to answer for itself. */
const withoutMembers = () => seeds().slice(1);

function renderCard(seed = seeds()) {
  return renderFactoryPage({
    page: () => <SetupCard />,
    path: '/',
    org,
    factory,
    seed,
  });
}

describe('SetupCard', () => {
  it('shows the count, the ring, and each to-do row with its action link', async () => {
    renderCard();

    expect(await screen.findByRole('heading', { name: 'Finish setting up' })).toBeTruthy();
    expect(screen.getByText('6 of 9 done')).toBeTruthy();
    expect(screen.getByRole('img', { name: '6 of 9 done' })).toBeTruthy();

    expect(screen.getByRole('link', { name: 'Invite' }).getAttribute('href')).toBe('/o/acme/members');
    expect(screen.getByRole('link', { name: 'Connect' }).getAttribute('href')).toBe(
      '/o/acme/connections/new?kind=slack',
    );
    expect(screen.getByRole('link', { name: 'Add' }).getAttribute('href')).toBe('/o/acme/knowledge/new');
  });

  it('marks the finished rows as done and offers no action for them', async () => {
    renderCard();

    await screen.findByRole('heading', { name: 'Finish setting up' });
    expect(screen.getAllByText('Done')).toHaveLength(6);
    expect(screen.getAllByText('To do')).toHaveLength(3);
    expect(screen.getAllByRole('link')).toHaveLength(3);
    expect(screen.getByText('GitHub, 1 repository')).toBeTruthy();
    expect(screen.getByText('The line, 4 stations')).toBeTruthy();
    expect(screen.getByText('3 triggers')).toBeTruthy();
  });

  it('hides itself when all nine rows are done', async () => {
    renderCard(
      seeds({
        invitations: [{ id: 'i-1' }],
        connections: [github, { id: 'c-slack', name: 'slack', kind: 'slack' } as Connection],
        knowledge: [{ id: 'k-1', name: 'handbook' }],
      }),
    );

    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Finish setting up' })).toBeNull());
  });

  it('closing the card remembers it: it stays hidden after a reload', async () => {
    const first = renderCard();
    fireEvent.click(await screen.findByRole('button', { name: 'Hide setup' }));

    expect(screen.queryByRole('heading', { name: 'Finish setting up' })).toBeNull();
    expect(window.localStorage.getItem(setupHiddenKey(factory.id))).toBe('1');
    first.unmount();

    renderCard();
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Finish setting up' })).toBeNull());
  });

  it('shows loading with an sr-only label and aria-busy while the rows are read', async () => {
    vi.mocked(listMembers).mockReturnValue(new Promise(() => {}));
    renderCard(withoutMembers());

    const loading = await screen.findByText('Loading');
    expect(loading.classList.contains('sr-only')).toBe(true);
    expect(loading.parentElement?.getAttribute('aria-busy')).toBe('true');
  });

  it('shows the error with Retry, and Retry reads the rows again', async () => {
    vi.mocked(listMembers).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce([member]);
    renderCard(withoutMembers());

    expect(await screen.findByText('Could not load the setup checklist.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByText('6 of 9 done')).toBeTruthy();
    expect(listMembers).toHaveBeenCalledTimes(2);
  });
});
