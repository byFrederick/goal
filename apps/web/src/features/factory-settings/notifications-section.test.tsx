// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getOrgNotificationSettings } from '../../api/org-notifications';
import { defKeys, orgQuery } from '../../api/queries';
import type { Factory, Org, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { FactorySettingsPage } from './settings-page';

vi.mock(import('../../api/org-notifications'), async (original) => ({
  ...(await original()),
  getOrgNotificationSettings: vi.fn(),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  description: 'Triage.',
  work_types: ['incident'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: [],
};

async function renderSettings(can: string[] = []) {
  const orgWith: Org = { ...org, created_at: '2026-10-01T00:00:00Z', can };
  renderFactoryPage({
    page: () => <FactorySettingsPage />,
    path: 'settings',
    org,
    factory,
    seed: [
      [orgQuery(org.id).queryKey, orgWith],
      [defKeys.factory(org.id, factory.id), factory],
    ],
    topBar: true,
  });
  await screen.findByRole('heading', { name: 'Settings' });
  return screen.getByRole('region', { name: 'Notifications' });
}

describe('Settings › Notifications', () => {
  it('names the secret Slack is sent with', async () => {
    vi.mocked(getOrgNotificationSettings).mockResolvedValue({ slack_secret: 'slack-bot' });
    const group = await renderSettings();

    expect(await within(group).findByText('slack-bot')).toBeTruthy();
    expect(group.textContent).toContain('Sent with the secret slack-bot.');
  });

  it('says Slack is off when no secret is set', async () => {
    vi.mocked(getOrgNotificationSettings).mockResolvedValue({ slack_secret: null });
    const group = await renderSettings();

    expect(await within(group).findByText('Slack is off')).toBeTruthy();
  });

  it('links to the organization settings for a person who holds org:manage', async () => {
    vi.mocked(getOrgNotificationSettings).mockResolvedValue({ slack_secret: 'slack-bot' });
    const group = await renderSettings(['org:manage']);

    const link = await within(group).findByRole('link', { name: 'Change in organization settings' });
    expect(link.getAttribute('href')).toBe('/o/acme/settings');
  });

  it('shows no link without org:manage', async () => {
    vi.mocked(getOrgNotificationSettings).mockResolvedValue({ slack_secret: 'slack-bot' });
    const group = await renderSettings();

    await within(group).findByText('slack-bot');
    expect(within(group).queryByRole('link')).toBeNull();
  });

  it('shows a skeleton row while the setting loads', async () => {
    vi.mocked(getOrgNotificationSettings).mockReturnValue(new Promise(() => {}));
    const group = await renderSettings();

    expect(within(group).getByText('Loading')).toBeTruthy();
    expect(group.querySelector('[aria-busy="true"]')).toBeTruthy();
  });

  it('shows one line when the setting fails to load', async () => {
    vi.mocked(getOrgNotificationSettings).mockRejectedValue(new Error('network down'));
    const group = await renderSettings();

    expect(await within(group).findByText('Couldn’t load the Slack setting.')).toBeTruthy();
    expect(within(group).queryByText('Slack is off')).toBeNull();
  });
});
