// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
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

function renderPicker() {
  return renderFactoryPage({ page: () => <NewTriggerDialog />, path: 'intake/new', org, factory });
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
});
