// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { intakeQuery, orgNotificationsQuery, versionModelQuery } from '../../api/queries';
import type { Factory, Intake, IntakeTrigger, OrgSummary, VersionModel } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { WaysInCard } from './ways-in-card';

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
  slackSecret = null as string | null,
  model = published,
}: {
  triggers?: IntakeTrigger[];
  slackSecret?: string | null;
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
      [orgNotificationsQuery(org.id).queryKey, { slack_secret: slackSecret }],
    ],
  });
}

describe('WaysInCard', () => {
  it('draws no label row, even with an enabled label trigger', async () => {
    renderWays();

    await screen.findByRole('heading', { name: 'How work gets in' });
    expect(screen.queryByText('Label an issue')).toBeNull();
    expect(screen.queryByRole('button', { name: /^Copy label/ })).toBeNull();
  });

  it('copies goal run', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    renderWays();

    fireEvent.click(await screen.findByRole('button', { name: 'Copy command' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('goal run'));
    expect(screen.getByText('goal run')).toBeTruthy();
  });

  it('offers Connect for Slack while Slack notifications are not set', async () => {
    renderWays();

    expect(await screen.findByText('@software')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Connect' }).getAttribute('href')).toBe('/o/acme/secrets?new=true');
  });

  it('offers no Connect once Slack notifications are set', async () => {
    renderWays({ slackSecret: 'slack-webhook' });

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

  it('makes the New item row a phone target and hides the shortcut hint on touch', async () => {
    renderWays({ triggers: [] });

    const button = await screen.findByRole('button', { name: /New item/ });
    expect(button.className).toContain('max-md:min-h-11');
    expect(button.className).not.toContain('gap-x-2');
    expect(button.hasAttribute('aria-label')).toBe(false);
    expect(button.querySelector('span')?.className).toContain('pointer-coarse:hidden');
  });
});
