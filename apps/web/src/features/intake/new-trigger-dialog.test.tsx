// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';
import { connectionsQuery } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
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

function renderPicker(connections?: unknown[]) {
  return renderFactoryPage({
    page: () => <NewTriggerDialog />,
    path: 'intake/new',
    org,
    factory,
    seed: connections ? [[connectionsQuery('acme', { kind: 'github' }).queryKey, connections]] : [],
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

  it('draws plain source icons with no filled tile', async () => {
    renderPicker([{ name: 'github' }]);
    await screen.findByRole('dialog', { name: 'New trigger' });
    for (const name of ['Schedule', 'GitHub', 'Slack']) {
      const button = screen.getByRole('button', { name });
      expect(button.querySelector('[class*="bg-"]')).toBeNull();
      expect(button.querySelector('svg')).not.toBeNull();
    }
  });

  it('puts the title and the step on one row and lists suggestions one per row', async () => {
    renderPicker();
    const dialog = await screen.findByRole('dialog', { name: 'New trigger' });
    const title = within(dialog).getByText('New trigger', { selector: 'h2' });
    expect(title.parentElement?.className).toContain('flex-row');
    expect(title.parentElement?.contains(screen.getByText('Step 1 of 2: what starts it'))).toBe(true);
    const list = screen.getByText('Fix failing checks on pull requests').closest('ul') as HTMLElement;
    expect(list.className).toContain('flex-col');
    expect(list.className).not.toContain('grid-cols');
  });

  it('keeps GitHub choosable when the organization has a github connection', async () => {
    renderPicker([{ name: 'github' }]);
    await screen.findByRole('dialog', { name: 'New trigger' });
    expect(screen.getByRole('button', { name: 'GitHub' }).getAttribute('aria-disabled')).toBeNull();
    expect(screen.queryByRole('link', { name: /Connect/ })).toBeNull();
  });

  it('shows a Connect link to Connections instead of choosing GitHub when there is no connection', async () => {
    renderPicker([]);
    await screen.findByRole('dialog', { name: 'New trigger' });
    expect(screen.getByRole('button', { name: 'GitHub' }).getAttribute('aria-disabled')).toBe('true');
    expect(screen.getByRole('link', { name: 'Connect GitHub' }).getAttribute('href')).toBe('/o/acme/connections');
    expect(screen.getByRole('button', { name: 'Continue' }).hasAttribute('disabled')).toBe(true);
  });

  it('gives the unbuilt sources no Connect link', async () => {
    renderPicker([]);
    await screen.findByRole('dialog', { name: 'New trigger' });
    expect(screen.getAllByRole('link', { name: /Connect/ })).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'Slack' }).getAttribute('aria-disabled')).toBe('true');
  });

  it('does not focus the Close button on open, and Tab reaches it', async () => {
    renderPicker();
    const dialog = await screen.findByRole('dialog', { name: 'New trigger' });
    const close = screen.getByRole('button', { name: 'Close' });
    await waitFor(() => expect(document.activeElement).not.toBe(close));
    expect(dialog.contains(document.activeElement)).toBe(true);
    const user = userEvent.setup();
    for (let i = 0; i < 40 && document.activeElement !== close; i++) await user.tab();
    expect(document.activeElement).toBe(close);
  });
});
