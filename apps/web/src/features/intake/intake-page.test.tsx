// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NetworkError } from '../../api/client';
import { getIntake } from '../../api/intake';
import type { Factory, Intake, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { IntakePage } from './intake-page';

vi.mock('../../api/intake', () => ({ getIntake: vi.fn() }));

afterEach(() => {
  cleanup();
  vi.mocked(getIntake).mockReset();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'widgets',
  title: 'Widgets',
  work_types: [],
  stations: [],
  current: {
    id: 'version-3',
    number: 3,
    published_at: '2026-10-20T10:00:00Z',
    published_by: { kind: 'user', id: 'u', name: 'Freddy' },
  },
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'owner',
  can: ['definition:edit', 'item:operate'],
} as unknown as Factory;

const intake: Intake = {
  items: 48,
  from: '2026-10-20',
  to: '2026-10-26',
  sources: { github: 22, people: 11, cli: 0, api: 0, schedule: 7, webhook: 2 },
  window: '30d',
  origins: { person: 160, plan: 377, trigger: 4, follow_up: 31 },
  coming_up: [
    {
      id: 'item-561',
      ref: 'widgets#561',
      title: 'Add the migration that drops the connection tables',
      origin: 'follow-up',
      from_item: 'widgets#519',
      start: { at: '2026-11-19T09:00:00Z' },
      needs: [],
      created_at: '2026-11-01T09:00:00Z',
    },
    {
      id: 'item-562',
      ref: 'widgets#562',
      title: 'Remove the old connection path',
      origin: 'person',
      via: 'web',
      created_by: { kind: 'user', id: 'u1', name: 'Freddy Cruz' },
      needs: ['widgets#523'],
      created_at: '2026-11-02T09:00:00Z',
    },
  ],
  coming_up_total: 2,
  triage: {
    item: { id: 'item-570', ref: 'widgets#570', title: 'Follow-ups from #513', station: 'intake', follow_ups: 61 },
    waiting: [{ id: 'item-525', ref: 'widgets#525', title: 'Build knowledge as files', follow_ups: 9 }],
  },
  webhooks: [
    { id: 'hook-1', name: 'github-acme', sender: 'github', state: 'receiving' },
    { id: 'hook-2', name: 'deploys', sender: 'standard', state: 'no_trigger' },
  ],
  suggestions: [],
  triggers: [
    {
      name: 'flaky-test-on-main',
      enabled: true,
      source: 'github',
      on: 'github.check_run.failed',
      when: 'github.check_run.failed',
      create: { type: 'fix', station: 'Build', lane: null, expedite: true },
      days: [0, 1, 0, 0, 1, 0, 1],
      problem: null,
    },
    {
      name: 'jira-tickets',
      enabled: true,
      source: 'schedule',
      on: 'schedule',
      when: '*/15 * * * *, UTC',
      create: { type: 'feature', station: 'Spec', lane: null, expedite: false },
      check: { agent: 'jira-checker', max_usd: 1 },
      days: [0, 0, 0, 0, 0, 0, 2],
      problem: null,
    },
    {
      name: 'deploy-failed',
      enabled: false,
      source: 'webhook',
      on: 'webhook.cloudflare.deploy.failed',
      when: 'webhook.cloudflare.deploy.failed',
      create: { type: 'fix', station: 'Spec', lane: null, expedite: false },
      days: [0, 0, 0, 0, 0, 0, 0],
      problem: { kind: 'refused_deliveries', message: '2 events refused, bad signature' },
    },
  ],
};

function renderIntake() {
  return renderFactoryPage({
    page: () => <IntakePage />,
    path: 'intake/',
    org,
    factory,
  });
}

describe('IntakePage', () => {
  it('shows loading while the intake is read, with the app background and aria-busy', async () => {
    vi.mocked(getIntake).mockReturnValue(new Promise(() => {}));
    renderIntake();

    expect(await screen.findByText('Loading')).toBeTruthy();
    expect(document.querySelector('[aria-busy="true"]')).toBeTruthy();
  });

  it('shows the error with Try again when the intake cannot be read', async () => {
    vi.mocked(getIntake).mockRejectedValue(new NetworkError(new TypeError('fetch failed')));
    renderIntake();

    expect(await screen.findByText('goald is not answering')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('shows the empty state when there are no triggers', async () => {
    vi.mocked(getIntake).mockResolvedValue({ ...intake, triggers: [] });
    renderIntake();

    expect(await screen.findByRole('heading', { level: 2, name: 'No triggers yet' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'New trigger' }).getAttribute('href')).toBe('/o/acme/f/widgets/intake/new');
  });

  it('lists what is coming up with its start or its needs, and where each item came from', async () => {
    vi.mocked(getIntake).mockResolvedValue(intake);
    renderIntake();

    const coming = await screen.findByRole('region', { name: 'Coming up' });
    expect(within(coming).getByText('Add the migration that drops the connection tables')).toBeTruthy();
    expect(within(coming).getByRole('link', { name: 'widgets#519' }).getAttribute('href')).toBe(
      '/o/acme/f/widgets/items/519',
    );
    expect(within(coming).getByText('Waiting')).toBeTruthy();
    expect(within(coming).getByText('for widgets#523')).toBeTruthy();
    expect(within(coming).getByText('Freddy Cruz, New item')).toBeTruthy();
    // Only an item with a start time can be started now: one that waits for its needs has no button.
    expect(within(coming).getAllByRole('button', { name: /^Start now/ })).toHaveLength(1);
  });

  it('shows where items came from and asks the server again for another window', async () => {
    vi.mocked(getIntake).mockResolvedValue(intake);
    renderIntake();

    const origins = await screen.findByRole('region', { name: 'Where items came from' });
    expect(within(origins).getByRole('img', { name: 'People 160, Plans 377, Follow-ups 31, Triggers 4' })).toBeTruthy();
    expect((within(origins).getByRole('radio', { name: '30 days' }) as HTMLInputElement).checked).toBe(true);

    vi.mocked(getIntake).mockResolvedValue({
      ...intake,
      window: '7d',
      origins: { person: 5, plan: 0, trigger: 1, follow_up: 2 },
    });
    fireEvent.click(within(origins).getByRole('radio', { name: '7 days' }));

    await waitFor(() => expect(vi.mocked(getIntake).mock.calls.at(-1)?.[4]).toBe('7d'));
    expect(
      await within(origins).findByRole('img', { name: 'People 5, Plans 0, Follow-ups 2, Triggers 1' }),
    ).toBeTruthy();
  });

  it('shows the follow-ups to triage, or says there are none', async () => {
    vi.mocked(getIntake).mockResolvedValue(intake);
    renderIntake();

    const triage = await screen.findByRole('region', { name: 'Follow-ups to triage' });
    expect(within(triage).getByText('61')).toBeTruthy();
    expect(within(triage).getByRole('link', { name: 'Open' }).getAttribute('href')).toBe('/o/acme/f/widgets/items/570');
    expect(within(triage).getByText('Join it when widgets#525 lands')).toBeTruthy();
    cleanup();

    vi.mocked(getIntake).mockResolvedValue({ ...intake, triage: null });
    renderIntake();
    const none = await screen.findByRole('region', { name: 'Follow-ups to triage' });
    expect(within(none).getByText('No follow-ups are waiting for triage.')).toBeTruthy();
  });

  it('shows the triggers with their problems and switches, marks a check and counts the ones on', async () => {
    vi.mocked(getIntake).mockResolvedValue(intake);
    renderIntake();

    expect(await screen.findByRole('heading', { level: 1, name: 'Intake' })).toBeTruthy();
    const triggers = screen.getByRole('region', { name: 'Triggers' });
    expect(within(triggers).getByText('2 on')).toBeTruthy();
    expect(within(triggers).getByText('2 events refused, bad signature')).toBeTruthy();
    expect(within(triggers).getByText('expedite')).toBeTruthy();
    expect(within(triggers).getByText('Check')).toBeTruthy();
    expect(within(triggers).getByText('by jira-checker')).toBeTruthy();
    expect(within(triggers).getByRole('switch', { name: 'flaky-test-on-main' }).getAttribute('aria-checked')).toBe(
      'true',
    );
    expect(within(triggers).getByRole('switch', { name: 'deploy-failed' }).getAttribute('aria-checked')).toBe('false');
  });

  it('puts every explanation behind an ⓘ button and lists the webhooks open to the factory with their state', async () => {
    vi.mocked(getIntake).mockResolvedValue(intake);
    renderIntake();

    const hooks = await screen.findByRole('region', { name: 'Webhooks open to Widgets' });
    expect(within(hooks).getByText('github-acme')).toBeTruthy();
    expect(within(hooks).getByText('Receiving')).toBeTruthy();
    expect(within(hooks).getByText('No trigger yet')).toBeTruthy();
    expect(within(hooks).getByRole('link', { name: 'Manage' }).getAttribute('href')).toBe(
      '/o/acme/f/widgets/intake/webhooks',
    );
    expect(screen.getByRole('button', { name: 'About Coming up' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'About Triggers' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'About Follow-ups to triage' })).toBeTruthy();
  });

  it('says what a trigger needs, links Set up and reads Off', async () => {
    vi.mocked(getIntake).mockResolvedValue({
      ...intake,
      triggers: [
        {
          ...intake.triggers[2],
          name: 'errors-in-production',
          on: 'webhook.sentry.issue',
          problem: { kind: 'missing_connection', message: 'The factory has no webhook endpoint named sentry' },
        },
      ],
    } as Intake);
    renderIntake();

    const triggers = await screen.findByRole('region', { name: 'Triggers' });
    expect(within(triggers).getByText(/Needs a sentry webhook\./)).toBeTruthy();
    expect(within(triggers).getByRole('link', { name: 'Set up' }).getAttribute('href')).toBe(
      '/o/acme/f/widgets/intake/webhooks',
    );
    expect(within(triggers).getByText('Off')).toBeTruthy();
  });
});
