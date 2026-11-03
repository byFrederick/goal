// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import { deleteSecret, patchSecret, replaceSecretValue } from '../../api/secrets';
import type { AuditEntry } from '../../api/types';
import { DAY, iso, leaks, NOW, renderSecrets, secret } from './test-harness';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('../../api/secrets', () => ({
  listSecrets: vi.fn(async () => []),
  getSecret: vi.fn(),
  listSecretActivity: vi.fn(async () => ({ entries: [], next_before: null })),
  createSecret: vi.fn(),
  importSecrets: vi.fn(),
  patchSecret: vi.fn(async (_org: string, name: string) => secret({ name })),
  replaceSecretValue: vi.fn(async (_org: string, name: string) => secret({ name })),
  deleteSecret: vi.fn(async () => undefined),
}));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});

const by = { kind: 'user' as const, id: 'u', name: 'Freddy Cruz' };
const entry = (id: number, action: string, ago: number, data: Record<string, unknown> = {}): AuditEntry => ({
  id,
  at: iso(NOW - ago),
  actor: by,
  action,
  target: { kind: 'secret', id: 'id-github-token' },
  data,
});

const used = secret({
  name: 'github-token',
  description: 'A fine-grained GitHub token',
  last_used_at: iso(NOW - 120_000),
  expires_at: iso(NOW + 300 * DAY),
  remind_days: 14,
  uses: [
    { kind: 'definition', name: 'software' },
    { kind: 'definition', name: 'sre' },
    { kind: 'knowledge_source', name: 'goal-docs' },
  ],
});
const free = secret({ name: 'old-npm-token', description: 'npm publish token' });
const entries = {
  'github-token': [
    entry(3, 'secret.used', 120_000, { run: 147, role: 'frontend-engineer' }),
    entry(2, 'secret.value_replaced', 3 * DAY),
    entry(1, 'secret.created', 10 * DAY),
  ],
};

const card = (name: string) => screen.getByRole('region', { name }) as HTMLElement;

describe('a secret’s page', () => {
  it('lists where it is used, with a count, and the activity from the audit log', async () => {
    renderSecrets({ at: '/secrets/github-token', list: [used, free], entries });

    const uses = await screen.findByRole('region', { name: /Where it’s used/ });
    expect(within(uses).getByText('3 places')).toBeTruthy();
    expect(within(uses).getByText('goal-docs')).toBeTruthy();
    expect(within(uses).getByText('Knowledge source')).toBeTruthy();
    const activity = card('Activity');
    expect(within(activity).getByText('Used by run #147, frontend-engineer')).toBeTruthy();
    expect(within(activity).getByText('Replaced by Freddy Cruz')).toBeTruthy();
    expect(within(activity).getByText('Created by Freddy Cruz')).toBeTruthy();
  });

  it('shows no value, only a write-only field', async () => {
    renderSecrets({ at: '/secrets/github-token', list: [used], entries });

    const value = await screen.findByRole('region', { name: 'Value' });
    expect(within(value).getByText('Write-only')).toBeTruthy();
    expect(value.querySelector('input')).toBeNull();
  });

  it('says Not available for the activity to a role without the audit log', async () => {
    renderSecrets({ at: '/secrets/github-token', list: [used], can: ['secret:read', 'secret:manage'] });

    expect(
      await within(await screen.findByRole('region', { name: 'Activity' })).findByText(/Not available/),
    ).toBeTruthy();
  });

  it('disables Delete with its reason while the secret is in use, and sends nothing', async () => {
    renderSecrets({ at: '/secrets/github-token', list: [used], entries });

    const del = await screen.findByRole('region', { name: 'Delete' });
    const button = await within(del).findByRole('button', { name: 'Delete secret' });
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect(
      within(del).getByText('Remove it from the 3 places above first. Until then it can’t be deleted.'),
    ).toBeTruthy();
    fireEvent.click(button);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(deleteSecret).not.toHaveBeenCalled();
  });

  it('asks before it deletes a secret nothing uses, and returns to the list', async () => {
    const { router } = renderSecrets({ at: '/secrets/old-npm-token', list: [free], entries: {} });

    fireEvent.click(
      await within(await screen.findByRole('region', { name: 'Delete' })).findByRole('button', {
        name: 'Delete secret',
      }),
    );
    const dialog = await screen.findByRole('dialog', { name: /Delete old-npm-token/ });
    expect(deleteSecret).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete secret' }));

    await waitFor(() => expect(deleteSecret).toHaveBeenCalledWith('org-1', 'old-npm-token'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/secrets'));
  });

  it('keeps goald’s text in the dialog when it refuses with secret-in-use', async () => {
    vi.mocked(deleteSecret).mockRejectedValueOnce(
      new ApiError({
        title: 'Conflict',
        status: 409,
        detail: 'secret old-npm-token is in use by software',
        code: 'secret-in-use',
      }),
    );
    renderSecrets({ at: '/secrets/old-npm-token', list: [free], entries: {} });

    fireEvent.click(
      await within(await screen.findByRole('region', { name: 'Delete' })).findByRole('button', {
        name: 'Delete secret',
      }),
    );
    const dialog = await screen.findByRole('dialog', { name: /Delete old-npm-token/ });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete secret' }));

    expect(await within(dialog).findByText('secret old-npm-token is in use by software')).toBeTruthy();
  });

  it('replaces the value in a dialog, and the value reaches neither the page, the URL nor a cache', async () => {
    const view = renderSecrets({ at: '/secrets/github-token', list: [used], entries });
    const VALUE = 'ghp_brand-new-value-4321';

    fireEvent.click(
      await within(await screen.findByRole('region', { name: 'Value' })).findByRole('button', {
        name: 'Replace value',
      }),
    );
    const dialog = await screen.findByRole('dialog', { name: /Replace the value of github-token/ });
    fireEvent.change(within(dialog).getByLabelText('Value'), { target: { value: VALUE } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Replace value' }));

    await waitFor(() => expect(replaceSecretValue).toHaveBeenCalledWith('org-1', 'github-token', VALUE));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(leaks(VALUE, view)).toEqual([]);
  });

  it('saves the settings through patchSecret, and clears the expiry as an empty one', async () => {
    renderSecrets({ at: '/secrets/github-token', list: [used], entries });

    fireEvent.click(
      await within(await screen.findByRole('region', { name: 'Settings' })).findByRole('button', { name: 'Edit' }),
    );
    const settings = card('Settings');
    fireEvent.change(within(settings).getByLabelText('Description'), { target: { value: 'Shared token' } });
    fireEvent.change(within(settings).getByLabelText(/Expires/), { target: { value: '' } });
    fireEvent.click(within(settings).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(patchSecret).toHaveBeenCalledTimes(1));
    expect(patchSecret).toHaveBeenCalledWith('org-1', 'github-token', {
      description: 'Shared token',
      scope: { all_factories: true, factories: [] },
      expiry: {},
    });
  });

  it('shows none of the controls that change it to those without secret:manage', async () => {
    renderSecrets({ at: '/secrets/github-token', list: [used], entries, can: ['secret:read', 'audit:read'] });

    await screen.findByRole('region', { name: 'Value' });
    expect(screen.queryByRole('button', { name: 'Replace value' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete secret' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Delete' })).toBeNull();
  });
});
