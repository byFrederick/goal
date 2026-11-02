// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { connectionsQuery } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { connectionFixture } from '../../test/connections-route';
import { renderFactoryPage } from '../../test/route';
import { NewTriggerDialog } from './new-trigger-dialog';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'widgets',
  title: 'Widgets',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'owner',
  can: ['definition:edit'],
} as unknown as Factory;

function renderPicker(kinds: string[] = ['github', 'cloudflare']) {
  const connections = kinds.map((kind) => connectionFixture({ name: `${kind}-acme`, kind }));
  return renderFactoryPage({
    page: () => <NewTriggerDialog />,
    path: 'intake/new',
    org,
    factory,
    seed: [[connectionsQuery(org.id).queryKey, connections]],
  });
}

describe('New trigger picker', () => {
  it('lists Slack, Linear, Jira and Sentry as not available yet', async () => {
    renderPicker();
    expect(await screen.findByRole('dialog', { name: 'New trigger' })).toBeTruthy();
    expect(screen.getByText('Step 1 of 2: what starts it')).toBeTruthy();

    for (const name of ['Slack', 'Linear', 'Jira', 'Sentry']) {
      const source = screen.getByRole('button', { name });
      expect(source.getAttribute('aria-disabled')).toBe('true');
      expect(source.getAttribute('aria-pressed')).toBe('false');
    }
    fireEvent.focus(screen.getByRole('button', { name: 'Slack' }));
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it("groups the sources by the organization's own connections", async () => {
    renderPicker([]);
    await screen.findByRole('dialog', { name: 'New trigger' });

    const group = (title: string) => screen.getByRole('heading', { name: title }).parentElement as HTMLElement;
    expect(within(group('Built in')).getByRole('button', { name: 'Schedule' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Connected' })).toBeNull();
    const notConnected = group('Not connected');
    expect(within(notConnected).getByRole('button', { name: 'GitHub' })).toBeTruthy();
    expect(within(notConnected).getByRole('link', { name: 'Connect GitHub' })).toBeTruthy();
    // A source that is not built has no Connect: connecting it would do nothing.
    expect(within(notConnected).getByRole('button', { name: 'Slack' })).toBeTruthy();
    expect(within(notConnected).queryByRole('link', { name: 'Connect Slack' })).toBeNull();
  });

  it('puts a source under Connected only when its connection exists and it is built', async () => {
    renderPicker(['github', 'slack']);
    await screen.findByRole('dialog', { name: 'New trigger' });

    const connected = screen.getByRole('heading', { name: 'Connected' }).parentElement as HTMLElement;
    expect(within(connected).getByRole('button', { name: 'GitHub' })).toBeTruthy();
    expect(within(connected).queryByRole('button', { name: 'Slack' })).toBeNull();
    const notConnected = screen.getByRole('heading', { name: 'Not connected' }).parentElement as HTMLElement;
    expect(within(notConnected).getByRole('button', { name: 'Cloudflare' })).toBeTruthy();
    expect(within(notConnected).getByRole('button', { name: 'Slack' })).toBeTruthy();
  });

  it('lists the built-in and connected sources as choosable', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    for (const name of ['Schedule', 'Webhook', 'GitHub', 'Cloudflare']) {
      expect(screen.getByRole('button', { name }).getAttribute('aria-disabled')).toBeNull();
    }
  });

  it('lets only Issue labeled and Check run failed be chosen among the GitHub events', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    expect((screen.getByRole('radio', { name: 'Check run failed' }) as HTMLInputElement).checked).toBe(true);
    const opened = screen.getByRole('radio', { name: 'Issue opened' }) as HTMLInputElement;
    expect(opened.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(opened);
    expect(opened.checked).toBe(false);

    fireEvent.click(screen.getByRole('radio', { name: 'Issue labeled' }));
    expect((screen.getByRole('radio', { name: 'Issue labeled' }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole('radio', { name: 'Push to a branch' }).getAttribute('aria-disabled')).toBe('true');
  });

  it('continues to the trigger page with the chosen event in the address', async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    fireEvent.click(screen.getByRole('radio', { name: 'Issue labeled' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/widgets/intake/new/github'));
    expect(new URLSearchParams(router.state.location.search).get('on')).toBe('github.issues.labeled');
  });

  it('continues a schedule to its trigger page with the schedule event', async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    fireEvent.click(screen.getByRole('button', { name: 'Schedule' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/widgets/intake/new/schedule'));
    expect(new URLSearchParams(router.state.location.search).get('on')).toBe('schedule');
  });

  it('offers the suggested triggers under "Or start from a suggestion"', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    expect(screen.getByRole('heading', { name: 'Or start from a suggestion' })).toBeTruthy();
    const row = screen.getByText('Fix failing checks on pull requests').parentElement as HTMLElement;
    expect(within(row).getByText('Build')).toBeTruthy();
    expect(within(row).getByRole('button', { name: 'Use' }).hasAttribute('disabled')).toBe(false);
  });

  it('uses a suggestion: goes to its trigger page with its event in the address', async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    const row = screen.getByText('Weekly vulnerability sweep').parentElement as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: 'Use' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/widgets/intake/new/schedule'));
    expect(new URLSearchParams(router.state.location.search).get('on')).toBe('schedule');
  });

  it('disables Use for a suggestion whose event the schema does not accept', async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    const row = screen.getByText('Answer review comments by people').parentElement as HTMLElement;
    const use = within(row).getByRole('button', { name: 'Use' }) as HTMLButtonElement;
    expect(use.disabled).toBe(true);
    fireEvent.click(use);
    expect(router.state.location.pathname).toBe('/o/acme/f/widgets/intake/new');
  });

  it('groups the sources, links Connect for those not connected and filters them by search', async () => {
    renderPicker(['github']);
    await screen.findByRole('dialog', { name: 'New trigger' });

    for (const title of ['Built in', 'Connected', 'Not connected']) {
      expect(screen.getByRole('heading', { level: 3, name: title })).toBeTruthy();
    }
    expect(screen.getByRole('link', { name: 'Connect Cloudflare' }).getAttribute('href')).toBe('/o/acme/connections');
    expect(screen.getByRole('list', { name: 'New trigger steps' })).toBeTruthy();

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search sources' }), { target: { value: 'jir' } });
    expect(screen.getByRole('button', { name: 'Jira' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'GitHub' })).toBeNull();
    expect(screen.queryByRole('heading', { level: 3, name: 'Built in' })).toBeNull();
  });
});

describe('New trigger phone tap targets', () => {
  it('makes each source button at least 44 px below md and 40 px from md', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });
    for (const name of ['Schedule', 'Webhook', 'GitHub']) {
      const source = screen.getByRole('button', { name });
      expect(source.classList.contains('min-h-11')).toBe(true);
      expect(source.classList.contains('md:h-10')).toBe(true);
    }
  });

  it('makes each GitHub event row at least 44 px below md and 40 px from md', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });
    for (const name of ['Check run failed', 'Issue opened', 'Issue labeled']) {
      const row = screen.getByRole('radio', { name }).closest('div') as HTMLElement;
      expect(row.classList.contains('min-h-11')).toBe(true);
      expect(row.classList.contains('md:min-h-10')).toBe(true);
    }
  });
});
