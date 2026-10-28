// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { connectionsQuery, intakeQuery, versionModelQuery } from '../../api/queries';
import type { Connection, Factory, Intake, IntakeTrigger, OrgSummary, VersionModel } from '../../api/types';
import { modelOf } from '../../shared/definition/model';
import { renderFactoryPage } from '../../test/route';
import { labelRows, WaysInCard } from './ways-in-card';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
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

const labelTrigger = { name: 'factory-label', enabled: true, on: 'github.issues.labeled' } as IntakeTrigger;

/** The published model: the GitHub label trigger matches the label "factory" (its filter in the intake). */
const published = {
  model: {
    basics: {},
    line: {
      stations: [],
      intake: [{ name: 'factory-label', on: 'github.issues.labeled', enabled: true, filter: { label: 'factory' } }],
    },
  },
  ranges: {},
} as unknown as VersionModel;

const intakeWith = (triggers: IntakeTrigger[]) =>
  ({
    items: 0,
    from: null,
    to: null,
    sources: { people: 0, cli: 0, api: 0, schedule: 0, webhook: 0, github: 0 },
    triggers,
  }) as Intake;

function renderWays({
  triggers = [labelTrigger],
  connections = [] as Connection[],
  model = published,
}: {
  triggers?: IntakeTrigger[];
  connections?: Connection[];
  model?: VersionModel;
} = {}) {
  return renderFactoryPage({
    page: () => <WaysInCard />,
    path: '/',
    org,
    factory,
    seed: [
      [intakeQuery(org.id, factory.id).queryKey, intakeWith(triggers)],
      [versionModelQuery(org.id, factory.id, 'current').queryKey, model],
      [connectionsQuery(org.id).queryKey, connections],
    ],
  });
}

describe('labelRows', () => {
  it('reads the label of each enabled GitHub label trigger from its published entry', () => {
    const rows = labelRows([labelTrigger, { ...labelTrigger, name: 'off', enabled: false }], modelOf(published.model));
    expect(rows).toEqual([{ name: 'factory-label', label: 'factory' }]);
  });
});

describe('WaysInCard', () => {
  it('shows the label row for an enabled label trigger, and Copy writes the label', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    renderWays();

    expect(await screen.findByRole('heading', { name: 'How work gets in' })).toBeTruthy();
    expect(screen.getByText('Label an issue')).toBeTruthy();
    expect(screen.getByText('factory')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Copy label factory' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('factory'));
  });

  it('has no label row when no enabled label trigger exists', async () => {
    renderWays({ triggers: [{ ...labelTrigger, enabled: false }] });

    await screen.findByRole('heading', { name: 'How work gets in' });
    expect(screen.queryByText('Label an issue')).toBeNull();
    expect(screen.queryByRole('button', { name: /^Copy label/ })).toBeNull();
  });

  it('copies goal item create', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    renderWays();

    fireEvent.click(await screen.findByRole('button', { name: 'Copy command' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('goal item create'));
    expect(screen.getByText('goal item create')).toBeTruthy();
  });

  it('offers Connect for Slack while the organization has no slack connection', async () => {
    renderWays();

    expect(await screen.findByText('@software')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Connect' }).getAttribute('href')).toBe(
      '/o/acme/connections/new?kind=slack',
    );
  });

  it('offers no Connect once Slack is connected', async () => {
    renderWays({ connections: [{ id: 'c-slack', name: 'slack', kind: 'slack' } as Connection] });

    await screen.findByText('@software');
    expect(screen.queryByRole('link', { name: 'Connect' })).toBeNull();
  });

  it('says so when the factory has no triggers, and keeps the other ways in', async () => {
    renderWays({
      triggers: [],
      model: { model: { basics: {}, line: { stations: [] } }, ranges: {} } as unknown as VersionModel,
    });

    expect(await screen.findByText(/This factory has no triggers yet/)).toBeTruthy();
    expect(screen.getByRole('button', { name: /New item, or press/ })).toBeTruthy();
  });
});
