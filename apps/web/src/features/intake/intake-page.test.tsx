// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
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
  current: { id: 'version-3', number: 3, published_at: '2026-10-20T10:00:00Z', published_by: { kind: 'user', id: 'u', name: 'Freddy' } },
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
    page: () => <IntakePage group="software" onGroupChange={() => {}} />,
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
    vi.mocked(getIntake).mockRejectedValue(new Error('goald is unreachable'));
    renderIntake();

    expect(await screen.findByText('goald is not answering')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('shows the empty state when there are no items and no triggers', async () => {
    vi.mocked(getIntake).mockResolvedValue({ items: 0, from: null, to: null, sources: { github: 0, people: 0, cli: 0, api: 0, schedule: 0, webhook: 0 }, triggers: [] });
    renderIntake();

    expect(await screen.findByRole('heading', { level: 2, name: 'No triggers yet' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'New trigger' }).getAttribute('href')).toBe('/o/acme/f/widgets/intake/new');
  });

  it('shows where the items came from, the triggers with their problems and switches, and the suggestions', async () => {
    vi.mocked(getIntake).mockResolvedValue(intake);
    renderIntake();

    expect(await screen.findByRole('heading', { level: 1, name: 'Intake' })).toBeTruthy();
    expect(screen.getByRole('heading', { level: 2, name: 'Where the last 48 items came from' })).toBeTruthy();
    expect(screen.getByRole('img', { name: /GitHub 22, People, with New item 11/ })).toBeTruthy();

    const triggers = screen.getByRole('region', { name: 'Triggers' });
    expect(within(triggers).getByText('2')).toBeTruthy();
    expect(within(triggers).getByText('2 events refused, bad signature')).toBeTruthy();
    expect(within(triggers).getByText('expedite')).toBeTruthy();
    expect(within(triggers).getByRole('switch', { name: 'flaky-test-on-main' }).getAttribute('aria-checked')).toBe('true');
    expect(within(triggers).getByRole('switch', { name: 'deploy-failed' }).getAttribute('aria-checked')).toBe('false');
    expect(within(triggers).getByText('New item, the goal CLI and the API are always on')).toBeTruthy();

    expect(screen.getByRole('heading', { level: 2, name: 'Suggested triggers' })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Add' })).toHaveLength(3);
  });
});
